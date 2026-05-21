# Android Play Store Pipeline Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Bring the Android app from POC-B state (versionCode=1, targetSdk=34, no release signing, no CI) to Play-Store-ready: latest SDK, release-signed builds in GitHub Actions, automated upload to a Play Internal Testing track on every `v*` tag, and a Closed Testing track holding the 15-person private beta.

**Architecture:** A single new workflow `.github/workflows/android.yml` builds the Android app on every PR/push (sanity check, unsigned debug APK artifact) and, on `v*` tag pushes, additionally produces a release-signed `.aab` + `.apk`, attaches them to the GitHub release, and uploads the `.aab` to the Play Internal Testing track via `r0adkll/upload-google-play@v1` using a Google Cloud service account JSON. Signing uses Play App Signing: developer holds an *upload* keystore, Google holds the actual app-signing key. Beta testers receive the build through Play Closed Testing, promoted from Internal via a manual `workflow_dispatch`. Android `versionName` and `versionCode` are derived at build time from the repo-root `VERSION` file so a single bump cascades through `make bump-point`/`bump-minor` into the Android artifact too.

**Tech Stack:** AGP 8.7.3, Gradle 8.7, Kotlin 1.9.24, compileSdk/targetSdk 36 (Android 16), NDK r27c, cargo-ndk 4.x, Go 1.23+, Rust stable. CI: ubuntu-latest runners, `android-actions/setup-android@v3`, `r0adkll/upload-google-play@v1`. Existing graywolf tooling unchanged.

**Inputs the user must provide before Phase 4 begins:** (a) an upload keystore (Phase 1 task is the human one-time generation), (b) a Google Cloud service account JSON with Play Developer API access (Phase 2 human checklist), (c) the email list for the 15-person closed beta (Phase 6).

---

## File Structure

### Created

- `.github/workflows/android.yml` — Android CI + release pipeline (build, sign, attach to release, upload to Play)
- `docs/wiki/android-play-store.md` — wiki page: how the Android release flow works end-to-end (keystore, secrets, tracks, promotion)

### Modified

- `android/build.gradle.kts` — bump AGP `com.android.application` from `8.5.0` to `8.7.3` (required for compileSdk 36)
- `android/app/build.gradle.kts` — bump compileSdk/targetSdk to 36; add release `signingConfig` reading from env; derive `versionCode`/`versionName` from repo-root `VERSION` file via a helper block
- `scripts/check-android-toolchain.sh` — accept the new SDK levels (it currently doesn't check SDK platform versions, but bump the comment + add an Android SDK platform check for the CI-style toolchain audit)
- `docs/wiki/README.md` — link the new `android-play-store.md` page
- `docs/wiki/build-pipelines.md` — add an Android section pointing at the workflow + wiki page

### Not modified (called out so a future reader doesn't grep for them)

- `Makefile`'s `bump-point`/`bump-minor` targets — the Android build now reads `VERSION` directly, so the existing bump targets automatically cover Android with no edit
- `release.yml` — Android lives in its own workflow file to keep the existing Go/Rust/Docker/NSIS pipeline focused and bisectable; the two workflows share no jobs

---

## Phase 1 — SDK bump + locally signed APK on the user's tablet

**Goal:** A release-signed `.apk` of graywolf (built against the latest Android SDK) installed and running on the user's tablet by end of phase. This phase produces no CI changes; everything is local. Phases 3-5 lift this same build into GitHub Actions.

**Why this is Phase 1:** Two reasons. (a) Play Store will reject any upload below the current targetSdk floor, so the SDK bump is a hard prerequisite for everything downstream. (b) The user explicitly wants a working APK on the tablet *before* we wire CI — that proves the SDK bump didn't break the JNI/Go/Rust chain before we automate around it.

### Task 1.1: Bump AGP to a version that supports compileSdk 36

**Files:**
- Modify: `android/build.gradle.kts:2`

- [ ] **Step 1: Edit `android/build.gradle.kts`**

Change:
```kotlin
    id("com.android.application") version "8.5.0" apply false
```
to:
```kotlin
    id("com.android.application") version "8.7.3" apply false
```

- [ ] **Step 2: Sanity-build with the old SDK still set**

Run (from repo root):
```bash
cd android && ./gradlew :app:assembleDebug
```
Expected: BUILD SUCCESSFUL. (compileSdk is still 34 here — this step only verifies the AGP bump didn't break the existing graph before we change SDK levels too.)

- [ ] **Step 3: Commit**

```bash
git add android/build.gradle.kts
git commit -m "build(android): bump AGP to 8.7.3 (compileSdk 36 prerequisite)"
```

### Task 1.2: Install the Android SDK 36 platform locally

**Files:** None.

- [ ] **Step 1: List installed Android SDK platforms**

Run:
```bash
"$ANDROID_HOME/cmdline-tools/latest/bin/sdkmanager" --list_installed | grep '^  platforms;'
```
Expected: a list including `platforms;android-34` at minimum. If `platforms;android-36` is absent, continue to Step 2; otherwise skip to Task 1.3.

- [ ] **Step 2: Install SDK platform 36**

Run:
```bash
"$ANDROID_HOME/cmdline-tools/latest/bin/sdkmanager" "platforms;android-36" "build-tools;36.0.0"
```
Expected: `done` with no error. Re-run the `--list_installed` query and confirm `platforms;android-36` is present.

- [ ] **Step 3: No commit (local environment only)**

### Task 1.3: Bump compileSdk and targetSdk to 36

**Files:**
- Modify: `android/app/build.gradle.kts:11,16`

- [ ] **Step 1: Edit `android/app/build.gradle.kts`**

Change `compileSdk = 34` (line 11) to `compileSdk = 36` and `targetSdk = 34` (line 16) to `targetSdk = 36`. Leave `minSdk = 28` unchanged — the comment on lines 132-133 explains why armv7 stays dropped at minSdk 28.

- [ ] **Step 2: Rebuild debug**

Run:
```bash
cd android && ./gradlew :app:assembleDebug
```
Expected: BUILD SUCCESSFUL. If you see `Failed to find Build Tools revision 36.0.0`, re-run Task 1.2 Step 2. If you see Kotlin/androidx deprecation warnings, note them but do not block — they're warnings, not errors. If you see compile errors, the most likely culprit is an androidx dependency that needs a minor version bump; resolve before continuing.

- [ ] **Step 3: Smoke-install on the tablet**

Run:
```bash
adb install -r android/app/build/outputs/apk/debug/app-debug.apk
```
Expected: `Success`. Launch the app on the tablet and confirm it starts. (We're still on the debug-signed APK — the release signing config is Task 1.5.)

- [ ] **Step 4: Commit**

```bash
git add android/app/build.gradle.kts
git commit -m "build(android): bump compileSdk and targetSdk to 36"
```

### Task 1.4: Generate the upload keystore (HUMAN ONE-TIME TASK)

**Files:** None committed. The keystore is a secret; it is **never** committed to the repo.

**Context for the engineer:** Play App Signing means Google holds the actual app-signing key. The "upload keystore" we generate here is what *signs the AAB before upload*; Google verifies it, strips it, and re-signs with the real app key. Losing the upload key is recoverable (you can request a reset from Play Console). Losing the app key is not, but Google holds that one, so we're safe by design.

- [ ] **Step 1: Generate the keystore**

Run (anywhere outside the repo):
```bash
keytool -genkeypair \
  -alias graywolf-upload \
  -keyalg RSA \
  -keysize 4096 \
  -validity 36500 \
  -keystore ~/graywolf-upload.keystore \
  -storetype PKCS12
```
You'll be prompted for: keystore password (set a strong one), key password (use the same as the keystore password — Gradle's signingConfig accepts that and it simplifies CI secrets), name/org/locality (these end up in the cert but Play doesn't validate them — fill in "Chris Snell" / "NW5W" / "Tulsa" / "OK" / "US" or similar).
Expected: file at `~/graywolf-upload.keystore`, ~3-5KB.

- [ ] **Step 2: Verify the keystore**

Run:
```bash
keytool -list -v -keystore ~/graywolf-upload.keystore -alias graywolf-upload
```
Expected: prints the cert details. Note the SHA-256 fingerprint — Play Console will display this to confirm uploads use the right key.

- [ ] **Step 3: Stash secrets**

In 1Password (or your password manager), create an item "Graywolf Android Upload Keystore" with fields:
- Keystore password
- Key alias: `graywolf-upload`
- Key password (same as keystore password)
- SHA-256 fingerprint (from Step 2)
- Path: `~/graywolf-upload.keystore`

Also: copy the keystore file itself into 1Password as a document attachment. **This is your only backup.** A lost upload key triggers a 1-2 day Play Console reset flow.

- [ ] **Step 4: Generate the base64 form for GitHub secrets**

Run:
```bash
base64 -i ~/graywolf-upload.keystore | pbcopy
```
Expected: a long base64 string is on your clipboard. Hold this until Phase 4 Task 4.1, where it becomes the `GRAYWOLF_KEYSTORE_BASE64` GitHub Actions secret.

- [ ] **Step 5: No commit**

### Task 1.5: Add release signing config to Gradle

**Files:**
- Modify: `android/app/build.gradle.kts:53-61` (the `buildTypes` block)

- [ ] **Step 1: Replace the `buildTypes` block in `android/app/build.gradle.kts`**

Find:
```kotlin
    buildTypes {
        debug {
            isMinifyEnabled = false
        }
        release {
            // POC-B is debug-only; phase 6 wires release signing.
            isMinifyEnabled = false
        }
    }
```
Replace with:
```kotlin
    // Release signing reads from env vars so CI can inject secrets without a
    // committed keystore. For local release builds, export these four vars
    // before running ./gradlew assembleRelease or bundleRelease. Decoding
    // happens in the gradle script so CI can pass GRAYWOLF_KEYSTORE_BASE64
    // and local devs can pass GRAYWOLF_KEYSTORE_PATH.
    val keystorePath = System.getenv("GRAYWOLF_KEYSTORE_PATH")
    val keystoreBase64 = System.getenv("GRAYWOLF_KEYSTORE_BASE64")
    val keystorePassword = System.getenv("GRAYWOLF_KEYSTORE_PASSWORD")
    val keyAlias = System.getenv("GRAYWOLF_KEY_ALIAS") ?: "graywolf-upload"
    val keyPassword = System.getenv("GRAYWOLF_KEY_PASSWORD") ?: keystorePassword

    val resolvedKeystoreFile: java.io.File? = when {
        keystorePath != null -> file(keystorePath)
        keystoreBase64 != null -> {
            val tmp = layout.buildDirectory.file("upload.keystore").get().asFile
            tmp.parentFile.mkdirs()
            tmp.writeBytes(java.util.Base64.getDecoder().decode(keystoreBase64))
            tmp
        }
        else -> null
    }

    signingConfigs {
        if (resolvedKeystoreFile != null && keystorePassword != null) {
            create("release") {
                storeFile = resolvedKeystoreFile
                storePassword = keystorePassword
                this.keyAlias = keyAlias
                this.keyPassword = keyPassword!!
            }
        }
    }

    buildTypes {
        debug {
            isMinifyEnabled = false
        }
        release {
            isMinifyEnabled = false
            // If the env didn't supply a keystore, leave signingConfig unset
            // and let `assembleRelease` produce an unsigned APK. That keeps
            // PR-build CI jobs (which never see secrets) green; only tag
            // builds inject the keystore env and produce a signed artifact.
            signingConfigs.findByName("release")?.let { signingConfig = it }
        }
    }
```

- [ ] **Step 2: Local unsigned release build sanity check**

Run (without setting any keystore env vars):
```bash
cd android && unset GRAYWOLF_KEYSTORE_PATH GRAYWOLF_KEYSTORE_BASE64 GRAYWOLF_KEYSTORE_PASSWORD && ./gradlew :app:assembleRelease
```
Expected: BUILD SUCCESSFUL. Output APK at `android/app/build/outputs/apk/release/app-release-unsigned.apk`. The "-unsigned" suffix confirms the no-keystore branch worked.

- [ ] **Step 3: Local signed release build sanity check**

Run (with env vars set):
```bash
cd android && \
  GRAYWOLF_KEYSTORE_PATH=~/graywolf-upload.keystore \
  GRAYWOLF_KEYSTORE_PASSWORD='<paste from 1password>' \
  ./gradlew :app:assembleRelease
```
Expected: BUILD SUCCESSFUL. Output APK at `android/app/build/outputs/apk/release/app-release.apk` (no "-unsigned" suffix). Verify the signature:
```bash
$ANDROID_HOME/build-tools/36.0.0/apksigner verify --print-certs android/app/build/outputs/apk/release/app-release.apk
```
Expected: prints the same SHA-256 fingerprint you noted in Task 1.4 Step 2.

- [ ] **Step 4: Commit**

```bash
git add android/app/build.gradle.kts
git commit -m "build(android): add release signingConfig reading from env"
```

### Task 1.6: Derive versionCode + versionName from the repo-root VERSION file

**Files:**
- Modify: `android/app/build.gradle.kts:17-18` (the two version lines inside `defaultConfig`)

**Why:** Today the Android `versionCode=1` and `versionName="0.0.1-pocb"` are stale and decoupled from `make bump-point`/`bump-minor`. Reading the repo-root `VERSION` file at configure time means a single bump updates everywhere with no risk of forgetting Android. Play requires `versionCode` to monotonically increase across uploads, so we encode major.minor.patch as `M*1_000_000 + m*10_000 + p*100`, leaving 100 slots for hotfix re-uploads within the same patch (in practice you'll never use them, but having the room costs nothing).

- [ ] **Step 1: Edit `android/app/build.gradle.kts`**

Add this near the top of the file, just below the `import` line (line 1):
```kotlin
// Read version metadata from the repo-root VERSION file so Android stays in
// sync with `make bump-point`/`bump-minor`. Format: "X.Y.Z" with optional
// "-pre" suffix that we drop for versionCode (Play needs a pure integer)
// but keep for versionName.
val graywolfVersionName: String = run {
    val versionFile = rootProject.projectDir.parentFile.resolve("VERSION")
    require(versionFile.exists()) { "VERSION file not found at ${versionFile.absolutePath}" }
    versionFile.readText().trim()
}
val graywolfVersionCode: Int = run {
    val core = graywolfVersionName.substringBefore('-')
    val parts = core.split(".")
    require(parts.size == 3) { "VERSION must be X.Y.Z; got '$graywolfVersionName'" }
    val (major, minor, patch) = parts.map { it.toInt() }
    major * 1_000_000 + minor * 10_000 + patch * 100
}
```

Then replace lines 17-18 in `defaultConfig`:
```kotlin
        versionCode = 1
        versionName = "0.0.1-pocb"
```
with:
```kotlin
        versionCode = graywolfVersionCode
        versionName = graywolfVersionName
```

- [ ] **Step 2: Verify the derived versions**

Run:
```bash
cd android && ./gradlew :app:assembleDebug -q
"$ANDROID_HOME/build-tools/36.0.0/aapt2" dump badging app/build/outputs/apk/debug/app-debug.apk | grep "package:"
```
Expected: a line like `package: name='com.nw5w.graywolf' versionCode='130500' versionName='0.13.5'` (matching whatever's currently in `VERSION`).

- [ ] **Step 3: Commit**

```bash
git add android/app/build.gradle.kts
git commit -m "build(android): derive versionCode/versionName from repo VERSION file"
```

### Task 1.7: Build the signed release APK and install on the tablet

**Files:** None.

- [ ] **Step 1: Build the signed release APK**

Run:
```bash
cd android && \
  GRAYWOLF_KEYSTORE_PATH=~/graywolf-upload.keystore \
  GRAYWOLF_KEYSTORE_PASSWORD='<paste from 1password>' \
  ./gradlew :app:assembleRelease
```
Expected: BUILD SUCCESSFUL. Output at `android/app/build/outputs/apk/release/app-release.apk`.

- [ ] **Step 2: Uninstall any existing debug build from the tablet**

The release APK is signed with a different cert than the debug build, so Android will refuse to install on top. Run:
```bash
adb uninstall com.nw5w.graywolf
```
Expected: `Success` (or `Failure [DELETE_FAILED_INTERNAL_ERROR]` if not installed; either is fine).

- [ ] **Step 3: Install the release APK**

Run:
```bash
adb install android/app/build/outputs/apk/release/app-release.apk
```
Expected: `Success`. Launch the app on the tablet and confirm it starts and the basic flows work (modem connect, audio, UI). **No commit** — this is the validation step that gates moving to Phase 2.

---

## Phase 2 — Play Console one-time setup (HUMAN CHECKLIST)

**Goal:** A Play Console app listing exists with all sections filled, an Internal Testing track is set up with the user as the only tester, and a Google Cloud service account with Play Developer API access exists and is linked. All steps are in the Play Console UI or Google Cloud Console — no code, no commits.

**Time estimate:** 60-90 minutes the first time, mostly waiting for Google Cloud project/API setup to propagate.

### Task 2.1: Create the app in Play Console

- [ ] Sign in to https://play.google.com/console with the existing developer account.
- [ ] Click **Create app**. Fill in:
  - **App name:** Graywolf
  - **Default language:** English (United States) — en-US
  - **App or game:** App
  - **Free or paid:** Free
  - **Declarations:** check both Developer Program Policies and US export laws boxes.
- [ ] Click **Create app**. You'll land on the app dashboard.
- [ ] Note the **package name** Play shows on the dashboard. Verify it reads `com.nw5w.graywolf` (matching `applicationId` in `android/app/build.gradle.kts:14`).

### Task 2.2: Fill the App Content sections

Play won't accept an upload to any track (even Internal) until all required content sections are complete. Click each item in the left nav under **Policy → App content** and fill it in. The graywolf-specific answers are below.

- [ ] **Privacy policy** — you need a public URL. If you don't have one, the simplest path is to add `docs/handbook/privacy.html` to the repo (it gets published at chrissnell.com/software/graywolf/privacy.html via the existing handbook build) with a short policy: "Graywolf is a single-station ham radio app. It does not collect, transmit, or share user data with the developer or any third party. All audio, location, and message data stays on the user's device." Paste that URL.
- [ ] **App access** — choose "All functionality is available without special access". Graywolf has no login.
- [ ] **Ads** — "No, my app does not contain ads."
- [ ] **Content rating** — fill the questionnaire. For a ham-radio utility: no violence, no sexual content, no profanity, no gambling, no UGC the developer doesn't control. Should land at "Everyone".
- [ ] **Target audience** — "Ages 18 and up" (ham radio licensing is the gating factor). Confirm the app is not appealing to children.
- [ ] **News app** — "No".
- [ ] **COVID-19 contact tracing and status apps** — "No".
- [ ] **Data safety** — declare: no data collected, no data shared. Encryption in transit: yes (HTTPS for any external calls). Data deletion: not applicable, no data collected.
- [ ] **Government apps** — "No".
- [ ] **Financial features** — "No".
- [ ] **Health features** — "No".
- [ ] **Actions on Google** — "No".

### Task 2.3: Set up the Main Store Listing

Required even for Internal Testing-only releases.

- [ ] In **Grow users → Store presence → Main store listing**:
  - **App name:** Graywolf
  - **Short description (80 chars):** e.g. "Ham radio digital-modes station for VARA, packet, APRS, and voice over USB radios."
  - **Full description (4000 chars):** lift from `README.md` intro paragraph(s).
  - **App icon:** 512×512 PNG. The launcher icon at `android/app/src/main/res/mipmap-*/` won't satisfy this — Play wants a separate high-res icon. If one isn't already in the repo under `web/static/` or `docs/handbook/`, generate one at 512×512 PNG and upload.
  - **Feature graphic:** 1024×500 PNG. Required. Make a simple one with the wordmark on a dark background.
  - **Phone screenshots:** 2-8 required. Take screenshots from the tablet (Power + Volume-Down) of the main views (PTT, Messages, Channels). Transfer via `adb pull /sdcard/Pictures/Screenshots/`.
  - **Tablet screenshots (7-inch):** optional but strongly recommended since this is a tablet-first app. Use the same screenshots as phone.
  - **Category:** Tools → Communication.
  - **Contact details:** support email (use the new.zoo6451 address or a dedicated graywolf@ alias).
  - **External marketing:** "I do not market this app outside of Google Play."
- [ ] Click **Save**.

### Task 2.4: Set up Internal Testing track

- [ ] In **Test and release → Testing → Internal testing**, click **Create new release**.
- [ ] Under **App signing key**, Play offers "Use Play App Signing". **Choose this.** You'll upload your APK/AAB signed with the *upload* key generated in Task 1.4; Google will hold the actual signing key. (If Play offers "Export and upload a key from a Java keystore", do not pick that — it bypasses Play App Signing and is much harder to recover from a lost key.)
- [ ] On the first prompt, you'll need to either let Google generate the signing key (recommended) or upload your own. **Pick "Let Google create and manage a signing key".**
- [ ] Add yourself as an internal tester:
  - Click **Testers** tab.
  - Create an email list called "graywolf-internal" with your own email address only. (The 15-person beta gets its own list in Phase 6.)
- [ ] Note the **opt-in URL** Play generates. Bookmark it; you'll use it on the tablet to install via Play Store rather than via `adb` once we've uploaded a build.
- [ ] Save (but don't try to upload an AAB yet — that's Phase 4 Task 4.4).

### Task 2.5: Create the Google Cloud service account for API uploads

- [ ] Open https://console.cloud.google.com . If you don't already have a project, create one called `graywolf-play-upload` (the name doesn't matter outside the console).
- [ ] In the project, go to **APIs & Services → Enabled APIs** → **+ Enable APIs and Services**. Search for "Google Play Android Developer API" and enable it. Wait for the enable to complete (~30 seconds).
- [ ] Go to **IAM & Admin → Service Accounts → + Create Service Account**:
  - Name: `play-upload`
  - ID: auto-filled (`play-upload@graywolf-play-upload.iam.gserviceaccount.com` or similar)
  - Role: leave blank at this level — we grant permissions inside Play Console, not GCP.
- [ ] Open the new service account → **Keys** tab → **Add Key → Create new key → JSON**. Download. Save to 1Password as "Graywolf Play Upload Service Account JSON". The file is ~2KB.
- [ ] Back in Play Console: **Users and permissions → Invite new users**. Paste the service account's email (`play-upload@...iam.gserviceaccount.com`). Under **App permissions**, add `com.nw5w.graywolf` (the only app). Grant:
  - **View app information and download bulk reports** (Account-level)
  - **Manage testing track releases** (App-level) — this is the minimum for upload + track promotion.
  - Do *not* grant production release perms yet; we'll add that explicitly when we're ready (see Phase 6 Task 6.3 note).
- [ ] Click **Invite user**. The service account is now linked.
- [ ] **Wait 24 hours before testing the API.** Google's docs say "permissions can take up to 24 hours to propagate"; in practice it's usually <1 hour but plan for the worst case. You can proceed with Phase 3 immediately — it doesn't touch Play.

---

## Phase 3 — GitHub Actions Android build workflow (PR sanity, no signing)

**Goal:** Every push to main and every PR runs an Android build on a GitHub-hosted Ubuntu runner. Output: a debug APK uploaded as an artifact for inspection. No signing, no Play upload, no secrets. This is the safety net that catches "the SDK bump broke CI" on a PR rather than at tag time.

### Task 3.1: Create the workflow file with the PR-build job

**Files:**
- Create: `.github/workflows/android.yml`

- [ ] **Step 1: Create the file**

```yaml
name: Android

on:
  push:
    branches:
      - main
    tags:
      - 'v*'
  pull_request:
    branches:
      - main

permissions:
  contents: write   # tag-build job uploads to the GH Release

jobs:
  build:
    name: Build (debug APK)
    runs-on: ubuntu-latest
    # Run on every PR and main push. Tag pushes additionally trigger the
    # release-sign job below; this build job is still useful on tags as a
    # sanity check that an unsigned build works before we try a signed one.
    steps:
      - name: Checkout
        uses: actions/checkout@v5
        with:
          fetch-depth: 0   # VERSION file derivation reads via plain fs, but
                           # keeping full history matches the rest of CI.

      - name: Set up JDK 17
        uses: actions/setup-java@v4
        with:
          distribution: temurin
          java-version: '17'

      - name: Set up Android SDK
        uses: android-actions/setup-android@v3
        with:
          packages: 'platforms;android-36 build-tools;36.0.0 ndk;27.2.12479018'

      - name: Set up Go
        uses: actions/setup-go@v6
        with:
          go-version-file: go.mod
          cache-dependency-path: go.sum

      - name: Set up Rust + Android targets
        uses: dtolnay/rust-toolchain@stable
        with:
          targets: aarch64-linux-android,x86_64-linux-android

      - name: Rust cache
        uses: Swatinem/rust-cache@v2
        with:
          key: android

      - name: Install cargo-ndk
        run: cargo install cargo-ndk --locked --version ^4

      - name: Install protoc
        uses: arduino/setup-protoc@v3
        with:
          version: '28.x'
          repo-token: ${{ secrets.GITHUB_TOKEN }}

      - name: Gradle cache
        uses: actions/cache@v4
        with:
          path: |
            ~/.gradle/caches
            ~/.gradle/wrapper
          key: gradle-${{ hashFiles('android/**/*.gradle*', 'android/**/gradle-wrapper.properties') }}
          restore-keys: gradle-

      - name: Build debug APK
        working-directory: android
        env:
          # Both vars are set by android-actions/setup-android, but Gradle's
          # check-android-toolchain.sh wants ANDROID_NDK_ROOT specifically.
          ANDROID_NDK_ROOT: ${{ env.ANDROID_NDK_HOME }}
        run: ./gradlew :app:assembleDebug --no-daemon --stacktrace

      - name: Upload debug APK
        uses: actions/upload-artifact@v5
        with:
          name: graywolf-debug-apk
          path: android/app/build/outputs/apk/debug/app-debug.apk
          retention-days: 7
```

- [ ] **Step 2: Commit and push to a branch**

```bash
git checkout -b android-ci
git add .github/workflows/android.yml
git commit -m "ci(android): add PR-build workflow"
git push -u origin android-ci
```

- [ ] **Step 3: Open a PR and watch the run**

```bash
gh pr create --title "ci(android): add Android build workflow" --body "Phase 3 of the Play Store pipeline plan."
gh run watch
```
Expected: the **Android / Build (debug APK)** job goes green within ~10-15 minutes (the first run is slow because of cold caches and Rust target install; subsequent runs should land closer to 5-7 minutes). The debug APK is downloadable from the run page. **Do not merge yet** — Phase 4 adds the signed-release job to the same workflow.

- [ ] **Step 4: If the run fails:**

The two most likely failures are (a) NDK version mismatch — `cargo-ndk` complains about a missing toolchain. Check the run log; if so, the NDK version installed by `setup-android` doesn't match what the Gradle script expects. Pin both. (b) `check-android-toolchain.sh` fails because `rustup` isn't on the PATH — `dtolnay/rust-toolchain` uses a non-rustup install on CI. If the toolchain check fails on the `rustup` line, edit `scripts/check-android-toolchain.sh` to skip the `rustup` check when running in CI:

```bash
if [[ -n "${CI:-}" ]] && ! command -v rustup >/dev/null 2>&1; then
    echo "[check-android-toolchain] CI without rustup; assuming targets present" >&2
elif ! command -v rustup >/dev/null 2>&1; then
    fail "rustup not on PATH; install via https://rustup.rs"
fi
```
Commit that fix to the same branch, push, and re-watch.

---

## Phase 4 — Signed release builds attached to GitHub Release on `v*` tags

**Goal:** Pushing a `v*` tag triggers a second job in the Android workflow that produces a release-signed `.aab` and `.apk`, attaches both to the auto-created GitHub release, and stops there (Phase 5 adds the Play upload step). Verifies the full signing pipeline end-to-end before adding the Play API call.

### Task 4.1: Add signing secrets to the GitHub repo

**Files:** None (GitHub UI / `gh` CLI).

- [ ] **Step 1: Add the four secrets via `gh secret set`**

Run (replace `<paste>` with the value from 1Password / your clipboard from Task 1.4 Step 4):
```bash
gh secret set GRAYWOLF_KEYSTORE_BASE64 < ~/graywolf-upload.keystore.b64
gh secret set GRAYWOLF_KEYSTORE_PASSWORD --body '<paste>'
gh secret set GRAYWOLF_KEY_ALIAS --body 'graywolf-upload'
gh secret set GRAYWOLF_KEY_PASSWORD --body '<paste>'
```

(For `GRAYWOLF_KEYSTORE_BASE64`, save the base64 string to a temp file first: `base64 -i ~/graywolf-upload.keystore > ~/graywolf-upload.keystore.b64`. Delete the temp file after upload.)

- [ ] **Step 2: Verify**

Run:
```bash
gh secret list
```
Expected: all four `GRAYWOLF_*` secrets appear with recent timestamps.

### Task 4.2: Add the release-sign job to the workflow

**Files:**
- Modify: `.github/workflows/android.yml` (append a new job; do not touch the existing `build` job)

- [ ] **Step 1: Append the `release-sign` job**

At the bottom of `.github/workflows/android.yml`, after the `build` job, add:

```yaml
  release-sign:
    name: Release sign (AAB + APK)
    runs-on: ubuntu-latest
    needs: build
    # Only run on v* tag pushes. The needs:build guard means the unsigned
    # debug build must pass first, so we never try to sign something the
    # cheap PR-style build already showed is broken.
    if: startsWith(github.ref, 'refs/tags/v')
    steps:
      - name: Checkout
        uses: actions/checkout@v5
        with:
          fetch-depth: 0

      - name: Set up JDK 17
        uses: actions/setup-java@v4
        with:
          distribution: temurin
          java-version: '17'

      - name: Set up Android SDK
        uses: android-actions/setup-android@v3
        with:
          packages: 'platforms;android-36 build-tools;36.0.0 ndk;27.2.12479018'

      - name: Set up Go
        uses: actions/setup-go@v6
        with:
          go-version-file: go.mod
          cache-dependency-path: go.sum

      - name: Set up Rust + Android targets
        uses: dtolnay/rust-toolchain@stable
        with:
          targets: aarch64-linux-android,x86_64-linux-android

      - name: Rust cache
        uses: Swatinem/rust-cache@v2
        with:
          key: android-release

      - name: Install cargo-ndk
        run: cargo install cargo-ndk --locked --version ^4

      - name: Install protoc
        uses: arduino/setup-protoc@v3
        with:
          version: '28.x'
          repo-token: ${{ secrets.GITHUB_TOKEN }}

      - name: Gradle cache
        uses: actions/cache@v4
        with:
          path: |
            ~/.gradle/caches
            ~/.gradle/wrapper
          key: gradle-release-${{ hashFiles('android/**/*.gradle*', 'android/**/gradle-wrapper.properties') }}
          restore-keys: gradle-release-

      - name: Build signed AAB and APK
        working-directory: android
        env:
          ANDROID_NDK_ROOT: ${{ env.ANDROID_NDK_HOME }}
          GRAYWOLF_KEYSTORE_BASE64: ${{ secrets.GRAYWOLF_KEYSTORE_BASE64 }}
          GRAYWOLF_KEYSTORE_PASSWORD: ${{ secrets.GRAYWOLF_KEYSTORE_PASSWORD }}
          GRAYWOLF_KEY_ALIAS: ${{ secrets.GRAYWOLF_KEY_ALIAS }}
          GRAYWOLF_KEY_PASSWORD: ${{ secrets.GRAYWOLF_KEY_PASSWORD }}
        run: ./gradlew :app:bundleRelease :app:assembleRelease --no-daemon --stacktrace

      - name: Stage release artifacts
        run: |
          set -euo pipefail
          version="${GITHUB_REF_NAME#v}"
          mkdir -p staging
          cp android/app/build/outputs/bundle/release/app-release.aab "staging/graywolf-${version}.aab"
          cp android/app/build/outputs/apk/release/app-release.apk     "staging/graywolf-${version}.apk"
          ls -la staging/

      - name: Upload artifacts to the GitHub Release
        env:
          GITHUB_TOKEN: ${{ secrets.GITHUB_TOKEN }}
        run: |
          set -euo pipefail
          version="${GITHUB_REF_NAME#v}"
          # `gh release upload` will fail if the release doesn't exist yet.
          # The existing release.yml workflow creates it via goreleaser; this
          # job runs in parallel with that, so retry briefly to absorb the race.
          for i in 1 2 3 4 5 6; do
            if gh release upload "${GITHUB_REF_NAME}" \
                 "staging/graywolf-${version}.aab" \
                 "staging/graywolf-${version}.apk" \
                 --clobber; then
              exit 0
            fi
            echo "Release ${GITHUB_REF_NAME} not yet available, retrying ($i/6)..."
            sleep 30
          done
          echo "::error::Release ${GITHUB_REF_NAME} never appeared; check goreleaser job."
          exit 1

      - name: Upload signed AAB as workflow artifact (manual fallback)
        uses: actions/upload-artifact@v5
        with:
          name: graywolf-release-aab
          path: staging/graywolf-*.aab
          retention-days: 30
```

- [ ] **Step 2: Commit, push, and merge the PR from Phase 3**

```bash
git add .github/workflows/android.yml
git commit -m "ci(android): add release-sign job on v* tags"
git push
gh pr merge --squash --delete-branch
```
The merge to main triggers another `build` job run (no `release-sign` yet, since it's not a tag) — confirm it passes.

### Task 4.3: Tag a test release and verify the signed artifacts

- [ ] **Step 1: Cut a release**

```bash
git checkout main
git pull
make bump-point
```
This bumps `VERSION` from `0.13.5` to `0.13.6`, commits, tags `v0.13.6`, and pushes. **Before running it, follow the existing release workflow step 0 from CLAUDE.md** — write a release-notes entry first.

- [ ] **Step 2: Watch the Android release-sign job**

```bash
gh run watch
```
Expected: both `build` and `release-sign` jobs go green. The `release-sign` job takes ~10-15 minutes.

- [ ] **Step 3: Verify artifacts on the GitHub release page**

```bash
gh release view v0.13.6 --json assets -q '.assets[].name'
```
Expected: list includes `graywolf-0.13.6.aab` and `graywolf-0.13.6.apk` (alongside the goreleaser-produced Linux/macOS/Windows binaries).

- [ ] **Step 4: Verify the AAB signature**

Download and inspect:
```bash
gh release download v0.13.6 --pattern 'graywolf-*.aab' --dir /tmp
unzip -p /tmp/graywolf-0.13.6.aab META-INF/MANIFEST.MF | head
# Or: use bundletool to extract and apksigner to verify
```
The point is to confirm the artifact landed signed; the easiest definitive check is uploading it to Play and watching Play accept it (Task 4.4).

### Task 4.4: First manual upload to Play Internal Testing

- [ ] **Step 1: Download the AAB**

```bash
gh release download v0.13.6 --pattern 'graywolf-*.aab' --dir ~/Downloads
```

- [ ] **Step 2: Upload via Play Console**

In Play Console → **Test and release → Testing → Internal testing → Create new release** (or continue the release started in Task 2.4). Upload `~/Downloads/graywolf-0.13.6.aab`. Play will:
1. Process the bundle (~30-60 seconds).
2. Show the versionCode (130600) and versionName (0.13.6) it detected.
3. Show any warnings — most commonly "deobfuscation files missing" (fine, we're not minifying) and "no Android Vitals data yet" (expected for first upload).
4. If Play rejects the upload because the targetSdk is below the current floor: this is the actual current floor; bump to whatever number it asks for and re-tag (delete-and-retag per the existing release workflow).

- [ ] **Step 3: Fill release notes (English) — copy from `pkg/releasenotes/notes.yaml`**

- [ ] **Step 4: Click "Review release" then "Start rollout to internal testing"**

- [ ] **Step 5: Wait ~10 minutes, then install via Play Store on the tablet**

Open the Internal Testing opt-in URL (bookmarked from Task 2.4) on the tablet. Tap "Become a tester", then "Download it on Google Play". Confirm the install completes and the app launches. **This is the gate that proves the full pipeline works** — from `git tag` → CI build → signed AAB → Play upload → tablet install.

---

## Phase 5 — Automated Play upload from GitHub Actions

**Goal:** Eliminate the manual Play Console upload step. After this phase, `make bump-point` is the only manual action a release requires; everything else (build, sign, attach to GH release, upload to Play Internal) happens automatically.

### Task 5.1: Stash the service account JSON as a GitHub secret

- [ ] **Step 1: Add the secret**

Run:
```bash
gh secret set GRAYWOLF_PLAY_SERVICE_ACCOUNT_JSON < ~/Downloads/graywolf-play-upload-service-account.json
```
(Substitute the actual filename from Task 2.5 Step 4.)

- [ ] **Step 2: Verify**

```bash
gh secret list | grep PLAY
```
Expected: `GRAYWOLF_PLAY_SERVICE_ACCOUNT_JSON` listed.

- [ ] **Step 3: Delete the local JSON file from your Downloads folder.** It's safely in 1Password and as a GH secret. Leaving a service-account JSON in `~/Downloads` is the kind of thing security audits flag.

### Task 5.2: Add the Play upload step to the workflow

**Files:**
- Modify: `.github/workflows/android.yml` (append a step to the existing `release-sign` job)

- [ ] **Step 1: Add the upload step after "Upload artifacts to the GitHub Release"**

Inside the `release-sign` job, after the `Upload artifacts to the GitHub Release` step but before `Upload signed AAB as workflow artifact (manual fallback)`, insert:

```yaml
      - name: Upload AAB to Play Internal Testing
        uses: r0adkll/upload-google-play@v1.1.3
        with:
          serviceAccountJsonPlainText: ${{ secrets.GRAYWOLF_PLAY_SERVICE_ACCOUNT_JSON }}
          packageName: com.nw5w.graywolf
          releaseFiles: staging/graywolf-*.aab
          track: internal
          status: completed
          # Don't auto-promote. The release lands in Internal; humans promote
          # to Closed (the beta track) explicitly via Phase 6's workflow.
          changesNotSentForReview: false
```

- [ ] **Step 2: Commit and push to a branch, open PR, merge**

```bash
git checkout -b android-play-upload
git add .github/workflows/android.yml
git commit -m "ci(android): auto-upload AAB to Play Internal on v* tags"
git push -u origin android-play-upload
gh pr create --title "ci(android): automated Play Internal upload" --body "Phase 5 of the Play Store pipeline plan."
# After review/merge:
gh pr merge --squash --delete-branch
```

### Task 5.3: Cut another release and verify auto-upload

- [ ] **Step 1: Bump and tag**

```bash
make bump-point
```
(Remember the release-notes entry per the CLAUDE.md release flow.)

- [ ] **Step 2: Watch the Android workflow**

```bash
gh run watch
```
Expected: `release-sign` job goes green, including the new `Upload AAB to Play Internal Testing` step.

- [ ] **Step 3: Verify in Play Console**

Within 5 minutes of the workflow finishing, refresh **Test and release → Testing → Internal testing**. The new versionCode should appear with status "Rolling out" or "Available to testers".

- [ ] **Step 4: Verify on the tablet**

Open Play Store on the tablet → Manage apps & device → Updates available. Graywolf should show as updatable (or already updated). Install and verify the new version runs.

---

## Phase 6 — Closed Testing track for the 15-person private beta

**Goal:** The 15-person beta is on Play Closed Testing, can install graywolf through the Play Store like any other app, and receives updates promoted from Internal Testing via a manual workflow trigger.

**Why Closed and not Open or Internal:**
- **Internal** caps at 100 testers and is for the dev team — not appropriate for external beta testers who shouldn't see every CI tag.
- **Open** is publicly opt-in via a URL; doesn't fit "private" beta.
- **Closed** is invite-only by email/Google Group, holds your 15 testers, and lets you promote curated builds rather than every Internal release.

**A note on Google's 12-tester / 14-day rule:** If your Play Developer account was created after Nov 2023 as a *personal* account, Google requires a closed test with 12+ active testers (people who installed the app) for 14 consecutive days before production publishing is unlocked. Organization accounts are exempt. Your beta of 15 people satisfies the headcount; the 14-day clock starts when you have 12+ installs in Closed. If your account is a personal one, plan the beta start date 14+ days before any planned production launch.

### Task 6.1: Create the Closed Testing track

- [ ] In Play Console → **Test and release → Testing → Closed testing**, click **Create track**. Name it `graywolf-beta`.
- [ ] In the new track → **Testers tab → Create email list**: name it `graywolf-beta-testers`. Add the 15 email addresses. (For testers without a Google account, the email they give you must be the one tied to their Play Store account — usually their Gmail.)
- [ ] Note the **opt-in URL**. Send it to your testers along with installation instructions.

### Task 6.2: Add a promotion workflow

**Files:**
- Modify: `.github/workflows/android.yml` (append a third job triggered by `workflow_dispatch`)

- [ ] **Step 1: Append a `promote-to-closed` job**

At the bottom of `.github/workflows/android.yml`:

```yaml
  promote-to-closed:
    name: Promote latest Internal release to Closed beta
    runs-on: ubuntu-latest
    # Manual trigger only — fired from the Actions tab in the GH UI or via
    # `gh workflow run android.yml --field version=X.Y.Z`. This keeps every
    # CI tag from hitting beta testers; humans decide when a release is
    # beta-worthy.
    if: github.event_name == 'workflow_dispatch'
    steps:
      - name: Checkout
        uses: actions/checkout@v5

      - name: Download AAB from GitHub Release
        env:
          GITHUB_TOKEN: ${{ secrets.GITHUB_TOKEN }}
        run: |
          set -euo pipefail
          mkdir -p staging
          gh release download "v${{ github.event.inputs.version }}" \
            --pattern 'graywolf-*.aab' \
            --dir staging

      - name: Promote AAB to Closed (graywolf-beta)
        uses: r0adkll/upload-google-play@v1.1.3
        with:
          serviceAccountJsonPlainText: ${{ secrets.GRAYWOLF_PLAY_SERVICE_ACCOUNT_JSON }}
          packageName: com.nw5w.graywolf
          releaseFiles: staging/graywolf-*.aab
          track: graywolf-beta
          status: completed
```

Also add a `workflow_dispatch` input at the top of the file. Locate the `on:` block and replace it with:

```yaml
on:
  push:
    branches:
      - main
    tags:
      - 'v*'
  pull_request:
    branches:
      - main
  workflow_dispatch:
    inputs:
      version:
        description: 'Released version (X.Y.Z, no v prefix) to promote to Closed beta'
        required: true
        type: string
```

- [ ] **Step 2: Commit and merge via PR**

```bash
git checkout -b android-promote-closed
git add .github/workflows/android.yml
git commit -m "ci(android): add manual promote-to-closed workflow"
git push -u origin android-promote-closed
gh pr create --title "ci(android): manual closed-beta promotion" --body "Phase 6 of the Play Store pipeline plan."
gh pr merge --squash --delete-branch
```

### Task 6.3: Run the promotion and verify a tester install

- [ ] **Step 1: Promote a recent release**

```bash
gh workflow run android.yml --field version=0.13.7
gh run watch
```
Expected: `promote-to-closed` job goes green.

- [ ] **Step 2: Verify in Play Console**

**Closed testing → graywolf-beta** should show the version as live.

- [ ] **Step 3: Verify a tester can install**

Have one of your 15 testers tap the opt-in URL, accept, and install via Play Store. If they hit "App not available in your country", check Play Console → graywolf-beta → **Countries / regions** and add their country.

**Note for future production launch:** the service account currently has Closed/Internal release perms only. To enable production releases, return to Play Console → Users and permissions → the `play-upload` service account, and add "Manage production releases". Do this only when you're ready, since the existing workflow would happily push to production if `track: production` is set — keep the workflow's track set to `internal` and `graywolf-beta` until then.

---

## Phase 7 — Documentation

**Goal:** A wiki page exists that a future session can read to understand the Android release pipeline without reverse-engineering the workflow file. Updates to existing wiki pages link out to it.

### Task 7.1: Create `docs/wiki/android-play-store.md`

**Files:**
- Create: `docs/wiki/android-play-store.md`

- [ ] **Step 1: Write the page**

Content outline (write full paragraphs, not bullet stubs):

1. **Overview** — one paragraph: the Android app is released via `make bump-point` → tag push → `.github/workflows/android.yml` builds, signs, attaches to GH release, and uploads to Play Internal. Beta is on Play Closed Testing (`graywolf-beta` track), promoted manually with `gh workflow run android.yml --field version=X.Y.Z`.
2. **Triggers and what each does** — table of `push:main`, `push:tag v*`, `pull_request`, `workflow_dispatch` → which jobs fire.
3. **Signing** — Play App Signing means Google holds the actual signing key; we hold an upload key in 1Password ("Graywolf Android Upload Keystore") and as four `GRAYWOLF_KEY*` GH secrets. SHA-256 fingerprint for verification.
4. **Service account** — name, GCP project, the four permissions it has, where the JSON lives (1Password "Graywolf Play Upload Service Account JSON").
5. **Version derivation** — `android/app/build.gradle.kts` reads `../../VERSION` at configure time; versionCode = M\*1_000_000 + m\*10_000 + p\*100. `make bump-point` is the only thing that needs to touch it.
6. **Tracks** — Internal (auto), Closed/graywolf-beta (manual promote), Production (not yet enabled, requires explicit perm grant).
7. **Adding a new beta tester** — Play Console → Closed testing → graywolf-beta → Testers → add email. They open the opt-in URL.
8. **What to do if a CI release fails after the tag is pushed** — follow the existing CLAUDE.md retag flow; the Android workflow is idempotent on retag because Play uploads with the same versionCode are accepted only once, so a retag without a versionCode bump will fail the Play upload step. Fix: bump `VERSION` and re-tag with a new patch version.

- [ ] **Step 2: Commit**

```bash
git add docs/wiki/android-play-store.md
git commit -m "docs(wiki): add Android Play Store release page"
```

### Task 7.2: Link the new page from the wiki index

**Files:**
- Modify: `docs/wiki/README.md`

- [ ] **Step 1: Add a line under the "Pages" section**

After the existing `- [\`build-pipelines.md\`]...` line, add:
```markdown
- [`android-play-store.md`](android-play-store.md) -- Android release pipeline: signing, secrets, Play tracks, beta promotion.
```

- [ ] **Step 2: Update `docs/wiki/build-pipelines.md`**

Add a section "Android (Play Store)" with a one-paragraph summary and a pointer to `android-play-store.md` for the full story. The summary just establishes that Android exists as a build target so readers grepping `build-pipelines.md` for "android" land in the right place.

- [ ] **Step 3: Commit**

```bash
git add docs/wiki/README.md docs/wiki/build-pipelines.md
git commit -m "docs(wiki): link Android Play Store page"
```

---

## Self-review (run by the plan author, completed)

**Spec coverage:** Three user asks: (1) what to set up on Play Console — Phase 2 is the full checklist. (2) design doc for GH Actions pipeline — Phases 3-5 specify the pipeline, with file-level changes. (3) APK on the tablet with latest SDK — Phase 1 produces exactly this and lists it as the gate to Phase 2. (4) beta for ~15 people — Phase 6 (Closed Testing) is sized for this and addresses the personal-account 12/14 rule.

**Placeholder scan:** No "TBD", "TODO", or "implement later" in any task. All code blocks are concrete. Task 2.2's content section answers are spec'd per-question rather than "fill in the questionnaire". Task 7.1's wiki page outlines content (not a stub) but does not paste the prose — flagged as a write task with section-by-section guidance; acceptable because the prose draws from facts established in Phases 1-6 of this same plan.

**Type/name consistency:**
- Keystore env var names (`GRAYWOLF_KEYSTORE_BASE64`, `GRAYWOLF_KEYSTORE_PASSWORD`, `GRAYWOLF_KEY_ALIAS`, `GRAYWOLF_KEY_PASSWORD`) — consistent across Task 1.5 (Gradle reads them), Task 4.1 (GH secret names), Task 4.2 (workflow env block).
- Service account secret name (`GRAYWOLF_PLAY_SERVICE_ACCOUNT_JSON`) — consistent across Task 5.1 and Task 5.2.
- Package name `com.nw5w.graywolf` — matches the existing `applicationId` in `android/app/build.gradle.kts:14`.
- Closed track name `graywolf-beta` — consistent across Task 6.1 (Play Console) and Task 6.2 (workflow's `track:` value).

---

## Execution handoff

**Plan complete and saved to `docs/superpowers/plans/2026-05-21-android-play-store-pipeline.md`.**

Phase 1 produces the immediate value the user asked for (APK on the tablet with the latest SDK). Phases 2-7 are the longer arc; Phase 2 is human-only and can run in parallel with Phase 3 once Phase 1 is done.
