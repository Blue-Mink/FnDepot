#!/usr/bin/env python3
"""Build magpie 1.1.14 arm64 FPK: swap app/bin/magpie to arm64 binary + platform=arm + fnpack + verify."""
import hashlib
import json
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
ARCH = "arm64"
ARM64_BIN = "/tmp/magpie-arm64-1635"
ARM64_SHA = "a1553bfc2bdc6bda3c68437ccb37fe4be525507a2db2095d570abfe31449348c"
OUT = os.path.join(HERE, "dist", f"magpie-{VER}-fnos-{ARCH}.fpk")


def die(msg):
    print(f"FAIL: {msg}")
    sys.exit(1)


def sha256(p):
    h = hashlib.sha256()
    with open(p, "rb") as f:
        for c in iter(lambda: f.read(1 << 20), b""):
            h.update(c)
    return h.hexdigest()


def sha256_bytes(b):
    return hashlib.sha256(b).hexdigest()


# ---- 0. arm64 binary sanity ----
arm64_bin = open(ARM64_BIN, "rb").read()
assert sha256_bytes(arm64_bin) == ARM64_SHA, f"arm64 binary mismatch: {sha256_bytes(arm64_bin)}"
print(f"arm64 binary OK: {len(arm64_bin)}B sha256={ARM64_SHA[:12]}…")

# ---- 1. source sanity ----
man = open(os.path.join(HERE, "manifest"), encoding="utf-8").read()
assert re.search(r"^version\s+= 1\.1\.14$", man, re.M), "manifest version != 1.1.14"
assert "1.1.14：" in man, "changelog not updated for 1.1.14"
for f in ("wizard/install", "wizard/uninstall", "wizard/config",
          "app/ui/config", "app/ui/index.cgi", "app/magpie-gateway-proxy.py"):
    p = os.path.join(HERE, f)
    assert os.path.getsize(p) > 100, f"missing/too small: {f}"
for f in ("wizard/install", "wizard/uninstall", "wizard/config", "app/ui/config"):
    json.load(open(os.path.join(HERE, f), encoding="utf-8"))
ui_cfg = json.load(open(os.path.join(HERE, "app/ui/config"), encoding="utf-8"))
assert set(ui_cfg[".url"]) == {"magpie.Application", "magpie.Gateway"}, ui_cfg[".url"].keys()
main_sh = open(os.path.join(HERE, "cmd/main"), encoding="utf-8").read()
assert "export MAGPIE_NO_ACCOUNTS" not in main_sh, "launcher still forces accounts off"
print("source tree OK")

# ---- 2. staging copy (swap binary + platform=arm) ----
staging = tempfile.mkdtemp(prefix="magpie1114arm-")
ignore = shutil.ignore_patterns(".git", "dist", "build-records", "__pycache__",
                                "*.pyc", "magpie.fpk", "*.fpk")
shutil.copytree(HERE, os.path.join(staging, "tree"), ignore=ignore)
tree = os.path.join(staging, "tree")
# swap binary
bin_path = os.path.join(tree, "app", "bin", "magpie")
open(bin_path, "wb").write(arm64_bin)
os.chmod(bin_path, 0o755)
# 修正 manifest platform = x86 → arm（1.1.11/1.1.12 arm64 包曾遗漏）
man_path = os.path.join(tree, "manifest")
man_txt = open(man_path, encoding="utf-8").read()
assert "platform              = x86" in man_txt, "manifest platform 锚失配"
man_txt = man_txt.replace("platform              = x86", "platform              = arm", 1)
open(man_path, "w", encoding="utf-8").write(man_txt)
for root, _, files in os.walk(tree):
    if os.path.basename(root) == "cmd":
        for f in files:
            os.chmod(os.path.join(root, f), 0o755)
os.chmod(os.path.join(tree, "app", "ui", "index.cgi"), 0o755)
for root, dirs, _ in os.walk(tree):
    assert "__pycache__" not in dirs, f"pycache in staging: {root}"
print(f"staging OK (platform=arm): {tree}")

# ---- 3. fnpack build ----
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

# ---- 4. verify ----
t = tarfile.open(built)
names = [m.name for m in t.getmembers()]
pkg_man = t.extractfile("manifest").read().decode()
assert "version               = 1.1.14" in pkg_man
assert "platform              = arm" in pkg_man, "packaged manifest platform != arm"
mchk = re.search(r"^checksum\s+= (\w+)$", pkg_man, re.M)
assert mchk, "no checksum"
app_tgz = t.extractfile("app.tgz")
h = hashlib.md5()
for c in iter(lambda: app_tgz.read(1 << 20), b""):
    h.update(c)
assert mchk.group(1) == h.hexdigest(), "checksum mismatch"
app_tgz.seek(0)
a = tarfile.open(fileobj=app_tgz)
for m in a.getmembers():
    if m.name == "bin/magpie":
        b = a.extractfile(m).read()
        assert sha256_bytes(b) == ARM64_SHA, f"packaged binary not arm64: {sha256_bytes(b)}"
        break
else:
    die("bin/magpie not in app.tgz")
print(f"package verify OK (arm64 binary sha={sha256_bytes(b)[:12]}…, checksum={mchk.group(1)[:12]}…)")

# ---- 5. install ----
os.makedirs(os.path.dirname(OUT), exist_ok=True)
shutil.move(built, OUT)
with open(OUT + ".sha256", "w") as f:
    f.write(f"{sha256(OUT)}  magpie-{VER}-fnos-{ARCH}.fpk\n")
print(f"DELIVER {OUT} sha256={sha256(OUT)}")
