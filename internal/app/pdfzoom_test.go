package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/wicanr2/wincv-remake/internal/cell"
	"github.com/wicanr2/wincv-remake/internal/keys"
	"github.com/wicanr2/wincv-remake/internal/vfs"
)

// PDF 按 V 看整頁之後要能放大,而且是**用更高的解析度重畫**,
// 不是把 150 dpi 的點陣圖拉大 —— 放大之後看不看得到更多東西,差別在這裡。
func TestPDFPageImageZoom(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join("..", "pdf", "testdata", "rich.pdf")
	data, err := os.ReadFile(src)
	if err != nil {
		t.Skipf("沒有測試用的 PDF:%v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "a.pdf"), data, 0o644); err != nil {
		t.Fatal(err)
	}

	a := New(vfs.OS{}, dir)
	s := cell.New(80, 25)
	a.Draw(s)
	for i, e := range a.Browser.Entries {
		if e.Name == "a.pdf" {
			a.Browser.MoveTo(i, 20)
		}
	}
	a.HandleKey(keys.Named(keys.Enter))
	if a.Mode != ModeBrowse {
		t.Fatalf("開 PDF 之後的模式是 %v", a.Mode)
	}
	a.HandleKey(keys.Ch('V'))
	if a.Mode != ModeImage {
		t.Fatalf("按 V 之後的模式是 %v(狀態列:%s)", a.Mode, a.bv.status)
	}
	before := a.Image.Img.Bounds().Dx()
	if before == 0 {
		t.Fatal("整頁圖是空的")
	}

	a.HandleKey(keys.Ch('+'))
	if a.Image.Zoom != 1.5 {
		t.Fatalf("按 + 之後倍率是 %g,預期 1.5", a.Image.Zoom)
	}
	after := a.Image.Img.Bounds().Dx()
	if after <= before {
		t.Fatalf("圖沒有被重畫成更大的:%d → %d", before, after)
	}
	// 重畫過了就不該再點陣放大一次。
	if got := a.Image.Scale(); got != 1 {
		t.Errorf("重畫之後 Scale=%g,預期 1", got)
	}

	// 按 1 回到原尺寸,圖要跟著縮回去。
	a.HandleKey(keys.Ch('1'))
	if a.Image.Zoom != 1 {
		t.Fatalf("按 1 之後倍率是 %g", a.Image.Zoom)
	}
	if got := a.Image.Img.Bounds().Dx(); got != before {
		t.Errorf("回到原尺寸後寬度 %d,預期 %d", got, before)
	}
}

// pdfApp 開一份多頁 PDF 並進到整頁模式。
func pdfApp(t *testing.T) *App {
	t.Helper()
	dir := t.TempDir()
	data, err := os.ReadFile(filepath.Join("..", "pdf", "testdata", "twocol.pdf"))
	if err != nil {
		t.Skipf("沒有測試用的 PDF:%v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "a.pdf"), data, 0o644); err != nil {
		t.Fatal(err)
	}
	a := New(vfs.OS{}, dir)
	a.Draw(cell.New(80, 25))
	for i, e := range a.Browser.Entries {
		if e.Name == "a.pdf" {
			a.Browser.MoveTo(i, 20)
		}
	}
	a.HandleKey(keys.Named(keys.Enter))
	a.HandleKey(keys.Ch('V'))
	if a.Mode != ModeImage {
		t.Fatalf("按 V 之後的模式是 %v(%s)", a.Mode, a.bv.status)
	}
	return a
}

// 整頁模式下 PgDn / PgUp 換頁,而不是跳到目錄裡別的圖檔。
func TestPDFPageImagePaging(t *testing.T) {
	a := pdfApp(t)
	if a.pdf.Pages < 2 {
		t.Skip("測試用的 PDF 只有一頁")
	}
	_, page, _ := parsePDFURL(a.bv.url)
	if page != 1 {
		t.Fatalf("起點是第 %d 頁", page)
	}
	a.HandleKey(keys.Named(keys.PgDn))
	if _, p, _ := parsePDFURL(a.bv.url); p != 2 {
		t.Fatalf("PgDn 之後在第 %d 頁,預期 2", p)
	}
	if a.Mode != ModeImage {
		t.Fatalf("換頁之後跳出了整頁模式:%v", a.Mode)
	}
	a.HandleKey(keys.Named(keys.PgUp))
	if _, p, _ := parsePDFURL(a.bv.url); p != 1 {
		t.Fatalf("PgUp 之後在第 %d 頁,預期 1", p)
	}
	// 第一頁再往前不動,而且要講一句 —— 沒反應的按鍵讓人以為程式當了。
	a.Message = ""
	a.HandleKey(keys.Named(keys.PgUp))
	if _, p, _ := parsePDFURL(a.bv.url); p != 1 {
		t.Errorf("到頂還往前走到第 %d 頁", p)
	}
	if a.Message == "" {
		t.Error("到頂沒有訊息")
	}
}

// 換頁要保住放大倍率:讀表格的人翻頁之後還在讀表格。
func TestPDFPagingKeepsZoom(t *testing.T) {
	a := pdfApp(t)
	if a.pdf.Pages < 2 {
		t.Skip("測試用的 PDF 只有一頁")
	}
	a.HandleKey(keys.Ch('+'))
	a.HandleKey(keys.Ch('+'))
	want := a.Image.Zoom
	if want == 1 {
		t.Fatal("放大沒生效")
	}
	a.HandleKey(keys.Named(keys.PgDn))
	if a.Image.Zoom != want {
		t.Errorf("換頁之後倍率變成 %g,預期 %g", a.Image.Zoom, want)
	}
	if a.Image.Fit {
		t.Error("換頁之後回到了「縮放到視窗」")
	}
}

// Esc 退回瀏覽模式時要看到**同一頁**的文字,不是進來之前那一頁。
func TestPDFPagingUpdatesTextView(t *testing.T) {
	a := pdfApp(t)
	if a.pdf.Pages < 2 {
		t.Skip("測試用的 PDF 只有一頁")
	}
	a.HandleKey(keys.Named(keys.PgDn))
	a.HandleKey(keys.Named(keys.Esc))
	if a.Mode != ModeBrowse {
		t.Fatalf("Esc 之後的模式是 %v", a.Mode)
	}
	if _, p, _ := parsePDFURL(a.bv.url); p != 2 {
		t.Fatalf("退回瀏覽模式後在第 %d 頁,預期 2", p)
	}
	if !strings.Contains(a.bv.title, "2") {
		t.Errorf("標題沒有跟著換頁:%q", a.bv.title)
	}
}
