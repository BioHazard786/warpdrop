# WarpDrop for Android

A Kotlin + Jetpack Compose app (Material 3 Expressive, Material You and AMOLED themes, Google Sans Flex) on top of
the **same Go protocol the CLI uses** (`cli/internal/protocol`, `cli/internal/transfer`), so there is one Go
implementation of the protocol for the CLI and the app (the web app has the TypeScript one).

```
apps/android/app/ UI (ui/), AppController, Go-backed WarpCore (GoWarpCore.kt), MediaStore sink, foreground service
cli/mobile/       the gomobile-friendly Go API the app calls (files cross as file descriptors, never as bytes)
```

Other platforms get their own native UI over the same Go core (gomobile builds for iOS/macOS too); the UI is not
shared across operating systems.

## Nearby

The Send tab lists devices on the same network (Quick Share style) and Receive has the "visible" switch. Devices come
from two places that the Go core merges: the server's list and the local network. Android's `NsdManager` does the
mDNS (`GoNearby.kt`) and feeds the core through `AddLANPeer`; the announced TCP port comes from `ListenLAN`. Direct
connections work with no internet. The app is only visible and listening while it is on screen. On Android 16+ it asks
for the local-network permission (`ACCESS_LOCAL_NETWORK`).

## Build

```bash
android sdk install ndk/30.0.16248370 platforms/android-37.2    # once
go install golang.org/x/mobile/cmd/gomobile@latest              # once
make core                       # builds app/libs/warpcore.aar from ../../cli/mobile (needs the NDK)
./gradlew :app:assembleDebug
./gradlew :app:testDebugUnitTest :app:lintDebug
```

`make core` passes `-checklinkname=0` because pion's `anet` dependency uses a `go:linkname` that newer Go restricts.

## Try it against a local server

```bash
(cd ../../backend && go run ./cmd/server)           # :8080
adb reverse tcp:8080 tcp:8080                    # the phone reaches the laptop as 127.0.0.1:8080
```
Then Settings → Server → `http://127.0.0.1:8080` in the app. Go sockets are not subject to Android's cleartext
policy, so plain `http://` self-hosted servers work. "Always use relay" forces the encrypted relay path.

## Notes

- **Share links open the app** only when the server serves `/.well-known/assetlinks.json`: set `ANDROID_CERT_SHA256`
  (see `DEPLOY.md`) to the fingerprint of the key that signs the APK. The manifest verifies `drop.warplabs.info`;
  a self-hosted domain needs its own build.
- Received files are written as pending MediaStore Downloads and published only after the checksum matched.
- Icons are Material Symbols Rounded (Apache 2.0) saved as vector drawables in `app/src/main/res/drawable`,
  so the app needs no icon library.
