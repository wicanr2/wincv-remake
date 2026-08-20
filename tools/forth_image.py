#!/usr/bin/env python3
"""WINCV.IMG (Win32Forth v4 STC dictionary image) 解析器。

驗證狀態:
  [已驗證] header space 起點取自 image header 第 0x0c 欄 (值 0x122794)
  [已驗證] header record 版面: FF FF FF FF | 0 padding | name chars | count byte
                              | dword seq | dword f2 | dword xt
           9497 筆、8957 個唯一名稱,其中 3509 筆 xt 命中 code body。
  [已驗證] code body 起點特徵: 位址 A 的 dword 值 == A+4,其後多為 83 ED 04 8F 45 00
           全 image 3663 個,3634 個符合序言。
  [假設待驗] f2 欄位語意 (疑似 vocabulary / hash link),尚未確認。
  [假設待驗] seq 欄位為定義順序流水號,尚未逐筆對照。

用法:
  python3 tools/forth_image.py symbols WINCV.IMG > docs/re/symbols.tsv
  python3 tools/forth_image.py words   WINCV.IMG > docs/re/words.tsv
  python3 tools/forth_image.py header  WINCV.IMG
"""
import struct
import sys

STC_PROLOGUE = b"\x83\xed\x04\x8f\x45\x00"


def read(path):
    with open(path, "rb") as f:
        return f.read()


def image_header(d):
    """image 開頭 0x28 bytes 的欄位。名稱是推測,值是實測。"""
    fields = struct.unpack_from("<10I", d, 0)
    return {
        "raw": fields,
        "header_space_start": fields[3],  # 0x0c: 實測等於 header space 起點
        "code_space_end": fields[2],      # 0x08
        "header_space_size": fields[4],   # 0x10
        "app_base_hint": fields[8],       # 0x20
        "image_base": fields[9],          # 0x24
    }


def word_bodies(d):
    """回傳 code body 起始位址 (image-relative)。"""
    n = len(d)
    out = []
    for a in range(0, n - 4):
        if struct.unpack_from("<I", d, a)[0] == a + 4:
            out.append((a, d[a + 4:a + 10] == STC_PROLOGUE))
    return out


def symbols(d):
    """走訪 header space,回傳 (name, seq, f2, xt)。"""
    n = len(d)
    i = image_header(d)["header_space_start"]
    recs = []
    while i < n - 16:
        if d[i:i + 4] != b"\xff\xff\xff\xff":
            i += 1
            continue
        j = i + 4
        while j < n and d[j] == 0:
            j += 1
        start = j
        while j < n and 33 <= d[j] < 127:
            j += 1
        name = d[start:j]
        if name and j < n and d[j] == len(name):
            seq, f2, xt = struct.unpack_from("<III", d, j + 1)
            recs.append((name.decode("latin1"), seq, f2, xt))
            i = j + 13
        else:
            i += 4
    return recs


def main():
    if len(sys.argv) != 3:
        sys.exit(__doc__)
    cmd, path = sys.argv[1], sys.argv[2]
    d = read(path)
    if cmd == "header":
        h = image_header(d)
        print(f"image size          {len(d):#x}")
        for k, v in h.items():
            if k != "raw":
                print(f"{k:20s}{v:#x}")
        print("raw[0:10]           " + " ".join(f"{x:#010x}" for x in h["raw"]))
    elif cmd == "symbols":
        recs = symbols(d)
        bodies = {a for a, _ in word_bodies(d)}
        print("seq\txt\tin_code\tname")
        for name, seq, _f2, xt in recs:
            print(f"{seq:#x}\t{xt:#010x}\t{int(xt in bodies)}\t{name}")
        print(f"# records={len(recs)} unique={len({r[0] for r in recs})}",
              file=sys.stderr)
    elif cmd == "words":
        print("addr\tstc_prologue")
        hits = word_bodies(d)
        for a, ok in hits:
            print(f"{a:#010x}\t{int(ok)}")
        print(f"# bodies={len(hits)} with_prologue={sum(1 for _, ok in hits if ok)}",
              file=sys.stderr)
    else:
        sys.exit(__doc__)


if __name__ == "__main__":
    main()
