# WarpDrop

AirDrop, minus the walled garden. Send files device to device: browser, terminal, phone, desktop. Encrypted end to end, self-hostable, and the server never learns what you sent. It's not that it chooses not to look. It can't.

## Get it

| Where       | How                                                                                                                                                                                |
| ----------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| **Web**     | [drop.warplabs.info](https://drop.warplabs.info). Nothing to install, which is the whole point.                                                                                    |
| **CLI**     | `curl -fsSL drop.warplabs.info/install.sh \| bash`                                                                                                                                 |
|             | Windows: `scoop bucket add biohazard786 https://github.com/BioHazard786/scoop-bucket.git && scoop install biohazard786/warpdrop`                                                   |
|             | macOS: `brew tap BioHazard786/tap && brew install --cask warpdrop`                                                                                                                 |
| **Android** | `.apk` from the [latest release](https://github.com/BioHazard786/Warpdrop/releases/latest). Get `arm64-v8a` unless your phone predates TikTok.                                     |
| **Desktop** | Windows installer or Linux `.deb` / `.rpm` from the [latest release](https://github.com/BioHazard786/Warpdrop/releases/latest). Lives in your tray so nearby devices can find you. |

## Use it

**Same Wi-Fi?** Open WarpDrop on both devices, tap the other one, accept, check that the 6-digit codes match. Native apps (CLI, Android, desktop) don't even need the internet for this; they find each other over mDNS and talk directly.

**Anywhere else?** Send, get a code, hand it over:

```bash
warpdrop send report.pdf photos.zip
#   Code:  4821-amber-otter-kite-moth
#   Link:  https://drop.warplabs.info/r/4821#amber-otter-kite-moth

warpdrop receive 4821-amber-otter-kite-moth
```

Mix and match freely. Terminal to browser, phone to laptop, whatever. They all speak the same protocol.

## Why trust it

- **The secret never leaves your device.** The four words are the key. The server sees the room number and some ciphertext. Very boring for it.
- **Direct when possible, encrypted relay when not.** Strict NAT, corporate firewall: it falls back to a relay that only forwards sealed bytes.
- **No man in the middle.** Both sides prove they know the secret before a single file name moves. Guessing the room number gets you nothing.
- **Checksummed end to end.** Corrupt file? It aborts instead of handing it to you.
- **Resumes** from the last byte if the connection drops.
- **Up to 10 receivers** per code.

The fine print: anyone you give the full code to gets the files. That's not a bug, that's sharing.

## Things it won't do

- Folders: zip them.
- Over ~2,300 files in one go: zip them. Sensing a theme?
- Safari keeps the whole transfer in memory, so stay under ~1 GB there. Chrome, Edge and Firefox stream to disk and don't care.
- Nearby on guest Wi-Fi with client isolation. Use a code.

## Self-host

```bash
cp .env.example .env && docker compose up -d
```

One ~20 MB container, put any reverse proxy in front. Point the CLI at it once: `warpdrop config set server https://your.domain`. Everything else is in [DEPLOY.md](DEPLOY.md).

## Hack on it

`backend/` Go server · `web/` React app · `cli/` Go CLI · `apps/android/` · `apps/desktop/` · `protocol/` the [spec](protocol/PROTOCOL.md) everyone has to obey · `e2e/` Playwright.

CI runs every check; the commands are in [.github/workflows/ci.yml](.github/workflows/ci.yml). Releasing: bump [`VERSION`](VERSION), add the Android changelog under `apps/android/fastlane/metadata/android/en-US/changelogs/`, push the `v<VERSION>` tag, go make tea. One version for everything.

## Credits

Inspired by [FilePizza](https://github.com/kern/filepizza), [fs-cli](https://github.com/spectre10/fs-cli) and [croc](https://github.com/schollz/croc). Built to learn WebRTC and to avoid doing actual work.

The public instance runs on Oracle's free tier because I'm cheap. If it's down, you know what to do: [DEPLOY.md](DEPLOY.md).
