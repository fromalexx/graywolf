# Android Phase 2 — platformsvc plumbing + POC debt close-out

**Status:** spec, ready for `/superpowers:writing-plans`
**Date:** 2026-05-09
**Repo:** all work lands in `~/dev/graywolf` on a feature branch off
`main`. All paths in this spec are relative to the graywolf repo root.
**Scope:** the first non-POC phase. Two parallelizable tracks:

1. **`proto/platform.proto` + `pkg/platformproto/` + `pkg/platformsvc/`
   + Kotlin `PlatformServer` skeleton.** The Go ↔ Kotlin contract that
   phases 4-5 (GPS, PTT, USB enumeration) build on top of. Phase 2
   ships the wire format and the framing, plus a round-trip
   `Hello` + `GpsFix` message exchange covered by unit tests on both
   sides. **No adapters wired to real hardware yet.**

2. **POC-B/C/D inherited debt close-out.** The committed `.so`
   binaries get evicted from git, replaced by a Gradle
   `externalNativeBuild` task that invokes `cargo-ndk` (Rust cdylib)
   and the NDK's Go cross-compile (Go ELF) at assemble time.
   `armeabi-v7a` and `x86_64` ABIs added. Toolchain PATH made
   explicit. `GOWORK=off` automated so the next clean checkout
   doesn't trip on `go.work` redirecting `go:embed` sources.

Phase 2 is **pure plumbing**. No new operator-visible features.
**Required before phase 3** (the real SPA needs the platformsvc Go
side at least skeletal so `pkg/gps/android.go` and the future
adapters compile in the build-tagged `cmd/graywolf/main_android.go`).

---

## 1. Definition of done

Phase 2 is complete when **all of the following hold simultaneously**.
This is a single gate; no green/yellow/red split because phase 2 is
execution work, not derisking.

### Track A — proto / platformsvc / PlatformServer

1. **`proto/platform.proto` exists, lints clean, generates Go and
   Kotlin bindings.** Single top-level `PlatformMessage` envelope
   with a `oneof body`. Message types:
   `Hello`, `GpsFix`, `BatteryState`, `UsbAttach`, `UsbDetach`,
   `UsbDeviceListRequest`, `UsbDeviceListResponse`,
   `UsbSelectRequest`, `UsbSelectResponse`,
   `PttKeyRequest`, `PttUnkeyRequest`, `PttAck`,
   `AudioDeviceListRequest`, `AudioDeviceListResponse`,
   `AudioRouteChanged`, `Error`. Schema version uint32 in `Hello`.
2. **`pkg/platformproto/` ships committed-and-generated Go bindings.**
   `make proto` regenerates them; CI drift guard (parallels invariant
   11) catches divergence between source `.proto` and committed
   bindings.
3. **`pkg/platformsvc/` provides a typed Go API** consumed by the
   future `pkg/gps/android.go`, `pkg/pttdevice/android.go`, and
   modembridge PTT relay. Surface:
   ```go
   type Client interface {
       Connect(ctx context.Context) error
       Hello(ctx context.Context, schemaVersion uint32) (*HelloResponse, error)
       SubscribeGpsFix(ctx context.Context, ch chan<- *GpsFix) error
       SubscribeAudioRouteChanged(ctx context.Context, ch chan<- *AudioRouteChanged) error
       ListUsbDevices(ctx context.Context, class UsbClass) ([]UsbDevice, error)
       SelectUsbDevice(ctx context.Context, vid, pid uint16) (*UsbHandle, error)
       KeyPtt(ctx context.Context, method PttMethod, handle *UsbHandle) (*PttAck, error)
       UnkeyPtt(ctx context.Context, method PttMethod, handle *UsbHandle) (*PttAck, error)
       Close() error
   }
   ```
   Reconnect with backoff on EOF. Hello-version mismatch terminates
   the client and returns a typed error.
4. **Kotlin `PlatformServer.kt` skeleton** at
   `android/app/src/main/kotlin/com/nw5w/graywolf/platformsvc/`.
   Binds the UDS at `${cacheDir}/platform.sock`, accepts one Go
   client at a time, dispatches messages to per-message-type
   handlers. Phase 2 ships **only** the `Hello` and `GpsFix`
   handlers — `GpsFix` handler accepts the message (no
   `LocationManager` wiring yet) and broadcasts to subscribers.
   Other message types return a typed `Error{code:
   NOT_IMPLEMENTED}`.
5. **Round-trip unit tests pass on both sides.** Go side: a fake
   `PlatformServer` (in-process loopback `net.Pipe`) round-trips
   every proto message type, asserts schema-version-mismatch
   handling, asserts reconnect-with-backoff. Kotlin side: a fake
   Go client round-trips Hello + GpsFix at minimum, asserts the
   length-prefix framing matches the wire spec.
6. **Drift guard CI test:** `go test ./pkg/platformproto/...` fails
   if the committed Go bindings differ from what `make proto` would
   regenerate. (Parallels invariant 11.)

### Track B — debt close-out

7. **`android/app/src/main/jniLibs/` is `.gitignore`'d.** No `.so`
   files committed. The directory is created at assemble time by
   the build task.
8. **Gradle `externalNativeBuild` task produces both `.so` files
   per ABI.** A Gradle task (or a `Makefile` target the Gradle
   build invokes) runs:
   - `cargo ndk -t arm64-v8a -t armeabi-v7a -t x86_64 -P 26 -o
     android/app/src/main/jniLibs build --lib --release` for the
     Rust cdylib.
   - `GOOS=android GOARCH={arm64,arm,amd64} CGO_ENABLED=0
     GOWORK=off go build` for the Go ELF (renamed
     `lib<name>.so` per N1).
   Both sides pinned to the same NDK platform API (`-P 26`,
   matching POC-B's runtime requirement).
9. **All three ABIs build clean** in CI: `arm64-v8a` (primary),
   `armeabi-v7a` (legacy 32-bit), `x86_64` (emulator).
10. **Toolchain requirements documented and asserted.** A
    `scripts/check-android-toolchain.sh` (or equivalent) verifies:
    - `rustc` is rustup-managed, has `aarch64-linux-android` +
      `armv7-linux-androideabi` + `x86_64-linux-android` targets
      installed.
    - `cargo-ndk` 4.x is on PATH.
    - `ANDROID_NDK_ROOT` (or `ANDROID_NDK_HOME`) points at NDK r27c
      or newer.
    - JDK is 17.
    - `go` is 1.23+ with the Android cross-compile targets ready.
    Run as a Gradle preBuild task; fails with a clear message
    pointing at setup docs.
11. **`GOWORK=off` automated.** The Gradle Go-build task sets
    `GOWORK=off` itself; no manual env-var management. POC-B/C/D's
    `scratch/poc-b/build-and-install.sh` is retired.
12. **Existing `cmd/graywolf-pocb/main.go` keeps building** through
    the new pipeline (still arm64 only on the operator's bench is
    fine; phase 3 retires the stub). Added ABIs need only the real
    `cmd/graywolf` (which doesn't exist on Android yet — that's
    phase 3) plus a build-tagged stub so the cross-compile
    succeeds.

### Track C — integration

13. **Existing POC-D APK still works** end-to-end: `assembleDebug`
    succeeds, install + launch decodes RX frames, all four PTT
    transports key the radio, combined TX-test demo decodes on the
    reference station. Phase 2's plumbing changes do not regress
    POC-D's bench validation.
14. **Real-device smoke** on the Topicon T865:
    - APK contains all three ABI's `.so` files (verified via
      `unzip -l app-debug.apk | grep '\.so$'` showing entries under
      `lib/arm64-v8a/`, `lib/armeabi-v7a/`, `lib/x86_64/`).
    - APK installs and runs decode on the tablet (uses arm64
      automatically per Android's ABI selection).
    - Service connects to `pkg/platformsvc/` Go client to the
      Kotlin `PlatformServer` over the new UDS, exchanges Hello,
      logs the agreed schema version. **Even though no real
      `GpsFix` is produced** — phase 4 adds the
      `LocationManager` wire — the connect-and-handshake exercises
      the wire format on real hardware.

---

## 2. Out of scope

- **Real `LocationManager` wiring.** Phase 4. The Kotlin
  `GpsAdapter` does not exist yet. Phase 2's `PlatformServer`
  accepts the `GpsFix` proto and broadcasts to subscribers, but no
  fix ever arrives on the Kotlin side because no producer is
  wired.
- **`UsbAdapter` proto integration.** Phase 5. POC-D's
  `UsbPttAdapter` is direct WebView ↔ Kotlin via
  `@JavascriptInterface`; phase 5 swaps that for the proto path.
  Phase 2 ships the message types but no adapter consumes them.
- **`AudioAdapter` proto integration.** POC-B's AudioPump is
  direct Kotlin ↔ Rust JNI; that does not change. The
  `AudioRouteChanged` proto exists but no producer/consumer is
  wired in phase 2.
- **`pkg/gps/android.go`, `pkg/pttdevice/android.go`,
  `cmd/graywolf/main_android.go`.** Build-tagged stubs that compile
  but do nothing are acceptable in phase 2 if needed for the
  cross-compile to succeed; real implementations are phases 3-5.
- **Bearer token / WebView wiring changes.** POC-B's hand-written
  page survives phase 2 unchanged. Phase 3 retires it.
- **CI workflow files (`.github/workflows/release-android.yml`,
  `android-build-smoke.yml`).** Phase 6. Phase 2 verifies builds
  on the operator's bench; CI lands later.
- **TX audio streaming.** Phase 5.
- **Hot-plug handling.** Phase 5.
- **`pkg/diagcollect/review_android.go`.** Phase 7-ish.

If the executor finds themselves wiring a `LocationListener`,
opening a USB device, or generating SPA bundles, they have left
phase 2 scope.

---

## 3. Reference hardware

| Role | Choice |
|---|---|
| Tablet | Topicon T865 (Galaxy Tab S6, arm64-v8a, Android 14 / API 34) |
| Builder | Whatever the operator currently uses (Alpine `block.local` per POC-D logs); macOS host should also work since cargo-ndk + Android NDK are cross-platform |
| Cables / radio | Same Digirig + UV-5R chain from POC-A through POC-D, only required for the integration smoke (criterion #13/14) |

Phase 2 is mostly desk work — build pipeline + proto + unit tests —
and only touches hardware for the final smoke pass. An emulator
running x86_64 satisfies the multi-ABI smoke criterion #14 if a
physical x86_64 device isn't around.

---

## 4. New / changed code

### 4.1 Proto (`proto/platform.proto`)

New file. `syntax = "proto3"; package graywolf.platform;`. Single
top-level envelope:

```proto
message PlatformMessage {
  oneof body {
    Hello hello = 1;
    GpsFix gps_fix = 2;
    BatteryState battery_state = 3;
    UsbAttach usb_attach = 4;
    UsbDetach usb_detach = 5;
    UsbDeviceListRequest usb_list_req = 6;
    UsbDeviceListResponse usb_list_resp = 7;
    UsbSelectRequest usb_select_req = 8;
    UsbSelectResponse usb_select_resp = 9;
    PttKeyRequest ptt_key_req = 10;
    PttUnkeyRequest ptt_unkey_req = 11;
    PttAck ptt_ack = 12;
    AudioDeviceListRequest audio_list_req = 13;
    AudioDeviceListResponse audio_list_resp = 14;
    AudioRouteChanged audio_route_changed = 15;
    Error error = 16;
  }
}

message Hello {
  uint32 schema_version = 1;
  string client_version = 2;  // graywolf semver, "v0.13.2-..."
  string server_version = 3;  // populated in the response only
}

message GpsFix {
  double lat = 1;
  double lon = 2;
  double alt_m = 3;
  double speed_mps = 4;
  double course_deg = 5;
  int64 time_unix_ms = 6;
  double hdop = 7;
  uint32 num_sats = 8;
  GpsSource source = 9;
}

enum GpsSource {
  GPS_SOURCE_UNKNOWN = 0;
  GPS_SOURCE_ANDROID_FUSED = 1;
  GPS_SOURCE_ANDROID_GPS = 2;
  GPS_SOURCE_ANDROID_NETWORK = 3;
}

// ... remaining messages, see design doc §4.2
```

The full message set comes from design doc §4.2; this spec just
locks the envelope shape and the schema-versioning approach.

Wire framing matches existing `proto/graywolf.proto`:
`[4 BE bytes length][serialized PlatformMessage]`.

### 4.2 Generated Go bindings (`pkg/platformproto/`)

`make proto` runs `protoc --go_out=...` and writes
`pkg/platformproto/platform.pb.go`. Committed to git per invariant
11. CI drift guard fails if the committed file diverges from
`make proto`'s output.

### 4.3 Go client (`pkg/platformsvc/`)

New package. Files:

| File | Purpose |
|---|---|
| `client.go` | `Client` interface, public API |
| `client_impl.go` | UDS connect + framing + length-prefix codec + reconnect with backoff |
| `subscriptions.go` | `SubscribeGpsFix`, `SubscribeAudioRouteChanged` — fan-out from a single read goroutine to typed channels |
| `client_test.go` | round-trip every proto message type through `net.Pipe` loopback |
| `framing_test.go` | length-prefix encode/decode edge cases (empty payload, oversized payload, truncated read) |
| `reconnect_test.go` | backoff curve, max attempts, EOF mid-read |
| `hello_mismatch_test.go` | schema-version mismatch returns typed error |

Build-tagged `//go:build android` per design §4.3 (the package only
exists on Android; desktop graywolf doesn't need it).

### 4.4 Kotlin server
(`android/app/src/main/kotlin/com/nw5w/graywolf/platformsvc/`)

New files. Phase 2 ships only the skeleton + Hello/GpsFix handlers:

| File | Purpose |
|---|---|
| `PlatformServer.kt` | UDS listener, accept loop, framing, dispatch |
| `MessageHandler.kt` | sealed-class hierarchy of per-message-type handlers; phase 2 implements only `HelloHandler` and `GpsFixHandler` |
| `WireCodec.kt` | length-prefix framing on the Kotlin side |
| `PlatformServerTest.kt` | unit test exercising Hello + GpsFix with a fake Go client |
| `WireCodecTest.kt` | framing round-trip tests |

`GraywolfService.kt` extended to bring `PlatformServer` up at
startup, after `loadLibrary` and before exec'ing the Go child. The
Go child's existing modembridge UDS path is unchanged; the
platform UDS is a *second* socket alongside it.

### 4.5 Build pipeline

The big lift. Two sub-deliverables:

#### 4.5.1 Gradle `externalNativeBuild` task

`android/app/build.gradle.kts` gains a task that runs at
`preBuild` time and produces `.so` files for all three ABIs into
`android/app/src/main/jniLibs/<abi>/`.

For Rust: shells out to `cargo ndk -t <abi> -P 26 build --lib
--release`. Output `lib<crate>.so` lands under the right ABI
directory.

For Go: shells out to `GOOS=android GOARCH=<arch>
CGO_ENABLED=0 GOWORK=off go build -o
android/app/src/main/jniLibs/<abi>/libgraywolf.so
./cmd/graywolf-pocb` (with future support for `./cmd/graywolf`
once phase 3 lands `main_android.go`).

`GOWORK=off` is set by the Gradle task itself, not by an env-var
the operator has to remember.

`armeabi-v7a` / `x86_64` builds for both Rust and Go must succeed
even if the binaries are never run — multi-ABI APK is required for
Play Store anyway.

#### 4.5.2 Toolchain check script

`scripts/check-android-toolchain.sh` verifies the prerequisites
listed in criterion #10. Gradle invokes it as part of preBuild;
exits non-zero with a clear message if anything is missing.

### 4.6 `.gitignore` changes

```
android/app/src/main/jniLibs/
```

Plus removal of the currently-committed `.so` files. The git
history retains them for archaeology; `git rm` cleans the working
tree state.

### 4.7 Scripts retirement

`scratch/poc-b/build-and-install.sh` retired. Replaced by
`./gradlew assembleDebug` which now does everything the script
did, plus more, plus without manual env-var dancing.

---

## 5. Run procedure

1. Branch off main. `make proto` generates the `pkg/platformproto/`
   bindings.
2. Implement Track A (proto + Go client + Kotlin server) with unit
   tests landing first.
3. Implement Track B (Gradle `externalNativeBuild`, ABI multiplier,
   toolchain check) — parallelizable with Track A.
4. Run the two tracks' unit-test suites: `go test ./pkg/platformsvc/...
   ./pkg/platformproto/...` + `./gradlew test`. Both green.
5. Run drift guard: `make proto && git diff --exit-code
   pkg/platformproto/`. Clean.
6. `./gradlew assembleDebug`. APK builds. `unzip -l
   app-debug.apk | grep '\.so$'` shows entries for all three ABIs.
7. `adb install -r` on the Topicon T865.
8. Tap icon. Confirm via `adb logcat -s GraywolfService:*
   PlatformServer:*` that the new platform UDS bound, the Go
   client connected, the Hello handshake completed, and the
   schema version logged.
9. Re-run POC-D's PTT and TX-test exercises against the new APK.
   All four PTT transports key the radio. Combined TX-test demo
   decodes on the reference station. **No regressions vs. POC-D
   green verdict.**
10. Capture logcat → `scratch/phase-2/run.log`.

---

## 6. Deliverables

| Artifact | Location |
|---|---|
| Proto contract | `proto/platform.proto` |
| Generated Go bindings | `pkg/platformproto/platform.pb.go` |
| Go client | `pkg/platformsvc/*.go` |
| Kotlin server | `android/app/src/main/kotlin/com/nw5w/graywolf/platformsvc/*.kt` |
| Service wiring | `android/app/src/main/kotlin/com/nw5w/graywolf/GraywolfService.kt` (extended) |
| Build task | `android/app/build.gradle.kts` (extended) |
| Toolchain check | `scripts/check-android-toolchain.sh` |
| `.gitignore` | extended; committed `.so` files removed |
| Run report | `.context/2026-05-XX-android-phase-2-results.md` |

Run report captures:

- Toolchain versions actually used.
- All 14 definition-of-done criteria pass/fail.
- APK size before vs. after (multi-ABI APK is bigger; record the
  delta so phase 6's Play Store split-APK decision has a baseline).
- Any drift between the design doc's §4.2 message set and what
  ended up in `proto/platform.proto`.
- Phase 3 prerequisite status: are the build-tagged stubs in
  `pkg/gps/android.go`, `pkg/pttdevice/android.go`,
  `cmd/graywolf/main_android.go` in place such that phase 3 can
  swap in real implementations without restructuring?

---

## 7. Plan structure hint for `/superpowers:writing-plans`

Bigger than POC-A/C/D, smaller than POC-B. Two tracks, each
parallelizable internally; Track A and Track B are mostly
independent and can run in parallel.

**Track A — proto / platformsvc / PlatformServer (sequential within track):**

1. `proto/platform.proto` design + write. `make proto` regenerates
   Go bindings.
2. `pkg/platformsvc/` Go client with `net.Pipe` round-trip unit
   tests covering every proto message type. Reconnect + Hello
   mismatch tests.
3. Drift-guard CI test for `pkg/platformproto/`.
4. Kotlin `WireCodec` + `PlatformServer` skeleton. Hello +
   GpsFix handlers only. Kotlin unit tests.
5. `GraywolfService.kt` extension to bring up `PlatformServer`
   alongside the existing modem path.
6. Real-device handshake smoke (criterion #14 partial).

**Track B — debt close-out (sequential within track):**

1. Gradle `externalNativeBuild` task wrapping `cargo-ndk` for
   Rust. Single-ABI (arm64) first, prove it works.
2. Add the Go cross-compile to the same task. `GOWORK=off`
   automated.
3. Add `armeabi-v7a` and `x86_64` to the ABI matrix. Both build
   clean even if not run.
4. `scripts/check-android-toolchain.sh`. Wire into Gradle
   preBuild.
5. Remove committed `.so` files. Add `jniLibs/` to `.gitignore`.
6. Retire `scratch/poc-b/build-and-install.sh`. `./gradlew
   assembleDebug` is now the canonical build command.
7. Multi-ABI APK smoke (criterion #14 final).

**Integration:**

8. POC-D regression: install the new multi-ABI APK on the T865,
   verify all four PTT transports + TX-test still work.
9. Run report.

Tasks A1-A6 sequential. Tasks B1-B7 sequential. A and B are
mostly parallelizable (B7 needs A1's `proto/` to be in tree so
the `make proto` invocation succeeds, but everything else
parallelizes). Task 8 depends on both A and B. Task 9 depends on
8.

**Traps the planner should call out:**

- **`make proto` for Kotlin bindings.** Phase 2 must not commit
  Kotlin proto bindings — the `com.google.protobuf` Gradle plugin
  generates them at `assembleDebug` time. If the planner adds a
  committed-Kotlin-bindings step, fail it.
- **JNI symbol vs. proto envelope.** The existing JNI surface
  (`modemStart`, `modemPushSamples`, etc.) does not move to proto
  in phase 2. JNI stays JNI; the proto envelope is only for the
  Go ↔ Kotlin path.
- **`PlatformServer` accept loop and Service lifecycle.** If the
  Service is killed and restarted by the OS, the UDS path must be
  cleaned up before re-bind (POC-B's
  `feedback_uds_unlink_before_bind` lesson applies here too).
- **Schema version on the wire.** The Hello handshake's schema
  version is **not** the same as graywolf's release version. Use
  a separate uint32 so a graywolf release that doesn't change the
  schema doesn't force a Hello renegotiation.
- **Build-tagged stubs.** `pkg/platformsvc/` has
  `//go:build android` per design §4.3. Desktop builds must not
  pull in the package. Verify with `cd graywolf && go build
  ./...` on a non-Android host.
- **Multi-ABI cargo-ndk invocations.** `cargo ndk` accepts
  multiple `-t` flags in one invocation but writes outputs into
  per-ABI subdirectories. The Gradle task must handle the output
  layout correctly; don't re-run cargo-ndk per ABI sequentially
  (slow + cache-busting).
- **Go cross-compile per ABI.** `GOOS=android GOARCH=arm`,
  `GOARCH=arm64`, `GOARCH=amd64` — all three needed. The
  resulting binaries get renamed `libgraywolf.so` per N1; don't
  forget to rename for each ABI.
- **`-P 26` matters.** POC-B/C/D all built against API 26
  (cargo-ndk's `-P` flag). Lock this in the Gradle task; the
  `aaudio` link target requires API ≥ 26.
- **`GOWORK=off` automation.** Lift directly from POC-D's
  `feedback_goembed_goworkoff_in_worktrees` memory. Set in the
  Gradle Exec task's `environment {}` block.

---

## 8. Stop conditions

Surface to the user, do not press through, if any of:

- The `com.google.protobuf` Gradle plugin can't generate Kotlin
  bindings for the proto envelope shape (some plugins don't
  support `oneof` well — verify early).
- `cargo ndk` fails to cross-compile for `armeabi-v7a` because a
  Rust dep doesn't have an armv7 target. Surface; may need to
  drop armv7 support or pin a different version of the offending
  dep.
- Multi-ABI APK exceeds Play Store's 200 MB upload limit. POC-D's
  arm64-only APK was small (~12 MB); 3× shouldn't blow the limit,
  but if it does, phase 6's split-APK strategy needs to come
  forward.
- Real-device handshake fails — Hello message doesn't round-trip,
  or framing mismatches between Go and Kotlin sides. Indicates a
  framing-or-codec bug; do not paper over with retries.

---

## 9. What phase 2 unlocks

Phase 2 produces no operator-visible feature, but it makes the
following phases possible:

| Phase | Phase 2 dep |
|---|---|
| 3 | `pkg/platformsvc/` Go client must compile and connect; real `cmd/graywolf` build-tagged Android entry needs the `platformsvc` package as a dep. Build pipeline must produce all three ABIs cleanly. |
| 4 | `GpsFix` proto + `PlatformServer.GpsFixHandler` already in place; phase 4 just adds the Kotlin `GpsAdapter` that *produces* `GpsFix` messages and the Go `pkg/gps/android.go` that *consumes* them. |
| 5 | `Ptt*` + `Usb*` protos already in place; phase 5 wires Kotlin `UsbAdapter` to `PlatformServer` and Rust modem PTT drivers to the Go relay. **Phase 5 also reuses POC-D's `UsbPttAdapter` Kotlin code, just rewires its trigger surface from `@JavascriptInterface` to the proto path.** |
| 6 | Multi-ABI build pipeline already in place; phase 6 wraps it in the GitHub Actions release workflow + signing keys. |

Without phase 2, phases 3-6 would each have to invent their own
slice of this plumbing, and the slices would diverge.
