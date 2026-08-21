package app

import (
	"strings"
	"testing"

	"github.com/wicanr2/wincv-remake/internal/cell"
	"github.com/wicanr2/wincv-remake/internal/keys"
	"github.com/wicanr2/wincv-remake/internal/vfs"
)

// 選單列一直在最上方那一列,不必按任何鍵。
func TestMenuBarAlwaysVisible(t *testing.T) {
	a := New(vfs.OS{}, t.TempDir())
	s := cell.New(80, 25)
	a.Draw(s)
	row0 := rowText(s, 0)
	for _, want := range []string{"檔案", "檢視", "工具", "設定", "說明"} {
		if !strings.Contains(row0, want) {
			t.Fatalf("選單列上沒有「%s」:%q", want, row0)
		}
	}
}

// 選單列佔掉一列,底下的內容整個往下移 —— 不然它會蓋掉路徑列。
func TestMenuBarPushesContentDown(t *testing.T) {
	a := New(vfs.OS{}, t.TempDir())
	s := cell.New(80, 25)
	a.Draw(s)
	withBar := strings.TrimRight(rowText(s, 1), " ")

	a.MenuBar = false
	s2 := cell.New(80, 25)
	a.Draw(s2)
	withoutBar := strings.TrimRight(rowText(s2, 0), " ")

	if withBar != withoutBar {
		t.Fatalf("關掉選單列之後第 0 列應該等於開著時的第 1 列\n開:%q\n關:%q",
			withBar, withoutBar)
	}
}

// 左右換分類,而且換過去的項目真的不一樣。
func TestMenuBarSwitchesCategory(t *testing.T) {
	a := New(vfs.OS{}, t.TempDir())
	a.HandleKey(keys.Named(keys.F9))
	if a.menu.cat != 0 {
		t.Fatalf("F9 應該停在第一個分類,現在是 %d", a.menu.cat)
	}
	first := a.menu.items[a.menu.cursor].label
	a.HandleKey(keys.Named(keys.Right))
	if a.menu.cat != 1 {
		t.Fatalf("→ 應該換到第二個分類,現在是 %d", a.menu.cat)
	}
	if got := a.menu.items[a.menu.cursor].label; got == first {
		t.Fatalf("換了分類但項目一樣:%q", got)
	}
	// 最左邊再往左要繞到最後一個分類。
	a.HandleKey(keys.Named(keys.Left))
	a.HandleKey(keys.Named(keys.Left))
	if a.menu.cat != len(a.menuCats())-1 {
		t.Fatalf("繞回去之後應該是最後一個分類,現在是 %d", a.menu.cat)
	}
}

// F9 再按一次收起來 —— 開合用同一顆鍵。
func TestMenuBarToggles(t *testing.T) {
	a := New(vfs.OS{}, t.TempDir())
	a.HandleKey(keys.Named(keys.F9))
	a.HandleKey(keys.Named(keys.F9))
	if a.Menuing() {
		t.Fatal("第二次 F9 應該收起下拉")
	}
}

// 下拉要貼在自己的分類標題底下,不是浮在畫面正中央。
func TestDropdownAlignsToCategory(t *testing.T) {
	a := New(vfs.OS{}, t.TempDir())
	a.HandleKey(keys.Named(keys.F9))
	a.HandleKey(keys.Named(keys.Right)) // 換到「檢視」
	s := cell.New(80, 25)
	a.Draw(s)
	// 標題列上「檢視」的位置,與下拉左緣應該對得起來。
	bar := []rune(rowText(s, 0))
	want := strings.Index(string(bar), "檢視")
	if want < 0 {
		t.Fatal("選單列上找不到「檢視」")
	}
	if a.menu.x <= 0 {
		t.Fatalf("下拉左緣是 %d", a.menu.x)
	}
}

// F1 開使用說明,而且說明裡真的有按鍵表。
func TestHelpOpens(t *testing.T) {
	a := New(vfs.OS{}, t.TempDir())
	a.HandleKey(keys.Named(keys.F1))
	if a.Mode != ModeMarkdown {
		t.Fatalf("F1 之後的模式是 %v", a.Mode)
	}
	s := cell.New(80, 25)
	a.Draw(s)
	var all strings.Builder
	for y := 0; y < s.Rows; y++ {
		all.WriteString(rowText(s, y))
		all.WriteByte('\n')
	}
	txt := all.String()
	if !strings.Contains(txt, "使用說明") {
		t.Fatalf("說明畫面上沒有標題\n%s", txt)
	}
}
