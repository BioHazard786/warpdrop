#!/usr/bin/env python3
"""Drive the REAL system Firefox (no Playwright) through a large download and watch its memory and the file on disk.

Playwright's patched Firefox handles downloads differently from stock Firefox, so this is the authoritative check for
"memory stays flat while streaming a large file".

  usage: firefox_real.py <share-link> <download-dir> [rss-limit-mb]

Start the server and a CLI sender first (see e2e/manual/README.md). Needs: python3, firefox. Uses Marionette only.
"""
import json, os, signal, socket, subprocess, sys, tempfile, time, shutil

link, dldir = sys.argv[1], sys.argv[2]
LIMIT = int(sys.argv[3]) if len(sys.argv) > 3 else 6000
os.makedirs(dldir, exist_ok=True)

profile = tempfile.mkdtemp(prefix="wd-ff-")
with open(os.path.join(profile, "user.js"), "w") as f:
    for k, v in {
        "marionette.port": 2829,
        "browser.download.folderList": 2,
        "browser.download.dir": dldir,
        "browser.download.useDownloadDir": True,
        "browser.download.always_ask_before_handling_new_types": False,
        "browser.shell.checkDefaultBrowser": False,
        "datareporting.policy.dataSubmissionEnabled": False,
        "media.peerconnection.ice.obfuscate_host_addresses": False,
    }.items():
        f.write(f"user_pref({json.dumps(k)}, {json.dumps(v).lower() if isinstance(v, bool) else json.dumps(v)});\n")

ff = subprocess.Popen(["firefox", "--headless", "--marionette", "--no-remote", "--new-instance", "--profile", profile],
                      stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)

def tree_rss_mb(root):
    out = subprocess.run(["ps", "-eo", "pid=,ppid=,rss="], capture_output=True, text=True).stdout.split("\n")
    rows = [tuple(map(int, l.split())) for l in out if l.strip()]
    kids, rss = {}, {}
    for pid, ppid, r in rows:
        kids.setdefault(ppid, []).append(pid); rss[pid] = r
    total, stack = 0, [root]
    while stack:
        p = stack.pop(); total += rss.get(p, 0); stack += kids.get(p, [])
    return total / 1024

class Marionette:
    def __init__(self):
        for _ in range(100):
            try:
                self.s = socket.create_connection(("127.0.0.1", 2829)); break
            except OSError:
                time.sleep(0.3)
        else:
            raise SystemExit("Firefox did not open its Marionette port")
        self.n = 0; self.buf = b""; self._read()  # handshake
    def _read(self):
        while b":" not in self.buf: self.buf += self.s.recv(65536)
        ln, rest = self.buf.split(b":", 1); ln = int(ln)
        while len(rest) < ln: rest += self.s.recv(65536)
        self.buf = rest[ln:]; return json.loads(rest[:ln])
    def cmd(self, name, params=None):
        self.n += 1; msg = json.dumps([0, self.n, name, params or {}]).encode()
        self.s.sendall(str(len(msg)).encode() + b":" + msg)
        while True:
            r = self._read()
            if r[0] == 1 and r[1] == self.n:
                if r[2]: raise RuntimeError(r[2]); return r[3]
                return r[3]
    def js(self, script):
        return self.cmd("WebDriver:ExecuteScript", {"script": script, "args": []})["value"]

def listing():
    return {n: os.path.getsize(os.path.join(dldir, n)) for n in os.listdir(dldir)}

try:
    m = Marionette()
    m.cmd("WebDriver:NewSession", {"capabilities": {}})
    m.cmd("WebDriver:Navigate", {"url": link})
    for _ in range(120):
        if m.js("return [...document.querySelectorAll('button')].some(b => b.textContent.includes('Accept and download'))"): break
        time.sleep(0.5)
    else:
        raise SystemExit("never saw the Accept button: " + m.js("return document.body.innerText.slice(0,300)"))
    base = tree_rss_mb(ff.pid); peak = base; t0 = time.time()
    print(f"offered; Firefox RSS baseline {base:.0f} MB", flush=True)
    m.js("[...document.querySelectorAll('button')].find(b => b.textContent.includes('Accept and download')).click()")
    last = 0
    while True:
        rss = tree_rss_mb(ff.pid); peak = max(peak, rss)
        txt = m.js("return document.querySelector('main').innerText")
        files = listing(); on_disk = max(files.values(), default=0) / 1048576
        if int(time.time() - t0) % 5 == 0 and int(time.time()-t0) != last:
            last = int(time.time() - t0)
            print(f"+{last:4d}s  RSS {rss:6.0f} MB  on disk {on_disk:7.0f} MB  {files and list(files)[0]}", flush=True)
        if "Received" in txt and "file" in txt: break
        if "Transfer failed" in txt: raise SystemExit("TRANSFER FAILED: " + txt[-200:])
        if rss > LIMIT: raise SystemExit(f"ABORTED: Firefox memory {rss:.0f} MB passed the {LIMIT} MB limit")
        if time.time() - t0 > 1500: raise SystemExit("timed out")
        time.sleep(1)
    time.sleep(3)
    done = listing()
    print(f"\nDONE in {time.time()-t0:.0f}s. RSS baseline {base:.0f} MB, PEAK {peak:.0f} MB (growth {peak-base:.0f} MB). Files: {done}")
finally:
    ff.send_signal(signal.SIGTERM)
    try: ff.wait(10)
    except Exception: ff.kill()
    shutil.rmtree(profile, ignore_errors=True)
