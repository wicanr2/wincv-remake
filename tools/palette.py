#!/usr/bin/env python3
"""從 WINCV.IMG 抽出 29 個具名顏色的 RGB。

顏色是 Forth 的 word,body 有 0x24 個位元組,其中第 8-10 個就是 R、G、B
(Win32 的 COLORREF 是 0x00BBGGRR,小端存放後記憶體順序正好是 R G B)。
word 的 xt 在它的 Forth 標頭裡,位置是「名字結尾 + 9」的那個 dword:

    <名字> <長度位元組> <dword seq> <dword f2> <dword xt>

用法:tools/palette.py [original/app/WINCV.IMG]
輸出可以直接貼進 internal/render/raster.go 的 DefaultPalette。
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


def main():
    path = sys.argv[1] if len(sys.argv) > 1 else "original/app/WINCV.IMG"
    d = open(path, "rb").read()
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
