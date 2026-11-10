# 產生檔案列表比對用的目錄。內容與時間戳固定,可重現。
import os, sys
root = sys.argv[1]
os.makedirs(root, exist_ok=True)
def touch(name, size=0, t=1_000_000_000):
    p = os.path.join(root.encode(), name if isinstance(name, bytes) else name.encode())
    with open(p, 'wb') as f: f.write(b'x' * size)
    os.utime(p, (t, t))
for i in range(40):
    touch(f"file{i:02d}.dat", size=i * 1234567, t=900_000_000 + i * 86_400 * 17)
touch("README"); touch("readme", 5); touch("ReadMe.txt", 7); touch("readme.TXT", 7)
touch("a-very-long-filename-that-overflows.extension", 3)
touch("中文檔名.txt", 12345); touch("全形ＡＢＣ.dat", 1)
touch(b"\xa4\xa4\xa4\xe5big5.txt", 42)       # Big5「中文」的原始位元組,不是合法 UTF-8
touch(b"bad\xff\xfename.bin", 9)
touch(".hidden", 2); touch("noext", 8); touch("x.y.z", 9)
# ANSI 色碼:前景、背景、亮色(1 是亮色不是粗體)、重設、沒有結束字元的序列
ansi = ("\x1b[1;31m紅色亮\x1b[0m 一般 \x1b[44;33m藍底黃字\x1b[0m\n"
        "\x1b[32mgreen\x1b[1m bright\x1b[22m dim\x1b[39m default\n"
        "\x1b[100m\x1b[97mbright bg\x1b[0m\r\n"
        "crlf line\rcr line\n\n"
        "tab\there\x1b[5m unterminated \x1b[31")
p = os.path.join(root, "ansi.ans")
with open(p, "wb") as f: f.write(ansi.encode())
os.utime(p, (1_000_000_000, 1_000_000_000))
for d in ["subdir", "Another Dir", "目錄"]:
    os.makedirs(os.path.join(root, d), exist_ok=True)
    os.utime(os.path.join(root, d), (1_200_000_000, 1_200_000_000))
link = os.path.join(root, "link-to-subdir")
if not os.path.lexists(link): os.symlink("subdir", link)

# 壓縮檔場景(壓縮檔當目錄瀏覽):放在 arch/ 底下,時間戳固定。
import zipfile, tarfile, io
arch = os.path.join(root, "arch")
os.makedirs(arch, exist_ok=True)

class RawName(zipfile.ZipInfo):
    """檔名照原始位元組寫進去(Python 預設把非 ASCII 寫成 UTF-8 並設旗標)。"""
    def __init__(self, raw, date_time):
        super().__init__(raw.decode("latin-1"), date_time=date_time)
        self.raw = raw
        self.compress_type = zipfile.ZIP_DEFLATED
    def _encodeFilenameFlags(self):
        return self.raw, self.flag_bits

zp = os.path.join(arch, "big5.zip")
with zipfile.ZipFile(zp, "w") as z:
    # 只存檔案、不存目錄項:中間層目錄要由 vfs 補出來。
    for raw, size, dt in [
        (b"readme.txt", 120, (2011, 11, 25, 10, 0, 0)),
        (b"docs/guide.txt", 3456, (2001, 1, 2, 3, 4, 6)),
        (b"docs/img/a.bmp", 78901, (1999, 12, 31, 23, 58, 0)),
        (b"\xa4\xa4\xa4\xe5/\xc0\xc9\xae\xd7.txt", 42, (1998, 7, 8, 9, 10, 12)),
        (b"\xa4\xa4\xa4\xe5.doc", 9999, (1997, 3, 4, 5, 6, 8)),
        (b"empty/", 0, (2005, 5, 5, 5, 5, 4)),
    ]:
        z.writestr(RawName(raw, dt), b"" if raw.endswith(b"/") else b"z" * size)
os.utime(zp, (1_000_000_000, 1_000_000_000))

tp = os.path.join(arch, "dot.tar.gz")
buf = io.BytesIO()
with tarfile.open(fileobj=buf, mode="w", format=tarfile.GNU_FORMAT) as t:
    for name, size, mt, kind in [
        ("./docs", 0, 1_100_000_000, tarfile.DIRTYPE),
        ("./docs/a.txt", 11, 1_100_000_100, tarfile.REGTYPE),
        ("./b.txt", 22, 1_100_000_200, tarfile.REGTYPE),
        ("top/deep/c.txt", 33, 1_100_000_300, tarfile.REGTYPE),
        (b"\xa4\xa4\xa4\xe5.txt".decode("utf-8", "surrogateescape"), 44, 1_100_000_400, tarfile.REGTYPE),
    ]:
        ti = tarfile.TarInfo(name)
        ti.size, ti.mtime, ti.type, ti.mode = size, mt, kind, 0o644
        t.addfile(ti, io.BytesIO(b"t" * size) if kind == tarfile.REGTYPE else None)
import gzip
with open(tp, "wb") as f:
    f.write(gzip.compress(buf.getvalue(), mtime=0))
os.utime(tp, (1_000_000_000, 1_000_000_000))
os.utime(arch, (1_200_000_000, 1_200_000_000))

# 編輯器場景(語法上色):跨行註解、字串、數字、Tab、全形字。
ed = os.path.join(root, "edit")
os.makedirs(ed, exist_ok=True)
with open(os.path.join(ed, "sample.c"), "w", encoding="utf-8", newline="\n") as f:
    f.write("""/* 跨行註解的第一行
   第二行 int return 都不該上色 */
#include <stdio.h>

int main(void) {
\tint n = 42, h = 0x1F;\t// 行註解:中文
\tchar *s = "字串 \\"含跳脫\\" return";
\tfor (n = 0; n < 10; n++) { /* 同一行開關 */ h += n; }
\treturn 0;
}
""")
with open(os.path.join(ed, "sample.pas"), "w", encoding="utf-8", newline="\r\n") as f:
    f.write("""program Hello;
{ Pascal 的註解 }
VAR i : Integer;
BEGIN
  For i := 1 To 3 Do WriteLn('第 ', i, ' 次');
END.
""")

# 看圖場景:32 位元 TGA 的 alpha 漸層(半透明疊色)與 PGM 灰階。
im = os.path.join(root, "img")
os.makedirs(im, exist_ok=True)
w, h = 64, 48
tga = bytearray(18)
tga[2] = 2                                  # 不壓縮 RGB
tga[12:14] = w.to_bytes(2, "little")
tga[14:16] = h.to_bytes(2, "little")
tga[16] = 32
tga[17] = 0x28                              # 原點在上、8 位元 alpha
for y in range(h):
    for x in range(w):
        tga += bytes([x * 4 % 256, y * 5 % 256, 200, x * 255 // (w - 1)])   # BGRA
with open(os.path.join(im, "grad.tga"), "wb") as f: f.write(tga)
with open(os.path.join(im, "gray.pgm"), "wb") as f:
    f.write(b"P5\n32 16\n255\n" + bytes((x * 8 + y * 3) % 256 for y in range(16) for x in range(32)))
for n in ("grad.tga", "gray.pgm"):
    os.utime(os.path.join(im, n), (1_000_000_000, 1_000_000_000))

# 後備字型的場景:Big5 以外的字(简体、日文、韓文、希臘、西里爾、帶附加符號的拉丁字母、符號)。
uni = (
    "繁體:倚天中文 WinCV 檔案瀏覽\n"
    "简体:简体中文汉字为这个们来对说时国后会还发经现开问\n"
    "日本語:ひらがな カタカナ 漢字\n"
    "한국어:한글 음절 조합\n"
    "Ελληνικά: άέήίόύώ ΐΰ\n"
    "Русский: съешь же ещё этих мягких булок\n"
    "Latin: ÃÑÕĞĨŎŨŬ ąćęłńśźż ŞğıİÇ\n"
    "符號:①②③★☆♠♥←↑→↓√∞≠≤≥\n"
)
with open(os.path.join(root, "unicode.txt"), "w", encoding="utf-8", newline="\n") as f:
    f.write(uni)
os.utime(os.path.join(root, "unicode.txt"), (1_000_000_000, 1_000_000_000))

# markdown 檢視:標題、清單、表格、程式碼、引言、連結,加兩張內嵌圖(與 img/ 同一份)。
md = os.path.join(root, "md")
os.makedirs(os.path.join(md, "img"), exist_ok=True)
for n in ("grad.tga", "gray.pgm"):
    with open(os.path.join(im, n), "rb") as src, open(os.path.join(md, "img", n), "wb") as dst:
        dst.write(src.read())
doc = """# WinCV 文件檢視

原版沒有 markdown,重製版**順手**支援,`README.md` 這類文件可以*直接*看。

## 清單

- 第一項
- 第二項,內文夠長才會折行:這是一段夠長的內文，用來確認折行與縮排是否與 Go 版一致。
  1. 編號一
  2. 編號二

> 引言:看文件的人要的是「這裡有張圖」。

| 格式 | 自寫 | 備註 |
|:---|:---:|---:|
| LZH | 是 | 675 個成員 |
| ACE | 是 | 269 個成員 |

```go
func main() { println("hello") }
```

![漸層](img/grad.tga)

---

![灰階](img/gray.pgm) [連結](https://example.com)

結尾。
"""
with open(os.path.join(md, "doc.md"), "w", encoding="utf-8", newline="\n") as f:
    f.write(doc)
for p in ("img/grad.tga", "img/gray.pgm", "doc.md", "img", "."):
    os.utime(os.path.join(md, p), (1_000_000_000, 1_000_000_000))

# EPUB:目錄、兩個章節,第二章有一張書內的圖(與 img/ 同一份 PGM)。zip 成員的時間固定,
# 檔案逐位元組可重現。
import zipfile
bk = os.path.join(root, "book")
os.makedirs(bk, exist_ok=True)
with open(os.path.join(im, "gray.pgm"), "rb") as src:
    gray = src.read()
book_files = [
    ("mimetype", b"application/epub+zip"),
    ("META-INF/container.xml", b'<?xml version="1.0"?>\n<container xmlns="urn:oasis:names:tc:opendocument:xmlns:container" version="1.0">\n<rootfiles><rootfile full-path="OEBPS/content.opf" media-type="application/oebps-package+xml"/></rootfiles></container>'),
    ("OEBPS/content.opf", """<?xml version="1.0"?>
<package xmlns="http://www.idpf.org/2007/opf" xmlns:dc="http://purl.org/dc/elements/1.1/" version="2.0">
  <metadata><dc:title>測試之書</dc:title><dc:creator>作者甲</dc:creator></metadata>
  <manifest>
    <item id="ncx" href="toc.ncx" media-type="application/x-dtbncx+xml"/>
    <item id="a" href="a.xhtml" media-type="application/xhtml+xml"/>
    <item id="b" href="b.xhtml" media-type="application/xhtml+xml"/>
    <item id="p" href="img/gray.pgm" media-type="image/x-portable-graymap"/>
  </manifest>
  <spine toc="ncx"><itemref idref="a"/><itemref idref="b"/></spine>
</package>""".encode()),
    ("OEBPS/toc.ncx", """<?xml version="1.0"?>
<ncx xmlns="http://www.daisy.org/z3986/2005/ncx/" version="2005-1"><navMap>
<navPoint><navLabel><text>楔子</text></navLabel><content src="a.xhtml"/></navPoint>
<navPoint><navLabel><text>正文</text></navLabel><content src="b.xhtml"/></navPoint>
</navMap></ncx>""".encode()),
    ("OEBPS/a.xhtml", "<html><body><h1>楔子</h1><p>這是楔子的內文，夠長才會折行：一本書就是一份目錄加上一堆用連結串起來的章節。</p><ul><li>第一項</li><li>第二項</li></ul></body></html>".encode()),
    ("OEBPS/b.xhtml", '<html><body><h1>正文</h1><p>這是正文的內文。</p><p><img src="img/gray.pgm" alt="灰階"/></p><p>圖後的文字。</p></body></html>'.encode()),
    ("OEBPS/img/gray.pgm", gray),
]
with zipfile.ZipFile(os.path.join(bk, "book.epub"), "w") as zf:
    for name, data in book_files:
        zi = zipfile.ZipInfo(name, date_time=(2001, 9, 9, 0, 0, 0))
        zi.compress_type = zipfile.ZIP_STORED if name == "mimetype" else zipfile.ZIP_DEFLATED
        zf.writestr(zi, data)
for p in ("book.epub", "."):
    os.utime(os.path.join(bk, p), (1_000_000_000, 1_000_000_000))
