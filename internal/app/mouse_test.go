package app

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/wicanr2/wincv-remake/internal/cell"
	"github.com/wicanr2/wincv-remake/internal/keys"
	"github.com/wicanr2/wincv-remake/internal/vfs"
)

// clickApp 建一個有幾個檔案的瀏覽器,並先畫一次 —— Click 要靠最近一次
// Draw 才知道清單有幾列。
func clickApp(t *testing.T) *App {
	t.Helper()
	dir := t.TempDir()
	for _, n := range []string{"a.txt", "b.txt", "c.txt", "d.txt"} {
		if err := os.WriteFile(filepath.Join(dir, n), []byte(n+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	a := New(vfs.OS{}, dir)
	a.Draw(cell.New(80, 25))
	return a
}

// 單擊清單移游標;第 0 列是路徑列,點它不算。
func TestClickMovesCursor(t *testing.T) {
	a := clickApp(t)
	if !a.Click(10, 3, false) {
		t.Fatal("點清單沒有反應")
	}
	if a.Browser.Cursor != 2 {
		t.Fatalf("游標在 %d,預期 2(第 3 列扣掉路徑列)", a.Browser.Cursor)
	}
	if a.Click(10, 0, false) {
		t.Error("點路徑列不該有反應")
	}
	// 清單只有 4 個檔(加 ..),點到空白列不動。
	if a.Click(10, 20, false) {
		t.Error("點空白列不該有反應")
	}
}

// 雙擊開啟:點一個文字檔要進到文字檢視。
func TestDoubleClickOpens(t *testing.T) {
	a := clickApp(t)
	a.Click(10, 2, false)
	e := a.Browser.Current()
	if e == nil || e.IsDir {
		t.Fatalf("第 2 列不是檔案:%+v", e)
	}
	a.Click(10, 2, true)
	if a.Mode != ModeViewer {
		t.Fatalf("雙擊之後模式是 %v,預期文字檢視", a.Mode)
	}
}

// 點選單列展開對應的分類,再點同一個收起來。
func TestMenuBarClick(t *testing.T) {
	a := clickApp(t)
	// 版面:第 1 格起,每個標題「 檔案 」佔 6 格。「檢視」在 7–12。
	a.MenuBarClick(8)
	if !a.Menuing() || a.menu.cat != 1 {
		t.Fatalf("點「檢視」後 active=%v cat=%d", a.Menuing(), a.menu.cat)
	}
	a.MenuBarClick(8)
	if a.Menuing() {
		t.Error("再點一次同一個分類應該收起來")
	}
	a.MenuBarClick(2)
	if !a.Menuing() || a.menu.cat != 0 {
		t.Fatalf("點「檔案」後 active=%v cat=%d", a.Menuing(), a.menu.cat)
	}
	// 點在分類以外的空白處收起來。
	a.MenuBarClick(60)
	if a.Menuing() {
		t.Error("點空白處應該收起來")
	}
}

// 下拉開著時點內容區:只收選單,不順便做那一下。
func TestClickOutsideClosesMenu(t *testing.T) {
	a := clickApp(t)
	a.HandleKey(keys.Named(keys.F9))
	before := a.Browser.Cursor
	a.Click(10, 3, false)
	if a.Menuing() {
		t.Fatal("點內容區應該收起選單")
	}
	if a.Browser.Cursor != before {
		t.Error("收選單那一下不該同時移游標")
	}
}

// 點下拉的項目等於走到那一項按 Enter;分隔線不算。
func TestMenuDropClick(t *testing.T) {
	a := clickApp(t)
	a.HandleKey(keys.Named(keys.F9))
	a.selectCat(4) // 說明
	a.MenuLayer(80, 25)
	items := a.menu.items
	// 找一個不是分隔線的項目。
	want := -1
	for i, it := range items {
		if !it.sep {
			want = i
			break
		}
	}
	if want < 0 {
		t.Skip("說明選單沒有項目")
	}
	if !a.MenuDropClick(want) {
		t.Fatal("點項目沒反應")
	}
	if a.Menuing() {
		t.Error("執行之後選單應該收起來")
	}
}

// 滾輪翻成上下鍵。
func TestWheel(t *testing.T) {
	a := clickApp(t)
	a.Wheel(3)
	if a.Browser.Cursor != 3 {
		t.Fatalf("往下滾 3 格後游標在 %d", a.Browser.Cursor)
	}
	a.Wheel(-2)
	if a.Browser.Cursor != 1 {
		t.Fatalf("往上滾 2 格後游標在 %d", a.Browser.Cursor)
	}
}

// 編輯器:點到哪個字游標就到哪。全形字兩格,點後半格也算那個字。
func TestClickEditor(t *testing.T) {
	a := clickApp(t)
	dir := a.Browser.Dir
	p := filepath.Join(dir, "e.txt")
	if err := os.WriteFile(p, []byte("ab中文cd\nxyz\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	a.Browser.Reload()
	for i, e := range a.Browser.Entries {
		if e.Name == "e.txt" {
			a.Browser.MoveTo(i, 20)
		}
	}
	a.HandleKey(keys.Ch('E'))
	if a.Mode != ModeEdit {
		t.Fatalf("E 之後模式是 %v", a.Mode)
	}
	a.Draw(cell.New(80, 25))
	a.Click(5, 0, false) // 「文」的後半格(a b 中=2,3 文=4,5)
	if a.Editor.Cur.Line != 0 || a.Editor.Cur.Col != 3 {
		t.Fatalf("游標在 %+v,預期 {0 3}", a.Editor.Cur)
	}
	a.Click(70, 1, false) // 超出行尾 → 行尾
	if a.Editor.Cur.Line != 1 || a.Editor.Cur.Col != 3 {
		t.Fatalf("游標在 %+v,預期 {1 3}", a.Editor.Cur)
	}
}
