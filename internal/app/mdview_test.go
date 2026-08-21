package app

import (
	"bytes"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/wicanr2/wincv-remake/internal/cell"
	"github.com/wicanr2/wincv-remake/internal/keys"
	"github.com/wicanr2/wincv-remake/internal/vfs"
)

func mdFixture(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	var buf bytes.Buffer
	img := image.NewRGBA(image.Rect(0, 0, 64, 32))
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	must(os.WriteFile(filepath.Join(root, "pic.png"), buf.Bytes(), 0o644))
	must(os.MkdirAll(filepath.Join(root, "sub"), 0o755))
	must(os.WriteFile(filepath.Join(root, "secret.txt"), []byte("x"), 0o644))
	// 內文要夠長才折得出差別 —— 短文字在寬窄兩種版面下列數相同,
	// 那樣的測試看起來會通過,但它什麼都沒驗到。
	long := strings.Repeat("這是一段夠長的內文，用來確認換欄寬時會重新折行。", 4)
	must(os.WriteFile(filepath.Join(root, "a.md"), []byte(
		"# 標題\n\n"+long+"\n\n![圖](pic.png)\n\n結尾\n"), 0o644))
	must(os.WriteFile(filepath.Join(root, "esc.md"), []byte(
		"![壞](../../../etc/passwd)\n\n![絕對](/etc/passwd)\n"), 0o644))
	return root
}

func openMD(t *testing.T, root, name string) (*App, *cell.Screen) {
	t.Helper()
	a := New(vfs.OS{}, root)
	s := cell.New(60, 20)
	a.Draw(s)
	for i := 0; i < 40; i++ {
		if e := a.Browser.Current(); e != nil && e.Name == name {
			break
		}
		a.HandleKey(keys.Named(keys.Down))
	}
	if e := a.Browser.Current(); e == nil || e.Name != name {
		t.Fatalf("找不到 %s", name)
	}
	a.HandleKey(keys.Named(keys.Enter))
	return a, s
}

// .md 要走排版模式,不是純文字檢視。
func TestMarkdownModeOpens(t *testing.T) {
	a, s := openMD(t, mdFixture(t), "a.md")
	if a.Mode != ModeMarkdown {
		t.Fatalf("mode = %v, 想要 ModeMarkdown", a.Mode)
	}
	ov := a.Draw(s)
	if len(ov) != 1 {
		t.Fatalf("內嵌圖片沒有變成 overlay,拿到 %d 個", len(ov))
	}
	if ov[0].Rect.Dx() <= 0 || ov[0].Rect.Dy() <= 0 {
		t.Errorf("overlay 矩形是空的: %v", ov[0].Rect)
	}
	var b strings.Builder
	for y := 0; y < s.Rows; y++ {
		b.WriteString(rowText(s, y))
	}
	if !strings.Contains(b.String(), "標題") {
		t.Errorf("畫面上沒有標題:\n%s", b.String())
	}
}

// 一份來路不明的 .md 不該有辦法讓程式去讀文件目錄之外的檔案。
func TestMarkdownImageStaysInDir(t *testing.T) {
	a, _ := openMD(t, mdFixture(t), "esc.md")
	if a.Mode != ModeMarkdown {
		t.Fatalf("mode = %v", a.Mode)
	}
	for _, src := range []string{"../../../etc/passwd", "/etc/passwd"} {
		if _, err := a.mdImage(src); err == nil {
			t.Errorf("%q 竟然讀得到", src)
		}
	}
}

// 捲動:PgDn / End 之後不能跑出範圍,Esc 要退回檔案清單。
func TestMarkdownScrollAndExit(t *testing.T) {
	a, s := openMD(t, mdFixture(t), "a.md")
	a.Draw(s)
	a.HandleKey(keys.Named(keys.End))
	a.Draw(s)
	if a.md.top < 0 || a.md.top > len(a.md.lines) {
		t.Errorf("End 之後 top = %d,總列數 %d", a.md.top, len(a.md.lines))
	}
	a.HandleKey(keys.Named(keys.Home))
	if a.md.top != 0 {
		t.Errorf("Home 之後 top = %d", a.md.top)
	}
	a.HandleKey(keys.Named(keys.Up))
	if a.md.top != 0 {
		t.Errorf("在最上面按上鍵跑掉了,top = %d", a.md.top)
	}
	a.HandleKey(keys.Named(keys.Esc))
	if a.Mode != ModeBrowser {
		t.Errorf("Esc 之後 mode = %v", a.Mode)
	}
}

// 從 markdown 進看圖模式,Esc 要退回 markdown 而不是檔案清單。
func TestMarkdownEnterPictureThenBack(t *testing.T) {
	a, s := openMD(t, mdFixture(t), "a.md")
	a.Draw(s)
	a.HandleKey(keys.Named(keys.Enter))
	if a.Mode != ModeImage {
		t.Fatalf("Enter 之後 mode = %v, 想要 ModeImage", a.Mode)
	}
	a.HandleKey(keys.Named(keys.Esc))
	if a.Mode != ModeMarkdown {
		t.Errorf("Esc 之後 mode = %v, 想要退回 ModeMarkdown", a.Mode)
	}
	a.HandleKey(keys.Named(keys.Esc))
	if a.Mode != ModeBrowser {
		t.Errorf("再按一次 Esc 應該回檔案清單,mode = %v", a.Mode)
	}
}

// 視窗變窄要重排。不重排的話文字會被畫到畫面外。
func TestMarkdownRelayoutOnResize(t *testing.T) {
	a, _ := openMD(t, mdFixture(t), "a.md")
	wide := cell.New(80, 20)
	a.Draw(wide)
	nWide := len(a.md.lines)
	narrow := cell.New(24, 20)
	a.Draw(narrow)
	if len(a.md.lines) == nWide {
		t.Errorf("變窄之後列數沒變(%d),沒有重排", nWide)
	}
	for y := 0; y < narrow.Rows; y++ {
		if w := len([]rune(strings.TrimRight(rowText(narrow, y), " "))); w > 24 {
			t.Errorf("第 %d 列寬 %d,超出畫面", y, w)
		}
	}
}
