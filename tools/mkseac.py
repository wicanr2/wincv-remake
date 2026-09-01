"""產生 internal/pdf/testdata/seac.pdf。

裡面是一份自己寫的最小 Type1 字型,三個字形都是方塊,第三個用 seac
(把兩個標準字形拼成一個重音字)組出來。用方塊而不是真的字母,是因為
「重音有沒有偏一點」在真字母上看不出來,在方塊上差一格都看得到。

字形的 charstring 是 eexec 加密的,打開 PDF 看不到 —— 所以這支程式本身
就是那份 fixture 的規格:要確認裡面到底畫了什麼,看這裡。

    python3 tools/mkseac.py internal/pdf/testdata/seac.pdf
"""
import sys

def num(v):
    v = int(v)
    if -107 <= v <= 107:
        return bytes([v + 139])
    if 108 <= v <= 1131:
        v -= 108
        return bytes([(v >> 8) + 247, v & 0xFF])
    if -1131 <= v <= -108:
        v = -v - 108
        return bytes([(v >> 8) + 251, v & 0xFF])
    return bytes([255]) + int(v).to_bytes(4, "big", signed=True)

OPS = {"hsbw": b"\x0d", "rmoveto": b"\x15", "rlineto": b"\x05",
       "closepath": b"\x09", "endchar": b"\x0e", "seac": b"\x0c\x06"}

def cs(*toks):
    out = b""
    for t in toks:
        out += OPS[t] if isinstance(t, str) else num(t)
    return out

def encrypt(plain, r, pad):
    c1, c2 = 52845, 22719
    out = bytearray()
    for p in bytes([0x55] * pad) + plain:
        c = p ^ (r >> 8)
        out.append(c)
        r = ((c + r) * c1 + c2) & 0xFFFF
    return bytes(out)

# 三個字形。座標都是千分之一字身。
#   A       50..450 × 0..600 的方塊
#   acute   50..250 × 700..800 的小方塊
#   Aacute  用 seac 把上面兩個拼起來,重音再往右 300、往上 100
glyphs = {
    ".notdef": cs(0, 600, "hsbw", "endchar"),
    "A": cs(50, 600, "hsbw", 0, 0, "rmoveto", 400, 0, "rlineto",
            0, 600, "rlineto", -400, 0, "rlineto", "closepath", "endchar"),
    "acute": cs(50, 300, "hsbw", 0, 700, "rmoveto", 200, 0, "rlineto",
                0, 100, "rlineto", -200, 0, "rlineto", "closepath", "endchar"),
    # asb adx ady bchar achar seac;65 = A、194 = acute(StandardEncoding 的字碼)
    "Aacute": cs(50, 600, "hsbw", 50, 300, 100, 65, 194, "seac"),
}

clear = b"""%!FontType1-1.0: SeacTest 001.001
/FontName /SeacTest def
/PaintType 0 def
/FontType 1 def
/FontMatrix [0.001 0 0 0.001 0 0] readonly def
/Encoding 256 array
0 1 255 {1 index exch /.notdef put} for
dup 65 /A put
dup 194 /acute put
dup 200 /Aacute put
readonly def
/FontBBox {0 0 600 950} readonly def
currentdict end
currentfile eexec
"""

body = b"""userdict /RD {string currentfile exch readstring pop} executeonly put
userdict /ND {noaccess def} executeonly put
dup /Private 8 dict dup begin
/MinFeature {16 16} noaccess def
/password 5839 def
/lenIV 4 def
/BlueValues [] noaccess def
/ForceBold false def
end
/CharStrings 4 dict dup begin
"""
for name in [".notdef", "A", "acute", "Aacute"]:
    e = encrypt(glyphs[name], 4330, 4)
    body += b"/%s %d RD %s ND\n" % (name.encode(), len(e), e)
body += b"""end
end
readonly put
noaccess put
dup /FontName get exch definefont pop
mark currentfile closefile
"""

encbin = encrypt(body, 55665, 4)
# 用十六進位寫,整份字型就都是可讀的 ASCII;兩邊的解析器都認得。
hexed = b""
h = encbin.hex().encode()
for i in range(0, len(h), 64):
    hexed += h[i:i+64] + b"\n"
trailer = (b"0" * 64 + b"\n") * 8 + b"cleartomark\n"
font = clear + hexed + trailer
l1, l2, l3 = len(clear), len(hexed), len(trailer)

objs = {}
objs[1] = b"<</Type/Catalog/Pages 2 0 R>>"
objs[2] = b"<</Type/Pages/Kids[3 0 R]/Count 1>>"
objs[3] = (b"<</Type/Page/Parent 2 0 R/MediaBox[0 0 612 792]/Contents 4 0 R"
           b"/Resources<</Font<</F1 5 0 R>>>>>>")
content = b"BT /F1 100 Tf 72 500 Td (\\101\\302\\310) Tj ET\n"
objs[4] = b"<</Length %d>>\nstream\n" % len(content) + content + b"endstream"
objs[5] = (b"<</Type/Font/Subtype/Type1/BaseFont/SeacTest/FirstChar 65/LastChar 200"
           b"/Widths[600" + b" 0" * 128 + b" 300" + b" 0" * 5 + b" 600]"
           b"/FontDescriptor 6 0 R>>")
objs[6] = (b"<</Type/FontDescriptor/FontName/SeacTest/Flags 4"
           b"/FontBBox[0 0 600 950]/ItalicAngle 0/Ascent 950/Descent 0"
           b"/CapHeight 600/StemV 80/FontFile 7 0 R>>")
objs[7] = (b"<</Length %d/Length1 %d/Length2 %d/Length3 %d>>\nstream\n"
           % (len(font), l1, l2, l3) + font + b"\nendstream")

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
print("寫出", len(out), "位元組;Length1/2/3 =", l1, l2, l3)
