# MIFS for Android

Native Kotlin and Jetpack Compose Android client in a separate project. The iOS app and music server are unchanged.
Targets Android 16 (API 36), with Android 8.0+ (API 26) compatibility, including the OnePlus 6T on Android 11.

## Design

The interface carries the iOS app's artwork-led aesthetic into Android: an immersive player, large synced
lyrics, rounded selection controls, album-colored Recent cards, and a restrained violet browsing theme.
Discover and Recent follow the system's light/dark appearance, with an override in Settings. Manrope is
bundled locally under the SIL Open Font License (`licenses/Manrope-OFL.txt`); no font download is needed.

Layouts respect edge-to-edge system insets and cutouts. The editor uses a side-by-side layout on wide
screens. Artwork colors and soft backdrops are computed locally and cached, including on Android 11;
the design does not depend on newer-only blur APIs. Touch feedback, screen transitions, live lyric
emphasis, and playback progress support the primary browsing and editing actions.

## Included

- Top Songs from the existing MIFS server and Spotify search through its API.
- Song preparation with download progress, failure handling, retry, and cancellation on navigation.
- Real waveform editor with draggable boundaries, whole-song positioning, 5/10/15/20-second presets,
  1–20-second limits, lyric selection, and range-limited playback.
- Android share sheet for server clip links; Recent saves moments and reopens their audio and lyrics.
- Open received links through **Recent → Open a mif link**, or share a link into MIFS from another app.
  Received links must use the configured server origin. Public verified App Links are not configured yet.
- Audio document import, real on-device waveform decoding, and AAC clip export through Android Media3.
  Imported audio stays on the device; exported clips use a FileProvider for sharing.
- Editable music server address in Settings, persistent Recent history, and editor range restoration on rotation.

## Build and launch

Standard setup: JDK 17, Android SDK platform 36 and build-tools 35.0.0. Open this directory in Android
Studio, or set `JAVA_HOME` and `ANDROID_HOME` and run `./gradlew :app:assembleDebug`.
The checked-in Gradle wrapper uses Gradle 8.13 with a verified distribution checksum.

On this Mac, the JDK, SDK, and Gradle cache are installed in the git-ignored `.tools/` directory.
To rebuild, install, connect the local server over USB, and launch:

```sh
cd android
./scripts/run-device.sh
# With multiple phones connected:
./scripts/run-device.sh 346029d8
```

Enable USB debugging and authorize the computer on the phone. Start the existing server separately
with `make -C server run` from the repository root if it isn't running already.

The default server is `http://127.0.0.1:8080`. `adb reverse tcp:8080 tcp:8080` makes the Mac's server
available at that address on the attached phone. Reconnect the mapping after unplugging or rebooting.
For Wi-Fi, enter the Mac's reachable LAN URL in Settings; for sharing links outside your network, use
a public server URL. **Localhost links only play on devices with the USB mapping; they are not public links.**
Local audio attachments can be shared without a public server.

APK: `app/build/outputs/apk/debug/app-debug.apk` (development signing).

## Checks

```sh
./gradlew :app:testDebugUnitTest :app:lintDebug
adb reverse tcp:8080 tcp:8080
./gradlew :app:connectedDebugAndroidTest
```

Validated on the physical OnePlus 6T (Android 11) and a Pixel 8 emulator (Android 16): three device tests
passed on each, alongside three selection unit tests and Android lint. Portrait, landscape, light, and
dark layouts were visually reviewed. Local screenshots are in the git-ignored `build/screenshots/` folder.

The device suite needs the local server and its `Neon Harbor` seed song. It exercises catalog → editor →
preview → Android share sheet → Recent → playback without sending to a recipient. Another test decodes a
generated tone and checks the duration of a real AAC export. A rotation test checks that a selected range
survives landscape and portrait transitions. Unit tests exercise range bounds and resizing.

Local import supports formats decoded by the device, from one second to two hours. Protected music cannot
be imported. Audio export may differ by one codec frame at its boundaries. iMessage extensions are
iOS-only; the Android client sends through installed apps using the system share sheet.

Build compatibility follows the [Android Gradle plugin 8.11 documentation](https://developer.android.com/build/releases/agp-8-11-0-release-notes).
Audio export follows the [Media3 Transformer APIs](https://developer.android.com/media/media3/transformer/getting-started).
