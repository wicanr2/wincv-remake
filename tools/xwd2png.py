#!/usr/bin/env python3
"""XWD (X Window Dump, TrueColor) → PNG。

這台機器沒有 ImageMagick 的 convert,ffmpeg 也不吃 xwd,所以自己寫一個。

[雷] **每像素的位元組數要從檔頭讀,不能寫死 4。**
不同的 Xvfb(host 的與容器裡的)會給出 32 或 24 bits_per_pixel。
寫死 4 去讀 24 bpp 的資料,每個像素會少對一個位元組,結果是
**通道以 3 為週期輪轉** —— 畫面看起來像蓋了一層 R/G/B 直條紋,
而且純色區域最明顯。那看起來像「程式畫錯了」,實際上是轉檔工具錯了。

XWD 檔頭(big-endian uint32,依序):
    0 header_size   1 file_version   2 pixmap_format  3 pixmap_depth
    4 width         5 height         6 xoffset        7 byte_order
    8 bitmap_unit   9 bitmap_bit_order  10 bitmap_pad  11 bits_per_pixel
   12 bytes_per_line  13 visual_class  14 red_mask    15 green_mask
   16 blue_mask    ...                19 ncolors
"""
import struct
import sys
import zlib


def chunk(tag, data):
    return (struct.pack(">I", len(data)) + tag + data
            + struct.pack(">I", zlib.crc32(tag + data) & 0xFFFFFFFF))


def shift_of(mask):
    if mask == 0:
        return 0
    s = 0
    while not (mask >> s) & 1:
        s += 1
    return s


def main():
    if len(sys.argv) != 3:
        sys.exit("用法: xwd2png.py <in.xwd> <out.png>")
    d = open(sys.argv[1], "rb").read()
    h = struct.unpack(">25I", d[:100])
    hsz, w, height = h[0], h[4], h[5]
    byte_order, bpp, bpl, ncolors = h[7], h[11], h[12], h[19]
    rmask, gmask, bmask = h[14], h[15], h[16]

    nbytes = (bpp + 7) // 8
    if nbytes not in (3, 4):
        sys.exit(f"還沒支援 {bpp} bits_per_pixel")

    rsh, gsh, bsh = shift_of(rmask), shift_of(gmask), shift_of(bmask)
    off = hsz + ncolors * 12
    px = d[off:off + bpl * height]

    rows = []
    for y in range(height):
        base = y * bpl
        out = bytearray(b"\x00")
        for x in range(w):
            o = base + x * nbytes
            b = px[o:o + nbytes]
            if len(b) < nbytes:
                out += b"\x00\x00\x00"
                continue
            if byte_order == 0:  # LSBFirst
                v = int.from_bytes(b, "little")
            else:
                v = int.from_bytes(b, "big")
            out += bytes(((v & rmask) >> rsh,
                          (v & gmask) >> gsh,
                          (v & bmask) >> bsh))
        rows.append(bytes(out))

    png = (b"\x89PNG\r\n\x1a\n"
           + chunk(b"IHDR", struct.pack(">IIBBBBB", w, height, 8, 2, 0, 0, 0))
           + chunk(b"IDAT", zlib.compress(b"".join(rows), 6))
           + chunk(b"IEND", b""))
    open(sys.argv[2], "wb").write(png)


if __name__ == "__main__":
    main()
