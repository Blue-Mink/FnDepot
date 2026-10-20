#!/usr/bin/env python3
"""把 FPK manifest 升到 1.3.8 并前置 changelog。带断言，重复执行会拒绝。"""
import re
import sys

NEW = (
    "1.3.8 修复重装后系统代理开着但本地 CA 未入系统信任、GitHub HTTPS 降级纯隧道直连超时："
    "卸载流程会清掉系统信任里的 CA（-clear-system-ca），而重装保留 config 的 "
    "system_proxy.enabled=true，两者不一致时新 shell 走代理访问 github.com/raw 会被降级成"
    "纯隧道直连（本机 IPv4 黑洞 → 超时）；现安装/升级回调新增 -ensure-system-ca 自动检测"
    "「系统代理已开启但 CA 未信任」并补装（幂等，系统代理未开启则跳过，失败不阻断安装）。"
)

path = sys.argv[1] if len(sys.argv) > 1 else "fpk/manifest"
s = open(path, encoding="utf-8").read()

if "version=1.3.8" in s:
    sys.exit(f"{path}: 已经是 1.3.8，跳过")

assert s.count("version=1.3.7\n") == 1, f"{path}: 未找到唯一的 version=1.3.7 行"
s = s.replace("version=1.3.7\n", "version=1.3.8\n", 1)

m = re.search(r"^changelog=(.*)$", s, re.M)
assert m, f"{path}: 未找到 changelog 行"
assert not m.group(1).startswith("1.3.8"), f"{path}: changelog 已含 1.3.8"
s = s[: m.start(1)] + NEW + " " + m.group(1) + s[m.end(1):]

open(path, "w", encoding="utf-8").write(s)
print(f"{path}: 升版 1.3.8 完成，changelog 前置 {len(NEW)} 字符")
