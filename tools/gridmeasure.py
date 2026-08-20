#!/usr/bin/env python3
"""從原版截圖量測 cell grid 的行高與字寬。

作法:對指定矩形區域取「每一列有多少非背景像素」的剖面,再對剖面做自相關,
最高的非零位移就是列距。字寬同理,改取行剖面。

只吃 tools/xwd2png.py 產生的那種 8-bit RGB、非交錯 PNG。

用法:
  python3 tools/gridmeasure.py shot.png                    # 全圖
  python3 tools/gridmeasure.py shot.png 24 88 570 280      # x y w h
"""
import struct
import sys
import zlib


def read_png(path):
    d = open(path, "rb").read()
    assert d[:8] == b"\x89PNG\r\n\x1a\n"
    pos = 8
    w = h = None
    idat = b""
    while pos < len(d):
        ln = struct.unpack_from(">I", d, pos)[0]
        tag = d[pos + 4:pos + 8]
        body = d[pos + 8:pos + 8 + ln]
        if tag == b"IHDR":
            w, h, bd, ct = struct.unpack_from(">IIBB", body, 0)
            assert bd == 8 and ct == 2, "只支援 8-bit RGB"
        elif tag == b"IDAT":
            idat += body
        elif tag == b"IEND":
            break
        pos += 12 + ln
    raw = zlib.decompress(idat)
    stride = w * 3
    rows = []
    prev = bytearray(stride)
    p = 0
    for _ in range(h):
        f = raw[p]
        line = bytearray(raw[p + 1:p + 1 + stride])
        p += 1 + stride
        if f == 1:
            for i in range(3, stride):
                line[i] = (line[i] + line[i - 3]) & 0xFF
        elif f == 2:
            for i in range(stride):
                line[i] = (line[i] + prev[i]) & 0xFF
        elif f == 3:
            for i in range(stride):
                a = line[i - 3] if i >= 3 else 0
                line[i] = (line[i] + ((a + prev[i]) >> 1)) & 0xFF
        elif f == 4:
            for i in range(stride):
                a = line[i - 3] if i >= 3 else 0
                c = prev[i - 3] if i >= 3 else 0
                b = prev[i]
                pa, pb, pc = abs(b - c), abs(a - c), abs(a + b - 2 * c)
                pr = a if (pa <= pb and pa <= pc) else (b if pb <= pc else c)
                line[i] = (line[i] + pr) & 0xFF
        rows.append(bytes(line))
        prev = line
    return w, h, rows


def autocorr_period(profile, lo, hi):
    """回傳 (最佳週期, 分數表)。分數用位移後的內積正規化。"""
    n = len(profile)
    mean = sum(profile) / n if n else 0
    c = [v - mean for v in profile]
    scores = []
    for lag in range(lo, hi + 1):
        if lag >= n:
            break
        s = sum(c[i] * c[i + lag] for i in range(n - lag)) / (n - lag)
        scores.append((lag, s))
    if not scores:
        return None, []
    best = max(scores, key=lambda t: t[1])
    return best[0], scores


def main():
    if len(sys.argv) < 2:
        sys.exit(__doc__)
    path = sys.argv[1]
    w, h, rows = read_png(path)
    if len(sys.argv) >= 6:
        rx, ry, rw, rh = (int(v) for v in sys.argv[2:6])
    else:
        rx, ry, rw, rh = 0, 0, w, h

    # 背景 = 區域內最常見的顏色
    from collections import Counter
    cnt = Counter()
    for y in range(ry, min(ry + rh, h)):
        line = rows[y]
        for x in range(rx, min(rx + rw, w)):
            cnt[line[x * 3:x * 3 + 3]] += 1
    bg = cnt.most_common(1)[0][0]

    rowprof = []
    for y in range(ry, min(ry + rh, h)):
        line = rows[y]
        rowprof.append(sum(1 for x in range(rx, min(rx + rw, w))
                           if line[x * 3:x * 3 + 3] != bg))
    colprof = []
    for x in range(rx, min(rx + rw, w)):
        colprof.append(sum(1 for y in range(ry, min(ry + rh, h))
                           if rows[y][x * 3:x * 3 + 3] != bg))

    rp, rs = autocorr_period(rowprof, 8, 40)
    cp, cs = autocorr_period(colprof, 4, 32)
    print(f"區域          x={rx} y={ry} w={rw} h={rh}")
    print(f"背景色        #{bg.hex()}")
    print(f"列距(行高)    {rp} px")
    print(f"  前 6 名     {[l for l, _ in sorted(rs, key=lambda t: -t[1])[:6]]}")
    print(f"欄距(字寬)    {cp} px")
    print(f"  前 6 名     {[l for l, _ in sorted(cs, key=lambda t: -t[1])[:6]]}")
    if rp:
        print(f"區域可容納    {rh // rp} 列 x {rw // cp if cp else '?'} 欄")


if __name__ == "__main__":
    main()
