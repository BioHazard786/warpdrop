# WarpDrop

Privacy-first, self-hostable file sharing, device to device. Like AirDrop, but for people who don't trust "The Cloud™️" or just want to feel like a hacker sending files from a terminal.

- **Web**: [drop.warplabs.info](https://drop.warplabs.info)
- **CLI**: `curl -fsSL drop.warplabs.info/install.sh | bash`
  - **Scoop (Windows)**: `scoop bucket add biohazard786 https://github.com/BioHazard786/scoop-bucket.git && scoop install biohazard786/warpdrop`
  - **Brew (macOS)**: `brew tap BioHazard786/tap && brew install --cask warpdrop`
- **Android**: [apps/android/](apps/android/README.md) (build from source for now)
- **Self-hosting**: [DEPLOY.md](DEPLOY.md), one container and a reverse proxy.
- **Protocol**: [protocol/PROTOCOL.md](protocol/PROTOCOL.md)

---

## How it works

**Two ways to start a transfer**: share a code or link (works across any networks, described below), or pick a device from the **Nearby** list (same network, no code: open WarpDrop on both devices, pick one, the receiver accepts, and both screens show a matching 6-digit code). On the CLI: `warpdrop send --nearby file` / `warpdrop receive --nearby`. Between native clients (CLI, Android, desktop) devices on the same Wi-Fi also find each other over mDNS and connect directly over TCP, with no server and no internet needed; the Nearby list merges both sources. If a device does not show up (guest Wi-Fi with client isolation, mDNS blocked), use a share code. The first listen may trigger a firewall prompt on Windows and macOS; on Linux the receiver needs an inbound TCP port in 1025-65535 open. Details in [protocol/PROTOCOL.md](protocol/PROTOCOL.md#7-nearby-same-network-no-share-code).

The sender gets a code like `4821-amber-otter-kite-moth`. The number is the room; the four words are a secret that is generated on the sender's device and **never sent to the server** (on the web it lives in the link's `#fragment`, which browsers don't transmit).

1. The two devices meet through the server's signaling and try a **direct WebRTC connection**.
2. If that fails within 8 seconds (strict NAT, corporate network), they fall back to an **encrypted relay** through the same server. Every frame is AES-256-GCM sealed, so the server only ever forwards ciphertext. No separate TURN server needed.
3. Before any file name is sent, both sides prove they know the secret words (X25519 key exchange, with a MAC bound to the connection's DTLS fingerprints). Someone who guesses the room number gets nothing, and a malicious server can't sit in the middle.
4. Files are checksummed (xxHash64) end to end. A mismatch aborts the download instead of handing you a corrupt file.
5. A dropped connection **resumes from the last byte** on whichever transport comes back.

One share code serves up to 10 receivers at once.

### What the server can and cannot see

| Can see                                                                                           | Cannot see                                  |
| ------------------------------------------------------------------------------------------------- | ------------------------------------------- |
| The room number, IP addresses, timing, how many bytes moved, device labels like "Chrome on Linux" | The secret words, file names, file contents |

That holds for the relay too. The one thing to know: this is end-to-end encryption between you and whoever has the code. Anyone you give the full code to can receive the files.

---

## Web and CLI work together

Send from a terminal, receive in a browser (or the reverse). Same protocol, same checks.

```bash
# send
warpdrop send report.pdf photos.zip
#   Code:  4821-amber-otter-kite-moth
#   Link:  https://drop.warplabs.info/r/4821#amber-otter-kite-moth

# receive (code or full link)
warpdrop receive 4821-amber-otter-kite-moth
warpdrop receive 4821-amber-otter-kite-moth --dir ~/Downloads --yes
```

Useful flags: `--keep-open` (keep serving after the first delivery), `--relay` (skip WebRTC), `--zip` (receive as one zip), `--server https://your.domain` (or `$WARPDROP_SERVER`). Self-hosting? Save the server once with `warpdrop config set server https://your.domain` (`config get` shows what is in effect and why; flag beats environment beats saved beats default).

The CLI never overwrites an existing file (it saves `name (1).ext`), writes to `name.part` until the checksum matches, strips path tricks and control characters from names, and cleans up its partial files on Ctrl-C (exit code 130).

### Limits and browser notes

- A transfer can hold about **2,300 files** (the file list is one message). Zip bigger sets.
- Folders aren't supported directly. Zip them first.
- **Chrome, Edge and Firefox** stream straight to disk, so size is limited by your disk, not your RAM (a 5 GB file was verified in Firefox with flat memory).
- **Safari** can't stream to disk, so it buffers the whole transfer in memory. Keep it under about 1 GB, or use the CLI.
- Several files arrive as one zip (ZIP64, so more than 4 GB is fine).

### Upgrading from v1

v2 is **not compatible** with v1 clients or servers. Update the CLI and redeploy the server together. The CLI flags `--domain`, `--stun`, `--turn`, `--turn-user` and `--turn-pass` are gone: use `--server`, and the server tells clients which STUN/TURN to use.

---

## Self-hosting

```bash
cp .env.example .env      # everything is optional
docker compose up -d      # the app on 127.0.0.1:8080
```

Put your own Caddy or Nginx in front (ready-to-use samples in [`deploy/`](deploy/)). The image is a ~20 MB static binary with the web app embedded, runs as a non-root user, and needs no build arguments or per-environment rebuilds. TURN is optional (`docker compose --profile turn up -d`); most people never need it because the built-in relay covers strict networks. Details, every setting, and troubleshooting are in [DEPLOY.md](DEPLOY.md).

---

## Development

```
backend/    Go: signaling, relay, /api/config, embedded web app (one binary)
web/        Vite + React 19 + TanStack Router + shadcn (Base UI) + Tailwind v4
cli/        Go CLI (pion WebRTC)
protocol/   PROTOCOL.md plus the fixtures both web and CLI are tested against
e2e/        Playwright: web to web, CLI to web, relay, fault injection
```

```bash
# backend + web with hot reload
(cd backend && go run ./cmd/server)            # :8080
(cd web && bun install && bun run dev)         # proxies /ws /relay /api to :8080

# checks (all are run by CI)
(cd backend && go vet ./... && go test -race ./...)
(cd cli     && go vet ./... && go test -race ./...)
(cd web     && bun run lint && bun run fmt:check && bun x tsc -b && bun x vitest run)
(cd e2e     && bun install && bun x playwright install chromium firefox && bun x playwright test)
```

Web and CLI crypto are checked against the same known-answer vectors (`protocol/fixtures/`, regenerated by `web/scripts/gen-fixtures.mjs`), so the two implementations cannot drift apart. The CLI-to-CLI throughput comparison against the old multi-channel design is in [`cli/bench/`](cli/bench/README.md).

### Releases

One version for everything (CLI, server image, Android, desktop), kept in [`VERSION`](VERSION). To release: edit
`VERSION`, add `apps/android/fastlane/metadata/android/en-US/changelogs/<versionCode>.txt` (the code is `M*10000 + m*100 + p`,
so 2.1.0 is `20100`), commit, then push the tag `v<VERSION>`. The workflow refuses a tag that differs from `VERSION`.
It publishes the CLI archives and the server image, plus the signed Android APKs (one per ABI), the Linux `.deb`/`.rpm`
and the Windows installer, all on one GitHub release.

---

## Credits

Inspired by [FilePizza](https://github.com/kern/filepizza), [fs-cli](https://github.com/spectre10/fs-cli), and [croc](https://github.com/schollz/croc). Reimagined because I wanted to learn WebRTC and needed a project to procrastinate on my actual work.

The public demo runs on an Oracle Cloud Free Tier instance because I'm cheap. If it's down, host it yourself.
