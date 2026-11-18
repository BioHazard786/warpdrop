#!/usr/bin/env python3
"""Run a command under a pseudo-terminal (the old CLI's progress UI needs one); answer [Y/n] prompts with y."""
import os, pty, select, sys
pid, fd = pty.fork()
if pid == 0:
    os.execvp(sys.argv[1], sys.argv[1:])
buf = b""
while True:
    try:
        r, _, _ = select.select([fd], [], [], 30)
        if not r:
            continue
        data = os.read(fd, 65536)
    except OSError:
        break
    if not data:
        break
    sys.stdout.buffer.write(data); sys.stdout.buffer.flush()
    buf = (buf + data)[-200:]
    if b"[Y/n]" in buf:
        os.write(fd, b"y\n"); buf = b""
_, status = os.waitpid(pid, 0)
sys.exit(os.waitstatus_to_exitcode(status))
