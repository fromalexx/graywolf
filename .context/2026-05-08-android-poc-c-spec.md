# Android POC-C — TX audio path proof

**Status:** spec, ready for `/superpowers:writing-plans`
**Date:** 2026-05-08
**Repo:** all work lands in `~/dev/graywolf` on a feature branch off
`main`. No new repo, no fork, no submodule. All paths in this spec are
relative to the graywolf repo root.
**Scope:** prove the Android TX audio path. Build one canned APRS
position frame in Rust using the existing graywolf TX DSP, hand the
PCM16 buffer to Kotlin via JNI, write it through `AudioTrack` to the
phone's default audio output, decode the resulting audio on a
reference receiver (laptop running desktop graywolf-modem or
direwolf), confirm the decoded payload byte-for-byte matches the
source. **No PTT. No txgovernor. No streaming. No flow control.
One frame, one buffer, one playback, done.**

POC-A proved RX. POC-B proved end-to-end production topology. POC-C
proves the last unknown audio path — TX out via `AudioTrack` —
before phase 5 commits to a TX schedule. Design `§5.2` step 7 marked
the TX path TBD; this POC closes that gap.

If `AudioTrack` doesn't behave symmetric to `AudioRecord` (HAL gain
shaping on output, default-routing differences, latency/clip
behavior, sample-rate forcing), discover it in a 2-day POC, not in
the middle of phase 5.

---

## 1. Success criteria

POC-C is **green** when all four hold on the reference tablet:

1. **Frame builds in Rust.** The cdylib's existing TX DSP synthesizes
   a valid AX.25 UI frame for `NW5W-8>APZGRY:!4715.20N/12203.50W<
   POC-C TX test - NW5W bench` into a 22 050 Hz mono PCM16 buffer
   (or whatever sample rate matches POC-B's RX path; see §4.2.1
   note). Buffer length matches expected on-air duration for a 1200-
   baud AFSK frame plus the standard txdelay/txtail padding.
2. **Kotlin plays it through `AudioTrack`.** Service exposes a debug
   "TX test" trigger (button in the WebView, or a tap-to-fire button
   on `MainActivity`'s screen — implementer's call). Trigger calls a
   new JNI `modemBuildTestFrame` returning the PCM bytes, then
   writes them to `AudioTrack(STREAM_VOICE_CALL or USAGE_MEDIA,
   PCM16, 22050, MONO, blocking-write mode)`. Playback completes
   without underrun, no clipping warnings.
3. **Reference receiver decodes it.** Laptop running
   `graywolf-modem` (desktop) or `direwolf` against the audio chain
   (see §3) decodes the frame. The decoded text matches the
   source string exactly:
   ```
   NW5W-8>APZGRY:!4715.20N/12203.50W< POC-C TX test - NW5W bench
   ```
   Source callsign, destination, info field, symbol — all bit-
   identical.
4. **Test repeats cleanly.** Five consecutive triggers each decode
   green on the reference. No drift, no buffer-reuse bug, no
   `AudioTrack` resource leak (verifiable via `adb shell dumpsys
   audio | grep -A2 'AudioFlinger'`).

POC-C is **red** if any of:

- `AudioTrack` won't open at the requested format (HAL forces a
  different sample rate or channel count beyond what cpal-style
  resampling can compensate for).
- Reference receiver hears audio but can't decode (HAL gain shaping
  or anti-DC filtering destroys the AFSK tones in a way no software
  level adjustment recovers).
- Decode succeeds but on < 3 of 5 trials. Indicates a timing /
  underrun / pump-bug that would not survive operational TX.
- Symbol comes through but it's wrong (e.g., `/[` instead of `/<`),
  pointing at a frame-construction bug worth fixing before phase 5
  inherits it.

POC-C is **yellow** (proceed with documented caveats) if:

- Decode works only with non-default `AudioTrack` parameters that
  POC-B's `AudioRecord` didn't need. Lock the parameters in the run
  report; phase 5 inherits the lock.
- Decode rate is 4/5 with a single random fail. One borderline trial
  is acceptable; document and move on.
- Audio level on reference is well off the desktop-graywolf default,
  forcing a software TX-side gain knob analogous to N9's RX gain.
  Acceptable; record what worked and add an `N10` invariant draft to
  the run report (locked in by phase 5 if it sticks).

---

## 2. Out of scope (do not pad the POC)

- **PTT.** No USB-serial RTS, no CM108 HID, no BT SPP. Reference
  receiver listens to a permanently-cabled audio path; nothing keys
  a radio. The TX-test trigger does not assert PTT on any line.
- **txgovernor.** No queueing, no admission control, no rate limit.
  The trigger fires the buffer to `AudioTrack` and that's the whole
  TX side.
- **`modembridge` integration.** The Go child does not participate.
  POC-C's trigger goes Kotlin → JNI → Rust directly.
- **Streaming TX.** No "drain samples on demand" callback pattern.
  Rust returns one whole PCM buffer; Kotlin plays it in one
  blocking write. Streaming arrives in phase 5 if needed.
- **RX-pause-during-TX.** AudioRecord may capture the modem's own
  output as bleed-back during this test. That's fine — POC-C
  doesn't care if RX corrupts during the trial. Phase 5 handles
  N6's pump-pause logic.
- **Multiple frames or different payloads.** One canned frame.
- **Symbol picker, callsign editor, SSID dropdown.** Hardcoded
  `NW5W-8` + motorcycle (`/<`) + the placeholder coords above.
  Operator-facing TX UI is phase-5+ work.
- **Real radio in the loop.** Reference receiver decodes from a
  wired audio path, not over RF. (Optional Stage 2 in §5 adds the
  radio for level-realism, but Stage 1 is enough for the verdict.)
- **TX timing measurements.** No ms-budget assertions for keying-
  to-audio latency. Phase 5 measures that with a real PTT.

If the executor finds themselves wiring PTT, building txgovernor
hooks, or adding a streaming JNI sample drain, they have left
POC-C scope.

---

## 3. Reference hardware

Same kit as POC-A and POC-B for the tablet side. Audio chain to the
reference receiver runs in two stages — Stage 1 is mandatory for
the verdict; Stage 2 is optional but worth doing in the same
session.

### Stage 1 — direct audio cable (mandatory)

Bypasses the radio entirely so any failures are unambiguously
about the Android audio path, not RF anomalies.

| Role | Choice |
|---|---|
| Tablet | Topicon T865 (arm64-v8a, Android 14) |
| Tablet audio out | USB-C → USB-A OTG → Digirig (same as POC-A/B) |
| Audio cable | 3.5 mm TRS, Digirig "audio out to radio mic" port → laptop line-in or USB-audio dongle on the laptop |
| Reference decoder | desktop graywolf-modem or `direwolf -c direwolf.conf` reading the laptop audio device |
| ADB transport | adb-over-Wi-Fi (USB-C port goes to the Digirig) |

The Digirig becomes a USB DAC for the tablet's audio output. The
"radio mic" cable that normally goes into the radio's mic jack
goes into the laptop instead. No PTT, no RF.

### Stage 2 — full audio chain through the radio (optional, same session)

Same setup, but the radio is in the loop. Operator manually keys
the radio's PTT button (no software PTT) right before the trigger,
holds for the ~2-3 s frame, then unkeys. The transmitted RF is
received by a second receiver (handheld or SDR) cabled to the
laptop. Verifies that the level shaping the radio applies on the
mic input doesn't destroy the AFSK envelope.

If Stage 1 is green and Stage 2 fails, the issue is radio-side gain
calibration, not Android-side — informative but not POC-C-blocking.

---

## 4. Build setup

### 4.1 Toolchain

Same as POC-B. No new tools. cargo-ndk, NDK r27c, JDK 17, Gradle,
SDK platform 34, Go 1.23+ for the host-side reference receiver if
not already running.

### 4.2 New / changed code

#### 4.2.1 Rust (`graywolf-modem/src/android/`)

Extend the existing JNI module from POC-B. No new top-level
modules.

- New JNI entry point
  `Java_com_nw5w_graywolf_jni_ModemBridge_modemBuildTestFrame(env)
  -> jbyteArray` (or `Vec<u8>` wrapped through whatever idiom the
  POC-B JNI module already uses for byte returns). Returns the PCM16
  buffer for the canned frame.
- Implementation calls the existing graywolf-modem AX.25 frame
  builder + AFSK modulator + soundcard-frame-construction pipeline.
  **The plan must locate and reuse the existing TX DSP path** —
  desktop graywolf-modem already builds and modulates frames; do
  not write a new modulator. If the existing path is too tangled
  to call from a single library function, surface this in the plan
  and refactor the minimum amount needed (analogous to POC-A's
  RX-loop lift into `lib.rs`).
- Sample-rate decision: match POC-B's RX rate (22 050 Hz mono
  PCM16) unless the existing TX DSP only emits at a different
  rate, in which case use that rate and document. `AudioTrack`
  accepts the same rate set `AudioRecord` does on this hardware.

The canned frame as bytes (the part Rust will modulate):

- Source: `NW5W-8`
- Destination: `APZGRY` (graywolf experimental APZ range; if the
  existing graywolf TX path uses a different default destination,
  prefer that for consistency. Plan-writer decides after grep.)
- Digipeater path: empty (no `WIDE1-1`, no `WIDE2-2`). Direct,
  point-to-point. The reference receiver is the only intended hop.
- AX.25 control: UI frame (`0x03`), PID `0xF0` (no L3 protocol).
- Info field: `!4715.20N/12203.50W< POC-C TX test - NW5W bench`
  - `!` — APRS position, no timestamp, no msg capability.
  - lat: `4715.20N`. **Operator should overwrite with bench coords
    before flight if running near a real APRS network**, but the
    placeholder above is fine for a closed-loop wired test where
    nothing transmits over RF.
  - sym table: `/` (primary).
  - lon: `12203.50W`.
  - sym code: `<` (motorcycle).
  - comment: ` POC-C TX test - NW5W bench` — leading space
    intentional; standard APRS position-comment formatting. The
    "POC-C TX test" prefix tells anyone who decodes a stray frame
    that this is not real operational traffic.

#### 4.2.2 Kotlin (`android/app/src/main/kotlin/com/nw5w/graywolf/`)

- New `audio/AudioTxTest.kt`: opens an `AudioTrack` at the same
  format as `AudioPump`'s RX side (PCM16, mono, 22 050 Hz),
  `AudioTrack.MODE_STATIC` (preferred — entire buffer written
  before play) or `MODE_STREAM` if `MODE_STATIC` doesn't work for
  buffers this size.
- `ModemBridge.kt`: declare the new `external fun
  modemBuildTestFrame(): ByteArray`.
- WebView trigger: simplest path is a new `<button>` on the
  POC-B WebView page POSTing to a new Go stub endpoint
  `/api/_internal/tx-test`, which calls back into Kotlin via the
  existing `WebAppInterface` bridge to fire `AudioTxTest.play()`.
  Alternative: surface the trigger on `MainActivity` directly as a
  Kotlin button, bypassing the Go round-trip — simpler and POC-C
  doesn't need the production REST shape for this. Implementer's
  call; document the choice.

#### 4.2.3 Go stub (`cmd/graywolf-pocb/main.go`)

Either:
- Add the `/api/_internal/tx-test` endpoint + bridge call, OR
- Skip the Go stub entirely (Kotlin button on Activity directly).

If the bridge round-trip lands in the stub, **note it's still
throwaway** — the real TX path in phase 5 goes through txgovernor
+ modembridge, not through a debug REST endpoint. The Go stub is
retired in phase 3.

#### 4.2.4 WebView page (if used)

Add one `<button>TX test frame</button>` plus a `<script>` that
POSTs `/api/_internal/tx-test` with the bearer token. No status
read-back required — the verdict comes from the reference
receiver's logs, not the WebView.

### 4.3 Build invocations

Same as POC-B. `cargo ndk -t arm64-v8a build --lib --release`,
`go build` for the stub if it changed, `./gradlew assembleDebug`,
`adb install -r`.

---

## 5. Run procedure

### Stage 1 — wired audio loop

1. Start desktop reference decoder on the laptop. Pick one:
   - `direwolf -c direwolf.conf` with `ADEVICE` pointing at the
     laptop audio input the Digirig cable feeds.
   - Desktop `graywolf-modem` configured for the same audio device.
   Verify it logs heartbeat / audio-level lines while idle.
2. Cable the Digirig's "to radio mic" output into the laptop
   audio input. Confirm signal makes it through (e.g., tap the
   tablet, watch the reference decoder's level meter twitch).
3. Connect tablet to laptop via Wi-Fi adb (`adb connect <ip>:5555`).
4. `adb install -r` the new POC-C build of POC-B's APK.
5. Tap icon. Foreground service notification appears (POC-B
   plumbing).
6. Trigger TX-test five times, ~10 seconds apart.
7. Capture reference decoder log → `scratch/poc-c/reference.log`.
8. Stop app from the foreground notification. Close reference
   decoder.

### Stage 2 — through the radio (optional)

9. Cable the Digirig output into the radio's mic jack instead of
   the laptop.
10. Plug a second receiver (HT or SDR) cabled to the laptop audio
    input.
11. Manually key the transmitting radio's PTT button. Hold for
    ~3 s.
12. Trigger TX-test once during the keyed window.
13. Unkey.
14. Repeat steps 11-13 four more times, spaced.
15. Capture reference decoder log → `scratch/poc-c/over-the-air.log`.

---

## 6. Deliverables

| Artifact | Location |
|---|---|
| Rust JNI test-frame builder | `graywolf-modem/src/android/mod.rs` (extension) |
| Kotlin AudioTrack TX test | `android/app/src/main/kotlin/com/nw5w/graywolf/audio/AudioTxTest.kt` |
| WebView button + JS (if that path chosen) | `cmd/graywolf-pocb/pocb_index.html` |
| Optional Go endpoint | `cmd/graywolf-pocb/main.go` |
| Reference-receiver capture | `scratch/poc-c/reference.log`, optional `scratch/poc-c/over-the-air.log` |
| Run report | `.context/2026-05-XX-android-poc-c-results.md` (filled in after the run, force-added per repo convention) |

The run report captures:

- Toolchain versions.
- Whether each of the four success criteria was met.
- Trial results table (5 trials × Stage 1 + 5 trials × Stage 2).
- The actual `AudioTrack` parameters that worked (sample rate,
  channel mask, encoding, buffer-size mode).
- TX-side software gain or attenuation if any was needed (precursor
  to a possible N10 invariant).
- Any HAL or routing surprises captured from logcat.
- Verdict: green / yellow / red.
- If yellow or red: specific blockers, proposed phase-5 design
  adjustments, whether to revise design `§5.2` before phase 5
  starts.

---

## 7. Plan structure hint for `/superpowers:writing-plans`

Smaller than POC-B. Roughly five tasks:

1. **Locate and lift the existing TX DSP.** Grep for the desktop
   graywolf-modem TX entry point (frame builder + AFSK modulator
   + soundcard frame writer). If callable as a library function
   already, fine. If not, refactor the minimum needed to expose
   `pub fn build_test_frame_pcm() -> Vec<u8>` from the same crate
   the cdylib already lives in.
2. **JNI test-frame builder.** Add `modemBuildTestFrame` to the
   POC-B JNI surface. Verify desktop tests still pass and the
   cdylib still cross-compiles for arm64.
3. **Kotlin AudioTxTest.** Open `AudioTrack` matching
   `AudioRecord`'s format. Single blocking write. Trigger wired
   to a button (Activity or WebView; pick one).
4. **Stage 1 run.** Five trials wired loop. Verdict on success
   criteria 1-4.
5. **Stage 2 run + report.** Optional five trials over-the-air,
   then write the run report regardless of Stage 2 outcome.

Tasks 1 and 2 are sequential. Task 3 is independent of 1-2 (the
Kotlin code can be wired against a stub `modemBuildTestFrame`
returning a sine wave for development). Tasks 4 and 5 are
sequential after 3.

Traps the planner should call out:

- **`AudioTrack` write modes.** `MODE_STATIC` requires the buffer
  fit in one allocation; for a 2-3 second 22 050 Hz mono PCM16
  frame that's ~130 KB, well within `AudioTrack`'s static
  capacity. If `MODE_STATIC` rejects the size, fall back to
  `MODE_STREAM` with blocking writes.
- **`AudioTrack` stream type / usage.** Default `STREAM_MUSIC` /
  `USAGE_MEDIA` may be ducked by the OS when other media plays.
  `STREAM_VOICE_CALL` is more aggressive about exclusive output
  routing but requires `MODIFY_AUDIO_SETTINGS`. Start with
  `USAGE_MEDIA`; escalate if the HAL won't route to the Digirig.
- **Sample-rate parity with RX.** Mismatched RX/TX rates work in
  POC-C (it's one-shot) but bite in phase 5 if the modem's TX DSP
  emits at a different rate than the demod consumes. If a mismatch
  is unavoidable here, log it loudly in the run report.
- **Default audio output routing.** Plugging the Digirig may not
  route output to it on every tablet OS version. Verify via
  `adb shell dumpsys audio | grep -i routing` before the first
  trigger.
- **AX.25 destination address.** `APZGRY` is a placeholder. If
  desktop graywolf TX uses a different default (e.g., `APZ001`,
  `APRS`, etc.), use that instead so phase 5 doesn't inherit a
  rename.
- **Latency-prebuffer interaction with `AudioTrack.write`.**
  `MODE_STREAM` blocking writes return only after data is
  consumed; with a large prebuffer the write may complete well
  before the audio actually plays. Either use `MODE_STATIC` (write
  → `play()` → `setNotificationMarkerPosition` callback) or sleep
  until the playback head reaches the end before logging "done."
  Skipping this risks claiming success while audio is still in
  the HAL pipeline.

---

## 8. Stop conditions for the executor

Surface to the user, do not press through, if any of:

- The graywolf TX DSP is not callable as a library function and
  the refactor needed to expose it is non-trivial (more than a
  few hours' work). This becomes a phase-5 prerequisite, not a
  POC-C task.
- `AudioTrack` won't open at any sane format on the reference
  tablet. Indicates HAL incompatibility that POC-A's
  `AudioRecord`-via-Java-API trick may need to repeat for the
  output side (e.g., go through `MediaPlayer` instead, or use
  AAudio output despite AAudio being unviable for input).
- Reference receiver hears audio but never decodes, regardless
  of cabling and gain. Indicates the HAL's output conditioning is
  destroying the AFSK envelope. Design `§5.2` needs rework before
  phase 5.
- Five trials produce wildly different decoded bytes (one
  consistent symbol-table or comment glitch is fine; randomness is
  not). Indicates a memory / threading / buffer-reuse bug in the
  POC-C code that should be fixed before phase 5 inherits the
  pattern.

These are the cases where pressing on iterates against
fundamentally bad assumptions; better to stop, surface, decide.

---

## 9. Why a separate POC and not a phase-5 sub-task

Phase 5 plans on PTT, txgovernor, modembridge wiring, hardware
matrix coverage, and TX timing measurement — all worth a week. If
the underlying `AudioTrack` path does not work the way symmetry
with `AudioRecord` suggests, that week becomes 2-3 and every other
sub-task waits. A 2-day POC-C derisks the audio path in isolation;
phase 5 then plans against a known-good foundation, the same way
POC-A's known-good RX foundation made POC-B planable.

POC-A: RX audio path. POC-B: end-to-end topology. POC-C: TX audio
path. After POC-C, every "is this even possible on Android?"
question is answered, and the rest of the build is execution.
