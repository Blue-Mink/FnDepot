#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""iStoreOS 网络预置（A1 方案，一次性 qemu 起机 + 串口注入 uci）

用法: istoreos-provision.py <disk.qcow2>

对目标磁盘做:
  1. qemu user-net 起机（KVM，串口走本机 TCP，不接触局域网）
  2. 等 shell → 注入: LAN 转 DHCP 客户端 / 关闭自身 DHCP 服务器与 RA/DHCPv6/NDP
  3. uci commit + 验证（回读确认）→ 关机
成功(看到 PROVOK)退出码 0，否则非 0。
"""
import socket
import subprocess
import sys
import time

DISK = sys.argv[1] if len(sys.argv) > 1 else "work.qcow2"
PORT = 54397
WORKDIR = "/tmp/istoreos-provision"
QEMU = ["qemu-system-x86_64",
        "-name", "isto-prov", "-enable-kvm", "-machine", "q35", "-m", "1024", "-smp", "2",
        "-drive", "if=pflash,format=raw,readonly=on,file=/usr/share/OVMF/OVMF_CODE.fd",
        "-drive", "if=pflash,format=raw,file={}/ovmf_vars.fd".format(WORKDIR),
        "-drive", "file={}".format(DISK),
        "-netdev", "user,id=n0", "-device", "virtio-net-pci,netdev=n0",
        "-serial", "tcp:127.0.0.1:{},server,nowait".format(PORT),
        "-display", "none"]

buf = b""
sock = None


def read_more(timeout=1.0):
    global buf
    sock.settimeout(timeout)
    try:
        d = sock.recv(4096)
        if d:
            buf += d
            return True
    except socket.timeout:
        return True
    except OSError:
        return False
    return False


def send(cmd):
    print("  >", cmd, flush=True)
    sock.sendall((cmd + "\n").encode())
    time.sleep(0.3)


def cap(cmd, timeout=60):
    """发命令并返回输出（提示符锚 ":~#" 之前的部分）"""
    global buf
    buf = b""
    send(cmd)
    end = time.monotonic() + timeout
    while time.monotonic() < end:
        i = buf.find(b":~#")
        if i >= 0:
            out, buf = buf[:i], b""
            lines = out.split(b"\r\n")
            return b"\n".join(lines[1:]).decode(errors="replace")
        read_more(0.5)
    out, buf = buf[:], b""
    return "TIMEOUT " + out.decode(errors="replace")[:200]


def boot():
    import os
    os.makedirs(WORKDIR, exist_ok=True)
    if not os.path.exists(WORKDIR + "/ovmf_vars.fd"):
        import shutil
        shutil.copyfile("/usr/share/OVMF/OVMF_VARS.fd", WORKDIR + "/ovmf_vars.fd")
    subprocess.Popen(QEMU, cwd=WORKDIR,
                     stdin=subprocess.DEVNULL, stdout=subprocess.DEVNULL,
                     stderr=subprocess.DEVNULL)


def cleanup():
    subprocess.run(["pkill", "-f", "[q]emu-system-x86_64.*isto-prov"],
                   stderr=subprocess.DEVNULL)
    time.sleep(1)


def connect():
    global sock
    for _ in range(90):
        try:
            sock = socket.create_connection(("127.0.0.1", PORT), 2)
            return
        except OSError:
            time.sleep(1)
    raise RuntimeError("serial port never opened")


def shell(timeout=240):
    """狂按回车直到拿到 root 提示符（GRUB 菜单收到回车也只是确认默认项）"""
    global buf
    end = time.monotonic() + timeout
    n = 0
    while time.monotonic() < end:
        try:
            sock.sendall(b"\r")
        except OSError:
            return False
        end2 = time.monotonic() + 6
        while time.monotonic() < end2:
            if b"root@" in buf:
                read_more(0.5)
                return True
            read_more(0.5)
        n += 1
        if n % 12 == 0:
            print("  ...waiting shell, tail:", buf[-160:].decode(errors="replace"), flush=True)
    return False


def main():
    cleanup()
    boot()
    try:
        connect()
    except RuntimeError as e:
        print("FAIL:", e)
        cleanup()
        return 2
    if not shell():
        print("FAIL: no shell")
        cleanup()
        return 2
    print("shell up; waiting for boot noise to settle...", flush=True)
    time.sleep(30)  # iStoreOS 首启 kmod 加载刷屏期
    cap("cat /etc/openwrt_release | head -3", 30)
    cmds = [
        "uci set network.lan.proto='dhcp'",
        "uci set network.lan.delegate='1'",
        "uci -q del network.lan.ipaddr",
        "uci -q del network.lan.netmask",
        "uci set dhcp.lan.ignore='1'",
        "uci set dhcp.lan.ra='disabled'",
        "uci set dhcp.lan.dhcpv6='disabled'",
        "uci set dhcp.lan.ndp='disabled'",
        "uci commit network",
        "uci commit dhcp",
    ]
    for c in cmds:
        cap(c, 30)
    out = cap("uci show network.lan.proto | grep -q dhcp && uci show dhcp.lan.ignore | grep -q 1 && echo PROVOK", 30)
    if "PROVOK" not in out:
        print("FAIL: provision verify ->", out[:200])
        cleanup()
        return 1
    cap("sync", 30)
    send("reboot -f")
    time.sleep(3)
    cleanup()
    print("PROVISION OK")
    return 0


if __name__ == "__main__":
    sys.exit(main())
