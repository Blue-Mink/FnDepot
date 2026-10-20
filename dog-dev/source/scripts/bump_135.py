#!/usr/bin/env python3
"""把 FPK manifest 升到 1.3.5 并前置 changelog。带断言，重复执行会拒绝。"""
import re
import sys

NEW = (
    "1.3.5 应用内文案与实现对齐：① 说明页、错误页、证书安装脚本与系统代理脚本的品牌残留"
    "（GitHub++）统一改为 Dog-dev，Windows 证书卸载脚本改按真实证书名过滤（旧过滤条件匹配不到，"
    "等于卸不掉）；② 关于页「仅监听自身端口」不再把从未监听的 TLS 端口列为监听端口；"
    "③ kspeeder 下载链接指向最新发布版 0.8.2（与推荐引擎版本一致）；"
    "④ Docker 加速区分「已启用」与「已写入系统」：未点「一键应用」时状态显示「待应用」并给出说明，"
    "不再让人以为 docker pull 已经走加速器；⑤ 总览指标与 Docker 页的代理端口取实际配置，"
    "改过端口后提示地址仍然正确；⑥ DNS 优选说明与实现口径一致。"
)

path = sys.argv[1] if len(sys.argv) > 1 else "fpk/manifest"
s = open(path, encoding="utf-8").read()

if "version=1.3.5" in s:
    sys.exit(f"{path}: 已经是 1.3.5，跳过")

assert s.count("version=1.3.4\n") == 1, f"{path}: 未找到唯一的 version=1.3.4 行"
s = s.replace("version=1.3.4\n", "version=1.3.5\n", 1)

m = re.search(r"^changelog=(.*)$", s, re.M)
assert m, f"{path}: 未找到 changelog 行"
assert not m.group(1).startswith("1.3.5"), f"{path}: changelog 已含 1.3.5"
s = s[: m.start(1)] + NEW + " " + m.group(1) + s[m.end(1):]

open(path, "w", encoding="utf-8").write(s)
print(f"{path}: 升版 1.3.5 完成，changelog 前置 {len(NEW)} 字符")
