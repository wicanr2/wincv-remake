package app

import (
	"archive/zip"
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/wicanr2/wincv-remake/internal/cell"
	"github.com/wicanr2/wincv-remake/internal/keys"
	"github.com/wicanr2/wincv-remake/internal/vfs"
)

// makeEPUB 造一本兩章的書,回傳它所在的目錄。
func makeEPUB(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	files := map[string]string{
		"mimetype": "application/epub+zip",
		"META-INF/container.xml": `<?xml version="1.0"?>
<container xmlns="urn:oasis:names:tc:opendocument:xmlns:container" version="1.0">
<rootfiles><rootfile full-path="OEBPS/content.opf"
 media-type="application/oebps-package+xml"/></rootfiles></container>`,
		"OEBPS/content.opf": `<?xml version="1.0"?>
<package xmlns:opf="http://www.idpf.org/2007/opf"
         xmlns:dc="http://purl.org/dc/elements/1.1/"
         xmlns="http://www.idpf.org/2007/opf" version="2.0">
  <metadata><dc:title>測試之書</dc:title><dc:creator>作者甲</dc:creator></metadata>
  <manifest>
    <item id="ncx" href="toc.ncx" media-type="application/x-dtbncx+xml"/>
    <item id="a" href="a.xhtml" media-type="application/xhtml+xml"/>
    <item id="b" href="b.xhtml" media-type="application/xhtml+xml"/>
  </manifest>
  <spine toc="ncx"><itemref idref="a"/><itemref idref="b"/></spine>
</package>`,
		"OEBPS/toc.ncx": `<?xml version="1.0"?>
<ncx xmlns="http://www.daisy.org/z3986/2005/ncx/" version="2005-1"><navMap>
<navPoint><navLabel><text>楔子</text></navLabel><content src="a.xhtml"/></navPoint>
<navPoint><navLabel><text>正文</text></navLabel><content src="b.xhtml"/></navPoint>
</navMap></ncx>`,
		"OEBPS/a.xhtml": `<html><body><h1>楔子</h1><p>這是楔子的內文。</p></body></html>`,
		"OEBPS/b.xhtml": `<html><body><h1>正文</h1><p>這是正文的內文。</p></body></html>`,
	}
	for name, body := range files {
		w, _ := zw.Create(name)
		w.Write([]byte(body))
	}
	zw.Close()
	if err := os.WriteFile(filepath.Join(dir, "book.epub"), buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func bookApp(t *testing.T) (*App, *cell.Screen) {
	t.Helper()
	a := New(vfs.OS{}, makeEPUB(t))
	a.CellW, a.CellH = 8, 16
	s := cell.New(70, 20)
	a.Draw(s)
	a.focusOn("book.epub")
	return a, s
}

// 按 Enter 開 .epub 要看到目錄,不是壓縮檔的內容。
func TestEnterEPUBShowsTOC(t *testing.T) {
	a, s := bookApp(t)
	a.HandleKey(keys.Named(keys.Enter))
	a.Draw(s)

	txt := screenText(s)
	for _, want := range []string{"測試之書", "作者甲", "楔子", "正文"} {
		if !strings.Contains(txt, want) {
			t.Fatalf("目錄上沒有 %q\n%s", want, txt)
		}
	}
	// [雷] EPUB 就是一個 zip。被當成壓縮檔進去的話會看到這些。
	for _, bad := range []string{"META-INF", "content.opf", "mimetype"} {
		if strings.Contains(txt, bad) {
			t.Fatalf("被當成壓縮檔打開了(看到 %q)\n%s", bad, txt)
		}
	}
}

// 點章節看內文,Backspace 回目錄。
func TestBookChapterNavigation(t *testing.T) {
	a, s := bookApp(t)
	a.HandleKey(keys.Named(keys.Enter)) // 開書
	a.Draw(s)
	a.HandleKey(keys.Named(keys.Enter)) // 第一個連結 = 第一章
	a.Draw(s)

	if txt := screenText(s); !strings.Contains(txt, "這是楔子的內文") {
		t.Fatalf("沒進到第一章\n%s", txt)
	}
	// 章末的導覽:目錄與下一節。
	if txt := screenText(s); !strings.Contains(txt, "下一節") {
		t.Errorf("章末沒有下一節\n%s", txt)
	}

	a.HandleKey(keys.Named(keys.Backspace))
	a.Draw(s)
	if txt := screenText(s); !strings.Contains(txt, "測試之書") {
		t.Fatalf("回不到目錄\n%s", txt)
	}
}

// 位址的解析要對得起來,而且書的路徑本身含 # 也不能拆錯。
func TestBookURLRoundTrip(t *testing.T) {
	for _, tc := range []struct {
		path string
		ch   int
	}{
		{"/tmp/a.epub", -1},
		{"/tmp/a.epub", 0},
		{"/tmp/a.epub", 42},
		{"/tmp/怪#名字.epub", 3},
	} {
		raw := bookURL(tc.path, tc.ch)
		p, c, ok := parseBookURL(raw)
		if !ok || p != tc.path || c != tc.ch {
			t.Errorf("%q → path=%q ch=%d ok=%v", raw, p, c, ok)
		}
	}
	if _, _, ok := parseBookURL("gopher://x/1"); ok {
		t.Error("gopher 位址不該被當成書")
	}
}

// 離開瀏覽模式要把書關掉,不然那個 zip 一直開著。
func TestEscClosesBook(t *testing.T) {
	a, s := bookApp(t)
	a.HandleKey(keys.Named(keys.Enter))
	a.Draw(s)
	if a.book == nil {
		t.Fatal("書沒開起來")
	}
	a.HandleKey(keys.Named(keys.Esc))
	if a.book != nil {
		t.Fatal("離開之後書還開著")
	}
}
