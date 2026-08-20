#!/usr/bin/env python3
"""從 WINCV.IMG 抽出 29 個具名顏色的 RGB。

顏色是 Forth 的 word,body 有 0x24 個位元組,其中第 8-10 個就是 R、G、B
(Win32 的 COLORREF 是 0x00BBGGRR,小端存放後記憶體順序正好是 R G B)。
word 的 xt 在它的 Forth 標頭裡,位置是「名字結尾 + 9」的那個 dword:

    <名字> <長度位元組> <dword seq> <dword f2> <dword xt>

image 裡其實有 46 個色彩 word、43 個查得到名字,那張斜線分隔清單上的
29 個只是**語法設定檔用得到**的子集。檔案清單的副檔名配色用的是
DIR-* 那幾個,不在清單上。

用法:
  tools/palette.py [WINCV.IMG]        只印清單上的 29 個
  tools/palette.py -all [WINCV.IMG]   掃出全部有名字的色彩 word
輸出可以直接貼進 internal/render/raster.go 的 DefaultPalette。

[雷] LTGRAY 在 image 裡定義了兩次(#C0C0C0 與 #C5C5C5)。這支程式取
先找到的那一個;哪一個才是語法上色用的還沒實測,見 CLAUDE.md §9 的 A13。
"""
import struct
import sys

# 名字與順序來自 image 0x5692d 的斜線分隔清單
NAMES = ("black dkgray red ltred green ltgreen blue ltblue yellow mildyellow "
         "ltyellow magenta ltmagenta cyan ltcyan gray white ltgray purple "
         "ltpurple orange ltorange gooseyellow bluegreen inkgreen mildwhite "
         "mildgreen mildcyan mildmagenta").split()

# 每個顏色 word 的 body 都以同一段開頭,拿來確認找到的是顏色而不是同名的別的東西
BODY_PREFIX = bytes.fromhex("f0000000d4d30100")


def is_word_char(b):
    return 65 <= b <= 90 or 97 <= b <= 122 or b == 45


def find_color(d, name):
    b = name.upper().encode()
    i = 0
    while True:
        i = d.find(b, i)
        if i < 0:
            return None
        end = i + len(b)
        # 名字後面緊接著長度位元組,而且前一個位元組不是字母(否則是子字串)
        if (end + 13 <= len(d) and d[end] == len(b)
                and not is_word_char(d[i - 1])):
            xt = struct.unpack("<I", d[end + 9:end + 13])[0]
            if 0 < xt < len(d) and d[xt:xt + 8] == BODY_PREFIX:
                return xt, d[xt + 8:xt + 11]
        i += 1


def scan_all(d):
    """掃出所有具有色彩 word 版面的物件,並從標頭反查名字。"""
    import re
    bodies = {}
    for m in re.finditer(re.escape(BODY_PREFIX), d):
        o = m.start()
        bodies[o] = (d[o + 8], d[o + 9], d[o + 10])
    names = {}
    hdr = 0x122794
    for m in re.finditer(b"\xff\xff\xff\xff", d[hdr:]):
        p = hdr + m.start() + 4
        while p < len(d) and d[p] == 0:
            p += 1
        q = p
        while q < len(d) and 33 <= d[q] <= 126:
            q += 1
        if q <= p or q + 13 > len(d) or d[q] != q - p:
            continue
        xt = struct.unpack("<I", d[q + 9:q + 13])[0]
        if xt in bodies:
            names.setdefault(xt, d[p:q].decode("latin1"))
    return bodies, names


def main():
    args = [a for a in sys.argv[1:] if a != "-all"]
    show_all = "-all" in sys.argv
    path = args[0] if args else "original/app/WINCV.IMG"
    d = open(path, "rb").read()
    if show_all:
        bodies, names = scan_all(d)
        print(f"# 色彩 word {len(bodies)} 個,其中 {len(names)} 個查得到名字")
        for o in sorted(bodies):
            r, g, b = bodies[o]
            print(f"  {o:08x}  #{r:02X}{g:02X}{b:02X}  {names.get(o, '(無名)')}")
        return
    missing = []
    for n in NAMES:
        got = find_color(d, n)
        if got is None:
            missing.append(n)
            print(f"\t// {n}: 找不到")
            continue
        _, (r, g, b) = got[0], got[1]
        print(f"\t{{0x{r:02X}, 0x{g:02X}, 0x{b:02X}, 0xFF}}, // {n}")
    if missing:
        sys.exit(f"\n有 {len(missing)} 個沒抽到: {missing}")


if __name__ == "__main__":
    main()
