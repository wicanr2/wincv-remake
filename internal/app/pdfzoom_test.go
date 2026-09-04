package app

import (
	"os"
	"path/filepath"
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
