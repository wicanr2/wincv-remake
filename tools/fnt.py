#!/usr/bin/env python3
"""NE `.FON` (Windows 2.x/3.x FNT bitmap font) 解析器。

WinCV 隨附四個 `.FON`,face name 分別是 cvga / cvga1018 / cvga1224
(WinCV.fon 與 cvga.fon 同名同尺寸)。程式以 AddFontResource 註冊它們,
再用 CreateFontIndirect 指名 face 取得半形字型。

FNT 2.0 header 版面(bytes,little-endian):
    0x00 dfVersion  W    0x42 dfType      W    0x55 dfCharSet   B
    0x02 dfSize     D    0x44 dfPoints    W    0x56 dfPixWidth  W
    0x06 dfCopyright 60  0x46 dfVertRes   W    0x58 dfPixHeight W
                         0x48 dfHorizRes  W    0x5A dfPitchAndFamily B
                         0x4A dfAscent    W    0x5B dfAvgWidth  W
                         0x4C dfIntLeading W   0x5D dfMaxWidth  W
                         0x4E dfExtLeading W   0x5F dfFirstChar B
                         0x50 dfItalic    B    0x60 dfLastChar  B
                         0x51 dfUnderline B    0x61 dfDefaultChar B
                         0x52 dfStrikeOut B    0x62 dfBreakChar B
                         0x53 dfWeight    W    0x63 dfWidthBytes W
                                               0x65 dfDevice    D
                                               0x69 dfFace      D
                                               0x71 dfBitsOffset D
    0x76 dfCharTable: (dfLastChar - dfFirstChar + 2) 筆 {W width, W offset}

字模是 **column-major**:每個字元先存第 0 欄的全部列(每 8 列一個 byte),
再第 1 欄……寬度 > 8 的字元因此分成多個 8-pixel 欄組。

用法:
  python3 tools/fnt.py info  original/app/cvga.fon
  python3 tools/fnt.py atlas original/app/cvga.fon out.pbm
  python3 tools/fnt.py glyph original/app/cvga.fon 0x41
"""
import struct
import sys


def ne_resources(d):
    ne = struct.unpack_from("<H", d, 0x3C)[0]
    rt = ne + struct.unpack_from("<H", d, ne + 0x24)[0]
    shift = struct.unpack_from("<H", d, rt)[0]
    p = rt + 2
    out = []
    while struct.unpack_from("<H", d, p)[0] != 0:
        tid = struct.unpack_from("<H", d, p)[0]
        cnt = struct.unpack_from("<H", d, p + 2)[0]
        p += 8
        for _ in range(cnt):
            off, ln = struct.unpack_from("<HH", d, p)
            out.append((tid & 0x7FFF, off << shift, ln << shift))
            p += 12
    return out


class Fnt:
    def __init__(self, d, base):
        self.d = d
        self.base = base
        u16 = lambda o: struct.unpack_from("<H", d, base + o)[0]
        u32 = lambda o: struct.unpack_from("<I", d, base + o)[0]
        self.version = u16(0x00)
        self.size = u32(0x02)
        self.points = u16(0x44)
        self.ascent = u16(0x4A)
        self.int_leading = u16(0x4C)
        self.ext_leading = u16(0x4E)
        self.weight = u16(0x53)
        self.charset = d[base + 0x55]
        self.pix_width = u16(0x56)
        self.pix_height = u16(0x58)
        self.pitch_family = d[base + 0x5A]
        self.avg_width = u16(0x5B)
        self.max_width = u16(0x5D)
        self.first = d[base + 0x5F]
        self.last = d[base + 0x60]
        self.default_char = d[base + 0x61]
        self.break_char = d[base + 0x62]
        self.width_bytes = u16(0x63)
        face_off = u32(0x69)
        end = d.find(b"\0", base + face_off)
        self.face = d[base + face_off:end].decode("latin1")

        n = self.last - self.first + 2
        self.chars = []
        for i in range(n):
            w, off = struct.unpack_from("<HH", d, base + 0x76 + i * 4)
            self.chars.append((w, off))

    def glyph(self, code):
        """回傳 list[str],每列一個 '.#' 字串。"""
        i = code - self.first
        if not (0 <= i < len(self.chars) - 1):
            return None
        w, off = self.chars[i]
        h = self.pix_height
        cols = (w + 7) // 8
        rows = ["" for _ in range(h)]
        for c in range(cols):
            colbase = self.base + off + c * h
            for y in range(h):
                b = self.d[colbase + y]
                for bit in range(8):
                    if c * 8 + bit < w:
                        rows[y] += "#" if b & (0x80 >> bit) else "."
        return rows


def load(path):
    d = open(path, "rb").read()
    for tid, off, _ln in ne_resources(d):
        if tid == 8:  # RT_FONT
            return Fnt(d, off)
    raise SystemExit("找不到 RT_FONT resource")


def cmd_info(f):
    print(f'face          "{f.face}"')
    print(f"version       {f.version:#06x}")
    print(f"points        {f.points}")
    print(f"pixel W x H   {f.pix_width} x {f.pix_height}   (0 = 變寬)")
    print(f"avg / max W   {f.avg_width} / {f.max_width}")
    print(f"ascent        {f.ascent}  int_leading {f.int_leading}  ext {f.ext_leading}")
    print(f"charset       {f.charset:#04x}  pitch_family {f.pitch_family:#04x}")
    print(f"chars         {f.first:#04x} - {f.last:#04x}  default {f.default_char:#04x}")
    widths = {}
    for i in range(f.last - f.first + 1):
        widths[f.chars[i][0]] = widths.get(f.chars[i][0], 0) + 1
    print(f"寬度分布       {dict(sorted(widths.items()))}")


def cmd_glyph(f, code):
    g = f.glyph(code)
    if g is None:
        raise SystemExit(f"{code:#04x} 不在字型範圍")
    print(f"{code:#04x} width={f.chars[code - f.first][0]}")
    for r in g:
        print(r)


def cmd_atlas(f, out):
    """16 x 16 排的字模圖,存成 PBM(不需要任何影像函式庫)。"""
    cw = max(w for w, _ in f.chars[:f.last - f.first + 1]) or 8
    ch = f.pix_height
    W, H = cw * 16, ch * 16
    bits = [[0] * W for _ in range(H)]
    for code in range(f.first, f.last + 1):
        g = f.glyph(code)
        if not g:
            continue
        gx, gy = (code % 16) * cw, (code // 16) * ch
        for y, row in enumerate(g):
            for x, c in enumerate(row):
                if c == "#":
                    bits[gy + y][gx + x] = 1
    with open(out, "wb") as fh:
        fh.write(b"P1\n%d %d\n" % (W, H))
        for row in bits:
            fh.write(("".join(str(b) for b in row) + "\n").encode())
    print(f"{out}  {W}x{H}  cell {cw}x{ch}")


def main():
    if len(sys.argv) < 3:
        sys.exit(__doc__)
    cmd, path = sys.argv[1], sys.argv[2]
    f = load(path)
    if cmd == "info":
        cmd_info(f)
    elif cmd == "glyph":
        cmd_glyph(f, int(sys.argv[3], 0))
    elif cmd == "atlas":
        cmd_atlas(f, sys.argv[3])
    else:
        sys.exit(__doc__)


if __name__ == "__main__":
    main()
