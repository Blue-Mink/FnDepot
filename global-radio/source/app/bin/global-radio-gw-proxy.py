#!/usr/bin/env python3
"""global-radio 飞牛统一网关 sidecar。

监听 unix socket，接收网关(/app/<prefix>/...)转发的请求：
  1. 剥前缀后转发到本机容器端口；
  2. HTML 绝对路径资源加前缀；
  3. 主 bundle 给 vue-router createWebHistory 注入 basename；
  4. gr-accel.js 的 /rb/ /imgproxy/ 改道前缀化。
"""
import os, re, socket, threading, sys

PREFIX = os.environ.get("GW_PREFIX", "/app/global-radio").rstrip("/")
PORT = int(os.environ.get("GW_PORT", "32676"))
SOCK = os.environ.get("GW_SOCK", "/tmp/gr-gw.sock")
HOST, HP = "127.0.0.1", 80
UP = ("127.0.0.1", PORT)

def strip_prefix(path):
    if path == PREFIX:
        return "/"
    if path.startswith(PREFIX + "/"):
        return path[len(PREFIX):] or "/"
    return None

HTML_ABS = re.compile(rb'((?:href|src|content)=")(/)')
JS_ANCHOR = re.compile(rb'\}\s*\(\s*\)\s*,routes:\[\{path:"/",name:"Home"')
JS_DONE_TPL = b'}("%s"),routes:[{path:"/",name:"Home"'
ACCEL_EDITS = [(b"'/rb/'", (b"'" + PREFIX.encode() + b"/rb/'")),
               (b"'/imgproxy/'", (b"'" + PREFIX.encode() + b"/imgproxy/'"))]

def rewrite_html(body):
    return HTML_ABS.sub(rb'\1' + PREFIX.encode() + rb'\2', body)

def rewrite_js(body):
    if JS_DONE_TPL in body:
        return body
    out, n = JS_ANCHOR.subn(b'}("' + PREFIX.encode() + b'"),routes:[{path:"/",name:"Home"', body)
    if n:
        sys.stderr.write("gr-gw: router basename injected (%d)\n" % n)
    return out

def rewrite_accel(body):
    if (PREFIX.encode() + b"/rb/").decode() in body.decode("utf8", "ignore"):
        return body
    for old, new in ACCEL_EDITS:
        body = body.replace(old, new)
    return body

def recv_until(hdr_end, sock, buf):
    while hdr_end not in buf:
        d = sock.recv(65536)
        if not d:
            break
        buf += d
    return buf

def handle(conn):
    try:
        buf = b""
        while b"\r\n\r\n" not in buf:
            d = conn.recv(65536)
            if not d:
                return
            buf += d
        head, _, rest = buf.partition(b"\r\n\r\n")
        lines = head.split(b"\r\n")
        method, pathq, _ver = lines[0].split(b" ", 2)
        path, _, qs = pathq.partition(b"?")
        path = path.decode("latin1")
        if path == PREFIX:
            conn.sendall(b"HTTP/1.1 301 Moved\r\nLocation: " + PREFIX.encode() + b"/\r\nContent-Length: 0\r\n\r\n")
            return
        upath = strip_prefix(path)
        if upath is None:
            conn.sendall(b"HTTP/1.1 404 Not Found\r\nContent-Length: 0\r\n\r\n")
            return
        headers = []
        for ln in lines[1:]:
            k = ln.split(b":", 1)[0].lower()
            if k in (b"host", b"accept-encoding", b"connection"):
                continue
            headers.append(ln)
        headers.append(b"Host: 127.0.0.1:%d" % PORT)
        headers.append(b"X-Forwarded-Prefix: " + PREFIX.encode())
        headers.append(b"Connection: close")
        target = upath + (("?" + qs.decode("latin1")) if qs else "")
        req = b"%s %s %s\r\n" % (method, target.encode("latin1"), _ver)
        req += b"\r\n".join(headers) + b"\r\n\r\n"
        up = socket.create_connection(UP, timeout=15)
        up.sendall(req + rest)
        rbuf = b""
        while b"\r\n\r\n" not in rbuf:
            d = up.recv(65536)
            if not d:
                break
            rbuf += d
        if not rbuf:
            up.close(); conn.sendall(b"HTTP/1.1 502 Bad Gateway\r\nContent-Length: 0\r\n\r\n"); return
        hraw, _, body = rbuf.partition(b"\r\n\r\n")
        hlines = hraw.split(b"\r\n")
        status = hlines[0]
        ctype = b""
        out_h = []
        for ln in hlines[1:]:
            k, _, v = ln.partition(b":")
            kl = k.lower()
            if kl == b"content-type":
                ctype = v.strip().lower()
            if kl in (b"content-length", b"transfer-encoding", b"content-encoding"):
                continue
            out_h.append(ln)
        # 读满 body
        if b"chunked" in b"".join(hlines[1:]).lower():
            while True:
                d = up.recv(65536)
                if not d:
                    break
                body += d
        else:
            while True:
                d = up.recv(65536)
                if not d:
                    break
                body += d
        up.close()
        if b"text/html" in ctype:
            body = rewrite_html(body)
        elif (b"javascript" in ctype or b"ecmascript" in ctype) and path.endswith(".js"):
            if upath == "/gr-accel.js":
                body = rewrite_accel(body)
            else:
                body = rewrite_js(body)
        out_h.append(b"Content-Length: %d" % len(body))
        out = status + b"\r\n" + b"\r\n".join(out_h) + b"\r\n\r\n" + body
        conn.sendall(out)
    except Exception as e:
        sys.stderr.write("gr-gw: %r\n" % e)
        try:
            conn.sendall(b"HTTP/1.1 502 Bad Gateway\r\nContent-Length: 0\r\n\r\n")
        except Exception:
            pass
    finally:
        try:
            conn.close()
        except Exception:
            pass

def main():
    if os.path.exists(SOCK):
        os.unlink(SOCK)
    srv = socket.socket(socket.AF_UNIX, socket.SOCK_STREAM)
    srv.bind(SOCK)
    os.chmod(SOCK, 0o666)
    srv.listen(128)
    sys.stderr.write("gr-gw listening %s -> %s prefix=%s\n" % (SOCK, UP, PREFIX))
    while True:
        c, _ = srv.accept()
        threading.Thread(target=handle, args=(c,), daemon=True).start()

if __name__ == "__main__":
    main()
