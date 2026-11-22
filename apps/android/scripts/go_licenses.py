#!/usr/bin/env python3
"""Writes AboutLibraries entries for the Go code compiled into warpcore.aar, so the in-app licenses page lists it too.

Usage: go_licenses.py <cli module dir> <aboutlibraries config dir>   (run by `make core`)
Each Go module gets its own license entry holding its exact LICENSE file: MIT and BSD require the copyright notice.
"""
import json
import os
import pathlib
import re
import subprocess
import sys

cli, out = sys.argv[1], pathlib.Path(sys.argv[2])
env = {**os.environ, "GOOS": "android", "GOARCH": "arm64", "CGO_ENABLED": "1"}  # the dependency set gomobile builds


def go(*args):
    return subprocess.check_output(["go", *args], cwd=cli, env=env, text=True).strip()


mods = set(go("list", "-deps", "-f", "{{with .Module}}{{if not .Main}}{{.Path}} {{.Version}} {{.Dir}}{{end}}{{end}}", "./mobile").splitlines())
mods.add(go("list", "-m", "-f", "{{.Path}} {{.Version}} {{.Dir}}", "golang.org/x/mobile"))  # gomobile's bind glue
mods.add(f"go {re.match(r'go([0-9.]+)', go('env', 'GOVERSION'))[1]} {go('env', 'GOROOT')}")  # the runtime and stdlib; distro builds add suffixes


def kind(text, path):
    if "Permission is hereby granted" in text:
        return "MIT", "MIT License"
    if "Neither the name" in text or "names of its contributors" in text:
        return "BSD-3-Clause", "BSD 3-Clause License"
    if "Redistribution and use" in text:
        return "BSD-2-Clause", "BSD 2-Clause License"
    sys.exit(f"go_licenses: can't identify the license of {path}; add it to kind()")


for sub in ("libraries", "licenses"):
    (out / sub).mkdir(parents=True, exist_ok=True)
    for old in (out / sub).glob("go_*.json"):
        old.unlink()

for line in sorted(mods):
    path, version, d = line.split(" ", 2)
    dirs = [pathlib.Path(d)] + ([pathlib.Path("/usr/share/licenses/golang")] if path == "go" else [])  # distro Go keeps it here
    lic = next((p for x in dirs if x.is_dir() for p in sorted(x.iterdir()) if re.match(r"(?i)^(license|copying)", p.name)), None)
    if lic is None:
        sys.exit(f"go_licenses: no LICENSE file in {d}")
    text = lic.read_text()
    spdx, name = kind(text, path)
    slug = "go_" + re.sub(r"[^A-Za-z0-9]+", "_", path)
    site = "https://go.dev" if path == "go" else f"https://pkg.go.dev/{path}"
    (out / "licenses" / f"{slug}.json").write_text(json.dumps(
        {"hash": slug, "name": name, "spdxId": spdx, "url": f"{site}?tab=licenses" if path != "go" else "https://go.dev/LICENSE", "content": text},
        indent=2) + "\n")
    (out / "libraries" / f"{slug}.json").write_text(json.dumps(
        {"uniqueId": f"go:{path}", "name": "Go standard library" if path == "go" else path, "artifactVersion": version.removeprefix("v"),
         "description": "", "website": site, "developers": [], "tag": "go", "licenses": [slug]},
        indent=2) + "\n")

print(f"go_licenses: wrote {len(mods)} Go modules to {out}")
