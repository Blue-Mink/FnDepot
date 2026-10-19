#!/usr/bin/env python3
"""magpie 控制台移动端 CSS 等长补丁（自适应版本，字节级）。

定位方式：HEAD 字符串 → 其后第一个 "// Routing, live:" 之前即为 CSS 区段。
改动：
  A. body.web .top 的 padding-top：env(safe-area-inset-top) → min(env(...), 12px)
  B. body.web .top #nav：补 padding-right: 12px
补长：裁剪一条注释补回等量字符。
"""
import sys, hashlib

HEAD = b"/* magpie \xe2\x80\x94 one quiet surface, real logos, controls that read as pop-up buttons. */"
JS_MARK = b"// Routing, live:"

OLD_A = b"calc(6px + env(safe-area-inset-top))"
NEW_A = b"calc(6px + min(env(safe-area-inset-top), 12px))"

OLD_B = b"overflow-x: auto; overflow-y: hidden; overscroll-behavior-x: contain; scrollbar-width: none;"
NEW_B = OLD_B + b" padding-right: 12px;"

CUT = b"/* 50% centres in the padding box; the 2px top padding puts the flex row a pixel lower */"


def main(path: str, write: bool) -> int:
    data = open(path, "rb").read()
    orig_len = len(data)

    idx = data.find(HEAD)
    assert idx >= 0, "CSS head not found"
    assert data.find(HEAD, idx + 1) < 0, "CSS head appears more than once"
    end = data.find(JS_MARK, idx)
    assert end > idx, "JS marker not found after CSS"
    css = data[idx:end]
    print(f"css segment: {len(css)} bytes @ {idx}")

    for name, s in (("OLD_A", OLD_A), ("OLD_B", OLD_B), ("CUT", CUT)):
        n = css.count(s)
        assert n == 1, f"{name} occurrences = {n}"
    print("anchors: all 3 present exactly once")

    delta = (len(NEW_A) - len(OLD_A)) + (len(NEW_B) - len(OLD_B))
    keep = len(CUT) - delta
    assert keep > 10, "comment too short"
    trimmed = CUT[:keep - 3].rstrip() + b" */"
    assert len(trimmed) == keep, f"trim {len(trimmed)} != {keep}"

    new_css = css.replace(OLD_A, NEW_A).replace(OLD_B, NEW_B).replace(CUT, trimmed)
    assert len(new_css) == len(css), f"css length changed {len(css)} -> {len(new_css)}"

    out = data[:idx] + new_css + data[end:]
    assert len(out) == orig_len, "binary length changed"
    reverted = new_css.replace(NEW_A, OLD_A).replace(NEW_B, OLD_B).replace(trimmed, CUT)
    assert reverted == css, "unexpected modification inside CSS"
    assert out[:idx] == data[:idx] and out[end:] == data[end:], "outside-CSS bytes changed"

    print(f"delta = +{delta}; only intended strings changed; binary length unchanged")
    print("sha256(before) =", hashlib.sha256(data).hexdigest())
    print("sha256(after)  =", hashlib.sha256(out).hexdigest())

    if write:
        with open(path, "r+b") as f:
            f.seek(idx)
            f.write(new_css)
        print("written in place")
    return 0


if __name__ == "__main__":
    sys.exit(main(sys.argv[1], "--write" in sys.argv))
