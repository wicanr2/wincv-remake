#!/usr/bin/env python3
"""比對原版與重製版的畫面,以**格點**為單位。

整張圖做 pixel diff 沒有用:差一個像素跟差一整片看起來一樣,
而且看不出是哪裡不同。這支程式把兩張圖依格點切開,逐格比對,
輸出「第幾列第幾行、什麼不一樣」。

三級驗收(CLAUDE.md §5.2):

    版面等價   同一格是不是都有字/都沒字
    屬性等價   前景色與背景色相同
    像素等價   字模的位元完全相同

原版的畫面不是整片格點:上面有 Win32 選單列與工具列。
所以要指定比對哪一塊(--ox/--oy 是原版那一塊的左上角像素座標)。
原版主畫面的格點量測見 docs/ui/main-screen.md。

用法:
  tools/celldiff.py 原版.png 重製版.png --ox 34 --oy 40 --cols 93 --rows 20
"""
import argparse
import struct
import sys
import zlib


def read_png(path):
    d = open(path, "rb").read()
    pos, idat, w, h = 8, b"", 0, 0
    while pos < len(d):
        ln = struct.unpack(">I", d[pos:pos + 4])[0]
        typ = d[pos + 4:pos + 8]
        data = d[pos + 8:pos + 8 + ln]
        pos += 12 + ln
        if typ == b"IHDR":
            w, h, depth, ctype = struct.unpack(">IIBB", data[:10])
            if depth != 8 or ctype != 2:
                sys.exit(f"{path}: 只支援 8-bit RGB,拿到 depth={depth} type={ctype}")
        elif typ == b"IDAT":
            idat += data
        elif typ == b"IEND":
            break
    raw = zlib.decompress(idat)
    rows, stride, prev, i = [], w * 3, bytearray(w * 3), 0
    for _ in range(h):
        f = raw[i]; i += 1
        line = bytearray(raw[i:i + stride]); i += stride
        if f == 1:
            for x in range(3, stride):
                line[x] = (line[x] + line[x - 3]) & 255
        elif f == 2:
            for x in range(stride):
                line[x] = (line[x] + prev[x]) & 255
        elif f == 3:
            for x in range(stride):
                a = line[x - 3] if x >= 3 else 0
                line[x] = (line[x] + ((a + prev[x]) >> 1)) & 255
        elif f == 4:
            for x in range(stride):
                a = line[x - 3] if x >= 3 else 0
                b, c = prev[x], (prev[x - 3] if x >= 3 else 0)
                p = a + b - c
                pa, pb, pc = abs(p - a), abs(p - b), abs(p - c)
                pr = a if (pa <= pb and pa <= pc) else (b if pb <= pc else c)
                line[x] = (line[x] + pr) & 255
        rows.append(bytes(line))
        prev = line
    return w, h, rows


def cell_of(rows, ox, oy, cw, ch, col, row):
    """回傳一格的像素 tuple,以及它用到哪些顏色。"""
    px = []
    for y in range(oy + row * ch, oy + row * ch + ch):
        for x in range(ox + col * cw, ox + col * cw + cw):
            px.append(rows[y][x * 3:x * 3 + 3])
    return tuple(px)


def classify(px):
    """把一格的像素分成 (底色, 前景色, 圖樣)。前景色為 None 表示整格同色。"""
    from collections import Counter
    c = Counter(px)
    if len(c) == 1:
        return px[0], None, 0
    bg = c.most_common(1)[0][0]
    fg = next(k for k in c if k != bg)
    if len(c) > 2:
        fg = None  # 三色以上,不當單純的字看
    bits = 0
    for i, p in enumerate(px):
        if p != bg:
            bits |= 1 << i
    return bg, fg, bits


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("orig")
    ap.add_argument("remake")
    ap.add_argument("--ox", type=int, default=34, help="原版格點左上角 x")
    ap.add_argument("--oy", type=int, default=40, help="原版格點左上角 y")
    ap.add_argument("--rx", type=int, default=0, help="重製版格點左上角 x")
    ap.add_argument("--ry", type=int, default=0, help="重製版格點左上角 y")
    ap.add_argument("--cw", type=int, default=8)
    ap.add_argument("--ch", type=int, default=16)
    ap.add_argument("--cols", type=int, default=93)
    ap.add_argument("--rows", type=int, default=20)
    ap.add_argument("--max", type=int, default=40, help="最多列出幾格差異")
    a = ap.parse_args()

    ow, oh, orows = read_png(a.orig)
    rw, rh, rrows = read_png(a.remake)

    layout = attr = pixel = same = 0
    shown = 0
    for row in range(a.rows):
        for col in range(a.cols):
            if (a.oy + (row + 1) * a.ch > oh or a.ox + (col + 1) * a.cw > ow or
                    a.ry + (row + 1) * a.ch > rh or a.rx + (col + 1) * a.cw > rw):
                continue
            o = cell_of(orows, a.ox, a.oy, a.cw, a.ch, col, row)
            r = cell_of(rrows, a.rx, a.ry, a.cw, a.ch, col, row)
            if o == r:
                same += 1
                continue
            obg, ofg, obits = classify(o)
            rbg, rfg, rbits = classify(r)
            kind = "像素"
            if (obits != 0) != (rbits != 0):
                kind = "版面"; layout += 1
            elif obg != rbg or ofg != rfg:
                kind = "屬性"; attr += 1
            else:
                pixel += 1
            if shown < a.max:
                shown += 1
                print(f"  列{row:2d} 行{col:2d} {kind}  原版 bg={obg.hex()} fg={ofg.hex() if ofg else '-'}"
                      f"  重製 bg={rbg.hex()} fg={rfg.hex() if rfg else '-'}")

    total = same + layout + attr + pixel
    print(f"\n共 {total} 格:相同 {same}、版面差 {layout}、屬性差 {attr}、像素差 {pixel}")
    if total:
        print(f"相同比例 {100.0 * same / total:.1f}%")
    return 1 if (layout or attr or pixel) else 0


if __name__ == "__main__":
    sys.exit(main())
