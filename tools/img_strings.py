#!/usr/bin/env python3
"""從 WINCV.IMG 抽出 Big5 字串。

畫面上的中文靠 Wine 的字型替換才看得到,而且會受 host 字型影響;
image 裡的位元組是原始真值,抽出來比 OCR 截圖可靠。

判準:一段連續位元組,只由 ASCII 可見字元與合法 Big5 雙位元組組成,
且至少含一個 Big5 字元、總長度達門檻。Big5 範圍:
    lead  0xA1-0xF9
    trail 0x40-0x7E, 0xA1-0xFE

光靠這個判準雜訊極多(x86 指令位元組常常剛好落在合法 Big5 範圍):
image 裡 15761 個候選只有 1293 個是真的。Forth 的字串是 **counted string**,
所以再加一道「前一個 byte 等於字串長度」的檢查,雜訊就幾乎消失。
預設開啟,`--raw` 可關掉。

用法:
  python3 tools/img_strings.py original/app/WINCV.IMG
  python3 tools/img_strings.py original/app/WINCV.IMG --min 8
  python3 tools/img_strings.py original/app/WINCV.IMG --raw     # 不做 counted 檢查
"""
import argparse
import sys


def is_lead(b):
    return 0xA1 <= b <= 0xF9


def is_trail(b):
    return 0x40 <= b <= 0x7E or 0xA1 <= b <= 0xFE


def scan(d, min_chars):
    n = len(d)
    i = 0
    out = []
    while i < n:
        start = i
        nb5 = 0
        j = i
        while j < n:
            b = d[j]
            if is_lead(b) and j + 1 < n and is_trail(d[j + 1]):
                nb5 += 1
                j += 2
            elif 0x20 <= b <= 0x7E:
                j += 1
            else:
                break
        if nb5 >= 1 and (j - start) >= min_chars:
            out.append((start, d[start:j]))
            i = j
        else:
            i = start + 1
    return out


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("image")
    ap.add_argument("--min", type=int, default=6, help="最短位元組長度")
    ap.add_argument("--raw", action="store_true",
                    help="不做 counted string 檢查(雜訊會暴增)")
    args = ap.parse_args()

    d = open(args.image, "rb").read()
    hits = scan(d, args.min)
    if not args.raw:
        hits = [(o, r) for o, r in hits if o > 0 and d[o - 1] == len(r)]
    for off, raw in hits:
        s = raw.decode("big5", errors="replace")
        print(f"{off:#010x}\t{len(raw)}\t{s}")
    print(f"# {len(hits)} 筆", file=sys.stderr)


if __name__ == "__main__":
    main()
