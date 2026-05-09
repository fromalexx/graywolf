# Android POC-D — Digirig + AIOC PTT path proof

**Status:** spec, finalized after POC-C run report
**Date:** 2026-05-08 (finalized 2026-05-08 evening, post-POC-C)
**Repo:** all work lands in `~/dev/graywolf` on a feature branch off
`main`. No new repo, no fork, no submodule. All paths in this spec are
relative to the graywolf repo root.

**Revision log:**
- 2026-05-08: initial draft (parked while POC-C ran).
- 2026-05-08 evening: finalized after POC-C YELLOW verdict (run
  report `.context/2026-05-08-android-poc-c-results.md`). Folded in:
  AX.25 destination corrected to `APGRWO` (not `APZGRY` — graywolf
  TX path uses `APGRWO`); combined-demo timing reflects measured
  834 ms frame envelope (not the original 3 s estimate); reference
  receiver options expanded to include the second-APRS-station +
  APRS-IS path POC-C used; new traps added (Homebrew rustc
  shadowing, AudioTrack post-build state lifecycle); new §9
  documenting POC-B/C carry-forward debt that POC-D inherits.
**Scope:** prove both Android PTT paths the operator's bench actually
uses. Two-part POC, single APK, both parts carry forward into phase 5:

1. **CP2102N RTS** path — Digirig PTT. Kotlin opens the USB-serial
   bridge via `usb-serial-for-android`, retains the `UsbSerialPort`,
   toggles RTS on key/unkey.
2. **CM108 HID GPIO** path — AIOC PTT (and Digirig's secondary HID
   path). Kotlin opens the CM108's HID interface only, sends an HID
   Set_Report to toggle GPIO bit 3.

**No `txgovernor`. No `pkg/platformsvc`. No proto wiring. No real
PTT-driver invocation from Rust.** The trigger goes Kotlin button →
Kotlin USB transfer → radio keys. POC-C's TX-test trigger optionally
chains in for an end-to-end "key + play frame + unkey" demo, but the
PTT path itself stands alone.

POC-A proved RX, POC-B end-to-end topology, POC-C TX audio. POC-D
proves the bench's actual PTT methods work and that **CM108 HID PTT
coexists with CM108 audio capture** — the one Android-specific
subtlety that POC-A already half-discovered (commit `7a71c53`: claim
only the HID interface, never the audio interfaces).

After POC-D, every "is this even possible on Android?" question the
operator's hardware kit poses is answered. Phase 5's PTT work
becomes integration of Rust modem PTT events with already-proven
Kotlin transports.

---

## 1. Success criteria

POC-D is **green** when all hold on the reference tablet:

### Part 1 — CP2102N RTS (Digirig)

1. **Enumeration.** Kotlin `UsbAdapter` recognizes the Digirig's
   CP2102N (vid `0x10C4`, pid `0xEA60`) on attach. System per-device
   permission dialog appears once; subsequent app launches reuse the
   grant when the operator checks "Use by default."
2. **Open + retain.** `usb-serial-for-android` opens the device,
   yields a `UsbSerialPort`, the Service retains the handle in a
   singleton. Re-opening succeeds across app restart cycles. No fd
   leak (`adb shell lsof -p <pid> | wc -l` stable across 5
   key/unkey cycles).
3. **Key/unkey.** Pressing the WebView "Key Digirig" button results
   in audible click on the radio + TX indicator illuminated; "Unkey
   Digirig" stops it. Five trials, zero misses.
4. **No interference with audio.** With POC-A/B's `AudioPump` running
   from the CM108 audio side of the same Digirig composite device,
   keying and unkeying the CP2102N RTS line does not interrupt
   `AudioRecord` capture. Decode rate stays inside POC-B's baseline.

### Part 2 — CM108 HID GPIO (AIOC, secondary Digirig path)

5. **Enumeration.** `UsbAdapter` recognizes the CM108 family (vid
   `0x0D8C`, pid `0x0012` for the Digirig's CM108; AIOC may present
   a different pid — the plan must enumerate and report whatever the
   operator's AIOC actually shows). System permission dialog on
   first attach.
6. **Per-interface claim.** Service opens `UsbDeviceConnection`,
   claims **only interface 3 (HID)**, leaves interfaces 1 + 2
   (audio class) untouched. Verified by `dumpsys media.audio_flinger`
   showing the audio path still active on the same physical device.
7. **HID Set_Report.** Kotlin sends a control transfer:
   - `requestType=0x21` (Class | Interface | H→D)
   - `request=0x09` (`SET_REPORT`)
   - `value=0x0200` (Output Report, ID 0)
   - `index=<HID interface number>`
   - `data=[0x00, 0x00, gpio_byte, 0x00]` where `gpio_byte` toggles
     bit 3 (PTT). Confirm the right bit empirically — Burr-Brown /
     CM108 datasheets disagree across revisions; the plan should
     try bits 0..3 and report which one keys the radio. Lock the
     finding in the run report.
8. **Key/unkey on radio.** "Key AIOC" button fires the
   bit-3-high transfer, radio keys. "Unkey AIOC" fires bit-3-low,
   radio unkeys. Five trials.
9. **Coexistence with audio.** **This is the most important
   criterion.** With `AudioPump` actively decoding (RX from POC-A/B's
   AudioRecord path on the same CM108), HID PTT toggles do not
   break, glitch, or pause audio capture. Decode rate stays inside
   POC-B's baseline. Failure here means CM108 HID PTT and CM108
   audio cannot coexist on Android, which would force a design
   change in phase 5b (probably "use CP2102N for PTT even when the
   AIOC is the audio device, via a second USB-serial dongle" — but
   AIOC by definition has no UART, so this would push AIOC support
   to v1.1 with a workaround).

### Combined criterion (optional but easy)

10. **End-to-end RF demo.** Combined with POC-C's TX-test trigger,
    a single button does "key Digirig (or AIOC) → play canned
    frame → unkey." A second receiver (HT or SDR + direwolf) hears
    and decodes the AFSK frame over RF. Operator-meaningful demo:
    "tap one button, my tablet transmits an APRS beacon over the
    air."

POC-D is **red** if any of:

- `usb-serial-for-android` cannot open the CP2102N at all (would
  invalidate the entire Android PTT plan).
- HID Set_Report on interface 3 fails with `EBUSY` /
  `ENODEV` / negative return regardless of which interface is
  claimed (would invalidate AIOC support on Android).
- CM108 HID writes succeed but break audio capture every time
  (criterion 9 fails). Design change required.
- Permission dialog requires fresh consent on every attach despite
  "Use by default" — significant UX problem worth fixing in POC-D
  rather than inheriting in phase 5.

POC-D is **yellow** if any of:

- HID GPIO bit number is non-standard; lock empirically.
- Permission dialog reappears occasionally (Android version-specific
  behavior); document and accept.
- Either path requires a non-default `setBaudRate` /
  `setLineEncoding` to even operate the modem-control bits (CP2102N
  shouldn't, but record what worked).

---

## 2. Out of scope

- **`txgovernor`, `modembridge`, `proto/platform.proto`,
  `pkg/platformsvc/`, Rust PTT drivers.** Phase 2 + 5 own those.
  POC-D is a Kotlin-only proof of the wire-toggling primitives.
- **PTT timing measurements.** No keying-to-audio latency budget,
  no debounce profiling. Phase 5 measures.
- **Hot-plug handling.** Plug device before app launch, leave
  plugged. `USB_DEVICE_DETACHED` broadcast handling lands in phase 5.
- **Permission UX polish.** First-launch system dialog is fine.
- **CM108 GPIO bits other than PTT.** Don't toggle other bits;
  AIOC's hardware decisions about non-PTT pins are not relevant
  here.
- **Bluetooth SPP PTT.** Phase 5c.
- **Multiple PTT methods active simultaneously.** One radio, one
  PTT path at a time per trial.
- **Per-radio TX level shaping** (carries forward from POC-C).
  No new audio work.
- **Hardware matrix beyond the operator's bench kit.** Pixel 6a /
  A54 / various AIOCs land in the design's §8.5 matrix during phase
  7 beta.

If the executor finds themselves wiring `txgovernor`, generating
proto schemas, or building a `pkg/pttdevice/android.go`, they have
left POC-D scope.

---

## 3. Reference hardware

| Role | Choice |
|---|---|
| Tablet | Topicon T865 (arm64-v8a, Android 14) |
| Primary cable | Digirig (CP2102N at `0x10C4:0xEA60` + CM108 at `0x0D8C:0x0012`) |
| Secondary cable | AIOC if the operator has one (any CM108-class HID PTT device) |
| Radio | Baofeng UV-5R, tuned 144.390 MHz (off-air or a dummy load with bleed RF for over-the-air verification) |
| Reference RX | Either: second APRS station with APRS-IS forwarding (POC-C's actual setup — operator's NW5W-5 station), OR a second HT / SDR + laptop direwolf. Both work. |
| ADB transport | adb-over-Wi-Fi |
| USB host | tablet's USB-C port. **Powered hub between tablet and the cables is recommended**: keying transmitters can pull current spikes the tablet's port may flinch at |

The CP2102N and CM108 paths share the Digirig — testing both on the
same physical device automatically exercises the coexistence
criterion. If the operator doesn't have an AIOC yet, criteria 5-9
can still be exercised against the Digirig's CM108 HID interface
(Digirig has bit 3 wired the same way AIOC does, per Digirig
schematics).

---

## 4. Build setup

### 4.1 Toolchain

Same as POC-B/C. No new tools.

### 4.2 New / changed code

#### 4.2.1 Gradle (`android/app/build.gradle.kts`)

Add `usb-serial-for-android`:

```kotlin
dependencies {
    implementation("com.github.mik3y:usb-serial-for-android:3.7.3")
    // ... existing deps
}
```

Plus the JitPack repo if not already present in the project's
`settings.gradle.kts`:

```kotlin
dependencyResolutionManagement {
    repositories {
        mavenCentral()
        maven("https://jitpack.io")
    }
}
```

Verify the library version matches the most recent released tag at
plan-execute time (3.7.3 was current as of late 2024; check
github.com/mik3y/usb-serial-for-android/releases for current).

#### 4.2.2 Manifest (`android/app/src/main/AndroidManifest.xml`)

Add:

```xml
<uses-feature android:name="android.hardware.usb.host" android:required="false" />
```

Plus the existing `FOREGROUND_SERVICE_CONNECTED_DEVICE` perm from
POC-B (already present). Optional but recommended: a
`<receiver>` for `USB_DEVICE_ATTACHED` so the system surfaces a
permission dialog without requiring the operator to launch the app
first. Out of scope for POC-D verdict but a 5-line addition.

#### 4.2.3 Kotlin (`android/app/src/main/kotlin/com/nw5w/graywolf/`)

New file:
- `usb/UsbPttAdapter.kt` — singleton owning retained
  `UsbSerialPort` (CP2102N) and / or `UsbDeviceConnection` + claimed
  HID interface (CM108). Exposes:
  ```kotlin
  fun keyDigirig(): Boolean
  fun unkeyDigirig(): Boolean
  fun keyAioc(): Boolean
  fun unkeyAioc(): Boolean
  fun status(): UsbPttStatus  // which devices recognized + open
  ```
  Internal state is a map of `UsbDevice` → opened transport.

WebView bridge:
- `WebAppInterface.kt` extended with the four key/unkey calls
  surfacing through `@JavascriptInterface`. POC-D doesn't need a
  REST round-trip — direct JS-to-Kotlin is faster to wire and the
  endpoint is throwaway.

Service hooks:
- `GraywolfService.onCreate` instantiates `UsbPttAdapter` and
  enumerates current attached devices once. No watcher loop yet
  (hot-plug is phase 5). Logs which devices were found.

#### 4.2.4 WebView page (`cmd/graywolf-pocb/pocb_index.html`)

Extend POC-B's hand-written page with four buttons:

```html
<button onclick="ptt('key_digirig')">Key Digirig (CP2102N RTS)</button>
<button onclick="ptt('unkey_digirig')">Unkey Digirig</button>
<button onclick="ptt('key_aioc')">Key AIOC (CM108 HID GPIO)</button>
<button onclick="ptt('unkey_aioc')">Unkey AIOC</button>
```

Plus a status read-out polling `UsbPttAdapter.status()` every 1 s
showing which devices the Service sees. Lives next to the existing
frames-decoded / uptime read-outs from POC-B.

If POC-C also landed by the time POC-D runs, add a fifth combined
button:

```html
<button onclick="combinedTxDemo('digirig')">
  Key Digirig + Play Test Frame + Unkey
</button>
```

`combinedTxDemo` sequence (timings reflect POC-C measurements: frame
envelope is 834 ms, not the 3 s the POC-C plan originally estimated):

1. `key_digirig` (or `key_aioc`) → RTS / HID write returns immediately.
2. Sleep ~300 ms (txdelay; gives the radio time to fully key before
   audio starts).
3. Fire POC-C's `tx_test` → AudioTrack plays the canned 834 ms frame.
4. Block on POC-C's playback-complete signal (already wired —
   `tx_test_done ok=true` from `OnPlaybackPositionUpdateListener` in
   the AudioTxTest helper).
5. Sleep ~50 ms (txtail).
6. `unkey_digirig` (or `unkey_aioc`).

Total wall-clock per trigger: ~1.2 s. This is the operator-meaningful
demo for criterion #10.

#### 4.2.5 No Rust changes

POC-D is Kotlin-only. The Rust cdylib doesn't gain a new JNI entry
point. POC-C handles TX audio; POC-D handles wire toggling; nothing
needs to change in the modem.

#### 4.2.6 No Go changes

The Go stub doesn't grow new endpoints. PTT is direct
WebView ↔ Kotlin via `@JavascriptInterface`. Throwaway.

### 4.3 Build invocations

Same as POC-B/C: `./gradlew assembleDebug`, `adb install -r`. No
new cargo commands.

---

## 5. Run procedure

### Stage 1 — Digirig CP2102N RTS

1. Plug Digirig into tablet (powered hub recommended).
2. Cable Digirig audio out into laptop input, run desktop
   `graywolf-modem` or `direwolf` for level monitoring.
3. Connect tablet over Wi-Fi adb.
4. Install POC-D APK. Tap icon. Foreground notification appears.
5. System prompts for USB permission for the Digirig (one prompt
   for the composite device; tap "Use by default" to retain).
6. WebView status row should show: `Digirig: CP2102N + CM108 open`.
7. Tap "Key Digirig" — radio's TX indicator lights, audible click.
8. Tap "Unkey Digirig" — TX stops.
9. Repeat 7-8 four more times, ~5 s apart.
10. While `AudioPump` is running (RX panel showing decoded frames
    from POC-B baseline), repeat 7-8 once and verify decode rate
    is unaffected.
11. Capture logcat → `scratch/poc-d/digirig.log`.

### Stage 2 — CM108 HID GPIO (Digirig HID path)

12. Same Digirig still plugged.
13. Tap "Key AIOC" (the button drives the CM108 HID GPIO regardless
    of whether the operator has an AIOC; Digirig's CM108 has the
    same wiring). Radio keys.
14. Tap "Unkey AIOC". Radio unkeys.
15. Repeat 13-14 four more times.
16. Coexistence: with `AudioPump` decoding, repeat 13-14 once and
    confirm decode rate stays at POC-B baseline. **Critical
    criterion #9.**
17. Capture logcat → `scratch/poc-d/cm108.log`.

### Stage 3 — AIOC (optional, only if operator has one)

18. Unplug Digirig. Plug AIOC.
19. WebView status row should now show: `AIOC: CM108 open`.
20. Repeat steps 13-16 against the AIOC.
21. Capture logcat → `scratch/poc-d/aioc.log`.

### Stage 4 — Combined RF demo (optional, requires POC-C green)

22. Re-plug Digirig if Stage 3 swapped to AIOC.
23. Run a second receiver tuned to 144.390 MHz with `direwolf`
    decoding into a logfile.
24. Tap the combined "Key + Play + Unkey" button five times spaced
    ~10 s.
25. Reference receiver should decode `NW5W-8>APGRWO:!4028.56N/
    11150.71W< POC-C TX test - NW5W bench` five times.
26. Capture reference log → `scratch/poc-d/over-the-air.log`.

---

## 6. Deliverables

| Artifact | Location |
|---|---|
| `usb-serial-for-android` Gradle dep | `android/app/build.gradle.kts` |
| Kotlin USB PTT adapter | `android/app/src/main/kotlin/com/nw5w/graywolf/usb/UsbPttAdapter.kt` |
| WebView bridge methods | `android/app/src/main/kotlin/com/nw5w/graywolf/webview/WebAppInterface.kt` (extended) |
| WebView page additions | `cmd/graywolf-pocb/pocb_index.html` (extended) |
| Stage logs | `scratch/poc-d/{digirig,cm108,aioc,over-the-air}.log` |
| Run report | `.context/2026-05-XX-android-poc-d-results.md` (filled in after the run, force-added per repo convention) |

The run report captures:

- Toolchain versions + USB device vid/pid actually seen for each
  cable family the operator tested.
- 10 stage criteria pass/fail table.
- Empirical CM108 GPIO bit number (in case it's not bit 3).
- Decode-rate measurements for the coexistence criteria (4 + 9 +
  16 stage). Compare against POC-B's baseline; flag any drop > 10%.
- Any HAL or routing surprises captured from logcat.
- Verdict: green / yellow / red.
- If yellow or red: blocker description, proposed phase-5 design
  adjustment, whether a design-doc revision is needed before
  phase 5 starts.

---

## 7. Plan structure hint for `/superpowers:writing-plans`

Smaller and shallower than POC-B; comparable to POC-C in scope.
Roughly six tasks:

1. **Gradle dependency.** Add `usb-serial-for-android` + JitPack
   repo. Verify `./gradlew assembleDebug` still produces a valid
   APK.
2. **`UsbPttAdapter` skeleton.** Singleton class, enumeration on
   construct, status() reporting which device families are
   recognized. No transfers yet.
3. **CP2102N RTS path.** Open via `usb-serial-for-android`,
   `setRTS(true/false)`. Wire to two `WebAppInterface` methods.
   Two WebView buttons. Verify on bench (Stage 1).
4. **CM108 HID GPIO path.** `UsbDeviceConnection.openDevice` + claim
   interface 3 + `controlTransfer` SET_REPORT. Empirically locate
   the right GPIO bit. Wire two more methods + buttons. Verify
   (Stage 2 + 3).
5. **Coexistence test.** With `AudioPump` running, exercise both
   PTT paths and confirm decode rate stays at POC-B baseline.
   This is the verdict-blocker for criterion #9.
6. **(Optional) Combined RF demo.** Only if POC-C has merged: add
   the combined "Key + Play + Unkey" button. Run reference receiver
   over the air, capture log.

Tasks 1, 2 sequential. Tasks 3 and 4 independent of each other —
parallelizable. Task 5 sequential after 3 and 4. Task 6 only after
POC-C is green.

Traps the planner should call out:

- **`UsbDeviceConnection.claimInterface(iface, force=true)` on the
  CM108's audio interfaces detaches `snd-usb-audio` and breaks
  capture.** POC-A learned this (commit `7a71c53`). Plan must claim
  **only** the HID interface (typically interface 3, but enumerate
  to confirm — `UsbInterface.getInterfaceClass() == USB_CLASS_HID`).
- **Per-device permission caching.** Android caches permission per
  vid/pid pair when the operator checks "Use by default." If the
  cache misses (rare but happens after OS upgrades), a fresh
  prompt appears. Don't try to suppress it — it's the user's
  consent record.
- **CP2102N modem-control init.** Some CP210x variants require an
  explicit `setLineEncoding` call before RTS toggles take effect;
  `usb-serial-for-android` handles this internally for most chips
  but verify by toggling RTS *before* any baud-rate set and seeing
  if the radio keys. If it doesn't, set baud first and retry.
- **CM108 HID Output Report layout.** The report is 4 bytes:
  `[0x00, 0x00, gpio_state, 0x00]`. The Set_Report `value` field
  (`0x0200`) encodes Output | ReportID 0. Some plan-writers will
  guess Input or Feature; don't.
- **GPIO bit number.** Datasheet says bit 3 is the standard PTT
  wiring. Verify empirically; AIOC firmware revisions have used
  bits 0-3 over time. Try one, watch the radio, lock the answer.
- **`AudioPump` thread priority during PTT writes.** Control
  transfers on USB are short (sub-ms typical) but `AudioRecord`
  buffer underruns at sub-ms granularity glitch the audio. If
  coexistence (criterion #9) shows decode regressions, profile the
  USB transfer duration; if it's borderline, bump the AudioPump
  thread to `THREAD_PRIORITY_URGENT_AUDIO`.
- **Homebrew `rustc` shadows rustup on the operator's bench.** POC-C
  hit this: `cargo ndk -t arm64-v8a` failed with "can't find crate
  for `core`" because Homebrew's `rustc 1.90.0` lacked the
  `aarch64-linux-android` standard library. Plan must prepend
  `~/.rustup/toolchains/stable-aarch64-apple-darwin/bin` to PATH
  before any cargo invocation, or document the requirement in a
  `scratch/poc-d/run.sh` so the next clean checkout doesn't trip.
  Phase 5's build automation needs to make this explicit; POC-D
  inherits the workaround.
- **AudioTrack post-build state.** Not directly POC-D's concern (no
  new AudioTrack code) but worth noting since the combined demo
  reuses POC-C's `AudioTxTest`: post-`Builder.build()` state for
  `MODE_STATIC` is `STATE_NO_STATIC_DATA` (= 2), not
  `STATE_INITIALIZED`. POC-C commit `78f5c3b` corrected the lifecycle
  check; if a future change re-tightens the state assertion,
  combined-demo trial 1 will fail with `tx_test_done ok=false`.

---

## 8. Stop conditions for the executor

Surface to the user, do not press through, if any of:

- `usb-serial-for-android` won't open the CP2102N. Indicates a
  library version / permission / fundamental USB host policy
  issue. Don't reimplement the library.
- HID Set_Report fails on interface 3 regardless of claim state.
  Indicates either the wrong interface number on this CM108
  variant or a kernel-side claim that survives our open. Surface;
  may need a different USB API shape.
- CM108 HID writes succeed but break audio capture every time
  (criterion #9 hard fail). This is a design-invalidating result
  for AIOC support on Android. Surface; design `§3.3` and phase
  5b need revision before continuing — possibly demoting AIOC to
  v1.1 with a "use a separate CP2102N dongle for PTT" workaround.
- The empirical CM108 GPIO bit search yields no working bit.
  Indicates either the hardware is wired non-standard or our HID
  packet format is wrong. Surface; check Digirig's schematic and
  AIOC's firmware source.

---

## 9. Inherited debt POC-D does not solve

These are POC-B/C carry-forwards that POC-D will also commit but
phase 5 must address before its first commit. Listed so the run
report doesn't have to rediscover them:

- **`android/app/src/main/jniLibs/arm64-v8a/libgraywolfmodem.so` and
  `libgraywolf.so` are committed to git.** ~1.1 MB and ~9.6 MB
  respectively, rebuilt on every Rust/Go change. POC-only acceptable;
  phase 5 must move .so production into Gradle `externalNativeBuild`
  (cargo-ndk wrapped) or a CI artifact pipeline, then `.gitignore`
  the directory.
- **arm64-v8a only.** Phase 5 plan has to add `armeabi-v7a` and
  `x86_64` (emulator) ABIs.
- **Toolchain PATH is implicit.** Rust toolchain selection
  (rustup-managed, not Homebrew's), NDK location, cargo-ndk `-P 26`
  flag — none enforced by the repo. Phase 5 must encode in a
  Makefile or Gradle task what the operator currently has to remember.

POC-D adds no new debt of its own beyond what POC-B/C already carry.

## 10. Why a separate POC and not a phase-5 sub-task

Phase 5 plans on `txgovernor` integration, modembridge wiring,
hardware matrix coverage, PTT timing measurement, and operator UX
for device selection — all worth a week minimum, plus another for
phase 5b CM108. If either of POC-D's two PTT primitives doesn't
work the way the design assumes, that week becomes 2-3 with
tangled "is it the proto, the relay, the audio, or the PTT?"
debugging.

POC-A: RX audio. POC-B: end-to-end topology. POC-C: TX audio.
**POC-D: PTT wire toggling.**

After POC-D every Android-specific primitive the operator's bench
kit needs is proven. Phases 2 onward become integration of known-
good primitives, not discovery of new ones.

POC-D combined with POC-C also gives the first **operator-
meaningful end-to-end demo**: tap a button, real APRS frame
transmits over real RF from a tablet. M1 (phase 4 — phone position
beacons) becomes a much smaller jump from there.
