# Manual checks that need a real browser

## Large downloads in real Firefox (`firefox_real.py`)

Playwright's patched Firefox handles downloads differently from stock Firefox (it holds them in memory), so it cannot
tell you whether memory stays flat. This script drives your **installed Firefox** over its Marionette protocol instead,
samples its memory, and watches the file grow on disk.

```bash
cd e2e && (bun install && bun x playwright test --list >/dev/null)   # builds nothing; see below
# 1. build once and start a server (the same binaries the E2E suite uses)
bun x playwright test --project=chromium -g "single file" >/dev/null  # global-setup builds e2e/.bin/{server,warpdrop}
PORT=18095 RATE_LIMIT_SCALE=100 ./.bin/server &
# 2. a CLI sender with a big file (use a real disk, not a RAM-backed /tmp)
head -c 5368709120 /dev/urandom > ~/big.bin
WARPDROP_SERVER=http://127.0.0.1:18095 ./.bin/warpdrop send ~/big.bin     # prints "Link: http://127.0.0.1:18095/r/NNNN#words"
# 3. receive it in real Firefox, aborting if its memory passes 5500 MB
python3 manual/firefox_real.py '<the link>' ~/dl 5500
sha256sum ~/big.bin ~/dl/big.bin
```

Result on Firefox 160, 5 GB over a direct connection: 146 s, memory baseline 1312 MB and peak 1925 MB, file written to
disk as it arrived, hashes identical.

## Why the Firefox service-worker keepalive matters

Stock StreamSaver only starts its service-worker heartbeat for non-transferable streams. Firefox supports transferable
streams, so the heartbeat never ran and Firefox terminated the idle worker ~30-60 s into any download, freezing it.
`web/public/mitm.js` now starts the keepalive unconditionally. The E2E test "a download that outlives Firefox's service
worker idle timeout still completes" guards this.
