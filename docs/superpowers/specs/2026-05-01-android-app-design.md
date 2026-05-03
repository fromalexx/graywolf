# Graywolf for Android - design spec

**Status:** draft, awaiting user review
**Date:** 2026-05-01
**Scope:** native Android app providing full standalone TNC + iGate functionality on a phone, sourced from the existing graywolf codebase with no fork.

---

## 1. Goal

Ship a Play Store Android app that runs the graywolf Go service and the
graywolf-modem Rust binary on a phone, fronted by the existing web UI in a
WebView. The app is a full TNC and iGate: it captures audio from a USB or
3.5mm-jack sound device, drives PTT through Android-supported methods,
runs all existing graywolf subsystems (AX.25, KISS server/client, AGW,
digipeater, iGate, beacons, livemap, offline maps, history, log buffer,
flare diagnostics), and feeds GPS from `LocationManager`.

Source of truth stays in the `graywolf` repo. The Android app is a new
build target, not a fork. New core graywolf features ride along
automatically; build tags gate code only where Android cannot host it.

## 2. Non-goals (v1)

- Acoustic-coupled audio (phone mic against radio speaker). Wired only.
- GPIO PTT, parallel port PTT, native serial PTT (`/dev/ttyUSB*`),
  hamlib-rigctl over local subprocess. Hamlib-rigctl over TCP retained
  because it is portable.
- iOS port. Different OS, different sandbox, different toolchain.
- F-Droid. Distribution is Play Store only (paid). Self-builders may
  still build their own APK from source under GPLv2; that is accepted.
- In-app updates checker (`pkg/updatescheck`). Play Store handles
  updates.
- Replacing the existing web UI with native Compose screens. WebView
  fronts the SPA.

## 3. Architecture

### 3.1 Process tree on Android

```
Android System
  +- GraywolfService (Kotlin foreground service)
  |    +- exec: libgraywolf.so          (Go, was bin/graywolf)
  |    +- exec: libgraywolfmodem.so     (Rust, was bin/graywolf-modem)
  |    +- hosts: platform-services UDS server (Kotlin)
  +- MainActivity
       +- WebView -> http://127.0.0.1:8080  (Go REST + embedded SPA)
```

Android requires that any binary intended for `Runtime.exec()` ship as
`lib*.so` inside the APK and be extracted to `nativeLibraryDir`. The
manifest declares `android:extractNativeLibs="true"` and
`useLegacyPackaging=true` so the OS extracts the binaries to disk
instead of memory-mapping them out of the APK zip.

### 3.2 IPC channels

Three channels under the Service. All use length-prefixed protobuf,
matching the existing graywolf wire format `[4 BE bytes length][message]`.

1. **Go <-> Rust modem** - existing `proto/graywolf.proto` UDS at
   `${app.cacheDir}/graywolf-modem.sock`. Extended with new PTT-event
   messages and three new `ConfigurePtt.method` enum values
   (`ANDROID_USB_SERIAL`, `ANDROID_USB_HID`, `ANDROID_BT_SPP`). All
   existing message types unchanged.

   The supported Android PTT method set is: existing `NONE` (operator
   relies on the radio's VOX from transmitted audio), plus the three
   new enum values above. Other existing PTT methods (GPIO, native
   serial, hamlib subprocess, CM108 via direct hidraw write, etc.)
   compile out of the Android binary via `cfg(target_os="android")`.
   Hamlib over TCP (`HAMLIB_RIGCTL_TCP`) is portable and remains
   available.

2. **Go <-> Kotlin platform services** - new `proto/platform.proto` UDS
   at `${app.cacheDir}/platform.sock`. Bidirectional. Carries:
   - `GpsFix` (Kotlin -> Go) - location updates
   - `UsbAttach` / `UsbDetach` (Kotlin -> Go) - hot-plug events
   - `UsbDeviceList` request/response (Go -> Kotlin) - settings UI
     enumerator
   - `UsbSelect` request/response (Go -> Kotlin) - user picks device,
     Kotlin opens the fd and retains it
   - `PttKeyRequest` / `PttUnkeyRequest` / `PttAck` (Go -> Kotlin) -
     relayed from Rust modem PTT events
   - `AudioDeviceList` request/response (Go -> Kotlin) - settings UI
     enumerator (CPAL still owns the actual capture)
   - `BatteryState` (Kotlin -> Go) - charging/level for diagnostics

3. **WebView <-> Go** - existing HTTP, unchanged. The SPA is the same
   one served by desktop graywolf, embedded via `go:embed all:dist` per
   invariant 12.

A small `@JavascriptInterface` exposes Kotlin to the WebView for
system-required user prompts only (USB device picker confirmation,
runtime permission requests). Functional API surface stays in Go REST.

### 3.3 PTT relay rationale

Android does not expose a kernel CDC/FTDI/CP2102 driver. USB-serial
control happens entirely in userspace via `usb-serial-for-android`,
which requires a Java/Kotlin owner of the `UsbDeviceConnection`. CM108
USB HID and Bluetooth SPP serial have the same constraint - the fd is
held by Kotlin.

Therefore Rust drivers for these PTT methods do not touch hardware.
They emit `KeyPttRequest` / `UnkeyPttRequest` proto messages on the
existing Go<->Rust UDS, Go relays the message to Kotlin via
platform-services UDS, Kotlin actually toggles the line. The path is
microsecond-scale UDS hops; total budget under 5ms typical.

The TX ordering invariant is unchanged: PTT keyed before audio starts,
unkeyed after audio drains. txgovernor (invariant 16) remains the
single source of truth for the TX path.

### 3.4 Modem readiness signal (invariant 13)

Preserved unchanged. The Service execs the modem binary first, captures
its stdout, waits for the `\n` byte, then connects to the modem UDS.
Same code path as Linux desktop. The Service captures the modem's
stderr into the Android log buffer for support flows.

### 3.5 Version display string (invariant 14)

Preserved unchanged. Both binaries continue to produce
`v<Version>-<GitCommit>` and modembridge checks them at startup. The
APK's `versionName` matches the same `VERSION` file. CI rejects builds
where APK version differs from binary versions.

## 4. New code

### 4.1 Kotlin (`android/app/`)

| File | Purpose |
|---|---|
| `MainActivity.kt` | owns the WebView, lifecycle, requests POST_NOTIFICATIONS perm on launch |
| `GraywolfService.kt` | foreground service, owns child processes, the platform-services socket, and the supervisor restart loop |
| `binaries/BinaryLauncher.kt` | exec helpers: spawn `lib*.so` from `applicationInfo.nativeLibraryDir`, capture stdout/stderr, wait for readiness byte, supervise |
| `platformsvc/PlatformServer.kt` | UDS listener, framing, dispatch to adapters |
| `platformsvc/GpsAdapter.kt` | `LocationManager` listener -> `GpsFix` proto |
| `platformsvc/UsbAdapter.kt` | `UsbManager` enumerator + `usb-serial-for-android` driver + CM108 HID write + `BluetoothSocket` RFCOMM. Owns retained device connections. |
| `platformsvc/AudioAdapter.kt` | `AudioManager.getDevices()` enumerator. CPAL still does capture. |
| `platformsvc/BatteryAdapter.kt` | `BATTERY_CHANGED` broadcast -> `BatteryState` proto |
| `webview/WebAppInterface.kt` | minimal JavascriptInterface: USB picker confirmation, runtime perm prompt |
| `webview/PermissionBridge.kt` | wraps Android runtime permission flow |

Kotlin sources only. No Java. Min SDK 28, target SDK 34.

### 4.2 Proto (`proto/platform.proto`)

New file. Mirrors the envelope pattern from `graywolf.proto` (single
top-level `PlatformMessage` with `oneof body`). Generates Go bindings
into `pkg/platformproto/` (committed, regenerated by `make proto`,
covered by invariant 11's drift guard). Generates Kotlin bindings via
`com.google.protobuf` Gradle plugin during `assembleRelease`.

Message types:
- `Hello` (handshake, schema version)
- `GpsFix`
- `BatteryState`
- `UsbAttach`, `UsbDetach`
- `UsbDeviceListRequest`, `UsbDeviceListResponse`
- `UsbSelectRequest`, `UsbSelectResponse`
- `PttKeyRequest`, `PttUnkeyRequest`, `PttAck`
- `AudioDeviceListRequest`, `AudioDeviceListResponse`
- `Error` (generic, with code + message)

Schema versioning: `Hello.schema_version` (uint32). Mismatch -> Service
logs warning, terminates Go + modem, restarts. Same retag tolerance as
graywolf.proto.

### 4.3 Go packages

| Path | Build tag | Purpose |
|---|---|---|
| `pkg/platformsvc/` | `android` | UDS client, proto codec, retry + reconnect, exposed as a typed Go API consumed by `pkg/gps`, `pkg/pttdevice`, `pkg/modembridge` |
| `pkg/platformproto/` | none | generated proto bindings (committed) |
| `pkg/gps/android.go` | `android` | consumes `GpsFix` from platformsvc, feeds existing Fix pipeline |
| `pkg/gps/gpsd_unix.go`, `pkg/gps/serial_unix.go` | `!android` | existing gpsd + serial NMEA, hidden on Android |
| `pkg/pttdevice/android.go` | `android` | enumerator that reports the four Android PTT methods and queries Kotlin for live USB/BT device list |
| `pkg/pttdevice/linux.go`, `pkg/pttdevice/windows.go`, `pkg/pttdevice/darwin.go` | `!android` | existing enumerators, hidden on Android |
| `pkg/updatescheck/*.go` | `!android` | existing GitHub poll, hidden on Android |
| `cmd/graywolf/main_android.go` | `android` | entry that reads env-injected paths, no `signal.Notify`, no `os.Exit` on clean shutdown (Android Service kills the process) |
| `cmd/graywolf/main_unix.go`, `main_windows.go` | `!android` | existing entries, build-tagged |
| `pkg/diagcollect/review/` | `!android` | existing bubbletea TUI, hidden on Android |
| `pkg/diagcollect/review_android.go` | `android` | new HTTP/WebView review surface; uses the same scrubbed payload, exposes it through a `/api/flare/review` endpoint |

### 4.4 Rust modem changes (`graywolf-modem/`)

| File | Purpose |
|---|---|
| `src/tx/ptt_android_usb_serial.rs` | thin driver: emits `KeyPttRequest` / `UnkeyPttRequest` over IPC, no hardware access |
| `src/tx/ptt_android_usb_hid.rs` | same pattern |
| `src/tx/ptt_android_bt_spp.rs` | same pattern |
| `src/tx/mod.rs` | dispatches new method enum values |
| `Cargo.toml` | enables CPAL Oboe backend feature; adds `android_logger` for stderr -> Android log capture |

`build.rs` regenerates from the same `proto/graywolf.proto` (invariant 2
preserved). Cross-compile via `cargo-ndk -t arm64-v8a -t x86_64`.

Existing PTT drivers (`ptt_serial.rs`, `ptt_cm108.rs`, etc.) compile out
on `cfg(target_os="android")` so the binary stays small.

### 4.5 Build system

`Makefile` adds:

```makefile
android-modem:
	cargo ndk -t arm64-v8a -t x86_64 -o $(ANDROID_JNI_LIBS) \
		build --release --manifest-path graywolf-modem/Cargo.toml
	# rename target into libgraywolfmodem.so under arch dirs

android-graywolf:
	GOOS=android GOARCH=arm64 CGO_ENABLED=0 \
		go build -tags android -o $(ANDROID_JNI_LIBS)/arm64-v8a/libgraywolf.so ./cmd/graywolf
	GOOS=android GOARCH=amd64 CGO_ENABLED=0 \
		go build -tags android -o $(ANDROID_JNI_LIBS)/x86_64/libgraywolf.so ./cmd/graywolf

android: web proto android-modem android-graywolf
	cd android && ./gradlew bundleRelease assembleRelease
```

`make bump-*` extended to rewrite `android/app/build.gradle.kts`:
- `versionName` = VERSION
- `versionCode` = `major * 1000000 + minor * 1000 + patch` (e.g. `0.12.6` -> `12006`, `1.0.0` -> `1000000`). Capped at Play Store's 2^31 limit. Each release is strictly monotonic.

Invariant 3 grows by one file. The bump targets already commit
generated artifacts; the gradle file goes into the same commit.

### 4.6 CI

New workflow `.github/workflows/release-android.yml`:

- Trigger: tag push `v*`, after the existing Rust build matrix completes.
- Stages:
  1. Set up NDK r27, Java 17, Gradle.
  2. `make android-modem android-graywolf` for both arches.
  3. `cd android && ./gradlew bundleRelease assembleRelease` with the
     upload key from `secrets.ANDROID_UPLOAD_KEYSTORE_BASE64`.
  4. Upload `.aab` to Play Store internal testing track via
     `r0adkll/upload-google-play-action`.
- Outputs: `app-release.aab` (Play), `app-release.apk` (signed sideload
  artifact, attached to the GitHub release).

New workflow `.github/workflows/android-build-smoke.yml`:

- Trigger: PR touching `android/**`, `cmd/graywolf/**`,
  `graywolf-modem/**`, `proto/**`, `pkg/platformsvc/**`,
  `pkg/platformproto/**`, `pkg/gps/**`, `pkg/pttdevice/**`,
  `cmd/graywolf/main_android.go`.
- Stages:
  1. NDK + Java + Gradle setup.
  2. `make android-modem android-graywolf` (build only).
  3. `cd android && ./gradlew assembleDebug` (build only).
  4. Run Go unit tests with `-tags android` for the new packages.
- No emulator. Fast (target under 10 min).

Optional nightly `android-instrumented.yml` (separate, not gating)
boots an emulator and runs androidTest assertions.

### 4.7 Docs

| Path | Change |
|---|---|
| `docs/handbook/android.html` | new operator setup page: install from Play, OTG cable, Digirig/AIOC pairing, perm prompts, troubleshooting |
| `docs/handbook/installation.html` | new "Android" section linking to the above |
| `docs/handbook/ptt.html` | new section listing the four Android PTT methods |
| `docs/handbook/audio.html` | new section listing Android audio paths |
| `docs/wiki/system-topology.md` | new "Android" section with the three-socket diagram, lifecycle notes, paths |
| `docs/wiki/build-pipelines.md` | new row for Android target, new row for the smoke workflow |
| `docs/wiki/code-map.md` | new column or sub-table for Android-specific files |
| `docs/wiki/invariants.md` | new entries (see section 7 below) |

## 5. Data flow

### 5.1 Cold start

1. User taps app icon. `MainActivity.onCreate`.
2. `MainActivity` requests POST_NOTIFICATIONS perm (API 33+).
3. `MainActivity` starts `GraywolfService` via `startForegroundService`.
4. Service immediately calls `startForeground(notification, types)` with
   `connectedDevice | microphone | location` types.
5. Service binds the platform-services UDS at
   `${cacheDir}/platform.sock`.
6. Service execs `libgraywolfmodem.so` from
   `applicationInfo.nativeLibraryDir`, captures stdout, blocks until
   readiness byte `\n` arrives or 10s timeout.
7. Service execs `libgraywolf.so` with environment:
   - `GRAYWOLF_DB=${filesDir}/graywolf.db`
   - `GRAYWOLF_HISTORY_DB=${filesDir}/graywolf-history.db`
   - `GRAYWOLF_LOG_DB=${filesDir}/graywolf-logs.db`
   - `GRAYWOLF_TILE_CACHE=${filesDir}/tiles`
   - `GRAYWOLF_MODEM_SOCKET=${cacheDir}/graywolf-modem.sock`
   - `GRAYWOLF_PLATFORM_SOCKET=${cacheDir}/platform.sock`
   - `GRAYWOLF_LISTEN=127.0.0.1:8080`
   - `GRAYWOLF_PLATFORM=android`
8. Go binary connects to modem UDS (existing modembridge code,
   unchanged). Connects to platform-services UDS (new pkg/platformsvc).
9. Go binary writes a single newline `\n` to stdout once it has bound
   the HTTP listener and connected both UDS clients (modem + platform
   services). The Service blocks until that byte arrives or 10s
   timeout. This mirrors the existing modem readiness pattern
   (invariant 13).
10. WebView loads `http://127.0.0.1:8080/`. Existing SPA renders.

### 5.2 TX with USB-serial PTT (Digirig path)

1. Web UI clicks "Send beacon". POSTs to Go REST.
2. Go txgovernor admits the frame, hands to modembridge.
3. Rust modem prepares the audio frame, then emits
   `KeyPttRequest{method=ANDROID_USB_SERIAL}` on the Go<->Rust UDS.
4. Go modembridge receives the message, forwards to platform-services
   UDS as the same proto type re-wrapped in `PlatformMessage`.
5. Kotlin `UsbAdapter` looks up the retained `UsbSerialPort` for the
   selected device, calls `setRTS(true)`.
6. Kotlin sends `PttAck{success=true}` back. Go relays the ack to the
   modem on the Go<->Rust UDS.
7. Modem starts audio playback via CPAL.
8. Audio drains. Modem emits `UnkeyPttRequest`. Symmetric path.
9. Kotlin clears RTS. ACKs.

### 5.3 RX

1. CPAL Oboe backend captures from the Android default audio device
   (whichever the OS has routed - USB DAC, 3.5mm jack, or USB-OTG sound
   card).
2. Modem demodulates, emits `RxFrame` on Go<->Rust UDS (existing
   message type).
3. Go ingress fanout marks `Source: Modem`, distributes to APRS parser,
   KISS server fan-out, AGW server fan-out, igate, etc. (invariant 17
   preserved.)

### 5.4 GPS

1. `GpsAdapter` registers a `LocationListener` on RECORD-AUDIO grant
   (the listener itself needs ACCESS_FINE_LOCATION; requested at first
   need).
2. Each fix -> `GpsFix{lat, lon, alt, speed_mps, course_deg, time_unix,
   hdop, num_sats, source}` proto.
3. Kotlin sends on platform-services UDS.
4. Go `pkg/gps/android.go` reads from `pkg/platformsvc`, converts to
   the existing `gps.Fix` struct, hands to existing consumers (beacons,
   livemap, position-history DB).

### 5.5 USB device picker

1. Web UI Settings -> "PTT" tab -> "Choose device".
2. Frontend GETs `/api/ptt/devices?method=ANDROID_USB_SERIAL`.
3. Go REST handler asks `pkg/pttdevice` for the device list.
4. `pkg/pttdevice/android.go` sends `UsbDeviceListRequest{class=serial}`
   on platform-services UDS.
5. Kotlin `UsbAdapter` enumerates `UsbManager.deviceList`, filters by
   class, returns `UsbDeviceListResponse` with `[{vendor_id, product_id,
   device_name, has_permission}]`.
6. Go returns to frontend. UI renders list.
7. User clicks one. Frontend POSTs `/api/ptt/select`.
8. Go REST -> `pkg/pttdevice` -> `UsbSelectRequest` to Kotlin.
9. Kotlin calls `UsbManager.requestPermission()`. System dialog appears
   over the WebView. User taps "OK".
10. Kotlin opens `UsbDeviceConnection`, wraps in `UsbSerialPort`,
    retains in adapter map, returns `UsbSelectResponse{ok=true,
    handle_id="..."}`.
11. Subsequent PTT requests reference this device.

### 5.6 Suspend, screen-off, app-switch

- Foreground service prevents the OS from killing the process while
  graywolf is "running" per the user's notification.
- Wi-Fi sleep policy bumped to `WIFI_MODE_FULL_HIGH_PERF` for the
  service lifetime to keep iGate sessions alive on screen-off.
- App requests battery-optimization whitelist via the standard intent
  on first launch. If the user denies, log a warning and proceed; some
  data may sleep when the screen is off.
- Notification has a "Stop" action. Tapping it broadcasts to the
  Service, which sends SIGTERM to both children, waits 5s, sends
  SIGKILL on remainder, calls `stopSelf()`.

## 6. Error handling

| Failure | Detection | Response |
|---|---|---|
| Modem child crash | Service `Process.waitFor()` returns non-zero | log to `graywolf-logs.db` via Go relay; Service restarts modem with backoff (1s, 2s, 5s, 10s, capped); 3 crashes in 60s -> halt restarts, surface via WebView WS event channel |
| Go binary crash | same | Service kills modem too, restarts both, same backoff policy |
| Platform UDS disconnect (Kotlin server died, Go client got EOF) | `pkg/platformsvc` read error | retry with backoff; modem PTT method marked offline by `pkg/pttdevice` so txgovernor refuses TX; GPS source goes idle, beacons fall back to manual fix |
| USB device unplugged mid-QSO | `ACTION_USB_DEVICE_DETACHED` broadcast in `UsbAdapter` | send `UsbDetach{handle_id}` event; `pkg/pttdevice` marks the active method offline; in-flight TX gets `PttAck{success=false, error="device gone"}`; txgovernor logs and aborts the frame |
| RECORD_AUDIO denied | runtime perm flow returns DENY | modem reports "no audio device" via existing config error path; UI shows recoverable error with "grant permission" CTA |
| ACCESS_FINE_LOCATION denied | runtime perm flow returns DENY | GPS source unavailable; manual position-config UI still functional |
| POST_NOTIFICATIONS denied (API 33+) | runtime perm flow returns DENY | Service still runs (foreground services do not require this permission to run, only to show their notification). UI shows a degraded-experience banner |
| Schema-version mismatch on Hello | `pkg/platformsvc` rejects | Service logs error, terminates child binaries, restarts (likely a half-installed APK upgrade scenario) |
| `cargo-ndk` toolchain missing on dev machine | `make android` early failure | clear error message in Makefile target with link to setup docs |

The supervisor logic in `GraywolfService` mirrors the existing systemd
restart semantics for the desktop service, just expressed in Kotlin.

## 7. New invariants (added to `docs/wiki/invariants.md`)

These join the existing list. They are not yet in the wiki; this spec
specifies the additions for the implementation plan to write.

- **N1.** On Android, both binaries ship as `lib*.so` files in
  `nativeLibraryDir`. Manifest must keep `useLegacyPackaging=true` and
  `extractNativeLibs="true"` or the binaries cannot be exec'd. *Why:*
  Android's restriction on executing arbitrary files only allows
  `lib*.so` extracted to disk.
- **N2.** Three sockets under the Service. Renaming or relocating any
  of `graywolf-modem.sock`, `platform.sock`, or moving the HTTP listen
  off `127.0.0.1:8080` requires updates in Kotlin (Service env injection
  + WebView URL), Go (env parsing + listener), and the manifest
  (`usesCleartextTraffic=true` for localhost). *Why:* Three loosely
  coupled processes assume those identities at startup.
- **N3.** USB-class hardware (serial, HID, CM108, BT SPP) is owned by
  Kotlin on Android. Rust modem PTT drivers for these methods do not
  touch hardware - they emit proto events only. Any new Android PTT
  method follows this split. *Why:* Android does not expose
  `/dev/ttyUSB*` or HID-write paths to userspace; only Kotlin can hold
  the `UsbDeviceConnection`.
- **N4.** `pkg/platformproto/` is the single Go<->Kotlin contract.
  Schema bump + Hello version bump are coordinated. CI drift guard
  parallels invariant 11.
- **N5.** APK `versionName` matches `VERSION`; `versionCode` is derived
  numerically. Both rewritten by `make bump-*`. *Why:* Invariant 14's
  version-equality check between Go and Rust extends to the APK so
  Play-installed users cannot run mismatched halves.

## 8. Testing strategy

### 8.1 Cross-compile smoke (per-PR, gating)

`android-build-smoke.yml`:
- `make android-modem android-graywolf` - both arches build clean.
- `cd android && ./gradlew assembleDebug` - Kotlin compiles, manifest
  validates, jniLibs path right.
- `go test -tags android ./pkg/platformsvc/... ./pkg/platformproto/...
  ./pkg/gps/... ./pkg/pttdevice/...` - in-process unit tests with a
  fake Kotlin server.

Goal: no Android regressions slip into main from non-Android PRs.

### 8.2 Go unit tests

`pkg/platformsvc/`: round-trip every proto message type through a
loopback fake server. Reconnect logic. Hello handshake. Schema mismatch
abort.

`pkg/gps/android_test.go`: feed a synthetic `GpsFix` stream from a
fake platformsvc, assert the Fix pipeline sees expected outputs.

`pkg/pttdevice/android_test.go`: enumerate against a fake
`UsbDeviceListResponse`, assert filtered method/device pairings.

### 8.3 Kotlin unit tests (`android/app/src/test/`)

`PlatformServerTest`: framing, dispatch.
`BinaryLauncherTest`: readiness-byte wait, restart backoff (with mock
`Process`).
`UsbAdapterTest`: filter logic (hardware mocked).

### 8.4 Instrumented tests (`android/app/src/androidTest/`)

`BootSmokeTest`: launches the Service in an emulator, waits for the Go
binary to bind `:8080`, GETs `/api/version`, asserts the
`Go-Rust-APK` version triple matches.

`UdsRoundTripTest`: launches Service, sends a synthetic `GpsFix` from
test code, asserts it surfaces in the Go test endpoint
`/api/_internal/last-gps-fix` (test-only, build-tagged
`android,testintegration`).

Run nightly + on `android/**` changes. Not gating PR merge.

### 8.5 Hardware matrix (manual, pre-release)

Tracked in beta release notes:
- Pixel 8 + Digirig + Baofeng UV-5R
- Pixel 6a + AIOC + Yaesu FT-65R
- Samsung A54 + USB-C-to-3.5mm dongle + radio with VOX
- (Bluetooth) any phone + BT SPP serial adapter + radio

Each release blocks on a green pass for at least two of these. No
physical CI lab in v1.

## 9. Distribution and signing

- Single distribution channel: Google Play Store (paid).
- Signing: Play App Signing. Google holds the app signing key. The
  upload key is held in `secrets.ANDROID_UPLOAD_KEYSTORE_BASE64` plus
  `secrets.ANDROID_UPLOAD_KEY_PASSWORD` in GitHub Actions.
- F-Droid not pursued. Self-builders may build their own APK from
  source under GPLv2; that is acceptable. The unsigned APK is not
  hosted as an official artifact.
- Upgrade path: Play Store handles updates. The in-app
  `pkg/updatescheck` is build-tagged off on Android.

## 10. Permissions list

Manifest declarations:

| Permission | Why | Runtime prompt? |
|---|---|---|
| `INTERNET` | iGate, online tile fetch, REST | no (normal) |
| `ACCESS_NETWORK_STATE` | Wi-Fi vs cell awareness for igate retry policy | no |
| `RECORD_AUDIO` | RX from default audio device | yes (on first RX enable) |
| `ACCESS_FINE_LOCATION` | GPS for beacons, livemap | yes (on first GPS enable) |
| `POST_NOTIFICATIONS` (API 33+) | foreground service notification | yes (on first launch) |
| `BLUETOOTH_CONNECT` (API 31+) | BT SPP PTT method | yes (only when method selected) |
| `FOREGROUND_SERVICE` | base | no |
| `FOREGROUND_SERVICE_CONNECTED_DEVICE` (API 34+) | USB devices | no |
| `FOREGROUND_SERVICE_MICROPHONE` (API 34+) | audio capture | no |
| `FOREGROUND_SERVICE_LOCATION` (API 34+) | GPS in background | no |
| `WAKE_LOCK` | Wi-Fi keep-alive | no |

Per-USB-device permission is granted via the system dialog that
`UsbManager.requestPermission()` invokes; not declared in manifest.

## 11. Effort and phasing

Six to ten weeks focused. Suggested phase split for the implementation
plan:

| Phase | Duration | Deliverable |
|---|---|---|
| 1 | 1 wk | Cross-compile both binaries to `android/arm64`. `adb push` + `adb shell` exec smoke from a throwaway test app. CPAL Oboe audio loopback proven on a real device. |
| 2 | 1 wk | `proto/platform.proto`, `pkg/platformproto/`, `pkg/platformsvc/`, Kotlin `PlatformServer` skeleton. Round-trip Hello + GpsFix in unit tests. |
| 3 | 1 wk | `GraywolfService` + `MainActivity` + `BinaryLauncher` + `WebView`. End-to-end "tap icon, see graywolf SPA running on the phone" with no PTT or GPS yet. |
| 4 | 1 wk | `GpsAdapter` + `pkg/gps/android.go`. First useful end-to-end feature: phone position beacons over RF. |
| 5 | 2 wk | Three new PTT methods (USB serial, USB HID, BT SPP) plus VOX (existing `NONE`). Hardware matrix testing. UI gating to hide non-Android methods on the Android build. |
| 6 | 1 wk | `make android` target, `release-android.yml`, signing, Play Console upload to internal track. |
| 7 | 1-2 wk | Polish, handbook page, wiki updates, beta cycle, Play Store first-submission review. |

## 12. Risks tracked

1. **CPAL Oboe backend maturity.** Latest CPAL has Oboe support; verify
   the version graywolf-modem currently pins works on Android.
   Prototype loopback in phase 1 *before* committing to Kotlin work.
   Fallback: write a thin AAudio shim in Rust if CPAL's Oboe path is
   unstable.
2. **Android exec-from-`nativeLibraryDir` policy drift.** Google has
   tightened this several times. Set `useLegacyPackaging=true` and
   `extractNativeLibs="true"` explicitly. Track Android 16+ release
   notes during impl.
3. **Play Store first-submission review.** Apps that capture audio,
   hold USB host fds, and transmit RF receive manual review. Submit
   with a thorough Data Safety form and a privacy-policy URL.
4. **PTT relay latency.** Rust -> Go -> Kotlin -> RTS toggle. Each UDS
   hop is single-digit microseconds. Total budget under 5ms typical.
   Measured in phase 5 before claiming TX timing is acceptable for
   1200-baud AFSK.
5. **GPLv2 + paid Play Store leakage.** Anyone with NDK can self-build.
   Accepted. Decision is business, not technical.

## 13. Out of scope, recorded for later

- iOS port. Different sandbox; revisit only after Android v1 ships.
- Native Compose UI replacing the WebView. Possible but would fork the
  UX surface from desktop; not worth it until the WebView path proves
  inadequate.
- In-app purchase for "premium" features. Conflicts with GPLv2 (free
  features in source).
- Watch / Wear OS companion. No.
- Tasker / shortcut intents. Could be added later; not v1.
- Separate F-Droid build. User explicitly excluded.
- Offline maps tile *creation* on phone. The phone is a download client
  only; tiles are produced in `~/dev/graywolf-maps` per invariant 7.

---

## Appendix A: directory layout under graywolf root after impl

```
graywolf/
  android/                          # NEW
    app/
      build.gradle.kts
      src/main/
        AndroidManifest.xml
        kotlin/com/nw5w/graywolf/
          MainActivity.kt
          GraywolfService.kt
          binaries/BinaryLauncher.kt
          platformsvc/
            PlatformServer.kt
            GpsAdapter.kt
            UsbAdapter.kt
            AudioAdapter.kt
            BatteryAdapter.kt
          webview/
            WebAppInterface.kt
            PermissionBridge.kt
        res/
        assets/
      src/test/kotlin/
      src/androidTest/kotlin/
    gradle/
    build.gradle.kts
    settings.gradle.kts
  proto/
    graywolf.proto                  # extended (PTT events, new method enum values)
    platform.proto                  # NEW
  pkg/
    platformproto/                  # NEW (generated)
    platformsvc/                    # NEW
    gps/
      android.go                    # NEW
      gpsd_unix.go                  # build-tagged
      serial_unix.go                # build-tagged
    pttdevice/
      android.go                    # NEW
      linux.go, windows.go, darwin.go  # build-tagged
    diagcollect/
      review/                       # build-tagged !android
      review_android.go             # NEW
    updatescheck/                   # build-tagged !android
  cmd/graywolf/
    main_unix.go                    # extracted
    main_windows.go                 # extracted
    main_android.go                 # NEW
  graywolf-modem/src/tx/
    ptt_android_usb_serial.rs       # NEW
    ptt_android_usb_hid.rs          # NEW
    ptt_android_bt_spp.rs           # NEW
  Makefile                          # NEW targets: android-modem, android-graywolf, android
  .github/workflows/
    release-android.yml             # NEW
    android-build-smoke.yml         # NEW
  docs/handbook/
    android.html                    # NEW
  docs/wiki/                        # updated (system-topology, build-pipelines, code-map, invariants)
```
