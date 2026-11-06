"""fnradio 接管/恢复 CLI（身份感知 + 链式共存版）。

用法：
  python3 takeover_cli.py status     # 打印 target/upstream/chain_upstream 身份（只读）
  python3 takeover_cli.py take       # 接管官方 socket；成功打印 FNRADIO_UPSTREAM_SOCK=...，冲突 exit 1
  python3 takeover_cli.py restore    # 恢复官方 socket（含链式恢复）；成功 exit 0，冲突 exit 1
"""
import os
import sys

import takeover


def status() -> int:
    for label, path in (("target", takeover.TARGET),
                        ("upstream", takeover.UPSTREAM),
                        ("chain_upstream", takeover.CHAIN_UPSTREAM)):
        s = takeover.snapshot(path)
        pid = s.get("pid")
        exe = s.get("exe") or ""
        print(f"{label}={path} kind={s['kind']} inode={s['inode']} pid={pid} exe={exe}")
    return 0


def main():
    cmd = sys.argv[1] if len(sys.argv) > 1 else "status"
    if cmd == "status":
        return status()
    if cmd == "take":
        try:
            r = takeover.takeover()
            print(f"takeover -> {r}")
            # 告知调用方 relay 上游该指向哪（链式→fnmusic-ext，正常→official）
            if r == "chained":
                print(f"FNRADIO_UPSTREAM_SOCK={takeover.CHAIN_UPSTREAM}")
            else:
                print(f"FNRADIO_UPSTREAM_SOCK={takeover.UPSTREAM}")
            return 0
        except takeover.Unsafe as e:
            print(f"takeover 失败（fail-closed）：{e}", file=sys.stderr)
            return 1
    if cmd == "restore":
        try:
            r = takeover.restore()
            print(f"restore -> {r}")
            return 0
        except takeover.Unsafe as e:
            print(f"restore 失败（fail-closed）：{e}", file=sys.stderr)
            return 1
    print(f"未知命令：{cmd}")
    return 2


if __name__ == "__main__":
    sys.exit(main())
