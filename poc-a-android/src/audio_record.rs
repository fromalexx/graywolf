//! Audio capture via Java's `android.media.AudioRecord` invoked through
//! JNI. Replaces the AAudio (ndk::audio) path because AAudio's HAL
//! routing on this tablet rail-pins the USB-Audio class input at full
//! scale regardless of preset, and FU_VOLUME control transfers are
//! refused once the audio HAL has the device.
//!
//! AudioRecord goes through a different HAL code path that aprsdroid
//! has been running on identical Baofeng + CMedia hardware for years
//! without the saturation problem. Same Rust DSP pipeline downstream.

use std::sync::atomic::{AtomicBool, Ordering};
use std::sync::mpsc::SyncSender;
use std::sync::Arc;
use std::time::Duration;

use jni::objects::{JObject, JShortArray, JValue};
use jni::{AttachGuard, JavaVM};
use log::{error, info, warn};

const AUDIO_SOURCE_MIC: i32 = 1;
const CHANNEL_IN_MONO: i32 = 16;
const ENCODING_PCM_16BIT: i32 = 2;

/// Open an AudioRecord, start it, loop reading short samples and
/// forwarding to the demod's mpsc queue. Runs on its own thread so
/// android_main keeps owning the activity event loop.
///
/// Returns the negotiated sample rate the demod should be initialized
/// against, or an error if opening the recorder fails.
pub fn spawn(
    vm: Arc<JavaVM>,
    sample_rate: u32,
    stop: Arc<AtomicBool>,
    tx: SyncSender<Vec<i16>>,
) -> Result<u32, String> {
    let mut env = vm
        .attach_current_thread()
        .map_err(|e| format!("attach for AudioRecord open: {}", e))?;

    let buf_samples_min = call_min_buffer_size(&mut env, sample_rate as i32)?;
    if buf_samples_min <= 0 {
        return Err(format!(
            "AudioRecord.getMinBufferSize returned {}",
            buf_samples_min
        ));
    }
    // 4x the minimum buffer like aprsdroid; getMinBufferSize is in bytes.
    let buf_size_bytes = buf_samples_min * 4;
    info!(
        "AudioRecord: source=MIC rate={} mono PCM16 bufBytes={} (min={})",
        sample_rate, buf_size_bytes, buf_samples_min
    );

    let cls = env
        .find_class("android/media/AudioRecord")
        .map_err(|e| format!("find AudioRecord: {}", e))?;
    let recorder = env
        .new_object(
            cls,
            "(IIIII)V",
            &[
                JValue::Int(AUDIO_SOURCE_MIC),
                JValue::Int(sample_rate as i32),
                JValue::Int(CHANNEL_IN_MONO),
                JValue::Int(ENCODING_PCM_16BIT),
                JValue::Int(buf_size_bytes),
            ],
        )
        .map_err(|e| format!("new AudioRecord: {}", e))?;

    // Confirm the recorder initialized; getState() should return STATE_INITIALIZED (1).
    let state = env
        .call_method(&recorder, "getState", "()I", &[])
        .and_then(|v| v.i())
        .map_err(|e| format!("AudioRecord.getState: {}", e))?;
    if state != 1 {
        return Err(format!("AudioRecord state={} (want 1=INITIALIZED)", state));
    }

    env.call_method(&recorder, "startRecording", "()V", &[])
        .map_err(|e| format!("startRecording: {}", e))?;
    info!("AudioRecord started");

    // Need a process-global recorder ref so the dedicated read thread can
    // touch it via its own JNIEnv attach.
    let global_recorder = env
        .new_global_ref(&recorder)
        .map_err(|e| format!("new_global_ref: {}", e))?;
    drop(env);

    std::thread::Builder::new()
        .name("audio-record-pump".into())
        .spawn(move || {
            let mut env = match vm.attach_current_thread() {
                Ok(g) => g,
                Err(e) => {
                    error!("attach pump thread: {}", e);
                    return;
                }
            };
            let buf_len = (buf_size_bytes / 2) as usize; // bytes -> shorts
            let buf = match env.new_short_array(buf_len as i32) {
                Ok(a) => a,
                Err(e) => {
                    error!("new_short_array: {}", e);
                    return;
                }
            };
            let mut scratch = vec![0i16; buf_len];
            while !stop.load(Ordering::Relaxed) {
                let n = match read_into(&mut env, global_recorder.as_obj(), &buf, buf_len as i32) {
                    Ok(n) => n,
                    Err(e) => {
                        error!("AudioRecord.read: {}", e);
                        break;
                    }
                };
                if n <= 0 {
                    if n < 0 {
                        warn!("AudioRecord.read returned {}", n);
                    }
                    std::thread::sleep(Duration::from_millis(5));
                    continue;
                }
                let take = (n as usize).min(scratch.len());
                if let Err(e) = env.get_short_array_region(&buf, 0, &mut scratch[..take]) {
                    error!("get_short_array_region: {}", e);
                    break;
                }
                let chunk: Vec<i16> = scratch[..take].to_vec();
                if tx.try_send(chunk).is_err() {
                    // demod queue full; drop. Better than blocking the
                    // capture thread and stalling AudioRecord internals.
                }
            }
            // Stop + release the recorder gracefully.
            let _ = env.call_method(global_recorder.as_obj(), "stop", "()V", &[]);
            let _ = env.call_method(global_recorder.as_obj(), "release", "()V", &[]);
            info!("AudioRecord pump exited");
        })
        .map_err(|e| format!("spawn pump thread: {}", e))?;

    Ok(sample_rate)
}

fn call_min_buffer_size(env: &mut AttachGuard<'_>, sample_rate: i32) -> Result<i32, String> {
    let cls = env
        .find_class("android/media/AudioRecord")
        .map_err(|e| format!("find AudioRecord: {}", e))?;
    env.call_static_method(
        cls,
        "getMinBufferSize",
        "(III)I",
        &[
            JValue::Int(sample_rate),
            JValue::Int(CHANNEL_IN_MONO),
            JValue::Int(ENCODING_PCM_16BIT),
        ],
    )
    .and_then(|v| v.i())
    .map_err(|e| format!("AudioRecord.getMinBufferSize: {}", e))
}

fn read_into(
    env: &mut AttachGuard<'_>,
    recorder: &JObject,
    buf: &JShortArray<'_>,
    size: i32,
) -> Result<i32, String> {
    env.call_method(
        recorder,
        "read",
        "([SII)I",
        &[(buf).into(), JValue::Int(0), JValue::Int(size)],
    )
    .and_then(|v| v.i())
    .map_err(|e| format!("read: {}", e))
}
