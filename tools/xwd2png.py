#!/usr/bin/env python3
"""XWD (X Window Dump, TrueColor 24/32bpp) → PNG。

這台機器沒有 ImageMagick 的 convert,ffmpeg 也不吃 xwd,所以自己寫一個。
只支援 xwd 的 ZPixmap + 24/32 bpp,夠 Xvfb 用。
"""
import struct
import sys
import zlib


def chunk(tag, data):
    return (struct.pack(">I", len(data)) + tag + data
            + struct.pack(">I", zlib.crc32(tag + data) & 0xFFFFFFFF))


def main():
    if len(sys.argv) != 3:
        sys.exit("用法: xwd2png.py <in.xwd> <out.png>")
    d = open(sys.argv[1], "rb").read()
    h = struct.unpack(">25I", d[:100])
    hsz, w, height, bpl, ncolors = h[0], h[4], h[5], h[12], h[19]
    off = hsz + ncolors * 12
    px = d[off:off + bpl * height]

    rows = []
    for y in range(height):
        line = px[y * bpl:y * bpl + w * 4]
        out = bytearray(b"\x00")
        for x in range(w):
            b, g, r, _ = line[x * 4:x * 4 + 4]
            out += bytes((r, g, b))
        rows.append(bytes(out))

    png = (b"\x89PNG\r\n\x1a\n"
           + chunk(b"IHDR", struct.pack(">IIBBBBB", w, height, 8, 2, 0, 0, 0))
           + chunk(b"IDAT", zlib.compress(b"".join(rows), 6))
           + chunk(b"IEND", b""))
    open(sys.argv[2], "wb").write(png)


if __name__ == "__main__":
    main()
