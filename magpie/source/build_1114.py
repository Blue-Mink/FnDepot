#!/usr/bin/env python3
"""Build magpie 1.1.14 FPK: staging copy (exclude pycache/dist) + fnpack + verify."""
import hashlib
import os
import re
import shutil
import subprocess
import sys
import tarfile
import tempfile

HERE = os.path.dirname(os.path.abspath(__file__))
FNPACK = os.environ.get("FNPACK_BIN", "fnpack")
VER = "1.1.14"
OUT = os.path.join(HERE, "dist", f"magpie-{VER}-fnos-amd64.fpk")


def die(msg):
    print(f"FAIL: {msg}")
    sys.exit(1)


def md5(p):
    h = hashlib.md5()
    with open(p, "rb") as f:
        for c in iter(lambda: f.read(1 << 20), b""):
            h.update(c)
    return h.hexdigest()


def sha256(p):
    h = hashlib.sha256()
    with open(p, "rb") as f:
        for c in iter(lambda: f.read(1 << 20), b""):
            h.update(c)
    return h.hexdigest()


# ---- 1. source sanity ----
man = open(os.path.join(HERE, "manifest"), encoding="utf-8").read()
assert re.search(r"^version\s+= 1\.1\.14$", man, re.M), "manifest version != 1.1.14"
assert "1.1.14：" in man, "changelog not updated for 1.1.14"
for f in ("wizard/install", "wizard/uninstall", "wizard/config",
          "app/ui/config", "app/ui/index.cgi", "app/magpie-gateway-proxy.py"):
    p = os.path.join(HERE, f)
    assert os.path.getsize(p) > 100, f"missing/too small: {f}"
import json
for f in ("wizard/install", "wizard/uninstall", "wizard/config", "app/ui/config"):
    json.load(open(os.path.join(HERE, f), encoding="utf-8"))
ui_cfg = json.load(open(os.path.join(HERE, "app/ui/config"), encoding="utf-8"))
assert set(ui_cfg[".url"]) == {"magpie.Application", "magpie.Gateway"}, ui_cfg[".url"].keys()
assert "magpie.Application" in man, "desktop_applaunchname must exist in .url"
main_sh = open(os.path.join(HERE, "cmd/main"), encoding="utf-8").read()
assert "export MAGPIE_NO_ACCOUNTS" not in main_sh, "launcher still forces accounts off"
print("source tree OK")

# ---- 2. staging copy ----
staging = tempfile.mkdtemp(prefix="magpie1114-")
ignore = shutil.ignore_patterns(".git", "dist", "build-records", "__pycache__",
                                "*.pyc", "magpie.fpk", "*.fpk")
shutil.copytree(HERE, os.path.join(staging, "tree"), ignore=ignore)
tree = os.path.join(staging, "tree")
for root, _, files in os.walk(tree):
    if os.path.basename(root) == "cmd":
        for f in files:
            os.chmod(os.path.join(root, f), 0o755)
os.chmod(os.path.join(tree, "app", "ui", "index.cgi"), 0o755)
# no pycache may leak
for root, dirs, _ in os.walk(tree):
    assert "__pycache__" not in dirs, f"pycache in staging: {root}"
print(f"staging OK: {tree}")

# ---- 3. fnpack build (output lands in cwd) ----
env = dict(os.environ)
env["PATH"] = "/usr/bin:/bin:" + env.get("PATH", "")
r = subprocess.run([FNPACK, "build", "-d", tree], cwd=staging,
                   capture_output=True, text=True, env=env)
print(r.stdout[-2000:])
if r.returncode != 0:
    print(r.stderr[-3000:])
    die("fnpack build failed")
built = None
for f in os.listdir(staging):
    if f.endswith(".fpk"):
        built = os.path.join(staging, f)
assert built, "no fpk produced"
print(f"built: {os.path.basename(built)} {os.path.getsize(built)}B")

# ---- 4. verify package ----
t = tarfile.open(built)
names = [m.name for m in t.getmembers()]
outer = [n for n in names if "/" not in n]
for need in ("manifest", "app.tgz", "ICON.PNG", "ICON_256.PNG"):
    assert need in outer, f"outer missing {need}: {outer}"
for need in ("cmd/main", "cmd/install_callback", "cmd/uninstall_callback",
             "cmd/config_callback", "cmd/upgrade_callback", "cmd/install_init",
             "cmd/config_init", "cmd/upgrade_init", "cmd/uninstall_init",
             "config/privilege", "config/resource",
             "wizard/install", "wizard/uninstall", "wizard/config"):
    assert need in names, f"missing member {need}"
pkg_man = t.extractfile("manifest").read().decode()
assert "version               = 1.1.14" in pkg_man
mchk = re.search(r"^checksum\s+= (\w+)$", pkg_man, re.M)
assert mchk, "no checksum in manifest"
app_tgz = t.extractfile("app.tgz")
h = hashlib.md5()
for c in iter(lambda: app_tgz.read(1 << 20), b""):
    h.update(c)
assert mchk.group(1) == h.hexdigest(), f"checksum {mchk.group(1)} != md5(app.tgz) {h.hexdigest()}"
app_tgz.seek(0)
a = tarfile.open(fileobj=app_tgz)
anames = [m.name for m in a.getmembers()]
for need in ("bin/magpie", "ui/config", "ui/index.cgi", "ui/images/icon_0.png",
             "ui/images/icon_64.png", "ui/images/icon_256.png", "ui/images/icon_512.png",
             "magpie-gateway-proxy.py"):
    assert need in anames, f"app.tgz missing {need}"
assert not any("pyc" in n or "__pycache__" in n for n in anames), "pyc leaked into app.tgz"
# wizard content round-trip
for wf in ("install", "uninstall", "config"):
    pkg_w = json.loads(t.extractfile(f"wizard/{wf}").read().decode())
    src_w = json.load(open(os.path.join(HERE, "wizard", wf), encoding="utf-8"))
    assert pkg_w == src_w, f"wizard/{wf} differs in package"
# shell scripts in package match source
for sf in ("main", "install_callback", "uninstall_callback", "config_callback"):
    pkg_s = t.extractfile(f"cmd/{sf}").read()
    src_s = open(os.path.join(HERE, "cmd", sf), "rb").read()
    assert pkg_s == src_s, f"cmd/{sf} differs in package"
print(f"package verify OK (checksum={mchk.group(1)[:12]}… = md5(app.tgz))")

# ---- 5. install ----
os.makedirs(os.path.dirname(OUT), exist_ok=True)
shutil.move(built, OUT)
with open(OUT + ".sha256", "w") as f:
    f.write(f"{sha256(OUT)}  magpie-{VER}-fnos-amd64.fpk\n")
print(f"DELIVER {OUT} sha256={sha256(OUT)}")
