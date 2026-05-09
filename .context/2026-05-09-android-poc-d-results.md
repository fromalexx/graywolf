# Android POC-D — run report

**Date:** 2026-05-09
**Branch:** feature/android-poc-d
**Commit at end of run:** 7d1a6fd

## Verdict

**GREEN with caveats.** Both PTT primitives proven on the operator's bench:
Digirig keys via CP2102N RTS, AIOC keys via CDC-ACM DTR (firmware-locked
asymmetric line semantics). CM108 HID Set_Report writes coexist with
AudioRecord capture under load — criterion #9 (the verdict-blocker) holds.
Caveats are around (a) the CM108 HID layout in the original spec, which
was wrong and got rebuilt to match graywolf-modem's desktop driver, and
(b) AIOC's PTT path being CDC-ACM DTR/RTS rather than CM108 HID GPIO on
this firmware revision — the spec §3 footnote ("AIOC speaks bit 3 the
same way the Digirig does") was imprecise.

## Toolchain

- `rustc` 1.90.0 (Homebrew)
- `cargo` 1.90.0 (Homebrew)
- Go 1.26.2 (darwin/arm64 host, `GOOS=android GOARCH=arm64 CGO_ENABLED=0`)
  with **`GOWORK=off`** to bypass the parent repo's `go.work` redirect
- Android NDK: unchanged from POC-B/C (no Rust/JNI rebuild required)
- JDK 17 / Gradle 8.14.4 on Alpine builder (`block.local`)
- AGP 8.5 / Kotlin 1.9.x / minSdk 28 / targetSdk 34 / compileSdk 34
- `usb-serial-for-android` 3.10.0 via JitPack (latest at run time)
- Tablet: T865 (Galaxy Tab S6 LTE), Android 14
- Builder: `block.local` (Alpine), gradle assembleDebug rsync/scp/adb path
  via `scratch/poc-b/build-and-install.sh`

## Hardware as tested

| Role | Model | Notes |
|---|---|---|
| Tablet | Samsung T865 (Galaxy Tab S6) | Android 14, USB-C OTG |
| Powered hub | (in line) | per spec §3 |
| Primary cable | Digirig Mobile | composite: CP2102N (`0x10C4:0xEA60`) + CM108 (`0x0D8C:0x0012`) |
| Secondary cable | AIOC (skuep firmware ≥ 1.2.0) | `0x1209:0x7388`, 9 ifaces |
| Radio | Baofeng UV-5R | 144.390 MHz, low power |
| Reference RX | second APRS station w/ APRS-IS | over-the-air decode confirmed (criterion 10) |

## Devices enumerated

| Device | vid:pid | role | iface count | HID iface id |
|---|---|---|---|---|
| Digirig CP2102N | `0x10C4:0xEA60` | CP2102N | 1 | n/a |
| Digirig CM108 | `0x0D8C:0x0012` | CM108 | 6 | 3 |
| AIOC | `0x1209:0x7388` | AIOC (CDC-ACM PTT, also exposes CM108-shape HID) | 9 | 3 (HID accepts Set_Report but firmware does not drive GPIO) |

AIOC interface map (logcat dump, kept here because the descriptor names
matter for phase 5's transport-keying decisions):

```
iface[0] id=0 class=1 (Audio Control)        endpoints=0
iface[1] id=1 class=1 (Audio Streaming)      endpoints=0  (alt 0)
iface[2] id=1 class=1 (Audio Streaming)      endpoints=2 (0x02 OUT, 0x82 IN, isoc)
iface[3] id=2 class=1 (Audio Streaming)      endpoints=0  (alt 0)
iface[4] id=2 class=1 (Audio Streaming)      endpoints=1 (0x81 IN, isoc)
iface[5] id=3 class=3 (HID)                  endpoints=1 (0x83 IN, intr)
iface[6] id=4 class=2 (CDC Comm)             endpoints=1 (0x85 IN, intr)
iface[7] id=5 class=10 (CDC Data)            endpoints=2 (0x04 OUT, 0x84 IN, bulk)
iface[8] id=6 class=254 (DFU)                endpoints=0
```

## Success criteria

| # | Criterion | Stage | Pass | Notes |
|---|---|---|---|---|
| 1 | CP2102N enumerates on attach | 1 | ✅ | one-time perm dialog; "Use by default" persisted across relaunch |
| 2 | usb-serial-for-android opens + retains | 1 | ✅ | fd count 282 stable across 5+ key/unkey cycles |
| 3 | Key/unkey radio (5 trials) | 1 | ✅ | TX LED on radio toggles cleanly; tx-test frame plays through |
| 4 | RX coexistence under RTS toggles | 1 | ✅ | `frames decoded` ticked through every key/unkey, real APRS packet (KK7NWN-1) decoded mid-test |
| 5 | CM108 enumerates | 2 | ✅ | iface 3 detected as HID-class on both Digirig and AIOC |
| 6 | Per-interface claim, audio untouched | 2 | ✅ | `claimInterface(force=true)` only on HID iface; `dumpsys media.audio_flinger` shows AudioStreamIn frames-read grew from 4.46M to 105.7M between pre/post snapshots — AudioRecord kept running |
| 7 | HID Set_Report rc==4 | 2 | ✅ | every Set_Report returned rc=4 once the layout was corrected (see "Empirical findings") |
| 8 | Key/unkey radio via HID (5 trials) | 2 | ⚠️ partial | Digirig CM108 GPIO pins are not externally wired to the PTT line on Digirig hardware — the chip handles audio only and Digirig PTT is CP2102N RTS. AIOC tested separately; see Stage 3 |
| 9 | RX coexistence under HID writes (CRITICAL) | 2 | ✅ | counter kept ticking through 10+ rapid CM108 HID writes; no audio glitch, no frames-decoded stall |
| 9b | Same on AIOC | 3 | ✅ | AIOC CDC-ACM DTR/RTS toggles do not perturb AudioRecord (no HID writes used; AIOC's actual PTT is CDC-ACM, see findings) |
| 10 | Combined RF demo decodes | 4 | ✅ | over-the-air RX decode confirmed on second APRS station mid-Stage-1 |

## Empirical findings

### CM108 HID Output Report layout was wrong in the spec

Plan §1 criterion 7 specified `[0x00, 0x00, gpio, 0x00]`, with the GPIO
byte at offset 2. **This puts the value in the OR2 (data direction)
byte and leaves OR1 (output values) zero**, so the chip never configures
the pin as an output and the write silently no-ops even though
`controlTransfer` returns `rc=4`.

Corrected layout (matches `graywolf-modem/src/tx/ptt_cm108_unix.rs:43-47`
which keys the AIOC successfully on Linux desktop):

```
byte 0 = HID_OR0 (mode, always 0)
byte 1 = HID_OR1 (GPIO output values)
byte 2 = HID_OR2 (GPIO data direction, 1 = output)
byte 3 = HID_OR3 (SPDIF, unused)
```

The HID report ID (0) is sent in the SET_REPORT control transfer's
`wValue=0x0200`, **not** as a buffer prefix — so on-the-wire payload is
4 bytes, not 5. (Desktop hidapi prepends the report ID inside the buffer
because hidapi's API contract requires it; Android's `controlTransfer`
contract is the opposite. Easy to confuse the two.)

### CM108 GPIO pin numbering is 1-indexed

Plan said "bit 3" (0-indexed, mask 0x08). Datasheet + graywolf-modem
say "GPIO3" with 1-indexed pin numbering (mask 0x04). Switched to
1-indexed pin (default 3 → mask 0x04) for parity with the desktop
driver and the datasheet.

### AIOC PTT is CDC-ACM, not CM108 HID

AIOC firmware ≥ 1.2.0 (operator's hardware): **PTT asserted on `DTR=1`
AND `RTS=0`**. Driving DTR=1+RTS=1 (the naive "both lines high keys")
does not key. Driving RTS only does not key. Drop RTS to 0 on open and
hold there for both keyed and unkeyed states; toggle DTR to switch.

The AIOC also exposes a CM108-shape HID interface, but on this firmware
the HID Set_Report is accepted (`rc=4`) without driving any external
GPIO. The HID iface has only an INPUT endpoint (0x83 IN, interrupt),
no OUT — the firmware presents the CM108 surface for software-compat
but does not wire it through to PTT. APRSdroid's `UsbTnc.scala` confirms
this: it opens the AIOC as a CDC-ACM port and writes KISS frames; it
never toggles RTS/DTR for PTT and never sends HID Set_Reports.

### Digirig CM108 GPIO is not wired to PTT in hardware

Digirig hardware: CP2102N drives the PTT line; CM108 handles audio
in/out only. The CM108 chip's GPIO pins are not connected to anything
externally on Digirig boards. Set_Report writes succeed (chip accepts,
returns rc=4) but no PTT wire toggles. Plan §3 footnote ("Digirig has
bit 3 wired the same way AIOC does") is wrong; verified empirically on
the operator's bench. **This does not affect criterion #9** — coexistence
is about whether HID writes break AudioRecord, and they do not.

### CP2102N init order

Worked first try with `setParameters` *before* `port.rts = false` (the
default usb-serial-for-android sequence). No second toggle required.
The "some CP210x variants need setLineEncoding first" trap from spec §7
did not bite on this CP2102N.

### Decode rate

`frames decoded` counter ticked through every key/unkey transition on
both transports. KK7NWN-1 APRS packet decoded mid-Stage-1, confirming
RX path stays live during PTT activity. No quantitative before/after
fps measurement captured because the live RF environment was producing
intermittent packets rather than a continuous beacon.

### fd count across 5 RTS cycles

Single snapshot: 282 fds. Stable across the test window (no growth
visible in repeat checks). No fd leak detected.

## Traps hit

- **Spec §7 "AudioPump priority":** not relevant — no priority changes
  required during the test; AudioRecord stays alive through HID writes
  by virtue of the per-interface claim contract being honored.
- **Spec §7 "permission cache":** survived the relaunch; the system
  dialog only appeared once per vid/pid the first time the operator
  approved with "Use by default" checked.
- **Spec §7 "HID Output Report layout":** **bit me hard** — see
  empirical findings.
- **Spec §7 "GPIO bit number":** plan said bit 3 (0-indexed); reality
  is pin 3 (1-indexed). Same trap as the layout one.
- **Spec §7 "claimInterface":** held the line. `force=true` only on the
  HID iface, never on audio ifaces. AudioRecord uninterrupted.
- **Plan-write-time blind spot — `go.work`:** the parent repo carries a
  `go.work` that redirects the graywolf module to the main worktree, so
  `go build ./cmd/graywolf-pocb` from the POC-D worktree silently
  embedded the pre-POC-D `pocb_index.html` and the PTT panel never
  appeared in the WebView. Worked around with `GOWORK=off`. Phase 5 must
  either move the embedded HTML into a build-artifact path the gradle
  build owns, or document the `GOWORK=off` requirement in
  `build-and-install.sh`.
- **Plan-write-time blind spot — `tryOpen` thread:** `tryOpen` ran on
  the BroadcastReceiver thread (which is the main thread). The CP2102N
  init's first control-transfer stalled the main thread long enough to
  trip an Input-dispatch ANR after permission grant. Fixed by
  dispatching each open to a daemon worker thread. Phase 5's
  `pkg/pttdevice/` will inherit the same constraint — USB control
  transfers must not run on the dispatcher thread.
- **Plan-write-time blind spot — stale handle on hot-swap:** the
  initial `openCp2102n`/`openCm108` body said `if (handle != null) skip`,
  which kept a stale handle alive after the hardware was replaced
  (Digirig → AIOC, etc). Fixed to compare `device.deviceName` and
  replace+close the prior handle when a different device with the same
  role appears.

## Inherited debt acknowledged (per spec §9)

- `libgraywolfmodem.so` and `libgraywolf.so` committed to git (no change
  in POC-D; phase 5 must move to externalNativeBuild + a build target
  that the gradle build invokes itself, removing the `GOWORK=off go
  build` manual step).
- `arm64-v8a` only (phase 5 plan adds `armeabi-v7a` + `x86_64`).
- Toolchain PATH still implicit (Homebrew rustc, system Go, NDK via
  `~/android-sdk` on `block.local`); phase 5 must encode in
  Makefile / Gradle task.

## Stop conditions surfaced (if any)

- Initial Stage 2 attempt: AIOC Set_Report writes returned `rc=4` but
  did not key the radio at any GPIO bit 0..7. **Surfaced to user**:
  showed "rc=4 without keying" and asked to pick "document AIOC HID
  non-functional and move on" vs "implement CDC-ACM driver for AIOC".
  User chose the CDC-ACM path; AIOC docs (`DTR=1 AND RTS=0`) confirmed
  the right answer; AIOC keyed via DTR=1/RTS=0 on first try once the
  layout was corrected.
- Initial Stage 1 attempt: PTT panel did not render in the WebView even
  after the APK rebuilt. **Surfaced to user** (ran `strings` on
  `libgraywolf.so` and showed it was missing the `ptt-panel` literal),
  diagnosed as the `go.work` redirect, fixed with `GOWORK=off` rebuild.
- One ANR during initial Stage 1: `Input dispatching timed out... Waited
  5001ms for FocusEvent`. Caused by `tryOpen` running on the main
  thread. **Fixed in commit `41ece45`** before continuing.

## Phase-5 implications

- **CM108 HID layout:** lock the corrected 4-byte
  `[0, value, mask, 0]` layout in design.md §3.3 and in the phase-5
  proto. Document that the report ID lives in `wValue=0x0200`, not the
  buffer.
- **CM108 GPIO pin numbering:** lock to 1-indexed in the proto and
  config schema. Display values must match the datasheet's "GPIOn"
  naming, not 0-indexed bit positions.
- **AIOC transport:** add a CDC-ACM-DTR transport to
  `pkg/pttdevice/`. Semantics: DTR drives PTT, RTS held at 0. Cannot be
  unified with the generic CP2102N RTS transport (different line, AIOC
  firmware specifically requires RTS=0 in keyed state). Vid/pid
  `0x1209:0x7388` for default classification; firmware <1.2.0 will need
  a separate "DTR-only, RTS=ignore" variant if anyone is on it.
- **Digirig CM108 HID:** drop from default UI options; keep the code
  path for AIOC and any other CM108-emulating device whose GPIO is
  actually wired through. Document "Digirig PTT is CP2102N RTS only" in
  the phase-5 device docs.
- **Permission UX:** hot-plug + USB_DEVICE_DETACHED handling lands in
  phase 5. The replace-stale-handle fix here is enough for the bench
  but not for surprise unplugs in the field.
- **`go.work` foot-gun:** phase 5 must remove the manual `GOWORK=off`
  step. Either have gradle invoke the Go build from inside the worktree
  with `GOWORK=off` set automatically, or change the embed target so
  the worktree's `pocb_index.html` doesn't get masked by the parent
  repo's copy.
- **`tryOpen` threading contract:** every USB open must dispatch to a
  worker thread. Codify in the `pkg/pttdevice/` Go interface so this
  isn't easy to regress.
- **Status-row found-but-not-open:** spec says the status row should
  distinguish "device present, no permission" from "device absent". The
  initial `status()` only emitted `*_open` flags, so the WebView always
  showed found-but-no-perm devices as "missing". Added `cp2102n_found`
  / `cm108_found` / `aioc_found` flags in this run; phase 5's
  proto-driven status surface should keep them.
