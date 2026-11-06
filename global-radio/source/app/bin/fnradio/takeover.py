"""
fnradio socket 接管/恢复（身份感知 fail-closed 版）。

核心：任何 mv/unlink 之前，先用 SO_PEERCRED + /proc/<pid>/exe/cmdline 识别
socket 归属（官方 trim-music / 自己 radio_relay / 外来进程 / stale 空文件），
只在确认安全时才动作；发现外来接管（如飞牛音源扩展 fnmusic-ext）一律
fail-closed 拒绝，绝不 unlink/mv 别人的 socket。

身份判定只依赖内核（SO_PEERCRED 拿到监听者 pid → /proc/pid 拿 exe/cmdline），
不信任任何 socket 回包（"no business response is an identity"）。
"""
import ctypes
import errno
import json
import os
import socket
import stat
import struct
from pathlib import Path


class Unsafe(RuntimeError):
    pass


TARGET = "/var/run/trim_music.socket"
UPSTREAM = "/var/run/trim_music_upstream.socket"
# 链式代理中间层：fnmusic-ext 的 proxy 被 rename 到这里，作为 fnradio relay 的
# 上游（上游 = fnmusic-ext，fnmusic-ext 的上游仍是 official = UPSTREAM）。
CHAIN_UPSTREAM = "/var/run/fnradio_upstream.socket"
RADIO_MARKER = "radio_relay"   # 我们中继进程 cmdline 里的模块名
MUSIC_EXT_MARKER = "fnmusic-ext"


def _renameat2_noreplace(src: str, dst: str) -> None:
    libc = ctypes.CDLL(None, use_errno=True)
    rename = getattr(libc, "renameat2", None)
    if rename is None:
        raise Unsafe("renameat2 unavailable")
    if rename(-100, os.fsencode(src), -100, os.fsencode(dst), 1) != 0:
        raise Unsafe(f"renameat2 noreplace refused (errno={ctypes.get_errno()})")


def _inode(path: str):
    try:
        st = os.lstat(path)
    except FileNotFoundError:
        return None
    return (st.st_dev, st.st_ino)


def _is_socket(path: str) -> bool:
    try:
        return stat.S_ISSOCK(os.lstat(path).st_mode)
    except FileNotFoundError:
        return False


def _peer(path: str, timeout: float = 0.5):
    """connect() 后用 SO_PEERCRED 拿到监听者 pid；异常向上抛。"""
    s = socket.socket(socket.AF_UNIX)
    s.settimeout(timeout)
    try:
        s.connect(str(path))
        pid, _, _ = struct.unpack("3i", s.getsockopt(socket.SOL_SOCKET, socket.SO_PEERCRED, 12))
        return pid
    finally:
        s.close()


def _proc(pid: int):
    try:
        exe = os.readlink(f"/proc/{pid}/exe")
    except OSError:
        exe = ""
    try:
        cmdline = Path(f"/proc/{pid}/cmdline").read_bytes().replace(b"\0", b" ").decode("utf-8", "replace")
    except OSError:
        cmdline = ""
    return exe, cmdline


def _probe_service(path: str, timeout: float = 1.0):
    """连接 unix socket 发 GET /_ext/livez，返回 service 字段（识别 fnmusic-ext）。

    返回 None 表示连接失败 / 无该端点 / 非 JSON；只用内核已验过的 socket 上
    的独立应答来补充归类，绝不据此越过内核身份判定。
    """
    try:
        s = socket.socket(socket.AF_UNIX)
        s.settimeout(timeout)
        s.connect(str(path))
        s.sendall(b"GET /_ext/livez HTTP/1.1\r\nHost: unix\r\nConnection: close\r\n\r\n")
        data = b""
        while True:
            try:
                chunk = s.recv(4096)
            except socket.timeout:
                break
            if not chunk:
                break
            data += chunk
        s.close()
        head, _, body = data.partition(b"\r\n\r\n")
        if not body:
            return None
        return json.loads(body.decode("utf-8", "replace")).get("service")
    except Exception:
        return None


def snapshot(path: str):
    """识别 socket 归属，返回 {kind, inode, pid, exe, cmdline}。

    kind ∈ absent / stale / official / fnradio / foreign / unknown
    """
    ino = _inode(path)
    if ino is None:
        return {"kind": "absent", "inode": None}
    if not _is_socket(path):
        return {"kind": "unknown", "inode": ino}
    try:
        pid = _peer(path)
    except OSError as exc:
        # 连接被拒（无监听）且 inode 未变 → 残留空文件
        if exc.errno in (errno.ECONNREFUSED, errno.ENOENT) and _inode(path) == ino:
            return {"kind": "stale", "inode": ino}
        return {"kind": "unknown", "inode": ino}
    exe, cmdline = _proc(pid)
    name = os.path.basename(exe)
    # 官方 trim-music：优先看内核 exe 名，其次 cmdline（exe 可能因权限读不到，
    # 但 cmdline 通常可读；官方二进制名带连字符 trim-music，与中继的 trim_music 下划线不同）
    if name == "trim-music" or "trim-music" in cmdline:
        kind = "official"
    elif RADIO_MARKER in cmdline:
        kind = "fnradio"
    elif _probe_service(path) == MUSIC_EXT_MARKER:
        # 飞牛音源扩展 fnmusic-ext 的 proxy（/_ext/livez 报 service=fnmusic-ext）
        kind = "fnmusic-ext"
    else:
        kind = "foreign"
    return {"kind": kind, "inode": ino, "pid": pid, "exe": exe, "cmdline": cmdline}


def _unlink_stale(path: str, snap: dict) -> None:
    """只删「证明确实无监听」的 stale 文件；二次核验 inode 不变后才 unlink。"""
    if snap["kind"] != "stale":
        raise Unsafe(f"{path} is not stale; refuse unlink")
    if _inode(path) != snap["inode"] or snapshot(path)["kind"] != "stale":
        raise Unsafe(f"{path} changed before unlink")
    os.unlink(path)


def _reclaim_orphan_upstream(target_path: str, target_snap: dict, upstream_path: str, upstream_snap: dict) -> None:
    """回收「官方重启后留在 upstream 的孤儿监听者」。

    前提：target 仍是同一个官方（当前真正服务客户端的那个），upstream 是重启前
    的旧官方进程（无客户端连它）。只 unlink 路径（旧进程仍持 inode），随后
    target 官方会被 renameat2 到 upstream。
    """
    if upstream_snap["kind"] != "official":
        raise Unsafe("upstream orphan is not official; refuse reclaim")
    if snapshot(target_path) != target_snap or _inode(target_path) != target_snap["inode"]:
        raise Unsafe("target changed before reclaiming orphan upstream")
    if _inode(upstream_path) != upstream_snap["inode"]:
        raise Unsafe("upstream changed before reclaim")
    os.unlink(upstream_path)


def takeover(target: str = TARGET, upstream: str = UPSTREAM,
             chain_upstream: str = CHAIN_UPSTREAM) -> str:
    """接管官方 socket（改名为 upstream），供中继绑定原路径。fail-closed。

    链式共存：target 已被 fnmusic-ext 接管时，把 fnmusic-ext 改名到 chain_upstream、
    自己绑 target，relay 上游指向 chain_upstream（fnmusic-ext → official 透传），
    两个应用功能同时生效。
    """
    target = str(target)
    upstream = str(upstream)
    chain_upstream = str(chain_upstream)
    t = snapshot(target)
    u = snapshot(upstream)

    # 1) 已经是自己的中继 → 幂等跳过（区分正常 / 链式）
    if t["kind"] == "fnradio":
        return "chained" if snapshot(chain_upstream)["kind"] == "fnmusic-ext" else "already"

    # 2) target 被 fnmusic-ext 接管 → 链式接管（二者共存，不拒绝）
    if t["kind"] == "fnmusic-ext":
        c = snapshot(chain_upstream)
        if c["kind"] == "stale":
            _unlink_stale(chain_upstream, c)
            c = snapshot(chain_upstream)
        if c["kind"] != "absent":
            raise Unsafe(
                f"chain upstream occupied (kind={c['kind']}, pid={c.get('pid')}); "
                f"refuse chain takeover"
            )
        _renameat2_noreplace(target, chain_upstream)
        return "chained"

    # 3) target 被其他外来进程占用 → 冲突，拒绝
    if t["kind"] == "foreign":
        raise Unsafe(
            f"target owned by foreign process (pid={t.get('pid')}, exe={t.get('exe')}); "
            f"refuse takeover — 检测到未知应用已接管音乐 socket"
        )

    # 3) target 是 stale 空文件 → 回收后再看
    if t["kind"] == "stale":
        _unlink_stale(target, t)
        t = snapshot(target)

    # 4) upstream 是 stale → 回收
    if u["kind"] == "stale":
        _unlink_stale(upstream, u)
        u = snapshot(upstream)

    # 5) upstream 被外来/自己占用且不是官方 → 异常拓扑，拒绝
    if u["kind"] in ("foreign", "fnradio"):
        raise Unsafe(f"upstream not official (kind={u['kind']}); refuse takeover")

    # 6) target 是官方 → 正常接管
    if t["kind"] == "official":
        if u["kind"] == "official":
            # 官方同时占 target 和 upstream = 官方重启后的孤儿 → 回收 upstream 孤儿
            _reclaim_orphan_upstream(target, t, upstream, u)
        _renameat2_noreplace(target, upstream)
        return "taken"

    # 7) target absent
    if t["kind"] == "absent":
        if snapshot(chain_upstream)["kind"] == "fnmusic-ext":
            # 链式状态残留（relay 挂掉但 fnmusic-ext 仍在 chain_upstream）→ 重新链式
            return "chained"
        if u["kind"] == "official":
            # 官方已在 upstream（上次接管残留、中继槽空）→ 直接绑原路径
            return "bind"
        return "absent"   # 官方未起 → 直接绑原路径

    raise Unsafe(f"ambiguous socket topology (target={t['kind']}, upstream={u['kind']}); refuse takeover")


def restore(target: str = TARGET, upstream: str = UPSTREAM,
            chain_upstream: str = CHAIN_UPSTREAM) -> str:
    """恢复官方 socket（upstream 改回 target）。fail-closed。

    链式恢复：chain_upstream 上是 fnmusic-ext（链式接管状态）时，把 fnmusic-ext
    移回 target，official 不动，飞牛音乐回到走 fnmusic-ext 的拓扑。
    """
    target = str(target)
    upstream = str(upstream)
    chain_upstream = str(chain_upstream)
    t = snapshot(target)
    u = snapshot(upstream)
    c = snapshot(chain_upstream)

    # 绝不碰 target 上的活监听者（外来进程，或自己仍在跑的 relay）
    if t["kind"] in ("foreign", "fnradio", "fnmusic-ext"):
        raise Unsafe(f"target occupied by live listener (kind={t['kind']}); refuse restore")

    # 回收 stale target（自己挂掉的 relay 残留）
    if t["kind"] == "stale":
        _unlink_stale(target, t)
        t = snapshot(target)

    # 链式恢复：chain_upstream 上是 fnmusic-ext → 移回 target（优先于官方恢复）
    if c["kind"] == "fnmusic-ext":
        if t["kind"] != "absent":
            raise Unsafe(f"target not vacant for chain restore (kind={t['kind']})")
        _renameat2_noreplace(chain_upstream, target)
        return "chain_restored"

    # chain_upstream 残留 stale → 回收（链式接管/恢复中断留下的空文件）
    if c["kind"] == "stale":
        _unlink_stale(chain_upstream, c)

    if u["kind"] == "absent":
        return "no_upstream"
    if u["kind"] == "stale":
        _unlink_stale(upstream, u)
        return "reclaimed_stale_upstream"
    if u["kind"] in ("foreign", "fnradio", "fnmusic-ext"):
        raise Unsafe(f"upstream not official (kind={u['kind']}); refuse restore")

    # u 是官方
    if t["kind"] == "official":
        # 官方已在 target，upstream 的官方是孤儿 → 回收 upstream 孤儿
        _reclaim_orphan_upstream(target, t, upstream, u)
        return "official_already_at_target"
    if t["kind"] != "absent":
        raise Unsafe(f"ambiguous topology (target={t['kind']}); refuse restore")

    _renameat2_noreplace(upstream, target)
    return "restored"


def socket_exists_or_stale(path: str) -> bool:
    """判断 socket 文件是否还在（stale 也算存在，用于诊断）。"""
    return os.path.lexists(str(path))
