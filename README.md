# WarpDrop

Privacy-first, self-hostable file sharing, device to device. Like AirDrop, but for people who don't trust "The Cloud™️" or just want to feel like a hacker sending files from a terminal.

- **Web**: [warpdrop.qzz.io](https://warpdrop.qzz.io)
- **CLI**: `curl -fsSL install.warpdrop.qzz.io | bash`
  - **Scoop (Windows)**: `scoop bucket add biohazard786 https://github.com/BioHazard786/scoop-bucket.git && scoop install biohazard786/warpdrop`
  - **Brew (macOS)**: `brew tap BioHazard786/tap && brew install --cask warpdrop`
- **Self-hosting**: [DEPLOY.md](DEPLOY.md), one container and a reverse proxy.
- **Protocol**: [protocol/PROTOCOL.md](protocol/PROTOCOL.md)

---

## How it works

The sender gets a code like `4821-amber-otter-kite-moth`. The number is the room; the four words are a secret that is generated on the sender's device and **never sent to the server** (on the web it lives in the link's `#fragment`, which browsers don't transmit).

1. The two devices meet through the server's signaling and try a **direct WebRTC connection**.
2. If that fails within 8 seconds (strict NAT, corporate network), they fall back to an **encrypted relay** through the same server. Every frame is AES-256-GCM sealed, so the server only ever forwards ciphertext. No separate TURN server needed.
3. Before any file name is sent, both sides prove they know the secret words (X25519 key exchange, with a MAC bound to the connection's DTLS fingerprints). Someone who guesses the room number gets nothing, and a malicious server can't sit in the middle.
4. Files are checksummed (xxHash64) end to end. A mismatch aborts the download instead of handing you a corrupt file.
5. A dropped connection **resumes from the last byte** on whichever transport comes back.

One share code serves up to 10 receivers at once.

### What the server can and cannot see

| Can see | Cannot see |
|---|---|
| The room number, IP addresses, timing, how many bytes moved, device labels like "Chrome on Linux" | The secret words, file names, file contents |

That holds for the relay too. The one thing to know: this is end-to-end encryption between you and whoever has the code. Anyone you give the full code to can receive the files.

---

## Web and CLI work together

Send from a terminal, receive in a browser (or the reverse). Same protocol, same checks.

```bash
# send
warpdrop send report.pdf photos.zip
#   Code:  4821-amber-otter-kite-moth
#   Link:  https://warpdrop.qzz.io/r/4821#amber-otter-kite-moth

# receive (code or full link)
warpdrop receive 4821-amber-otter-kite-moth
warpdrop receive 4821-amber-otter-kite-moth --dir ~/Downloads --yes
```

Useful flags: `--keep-open` (keep serving after the first delivery), `--relay` (skip WebRTC), `--zip` (receive as one zip), `--server https://your.domain` (or `$WARPDROP_SERVER`).

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
docker compose up -d      # the app on 127.0.0.1:8080, the installer on 127.0.0.1:8000
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
installer/  install.sh and the tiny server that serves it
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

---

## Credits

Inspired by [FilePizza](https://file.pizza), [fs-cli](https://github.com/spectre10/fs-cli), and [croc](https://github.com/schollz/croc). Reimagined because I wanted to learn WebRTC and needed a project to procrastinate on my actual work.

The public demo runs on an Oracle Cloud Free Tier instance because I'm cheap. If it's down, host it yourself.
