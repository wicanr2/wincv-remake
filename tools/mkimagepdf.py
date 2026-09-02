"""產生 internal/pdf/testdata/image.pdf。

兩張圖,都用 ICCBased 色彩空間 —— 物件層對這個組合會回空但不報錯,
所以這一份盯的是「自己解」那條退路:

  ImA  FlateDecode 的原始取樣值(2×2 的紅綠藍黃),外加自己的 /SMask,
       遮罩把左上與右下兩格挖成透明。用棋盤而不是單色,是因為「上下
       顛倒了、左右反了」在單色上看不出來。
  ImB  DCTDecode。那份 JPEG 是拿本專案自己的 shading.pdf 裁一小塊來的,
       不是第三方素材。

    python3 tools/mkimagepdf.py internal/pdf/testdata/image.pdf <tile.jpg>
"""
import sys, zlib, struct

def jpegSize(d):
    i = 2
    while i < len(d):
        if d[i] != 0xFF:
            i += 1
            continue
        m = d[i + 1]
        if m in (0xC0, 0xC1, 0xC2):
            h, w = struct.unpack(">HH", d[i + 5:i + 9])
            return w, h
        if m in (0xD8, 0xD9) or 0xD0 <= m <= 0xD7:
            i += 2
            continue
        i += 2 + struct.unpack(">H", d[i + 2:i + 4])[0]
    raise SystemExit("這不是 JPEG")

jpg = open(sys.argv[2], "rb").read()
w, h = jpegSize(jpg)

rgb = bytes.fromhex("FF0000" "00FF00" "0000FF" "FFFF00")
smask = bytes([0x00, 0xFF, 0xFF, 0x00])
rgbz, smaskz = zlib.compress(rgb), zlib.compress(smask)
icc = zlib.compress(b"\x00" * 128)

objs = {}
objs[1] = b"<</Type/Catalog/Pages 2 0 R>>"
objs[2] = b"<</Type/Pages/Kids[3 0 R]/Count 1>>"
objs[3] = (b"<</Type/Page/Parent 2 0 R/MediaBox[0 0 612 792]/Contents 4 0 R"
           b"/Resources<</XObject<</ImA 6 0 R/ImB 8 0 R>>>>>>")
content = (
    "% A: FlateDecode samples in an ICCBased space, with its own /SMask\n"
    "q 160 0 0 160 60 600 cm /ImA Do Q\n"
    "% B: DCTDecode in an ICCBased space\n"
    "q 160 0 0 120 280 600 cm /ImB Do Q\n"
).encode("ascii")
objs[4] = b"<</Length %d>>\nstream\n" % len(content) + content + b"endstream"
objs[5] = (b"<</N 3/Alternate/DeviceRGB/Filter/FlateDecode/Length %d>>\nstream\n" % len(icc)
           + icc + b"\nendstream")
objs[6] = (b"<</Type/XObject/Subtype/Image/Width 2/Height 2/BitsPerComponent 8"
           b"/ColorSpace[/ICCBased 5 0 R]/SMask 7 0 R/Filter/FlateDecode/Length %d>>\nstream\n" % len(rgbz)
           + rgbz + b"\nendstream")
objs[7] = (b"<</Type/XObject/Subtype/Image/Width 2/Height 2/BitsPerComponent 8"
           b"/ColorSpace/DeviceGray/Filter/FlateDecode/Length %d>>\nstream\n" % len(smaskz)
           + smaskz + b"\nendstream")
objs[8] = (b"<</Type/XObject/Subtype/Image/Width %d/Height %d/BitsPerComponent 8"
           b"/ColorSpace[/ICCBased 5 0 R]/Filter/DCTDecode/Length %d>>\nstream\n" % (w, h, len(jpg))
           + jpg + b"\nendstream")

out = bytearray(b"%PDF-1.4\n")
offs = {}
for n in sorted(objs):
    offs[n] = len(out)
    out += b"%d 0 obj\n" % n + objs[n] + b"\nendobj\n"
xref = len(out)
last = max(objs)
out += b"xref\n0 %d\n" % (last + 1) + b"0000000000 65535 f \n"
for n in range(1, last + 1):
    out += b"%010d 00000 n \n" % offs[n]
out += b"trailer\n<</Size %d/Root 1 0 R>>\nstartxref\n%d\n%%%%EOF\n" % (last + 1, xref)
open(sys.argv[1], "wb").write(bytes(out))
print("寫出 %d 位元組;JPEG %d×%d" % (len(out), w, h))
