#!/usr/bin/env python3
"""Tiny fnOS gateway sidecar for magpie.

Listens on a Unix socket consumed by fnOS open-gateway. After stripping the
/app/magpie prefix:
  /v1/*, /v1beta/*, /mcp/*  -> gateway API  (MAGPIE_GW_PORT)
  everything else           -> web server   (MAGPIE_WEB_PORT, UI + /api)

Web responses are rewritten so the browser stays under the prefix:
  - "/api/ string literals  -> "/app/magpie/api/
  - absolute Location headers and href/src="/ -> prefixed
  - one-shot ?k= injection: a GET without a magpie cookie and without a
    k parameter is signed in transparently (key read from local file,
    never echoed to the client).
The web UI navigates by query string (?view=...) with root-relative
assets, so no SPA basename handling is needed.
"""
import asyncio
import os
from urllib.parse import urlsplit

SOCK = os.environ.get("MAGPIE_GATEWAY_SOCK", "/vol1/@appcenter/magpie/magpie-gateway.sock")
UP = os.environ.get("MAGPIE_UPSTREAM_HOST", "127.0.0.1")
WEB_PORT = int(os.environ.get("MAGPIE_WEB_PORT", "36140"))
GW_PORT = int(os.environ.get("MAGPIE_GW_PORT", "36141"))
PREFIX = os.environ.get("MAGPIE_GATEWAY_PREFIX", "/app/magpie").rstrip("/")
KEYFILE = os.environ.get("MAGPIE_WEB_KEYFILE", "/var/apps/magpie/var/web-key")

GW_ROUTES = ("/v1/", "/v1beta/", "/mcp/")
HOP = {
    b"connection", b"keep-alive", b"proxy-authenticate", b"proxy-authorization",
    b"te", b"trailer", b"transfer-encoding", b"upgrade",
}
DROP_RESP = {b"x-frame-options"}
REWRITE_CT = (b"text/html", b"application/javascript", b"text/javascript", b"text/css")
P = PREFIX.encode("latin1")

# CSS 移动端适配：原 patch_magpie_css2.py 是「等长二进制补丁」，现下沉到响应层
# （sidecar 对 text/css 响应做同样的字符串替换）。上游 app.css 这两处写法会随
# 版本漂移，故用「命中才替换」的宽松方式——未命中（上游已自行修复/改写）时静默
# 跳过，不破坏其它 CSS。不依赖二进制，magpie 原生自更新后适配依然生效。
CSS_PATCHES = (
    (b"calc(6px + env(safe-area-inset-top))",
     b"calc(6px + min(env(safe-area-inset-top), 12px))"),
    (b"overflow-x: auto; overflow-y: hidden; overscroll-behavior-x: contain; scrollbar-width: none;",
     b"overflow-x: auto; overflow-y: hidden; overscroll-behavior-x: contain; scrollbar-width: none; padding-right: 12px;"),
)

# 追加式移动端适配：设置/关于页多控件行在窄屏下溢出。
# 根因：上游 .prefs .val 有 max-width:60% 且 flex-wrap 默认 nowrap，flex 子项（按钮/下拉/
# 开关）又不收缩；窄屏（≤600px）时上游已让 .row.pref 换行堆叠（.who flex-basis:100%），
# 但 .val 仍卡在 60% 宽、不换行 → 隐私页「Find with a local model」开关、Masking rules
# 的 Add、关于页 Community 的 GitHub 按钮与 Version 的「Open the release page」按钮均被
# 裁出屏。这里统一放宽 .row.pref .val 满宽 + 换行（覆盖隐私页与关于页全部受影响行）。
# 幂等：带唯一标记，检测到已追加则跳过。
CSS_APPEND_MARK = b"/* magpie-mobile-pref-fixes */"
CSS_APPEND = (
    b"/* magpie-mobile-pref-fixes */"
    b"@media (max-width:600px){"
    b".row.pref .val{max-width:100%;flex-wrap:wrap;row-gap:8px}"
    b"}"
)

# 追加式移动端适配 2：设置页子导航（GENERAL/.../ABOUT）在窄屏换行成多行，上游
# .set-tabs 只有 row-gap:2px、按钮高 22px —— 行挨得太紧、点按区小易误触（与顶部
# 主导航 .seg 的 14px 间距观感不一致）。这里放宽行距/列距并加大点按高度。
# 仅 ≤600px 生效（桌面不换行，row-gap 无效，零影响）。幂等：唯一标记。
CSS_APPEND2_MARK = b"/* magpie-mobile-settabs-fixes */"
CSS_APPEND2 = (
    b"/* magpie-mobile-settabs-fixes */"
    b"@media (max-width:600px){"
    b".set-tabs{row-gap:10px;column-gap:6px}"
    b".set-tabs button{height:28px;padding:0 9px}"
    b"}"
)

# 追加式移动端适配 3：设置页顶部三行（brand 图标行 / Agents..Gateway 主导航 /
# General..About 子导航）在窄屏占用过高（合计 ~273px 才见到内容卡片）。
# 整体上移：压缩 header 上下留白、brand 最小高、seg 按钮高、set-tabs 行距与
# 按钮高、设置搜索框最小高。仅 ≤600px 生效；幂等：唯一标记。
CSS_APPEND3_MARK = b"/* magpie-mobile-topfix */"
CSS_APPEND3 = (
    b"/* magpie-mobile-topfix */"
    b"@media (max-width:600px){"
    b"body.web header.top{padding-top:2px;padding-bottom:4px}"
    b"body.web .top .brand{min-height:30px}"
    b"body.web .top #nav button{height:32px;min-height:32px;padding:0 10px}"
    b"#view-settings .set-head{margin-top:2px;margin-bottom:4px}"
    b".set-tabs{row-gap:6px}"
    b".set-tabs button{height:24px;padding:0 9px}"
    b"#view-settings .set-head input.sess-filter,#view-settings .set-head input.set-q{min-height:36px}"
    b"}"
)


def strip_prefix(path: str) -> str:
    if path == PREFIX:
        return "/"
    if path.startswith(PREFIX + "/"):
        return path[len(PREFIX):] or "/"
    return path


def is_gw_path(path: str) -> bool:
    if path in ("/v1", "/v1beta", "/mcp"):
        return True
    return any(path.startswith(r) for r in GW_ROUTES)


def read_key() -> str:
    try:
        with open(KEYFILE, "r") as f:
            return f.read().strip()
    except OSError:
        return ""


def rewrite_css(body: bytes) -> bytes:
    """CSS 移动端适配：对 text/css 响应做与二进制补丁同源的字符串替换。

    幂等：B 项的 new 以 old 为前缀（在 overflow 声明后追加 padding-right），
    若直接 replace，new 里的 old 前缀会被再次命中导致重复追加。故先还原
    new->old，再统一 old->new，保证多次执行结果不变。
    """
    for old, new in CSS_PATCHES:
        body = body.replace(new, old)
        body = body.replace(old, new)
    if CSS_APPEND_MARK not in body:
        body = body.rstrip() + b"\n" + CSS_APPEND
    if CSS_APPEND2_MARK not in body:
        body = body.rstrip() + b"\n" + CSS_APPEND2
    if CSS_APPEND3_MARK not in body:
        body = body.rstrip() + b"\n" + CSS_APPEND3
    return body


def rewrite_body(body: bytes, ctype: bytes) -> bytes:
    if not any(x in ctype.lower() for x in REWRITE_CT):
        return body
    if b"text/css" in ctype.lower():
        body = rewrite_css(body)
    for q in (b'"', b"'", b"`"):
        body = body.replace(q + b"/api/", q + P + b"/api/")
    body = body.replace(b'href="/', b'href="' + P + b'/')
    body = body.replace(b'src="/', b'src="' + P + b'/')
    body = body.replace(P + P, P)
    return body


def prefix_location(v: bytes) -> bytes:
    if v.startswith(b"/") and not v.startswith(b"//"):
        return P + v
    return v


def inject_key(pathq: str) -> str:
    """Add k=<key> to a GET that carries neither cookie nor key."""
    if "?" in pathq:
        path, qs = pathq.split("?", 1)
    else:
        path, qs = pathq, ""
    if "k=" in qs:
        return pathq
    key = read_key()
    if not key:
        return pathq
    qs = (qs + "&" if qs else "") + "k=" + key
    return path + "?" + qs


async def read_headers(reader):
    return await reader.readuntil(b"\r\n\r\n")


async def pipe(reader, writer):
    try:
        while True:
            data = await reader.read(65536)
            if not data:
                break
            writer.write(data)
            await writer.drain()
    except Exception:
        pass
    finally:
        try:
            writer.close()
        except Exception:
            pass


async def read_chunked(reader) -> bytes:
    """把一个 chunked 响应体整段解出来（用于需要改写的 HTML/JS/CSS）。"""
    out = bytearray()
    while True:
        line = await reader.readuntil(b"\r\n")
        size = int(line.strip().split(b";")[0] or b"0", 16)
        if size == 0:
            while True:
                t = await reader.readuntil(b"\r\n")
                if t == b"\r\n":
                    break
            return bytes(out)
        out += await reader.readexactly(size)
        await reader.readexactly(2)


async def relay_chunked(reader, writer):
    """原样转发 chunked 流，边到边发（SSE / 长响应）。"""
    while True:
        line = await reader.readuntil(b"\r\n")
        size = int(line.strip().split(b";")[0] or b"0", 16)
        writer.write(line)
        if size == 0:
            while True:
                t = await reader.readuntil(b"\r\n")
                writer.write(t)
                if t == b"\r\n":
                    break
            await writer.drain()
            return
        remaining = size + 2
        while remaining > 0:
            chunk = await reader.read(min(remaining, 65536))
            if not chunk:
                raise EOFError("chunked stream ended early")
            writer.write(chunk)
            remaining -= len(chunk)
            await writer.drain()


async def handle(client_r, client_w):
    upstream_w = None
    try:
        head = await read_headers(client_r)
        lines = head.split(b"\r\n")
        parts = lines[0].split(b" ", 2)
        if len(parts) != 3:
            client_w.write(b"HTTP/1.1 400 Bad Request\r\nContent-Length:0\r\n\r\n")
            await client_w.drain()
            return
        method, target, version = parts
        target_s = target.decode("latin1")
        if target_s.startswith("http://") or target_s.startswith("https://"):
            u = urlsplit(target_s)
            pathq = (u.path or "/") + (("?" + u.query) if u.query else "")
        else:
            pathq = target_s
        if "?" in pathq:
            path, qs = pathq.split("?", 1)
            new_pathq = strip_prefix(path) + "?" + qs
        else:
            new_pathq = strip_prefix(pathq)
        path_only = new_pathq.split("?", 1)[0]
        port = GW_PORT if is_gw_path(path_only) else WEB_PORT
        cookie_l = b""
        out = [method + b" " + new_pathq.encode("latin1") + b" " + version]
        content_len = 0
        is_upgrade = False
        need_signin = method.upper() == b"GET" and port == WEB_PORT
        for line in lines[1:]:
            if not line:
                continue
            k = line.split(b":", 1)[0].lower()
            v = line.split(b":", 1)[1].strip() if b":" in line else b""
            if k == b"content-length":
                try:
                    content_len = int(v)
                except ValueError:
                    content_len = 0
                out.append(line)
            elif k == b"cookie":
                cookie_l = v.lower()
                out.append(line)
            elif k == b"upgrade":
                is_upgrade = True
                out.append(line)
            elif k == b"connection":
                if b"upgrade" in line.lower():
                    is_upgrade = True
                out.append(line)
            elif k == b"host":
                out.append(b"Host: " + UP.encode() + b":" + str(port).encode())
            elif k == b"accept-encoding":
                continue
            elif k in HOP:
                continue
            else:
                out.append(line)
        if need_signin and b"magpie_web_" not in cookie_l:
            new_pathq2 = inject_key(new_pathq)
            if new_pathq2 != new_pathq:
                out[0] = method + b" " + new_pathq2.encode("latin1") + b" " + version
        out += [b"X-Forwarded-Prefix: " + P, b"X-Forwarded-Proto: http", b"", b""]
        upstream_r, upstream_w = await asyncio.open_connection(UP, port)
        upstream_w.write(b"\r\n".join(out))
        if content_len:
            upstream_w.write(await client_r.readexactly(content_len))
        await upstream_w.drain()
        if is_upgrade:
            await asyncio.gather(pipe(client_r, upstream_w), pipe(upstream_r, client_w))
            return
        resp_head = await read_headers(upstream_r)
        rlines = resp_head.split(b"\r\n")
        status = rlines[0]
        headers = []
        clen = None
        ctype = b""
        te_chunked = False
        try:
            code = status.split(b" ", 2)[1]
        except IndexError:
            code = b""
        is_redirect = code[:3] in (b"301", b"302", b"303", b"307", b"308")
        for line in rlines[1:]:
            if not line:
                continue
            k = line.split(b":", 1)[0].lower()
            v = line.split(b":", 1)[1].strip() if b":" in line else b""
            if k == b"transfer-encoding" and b"chunked" in v.lower():
                te_chunked = True
            if k in DROP_RESP or k in HOP:
                continue
            if k == b"location" and port == WEB_PORT and is_redirect:
                v = prefix_location(v)
            if k == b"content-length":
                try:
                    clen = int(v)
                except ValueError:
                    clen = None
                continue
            if k == b"content-type":
                ctype = v
            headers.append(k + b": " + v)
        # 无 body 响应（204/304/HEAD）：上游 keep-alive 不关连接，读 body 会永久卡住。
        # 直接转发响应头（保留上游 ETag / Cache-Control），不读 body。
        if code[:3] in (b"204", b"304") or method.upper() == b"HEAD":
            headers.append(b"Connection: close")
            client_w.write(b"\r\n".join([status] + headers + [b"", b""]))
            await client_w.drain()
            return
        rewritable = port == WEB_PORT and any(x in ctype.lower() for x in REWRITE_CT)
        if te_chunked and not rewritable:
            # 流式透传（SSE / 无长度响应）：逐块转发，客户端不必等整包结束
            headers.append(b"Transfer-Encoding: chunked")
            headers.append(b"Connection: close")
            client_w.write(b"\r\n".join([status] + headers + [b"", b""]))
            await client_w.drain()
            await relay_chunked(upstream_r, client_w)
            return
        if te_chunked:
            body = await read_chunked(upstream_r)
        elif clen is not None:
            body = await upstream_r.readexactly(clen)
        else:
            chunks = []
            while True:
                data = await upstream_r.read(65536)
                if not data:
                    break
                chunks.append(data)
            body = b"".join(chunks)
        rewritten = False
        if rewritable:
            body = rewrite_body(body, ctype)
            rewritten = True
        if rewritten:
            # 静态资源（CSS/JS）带 ?v=hash 内容寻址版本号，保留上游 ETag 并允许条件请求
            # 复用缓存（避免每次打开页面都重新下载 1MB+ 的 app.js）；HTML 含签入/动态
            # 内容，保持 no-store。重写是确定性的，故条件请求 304 时缓存的「重写后」内容
            # 依然正确。
            headers = [h for h in headers if not h.lower().startswith(b"cache-control:")]
            if b"text/css" in ctype.lower() or b"javascript" in ctype.lower():
                headers.append(b"Cache-Control: no-cache")
            else:
                headers.append(b"Cache-Control: no-store")
        headers.append(b"Content-Length: " + str(len(body)).encode())
        headers.append(b"Connection: close")
        client_w.write(b"\r\n".join([status] + headers + [b"", b""]) + body)
        await client_w.drain()
    except Exception as e:
        try:
            msg = (str(e) or "proxy error").encode()
            client_w.write(b"HTTP/1.1 502 Bad Gateway\r\nContent-Type:text/plain\r\n"
                           b"Content-Length:" + str(len(msg)).encode() + b"\r\n\r\n" + msg)
            await client_w.drain()
        except Exception:
            pass
    finally:
        try:
            client_w.close()
        except Exception:
            pass
        if upstream_w:
            try:
                upstream_w.close()
            except Exception:
                pass


async def main():
    try:
        os.unlink(SOCK)
    except FileNotFoundError:
        pass
    d = os.path.dirname(SOCK)
    if d:
        os.makedirs(d, exist_ok=True)
    server = await asyncio.start_unix_server(handle, SOCK)
    os.chmod(SOCK, 0o777)
    print("magpie gateway proxy listening on %s web=%d gw=%d prefix=%s"
          % (SOCK, WEB_PORT, GW_PORT, PREFIX), flush=True)
    async with server:
        await server.serve_forever()


if __name__ == "__main__":
    asyncio.run(main())
