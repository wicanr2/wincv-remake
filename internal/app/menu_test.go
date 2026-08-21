package app

import (
	"strings"
	"testing"

	"github.com/wicanr2/wincv-remake/internal/cell"
	"github.com/wicanr2/wincv-remake/internal/keys"
	"github.com/wicanr2/wincv-remake/internal/vfs"
)

// 選單列一直在,不必按任何鍵。
//
// 它畫在自己的畫面上而不是主格點裡 —— 選單可以用與內容不同的字型與
// 大小,兩者的格點對不起來,所以由外殼各自光柵化再疊。
func TestMenuBarAlwaysVisible(t *testing.T) {
	a := New(vfs.OS{}, t.TempDir())
	l := a.MenuLayer(80, 25)
	if l.Bar == nil {
		t.Fatal("選單列不見了")
	}
	row0 := strings.TrimRight(rowText(l.Bar, 0), " ")
	for _, want := range []string{"檔案", "檢視", "工具", "設定", "說明"} {
		if !strings.Contains(row0, want) {
			t.Fatalf("選單列上沒有「%s」:%q", want, row0)
		}
	}
	// 收起來的時候不該有下拉。
	if l.Drop != nil {
		t.Error("沒展開卻有下拉")
	}
}

// 選單列不佔主畫面的格 —— 內容從第 0 列開始。
func TestMenuBarDoesNotStealContentRows(t *testing.T) {
	a := New(vfs.OS{}, t.TempDir())
	s := cell.New(80, 25)
	a.Draw(s)
	withBar := strings.TrimRight(rowText(s, 0), " ")

	a.MenuBar = false
	s2 := cell.New(80, 25)
	a.Draw(s2)
	withoutBar := strings.TrimRight(rowText(s2, 0), " ")

	if withBar != withoutBar {
		t.Fatalf("選單列動到了主畫面\n開:%q\n關:%q", withBar, withoutBar)
	}
	// 關掉之後連選單列都不該給。
	if l := a.MenuLayer(80, 25); l.Bar != nil {
		t.Error("關掉了還畫選單列")
	}
}

// 展開時要有下拉,而且裡面是那個分類的項目。
func TestDropdownHasItems(t *testing.T) {
	a := New(vfs.OS{}, t.TempDir())
	a.HandleKey(keys.Named(keys.F9))
	l := a.MenuLayer(80, 25)
	if l.Drop == nil {
		t.Fatal("展開了卻沒有下拉")
	}
	var all strings.Builder
	for y := 0; y < l.Drop.Rows; y++ {
		all.WriteString(rowText(l.Drop, y))
		all.WriteByte('\n')
	}
	txt := all.String()
	for _, want := range []string{"檢視", "編輯", "拷貝", "刪除"} {
		if !strings.Contains(txt, want) {
			t.Fatalf("「檔案」的下拉裡沒有 %q\n%s", want, txt)
		}
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
	first := a.MenuLayer(80, 25)
	a.HandleKey(keys.Named(keys.Right)) // 換到「檢視」
	second := a.MenuLayer(80, 25)

	if first.Drop == nil || second.Drop == nil {
		t.Fatal("沒有下拉")
	}
	// 換了分類,下拉就該往右移。
	if second.DropX <= first.DropX {
		t.Fatalf("下拉沒有跟著分類移動:%d → %d", first.DropX, second.DropX)
	}
	// 而且要對得上選單列上那個標題的位置。
	bar := rowText(second.Bar, 0)
	if idx := strings.Index(bar, "檢視"); idx < 0 {
		t.Fatal("選單列上找不到「檢視」")
	}
}

// 下拉不能超出畫面右緣 —— 超出去的部分就是看不到的部分。
func TestDropdownStaysOnScreen(t *testing.T) {
	a := New(vfs.OS{}, t.TempDir())
	a.HandleKey(keys.Named(keys.F9))
	for i := 0; i < 4; i++ {
		a.HandleKey(keys.Named(keys.Right))
	}
	l := a.MenuLayer(40, 25) // 很窄的畫面
	if l.Drop == nil {
		t.Fatal("沒有下拉")
	}
	if l.DropX+l.Drop.Cols > 40 {
		t.Fatalf("下拉超出右緣:x=%d w=%d cols=40", l.DropX, l.Drop.Cols)
	}
	if l.DropX < 0 {
		t.Fatalf("下拉跑到畫面外:x=%d", l.DropX)
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
