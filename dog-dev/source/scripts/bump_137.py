#!/usr/bin/env python3
"""把 FPK manifest 升到 1.3.7 并前置 changelog。带断言，重复执行会拒绝。"""
import re
import sys

NEW = (
    "1.3.7 修复 Docker 一键应用后拉取回退官方超时：daemon 配 "
    "registry-mirror=http://127.0.0.1:37710 后 /v2/ 请求的 Host 头是镜像站自身地址"
    "而非 docker.io，requestRegistryHost 把它误判成非 Docker Hub 注册表，上游链退化成"
    "[内置引擎, https://127.0.0.1]（命中飞牛本机 nginx 443→302→5667 证书校验失败），"
    "整次拉取回退官方直连超时；现 Host 为 IP 字面量（代理监听地址）时按 Docker Hub 处理。"
)

path = sys.argv[1] if len(sys.argv) > 1 else "fpk/manifest"
s = open(path, encoding="utf-8").read()

if "version=1.3.7" in s:
    sys.exit(f"{path}: 已经是 1.3.7，跳过")

assert s.count("version=1.3.6\n") == 1, f"{path}: 未找到唯一的 version=1.3.6 行"
s = s.replace("version=1.3.6\n", "version=1.3.7\n", 1)

m = re.search(r"^changelog=(.*)$", s, re.M)
assert m, f"{path}: 未找到 changelog 行"
assert not m.group(1).startswith("1.3.7"), f"{path}: changelog 已含 1.3.7"
s = s[: m.start(1)] + NEW + " " + m.group(1) + s[m.end(1):]

open(path, "w", encoding="utf-8").write(s)
print(f"{path}: 升版 1.3.7 完成，changelog 前置 {len(NEW)} 字符")
