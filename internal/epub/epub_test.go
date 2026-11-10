package epub_test

import (
	"archive/zip"
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf16"

	"github.com/wicanr2/wincv-remake/internal/epub"
	"github.com/wicanr2/wincv-remake/internal/markdown"
	"github.com/wicanr2/wincv-remake/internal/textenc"
)

// build 產生一本 EPUB。files 的鍵是 zip 內的路徑。
func build(t *testing.T, files map[string]string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "book.epub")
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, body := range files {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte(body)); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

const containerXML = `<?xml version="1.0"?>
<container version="1.0" xmlns="urn:oasis:names:tc:opendocument:xmlns:container">
  <rootfiles><rootfile full-path="OEBPS/content.opf"
     media-type="application/oebps-package+xml"/></rootfiles>
</container>`

// [雷] 這裡的 xmlns 宣告要**照真書那樣一次宣告好幾個**。
// 只宣告一個的話,「關掉 Strict 導致整份 OPF 解成空的」這個 bug
// 不會被觸發,測試會全過而真書一章都讀不出來。
const contentOPF = `<?xml version="1.0"?>
<package xmlns:opf="http://www.idpf.org/2007/opf"
         xmlns:dc="http://purl.org/dc/elements/1.1/"
         xmlns:dcterms="http://purl.org/dc/terms/"
         xmlns:xsi="http://www.w3.org/2001/XMLSchema-instance"
         xmlns="http://www.idpf.org/2007/opf" version="2.0" unique-identifier="id">
  <metadata>
    <dc:title>測試書</dc:title>
    <dc:creator>某某</dc:creator>
  </metadata>
  <manifest>
    <item id="ncx" href="toc.ncx" media-type="application/x-dtbncx+xml"/>
    <item id="c1" href="ch1.xhtml" media-type="application/xhtml+xml"/>
    <item id="c2" href="ch2.xhtml" media-type="application/xhtml+xml"/>
    <item id="img" href="images/p.png" media-type="image/png"/>
  </manifest>
  <spine toc="ncx">
    <itemref idref="c1"/>
    <itemref idref="c2"/>
  </spine>
</package>`

const tocNCX = `<?xml version="1.0"?>
<ncx xmlns="http://www.daisy.org/z3986/2005/ncx/" version="2005-1">
  <navMap>
    <navPoint id="n1" playOrder="1">
      <navLabel><text>第一章 開始</text></navLabel>
      <content src="ch1.xhtml"/>
    </navPoint>
    <navPoint id="n2" playOrder="2">
      <navLabel><text>第二章 結束</text></navLabel>
      <content src="ch2.xhtml#top"/>
    </navPoint>
  </navMap>
</ncx>`

func sample(t *testing.T) string {
	return build(t, map[string]string{
		"mimetype":               "application/epub+zip",
		"META-INF/container.xml": containerXML,
		"OEBPS/content.opf":      contentOPF,
		"OEBPS/toc.ncx":          tocNCX,
		"OEBPS/ch1.xhtml": `<html><body><h1>開始</h1>
			<p>第一章的內文。</p><img src="images/p.png" alt="插圖"/></body></html>`,
		"OEBPS/ch2.xhtml":    `<html><body><h1>結束</h1><p>第二章的內文。</p></body></html>`,
		"OEBPS/images/p.png": "\x89PNG\r\n\x1a\n假的",
	})
}

func TestOpenReadsMetadataAndSpine(t *testing.T) {
	b, err := epub.Open(sample(t))
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()

	if b.Title != "測試書" || b.Author != "某某" {
		t.Fatalf("書名 %q 作者 %q", b.Title, b.Author)
	}
	if len(b.Chapters) != 2 {
		t.Fatalf("解出 %d 章", len(b.Chapters))
	}
	// 章節標題取自 NCX,而且 #片段 要去掉才對得上檔案。
	if b.Chapters[0].Title != "第一章 開始" || b.Chapters[1].Title != "第二章 結束" {
		t.Fatalf("標題是 %q / %q", b.Chapters[0].Title, b.Chapters[1].Title)
	}
}

func TestBlocksAndImages(t *testing.T) {
	b, err := epub.Open(sample(t))
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()

	blocks, err := b.Blocks(0)
	if err != nil {
		t.Fatal(err)
	}
	var text strings.Builder
	var imgSrc string
	for _, blk := range blocks {
		for _, sp := range blk.Spans {
			text.WriteString(sp.Text)
		}
		if blk.Kind == markdown.Image {
			imgSrc = blk.Src
		}
	}
	if !strings.Contains(text.String(), "第一章的內文") {
		t.Fatalf("內文是 %q", text.String())
	}
	// 圖片的相對路徑要接成 zip 內的路徑,否則讀不到。
	if imgSrc == "" {
		t.Fatal("沒有解出圖片")
	}
	data, err := b.Image(imgSrc)
	if err != nil {
		t.Fatalf("讀不到圖 %q: %v", imgSrc, err)
	}
	if !bytes.HasPrefix(data, []byte("\x89PNG")) {
		t.Fatalf("讀出來的不是那張圖:%q", data)
	}
}

// [雷] OPF 寫 Text/ch1.xhtml、zip 裡是 text/ch1.xhtml 這種事很常見。
// 區分大小寫的話整本書一章都讀不出來。
func TestPathsAreCaseInsensitive(t *testing.T) {
	p := build(t, map[string]string{
		"META-INF/container.xml": containerXML,
		"OEBPS/content.opf": strings.Replace(contentOPF,
			`href="ch1.xhtml"`, `href="Ch1.XHTML"`, 1),
		"OEBPS/toc.ncx":   tocNCX,
		"OEBPS/ch1.xhtml": `<html><body><p>大小寫不同也要讀得到</p></body></html>`,
		"OEBPS/ch2.xhtml": `<html><body><p>二</p></body></html>`,
	})
	b, err := epub.Open(p)
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	blocks, err := b.Blocks(0)
	if err != nil {
		t.Fatal(err)
	}
	if len(blocks) == 0 {
		t.Fatal("第一章是空的")
	}
}

// container.xml 壞掉或不見時退回掃一個 .opf ——
// 讀不了整本書和少一個索引檔是完全不同的嚴重程度。
func TestSurvivesBrokenContainer(t *testing.T) {
	for _, c := range []string{"", "<container>壞掉的", `<container><rootfiles/></container>`} {
		p := build(t, map[string]string{
			"META-INF/container.xml": c,
			"OEBPS/content.opf":      contentOPF,
			"OEBPS/toc.ncx":          tocNCX,
			"OEBPS/ch1.xhtml":        `<html><body><p>一</p></body></html>`,
			"OEBPS/ch2.xhtml":        `<html><body><p>二</p></body></html>`,
		})
		b, err := epub.Open(p)
		if err != nil {
			t.Fatalf("container=%q 打不開:%v", c, err)
		}
		if len(b.Chapters) != 2 {
			t.Errorf("container=%q 解出 %d 章", c, len(b.Chapters))
		}
		b.Close()
	}
}

// EPUB 3 用 nav.xhtml 當目錄,沒有 NCX。
func TestEPUB3Nav(t *testing.T) {
	opf3 := `<?xml version="1.0"?>
<package xmlns:opf="http://www.idpf.org/2007/opf"
         xmlns:dc="http://purl.org/dc/elements/1.1/"
         xmlns:dcterms="http://purl.org/dc/terms/"
         xmlns="http://www.idpf.org/2007/opf" version="3.0">
  <metadata><dc:title>三版</dc:title></metadata>
  <manifest>
    <item id="nav" href="nav.xhtml" media-type="application/xhtml+xml" properties="nav"/>
    <item id="c1" href="ch1.xhtml" media-type="application/xhtml+xml"/>
  </manifest>
  <spine><itemref idref="c1"/></spine>
</package>`
	p := build(t, map[string]string{
		"META-INF/container.xml": containerXML,
		"OEBPS/content.opf":      opf3,
		"OEBPS/nav.xhtml": `<html><body><nav epub:type="toc"><ol>
			<li><a href="ch1.xhtml">序章</a></li></ol></nav></body></html>`,
		"OEBPS/ch1.xhtml": `<html><body><p>內文</p></body></html>`,
	})
	b, err := epub.Open(p)
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	if b.Title != "三版" {
		t.Errorf("書名是 %q", b.Title)
	}
	if len(b.Chapters) != 1 || b.Chapters[0].Title != "序章" {
		t.Fatalf("章節是 %+v", b.Chapters)
	}
}

// 不是 EPUB 的東西要明確報錯,不能裝作讀得懂。
func TestRejectsNonEPUB(t *testing.T) {
	p := build(t, map[string]string{"a.txt": "這只是個 zip"})
	if b, err := epub.Open(p); err == nil {
		b.Close()
		t.Fatal("普通的 zip 不該被當成 EPUB")
	}
	bad := filepath.Join(t.TempDir(), "x.epub")
	os.WriteFile(bad, []byte("根本不是 zip"), 0o644)
	if _, err := epub.Open(bad); err == nil {
		t.Fatal("壞檔不該打得開")
	}
}

func big5(t *testing.T, s string) string {
	t.Helper()
	b, err := textenc.EncodeBig5(s)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func utf16le(s string) string {
	out := []byte{0xFF, 0xFE}
	for _, u := range utf16.Encode([]rune(s)) {
		out = append(out, byte(u), byte(u>>8))
	}
	return string(out)
}

// 舊的中文電子書常把 OPF 與 NCX 寫成 Big5,並在 XML 宣告裡寫 encoding="big5"。
// encoding/xml 沒有 CharsetReader 時整份拒絕,書就打不開。
// UTF-16(有 BOM)是 EPUB 規格允許的另一種編碼,也要打得開。
func TestOpenNonUTF8OPF(t *testing.T) {
	decl := `<?xml version="1.0" encoding="big5"?>`
	for _, tc := range []struct{ name, opf, ncx string }{
		{"big5",
			big5(t, strings.Replace(contentOPF, `<?xml version="1.0"?>`, decl, 1)),
			big5(t, strings.Replace(tocNCX, `<?xml version="1.0"?>`, decl, 1))},
		{"utf-16",
			utf16le(strings.Replace(contentOPF, `<?xml version="1.0"?>`, `<?xml version="1.0" encoding="UTF-16"?>`, 1)),
			tocNCX},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := build(t, map[string]string{
				"META-INF/container.xml": containerXML,
				"OEBPS/content.opf":      tc.opf,
				"OEBPS/toc.ncx":          tc.ncx,
				"OEBPS/ch1.xhtml":        "<p>一</p>",
				"OEBPS/ch2.xhtml":        "<p>二</p>",
			})
			b, err := epub.Open(p)
			if err != nil {
				t.Fatal(err)
			}
			defer b.Close()
			if b.Title != "測試書" || b.Author != "某某" {
				t.Fatalf("書名 %q 作者 %q", b.Title, b.Author)
			}
			if len(b.Chapters) != 2 || b.Chapters[0].Title != "第一章 開始" {
				t.Fatalf("章節 %+v", b.Chapters)
			}
		})
	}
}

// 章節 XHTML 的編碼照 BOM、XML 宣告、meta charset 的順序決定,都沒有就判讀。
// BOM 不能留在內文裡:留著的話會多出一段只有 U+FEFF 的空段落。
func TestChapterEncoding(t *testing.T) {
	body := "<html><body><h1>開始</h1><p>第一章的內文。</p></body></html>"
	for _, tc := range []struct{ name, ch string }{
		{"utf-8 BOM", "\xef\xbb\xbf" + body},
		{"utf-16 BOM", utf16le(body)},
		{"XML 宣告", big5(t, `<?xml version="1.0" encoding="Big5"?>`+body)},
		{"meta", big5(t, `<html><head><meta http-equiv="Content-Type" content="text/html; charset=big5"/></head>`+body)},
		{"判讀", big5(t, body)},
		// 宣告寫錯(內容其實是 UTF-8)時不能照宣告解成亂碼。
		{"宣告錯", `<?xml version="1.0" encoding="big5"?>` + body},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := build(t, map[string]string{
				"META-INF/container.xml": containerXML,
				"OEBPS/content.opf":      contentOPF,
				"OEBPS/toc.ncx":          tocNCX,
				"OEBPS/ch1.xhtml":        tc.ch,
				"OEBPS/ch2.xhtml":        "<p>二</p>",
			})
			b, err := epub.Open(p)
			if err != nil {
				t.Fatal(err)
			}
			defer b.Close()
			blocks, err := b.Blocks(0)
			if err != nil {
				t.Fatal(err)
			}
			var got []string
			for _, blk := range blocks {
				var s strings.Builder
				for _, sp := range blk.Spans {
					s.WriteString(sp.Text)
				}
				got = append(got, s.String())
			}
			if len(got) != 2 || got[0] != "開始" || got[1] != "第一章的內文。" {
				t.Fatalf("解出 %q", got)
			}
		})
	}
}
