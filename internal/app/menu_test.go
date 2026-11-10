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
	for _, want := range []string{"1檔案", "2目錄/磁碟", "3網路", "4標記", "5檢視", "6轉檔/轉碼/其他", "7設定", "8說明"} {
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

func TestCJK24MenuChangesSelection(t *testing.T) {
	a := New(vfs.OS{}, t.TempDir())
	items := a.cjk24MenuItems()
	if len(items) != 7 {
		t.Fatalf("字形項目數是 %d，預期 7", len(items))
	}
	if !a.setCJK24("guoqiao") || a.CJK24 != "guoqiao" {
		t.Fatalf("國喬選擇失敗: %q", a.CJK24)
	}
	if a.setCJK24("不存在") || a.CJK24 != "guoqiao" {
		t.Fatalf("未知選擇改壞設定: %q", a.CJK24)
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

// 原版的選單是 `&1檔案` 到 `&8說明/…`,Alt-數字直接打開那一類。
// 下拉開著時按別的數字,換到那一類。
func TestMenuAltDigitOpensCategory(t *testing.T) {
	a := New(vfs.OS{}, t.TempDir())
	if !a.HandleKey(keys.AltCh('7')) || !a.menu.active || a.menu.cat != 6 {
		t.Fatalf("Alt-7 應該打開第七類(設定),現在 active=%v cat=%d", a.menu.active, a.menu.cat)
	}
	if !a.HandleKey(keys.AltCh('2')) || !a.menu.active || a.menu.cat != 1 {
		t.Fatalf("下拉開著時 Alt-2 應該換到第二類,現在 cat=%d", a.menu.cat)
	}
	// 超過分類數的數字不歸選單管。
	b := New(vfs.OS{}, t.TempDir())
	b.HandleKey(keys.AltCh('9'))
	if b.menu.active {
		t.Fatal("只有八類,Alt-9 不該打開選單")
	}
}

// 單按 Alt:收著時打開第一類,開著時(即使在子選單裡)一次全部收起。
// Esc 只退一層,這是 Win32 選單的慣例。
func TestMenuKeyAloneAltTogglesAll(t *testing.T) {
	a := New(vfs.OS{}, t.TempDir())
	a.HandleKey(keys.Named(keys.MenuKey))
	if !a.menu.active || a.menu.cat != 0 {
		t.Fatalf("單按 Alt 應該打開第一類,現在 active=%v cat=%d", a.menu.active, a.menu.cat)
	}
	a.HandleKey(keys.AltCh('7'))
	enterSub(t, a)
	a.HandleKey(keys.Named(keys.Esc))
	if !a.menu.active || a.menu.back != nil {
		t.Fatal("子選單裡 Esc 應該只退回上一層")
	}
	enterSub(t, a)
	if !a.HandleKey(keys.Named(keys.MenuKey)) || a.menu.active {
		t.Fatal("子選單裡單按 Alt 應該收起所有選單")
	}
}

// enterSub 在目前的下拉裡找第一個子選單並進去。
func enterSub(t *testing.T, a *App) {
	t.Helper()
	for i, it := range a.menu.items {
		if it.sub != nil {
			a.menu.cursor = i
			a.HandleKey(keys.Named(keys.Enter))
			if a.menu.back == nil {
				t.Fatal("沒有進到子選單")
			}
			return
		}
	}
	t.Fatal("這一類沒有子選單")
}

// 選了一個「什麼都沒改」的項目(字級已到頂),下拉照樣收起來,
// 而且要回報畫面有變 —— 否則外殼不重繪,畫面停在已經收起的下拉上,
// 點下拉的位置也不再有反應(GitHub issue #10)。
func TestMenuNoOpItemStillRedraws(t *testing.T) {
	a := New(vfs.OS{}, t.TempDir())
	a.MaxZoom, a.Zoom = 2, 2
	a.HandleKey(keys.AltCh('7'))
	found := false
	for i, it := range a.menu.items {
		if it.key == keys.CtrlCh('+') {
			a.menu.cursor, found = i, true
		}
	}
	if !found {
		t.Fatal("設定裡找不到「放大字體」")
	}
	if !a.HandleKey(keys.Named(keys.Enter)) {
		t.Fatal("字級已到頂時選「放大字體」,下拉收起來了卻回報沒變化")
	}
	if a.menu.active {
		t.Fatal("下拉應該收起來")
	}
	if a.Zoom != 2 {
		t.Fatalf("字級不該變,現在是 %d", a.Zoom)
	}
	// 滑鼠點下拉走同一條路。
	a.HandleKey(keys.AltCh('7'))
	for i, it := range a.menu.items {
		if it.key == keys.CtrlCh('+') {
			a.menu.cursor = i
		}
	}
	a.MenuLayer(80, 25) // 下拉要畫過一次才知道裝得下幾列
	row := a.menu.cursor - a.menu.top
	if !a.MenuDropClick(row) {
		t.Fatal("滑鼠點「放大字體」(已到頂)應該回報畫面有變")
	}
}

// 有子選單的項目(視窗大小、語言…)右邊不該出現按鍵欄。它們的 key 是零值,
// 印出來是 U+0000,畫面上是一個缺字框。
func TestSubmenuItemsHaveNoKeyColumn(t *testing.T) {
	a := New(vfs.OS{}, t.TempDir())
	a.Draw(cell.New(80, 25))
	a.HandleKey(keys.Named(keys.F9))
	for i := 0; i < 6; i++ {
		a.HandleKey(keys.Named(keys.Right))
	}
	l := a.MenuLayer(80, 25)
	if l.Drop == nil {
		t.Fatal("設定選單沒展開")
	}
	if !strings.Contains(screenText(l.Drop), "語言") {
		t.Fatalf("展開的不是設定選單:\n%s", screenText(l.Drop))
	}
	for y := 0; y < l.Drop.Rows; y++ {
		for x := 0; x < l.Drop.Cols; x++ {
			if c := l.Drop.At(x, y); c != nil && !c.Cont && c.Ch == 0 {
				t.Fatalf("第 %d 列第 %d 欄畫了 U+0000", y, x)
			}
		}
	}
}
