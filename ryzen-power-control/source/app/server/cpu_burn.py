#!/usr/bin/env python3
import os
import sys
import time


def main():
    parent_pid = int(sys.argv[1])
    seconds = int(sys.argv[2])
    deadline = time.monotonic() + seconds
    next_parent_check = 0.0
    value = 1
    while time.monotonic() < deadline:
        for _ in range(10000):
            value = (value * 31 + 7) & 0xFFFFF
        now = time.monotonic()
        if now >= next_parent_check:
            if os.getppid() != parent_pid:
                return 0
            next_parent_check = now + 0.25
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
