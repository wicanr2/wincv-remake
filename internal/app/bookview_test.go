package app

import (
	"archive/zip"
	"bytes"
	"fmt"
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

// --- PDF ------------------------------------------------------------------

// makePDFFile 手寫一份兩頁的最小 PDF,回傳它所在的目錄。
func makePDFFile(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	c1 := "BT /F1 12 Tf 72 720 Td (First) Tj 40 0 Td (page) Tj ET"
	c2 := "BT /F1 12 Tf 72 720 Td (Second) Tj 50 0 Td (page) Tj ET"
	objs := []string{
		"<</Type/Catalog/Pages 2 0 R>>",
		"<</Type/Pages/Kids[3 0 R 6 0 R]/Count 2>>",
		"<</Type/Page/Parent 2 0 R/MediaBox[0 0 612 792]/Contents 4 0 R" +
			"/Resources<</Font<</F1 5 0 R>>>>>>",
		fmt.Sprintf("<</Length %d>>\nstream\n%s\nendstream", len(c1), c1),
		"<</Type/Font/Subtype/Type1/BaseFont/Helvetica>>",
		"<</Type/Page/Parent 2 0 R/MediaBox[0 0 612 792]/Contents 7 0 R" +
			"/Resources<</Font<</F1 5 0 R>>>>>>",
		fmt.Sprintf("<</Length %d>>\nstream\n%s\nendstream", len(c2), c2),
	}
	var sb strings.Builder
	sb.WriteString("%PDF-1.4\n")
	offs := make([]int, len(objs)+1)
	for i, o := range objs {
		offs[i+1] = sb.Len()
		fmt.Fprintf(&sb, "%d 0 obj\n%s\nendobj\n", i+1, o)
	}
	x := sb.Len()
	fmt.Fprintf(&sb, "xref\n0 %d\n0000000000 65535 f \n", len(objs)+1)
	for i := 1; i <= len(objs); i++ {
		fmt.Fprintf(&sb, "%010d 00000 n \n", offs[i])
	}
	fmt.Fprintf(&sb, "trailer\n<</Size %d/Root 1 0 R>>\nstartxref\n%d\n%%%%EOF\n",
		len(objs)+1, x)
	if err := os.WriteFile(filepath.Join(dir, "doc.pdf"), []byte(sb.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

// 按 Enter 開 .pdf 要直接看到第一頁,不是頁碼清單 ——
// 一份文件的「目錄」只是一排頁碼,而使用者要的就是第一頁。
func TestEnterPDFShowsFirstPage(t *testing.T) {
	a := New(vfs.OS{}, makePDFFile(t))
	a.CellW, a.CellH = 8, 16
	s := cell.New(70, 20)
	a.Draw(s)
	a.focusOn("doc.pdf")
	a.HandleKey(keys.Named(keys.Enter))
	a.Draw(s)

	txt := screenText(s)
	if !strings.Contains(txt, "First") || !strings.Contains(txt, "page") {
		t.Fatalf("第一頁的文字沒出來\n%s", txt)
	}
	if !strings.Contains(txt, "下一頁") {
		t.Errorf("頁末沒有下一頁\n%s", txt)
	}
}

// 換頁與回頁碼清單。
func TestPDFPageNavigation(t *testing.T) {
	a := New(vfs.OS{}, makePDFFile(t))
	a.CellW, a.CellH = 8, 16
	s := cell.New(70, 20)
	a.Draw(s)
	a.focusOn("doc.pdf")
	a.HandleKey(keys.Named(keys.Enter))
	a.Draw(s)

	// 第一頁只有「頁碼清單」與「下一頁」兩個連結,往下一個就是下一頁。
	a.HandleKey(keys.Named(keys.Down))
	a.HandleKey(keys.Named(keys.Enter))
	a.Draw(s)
	if txt := screenText(s); !strings.Contains(txt, "Second") {
		t.Fatalf("沒換到第二頁\n%s", txt)
	}
	a.HandleKey(keys.Named(keys.Backspace))
	a.Draw(s)
	if txt := screenText(s); !strings.Contains(txt, "First") {
		t.Fatalf("回不到第一頁\n%s", txt)
	}
}

func TestPDFURLRoundTrip(t *testing.T) {
	for _, tc := range []struct {
		path string
		page int
	}{{"/tmp/a.pdf", 0}, {"/tmp/a.pdf", 1}, {"/tmp/a.pdf", 250}, {"/tmp/怪#名.pdf", 7}} {
		raw := pdfURL(tc.path, tc.page)
		p, n, ok := parsePDFURL(raw)
		if !ok || p != tc.path || n != tc.page {
			t.Errorf("%q → %q %d %v", raw, p, n, ok)
		}
	}
}

// 不同頁可以有同名的 XObject,圖片參照要帶頁碼。
func TestPDFImageRefCarriesPage(t *testing.T) {
	ref := pdfImgRef(7, "Im1.png")
	pg, name, ok := parsePDFImgRef(ref)
	if !ok || pg != 7 || name != "Im1.png" {
		t.Fatalf("%q → %d %q %v", ref, pg, name, ok)
	}
	if _, _, ok := parsePDFImgRef("epub:/x#1"); ok {
		t.Error("書的位址不該被當成 PDF 的圖")
	}
}

// 在 PDF 頁面上按 V 要看到整頁畫出來的圖,Esc 退回原來那一頁。
//
// 取文字看的是內容,看整頁看的是版面 —— 表格、圖表、公式抽成文字就沒了。
func TestPDFPageImage(t *testing.T) {
	a := New(vfs.OS{}, makePDFFile(t))
	a.CellW, a.CellH = 8, 16
	s := cell.New(70, 20)
	a.Draw(s)
	a.focusOn("doc.pdf")
	a.HandleKey(keys.Named(keys.Enter))
	a.Draw(s)

	a.HandleKey(keys.Key{Code: keys.Rune, R: 'v'})
	if a.Mode != ModeImage {
		t.Fatalf("按 V 沒有進看圖模式,現在是 %v(狀態:%s)", a.Mode, a.bv.status)
	}
	if a.Image == nil {
		t.Fatal("沒有畫出圖")
	}
	b := a.Image.Img.Bounds()
	// 612×792 點的頁面在 150 dpi 下大約 1275×1650。
	if b.Dx() < 1200 || b.Dy() < 1500 {
		t.Errorf("畫出來的尺寸是 %d×%d", b.Dx(), b.Dy())
	}
	a.HandleKey(keys.Named(keys.Esc))
	if a.Mode != ModeBrowse {
		t.Errorf("Esc 之後應該退回瀏覽模式,現在是 %v", a.Mode)
	}
}

// V 只在 PDF 上有意義。其他來源按了不該有反應,更不該當掉。
func TestPageImageOnlyForPDF(t *testing.T) {
	a, _ := bookApp(t)
	a.HandleKey(keys.Named(keys.Enter))
	before := a.Mode
	a.HandleKey(keys.Key{Code: keys.Rune, R: 'v'})
	if a.Mode != before {
		t.Errorf("在書上按 V 不該換模式(從 %v 變成 %v)", before, a.Mode)
	}
}
