#!/usr/bin/env python3
"""把幾張同樣大小的 PNG 拼成格子,中間留灰色間隔。只用標準函式庫。

    python3 -I tools/png-grid.py <欄數> <輸出.png> <輸入1.png> <輸入2.png> ...

只吃 8 位元的 RGB 或 RGBA、非交錯(celldump 的輸出就是這種)。
"""
import struct
import sys
import zlib

GAP = 6
GAP_RGB = (128, 128, 128)


def read_png(path):
    data = open(path, "rb").read()
    if data[:8] != b"\x89PNG\r\n\x1a\n":
        sys.exit(f"{path}: 不是 PNG")
    pos, idat, w = 8, b"", 0
    while pos < len(data):
        n, typ = struct.unpack(">I4s", data[pos:pos + 8])
        body = data[pos + 8:pos + 8 + n]
        pos += 12 + n
        if typ == b"IHDR":
            w, h, depth, ctype, _, _, inter = struct.unpack(">IIBBBBB", body)
            if depth != 8 or ctype not in (2, 6) or inter:
                sys.exit(f"{path}: 只支援 8 位元 RGB/RGBA、非交錯")
            bpp = 3 if ctype == 2 else 4
        elif typ == b"IDAT":
            idat += body
    raw = zlib.decompress(idat)
    stride = w * bpp
    rows, prev = [], bytearray(stride)
    for y in range(h):
        f = raw[y * (stride + 1)]
        line = bytearray(raw[y * (stride + 1) + 1:(y + 1) * (stride + 1)])
        for i in range(stride):
            a = line[i - bpp] if i >= bpp else 0
            b = prev[i]
            c = prev[i - bpp] if i >= bpp else 0
            if f == 1:
                line[i] = (line[i] + a) & 255
            elif f == 2:
                line[i] = (line[i] + b) & 255
            elif f == 3:
                line[i] = (line[i] + (a + b) // 2) & 255
            elif f == 4:
                p = a + b - c
                pa, pb, pc = abs(p - a), abs(p - b), abs(p - c)
                pr = a if pa <= pb and pa <= pc else (b if pb <= pc else c)
                line[i] = (line[i] + pr) & 255
        # 一律轉成 RGB:輸出不帶 alpha。
        rows.append(b"".join(bytes(line[j:j + 3]) for j in range(0, stride, bpp)))
        prev = line
    return w, h, rows


def write_png(path, w, h, rows):
    raw = b"".join(b"\x00" + r for r in rows)

    def chunk(t, b):
        return struct.pack(">I", len(b)) + t + b + struct.pack(">I", zlib.crc32(t + b))

    with open(path, "wb") as f:
        f.write(b"\x89PNG\r\n\x1a\n")
        f.write(chunk(b"IHDR", struct.pack(">IIBBBBB", w, h, 8, 2, 0, 0, 0)))
        f.write(chunk(b"IDAT", zlib.compress(raw, 9)))
        f.write(chunk(b"IEND", b""))


def main():
    cols, out, ins = int(sys.argv[1]), sys.argv[2], sys.argv[3:]
    imgs = [read_png(p) for p in ins]
    w, h = imgs[0][0], imgs[0][1]
    if any(i[0] != w or i[1] != h for i in imgs):
        sys.exit("輸入的圖大小不一")
    nrows = (len(imgs) + cols - 1) // cols
    W, H = cols * w + (cols + 1) * GAP, nrows * h + (nrows + 1) * GAP
    gap = bytes(GAP_RGB)
    canvas = [bytearray(gap * W) for _ in range(H)]
    for k, (_, _, rows) in enumerate(imgs):
        x0 = GAP + (k % cols) * (w + GAP)
        y0 = GAP + (k // cols) * (h + GAP)
        for y, r in enumerate(rows):
            canvas[y0 + y][x0 * 3:(x0 + w) * 3] = r
    write_png(out, W, H, [bytes(r) for r in canvas])


main()
