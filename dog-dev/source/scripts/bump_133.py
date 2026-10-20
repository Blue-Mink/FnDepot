#!/usr/bin/env python3
"""把 FPK manifest 升到 1.3.3 并前置 changelog。带断言，重复执行会拒绝。"""
import re
import sys

NEW = (
    "1.3.3 系统级代理修复：① 非 GitHub 域名改为透明透传（CONNECT 建隧道、HTTP 直接回源），"
    "此前一律 403，导致把 http(s)_proxy 指向本机后 pip/apt/浏览器访问国内站点直接失败；"
    "透传做三层护栏（仅本机与私网来源、仅 80/443、目标是 IP 时禁止内网/保留地址以防 SSRF）；"
    "② 镜像返回 Cloudflare 挑战页时判定该源失败并换下一个源（此前把挑战页当业务 404 透传，"
    "GitHub 网页打不开），网页请求在全源失败时回退直连官方；"
    "③ 新增 /proxy.pac（只让 GitHub 相关域名走代理、其余 DIRECT，浏览器与系统级客户端可用）"
    "与 /api/lan-access（Docker / git / curl / 嵌入式 URL 四类局域网接入片段）；"
    "④ 控制台「接入方式」改为 PAC、Git 只代理 GitHub、前缀下载、Docker 四项。"
)

path = sys.argv[1] if len(sys.argv) > 1 else "fpk/manifest"
s = open(path, encoding="utf-8").read()

if "version=1.3.3" in s:
    sys.exit(f"{path}: 已经是 1.3.3，跳过")

assert s.count("version=1.3.2\n") == 1, f"{path}: 未找到唯一的 version=1.3.2 行"
s = s.replace("version=1.3.2\n", "version=1.3.3\n", 1)

m = re.search(r"^changelog=(.*)$", s, re.M)
assert m, f"{path}: 未找到 changelog 行"
assert not m.group(1).startswith("1.3.3"), f"{path}: changelog 已含 1.3.3"
s = s[: m.start(1)] + NEW + " " + m.group(1) + s[m.end(1):]

open(path, "w", encoding="utf-8").write(s)
print(f"{path}: 升版 1.3.3 完成，changelog 前置 {len(NEW)} 字符")
