package app

import (
	"github.com/wicanr2/wincv-remake/internal/cell"
	"github.com/wicanr2/wincv-remake/internal/keys"
)

// TouchRows 是觸控功能列佔的列數。
//
// 兩列:上面一列是動作,下面一列是導覽。分兩列而不是擠成一列,
// 因為手指的目標至少要三格寬才點得準,一列放不下八個動作。
const TouchRows = 2

// touchButton 是功能列上的一個鍵。
type touchButton struct {
	label string
	key   keys.Key
	// wide 的鍵佔兩倍寬。常用的動作給大一點的目標。
	wide bool
}

// touchBar 回傳目前模式該顯示哪些動作。
//
// 隨模式換內容是刻意的:原版靠「一個鍵一個動作」達到不用離開清單就做完事,
// 觸控上的等價物是「當下能做的事就在手邊」。做成固定的一組鍵會退化成
// 一個小鍵盤 —— 那是把桌面介面硬搬過來,不是移植。
//
// 修飾鍵(Alt-X / Ctrl-X)不放上來。那些組合在觸控上沒有等價物,
// 該做的是把動作本身放進功能列。
func (a *App) touchBar() []touchButton {
	k := func(r rune) keys.Key { return keys.Ch(r) }
	switch a.Mode {
	case ModeViewer, ModeMarkdown:
		return []touchButton{
			{label: "尋找", key: keys.Named(keys.F7)},
			{label: "編碼", key: k('T')},
			{label: "中英", key: keys.Named(keys.F8)},
			{label: "放大", key: keys.CtrlCh('+')},
			{label: "縮小", key: keys.CtrlCh('-')},
			{label: "返回", key: keys.Named(keys.Esc), wide: true},
		}
	case ModeImage:
		return []touchButton{
			{label: "上一張", key: keys.Named(keys.PgUp)},
			{label: "下一張", key: keys.Named(keys.PgDn)},
			{label: "原尺寸", key: k('1')},
			{label: "資訊", key: k('I')},
			{label: "返回", key: keys.Named(keys.Esc), wide: true},
		}
	case ModeHex:
		return []touchButton{
			{label: "尋找", key: keys.Named(keys.F7)},
			{label: "返回", key: keys.Named(keys.Esc), wide: true},
		}
	default:
		return []touchButton{
			{label: "拷貝", key: k('C')},
			{label: "移動", key: k('M')},
			{label: "更名", key: k('R')},
			{label: "磁碟", key: keys.AltCh('D')},
			{label: "預視", key: keys.AltCh('P')},
			{label: "選單", key: keys.Named(keys.F1), wide: true},
		}
	}
}

// drawTouchBar 畫底部的觸控功能列。
//
// 這一段是 Android 版的介面草案(docs/plan/android.md 第二期),
// 畫出來的是真的程式碼而不是示意圖 —— 桌面版加上 -touch 就看得到,
// 這樣草案與實作之間不會有落差。
func (a *App) drawTouchBar(s *cell.Screen) {
	if s.Rows < TouchRows+2 {
		return
	}
	t := a.touchBar()
	y := s.Rows - TouchRows

	// 上面一列:動作。格位由 touchSpans 算,與 TouchKeyAt 共用 ——
	// 兩邊各算的話「畫在哪」與「點到什麼」會慢慢對不上。
	s.Fill(0, y, s.Cols, 1, ' ', cell.Black, cell.ToolGray)
	spans := a.touchSpans(s.Cols)
	for i, b := range t {
		if i >= len(spans) {
			break
		}
		x, w := spans[i].x, spans[i].w
		fg := cell.Black
		if b.wide {
			fg = cell.Red // 主要動作用不同顏色,不是靠大小分辨
		}
		s.Fill(x, y, w, 1, ' ', fg, cell.ToolGray)
		s.Print(x+centerPad(b.label, w), y, b.label, fg, cell.ToolGray)
		if i < len(t)-1 && x+w < s.Cols {
			s.Set(x+w-1, y, cell.VLine, cell.Gray, cell.ToolGray)
		}
	}

	// 下面一列:導覽。**位置**固定,標籤隨模式換。
	//
	// 位置固定是為了讓拇指記得住;但在讀文件時擺一個「標記」按鈕
	// 是按了沒反應的按鈕,那比位置變動更糟。所以固定的是格位不是字。
	y++
	s.Fill(0, y, s.Cols, 1, ' ', cell.LtGray2, cell.Blue)
	nav := a.touchNav()
	x := 0
	for i, n := range nav {
		w := s.Cols / len(nav)
		if i == len(nav)-1 {
			w = s.Cols - x
		}
		s.Print(x+centerPad(n.label, w), y, n.label, cell.LtGray2, cell.Blue)
		x += w
	}
}

// touchNav 是導覽列的五個格位。順序固定:返回 / 上 / 下 / 兩個模式相關的。
func (a *App) touchNav() []struct {
	label string
	key   keys.Key
} {
	type btn = struct {
		label string
		key   keys.Key
	}
	switch a.Mode {
	case ModeBrowser:
		return []btn{
			{"◀ 上層", keys.Named(keys.Left)},
			{"▲", keys.Named(keys.PgUp)},
			{"▼", keys.Named(keys.PgDn)},
			{"標記", keys.Ch(' ')},
			{"開啟 ▶", keys.Named(keys.Enter)},
		}
	default:
		return []btn{
			{"◀ 返回", keys.Named(keys.Esc)},
			{"▲", keys.Named(keys.PgUp)},
			{"▼", keys.Named(keys.PgDn)},
			{"開頭", keys.Named(keys.Home)},
			{"結尾 ▶", keys.Named(keys.End)},
		}
	}
}

// ListCursorRow 回傳游標在清單區的第幾列(0 起算,路徑列不算)。
// 觸控要把「點到第幾列」翻成幾次上下鍵,需要知道現在在哪一列。
// 回 -1 表示現在的模式沒有清單。
func (a *App) ListCursorRow() int {
	if a.Mode != ModeBrowser {
		return -1
	}
	return a.Browser.Cursor - a.Browser.Top
}

// TouchKeyAt 回傳觸控功能列上某一格對應的按鍵。
//
// row 是功能列裡的第幾列(0 = 動作列,1 = 導覽列),cols 是畫面寬度。
// 版面計算與 drawTouchBar 共用 touchSpans,不各算一次 ——
// 兩邊各算的話「畫在哪」與「點到什麼」會慢慢對不上,
// 而那種錯只有在真的用手指點下去才會發現。
func (a *App) TouchKeyAt(col, row, cols int) (keys.Key, bool) {
	switch row {
	case 0:
		bar := a.touchBar()
		for i, w := range a.touchSpans(cols) {
			if i < len(bar) && col >= w.x && col < w.x+w.w {
				return bar[i].key, true
			}
		}
	case 1:
		nav := a.touchNav()
		for i, n := range nav {
			w := cols / len(nav)
			x := i * w
			if i == len(nav)-1 {
				w = cols - x
			}
			if col >= x && col < x+w {
				return n.key, true
			}
		}
	}
	return keys.Key{}, false
}

// touchSpans 算出動作列上每個鍵的格位。
type touchSpan struct{ x, w int }

func (a *App) touchSpans(cols int) []touchSpan {
	t := a.touchBar()
	units := 0
	for _, b := range t {
		units++
		if b.wide {
			units++
		}
	}
	if units == 0 {
		return nil
	}
	out := make([]touchSpan, 0, len(t))
	x := 0
	for i, b := range t {
		w := cols / units
		if b.wide {
			w *= 2
		}
		if i == len(t)-1 {
			w = cols - x
		}
		if w < 1 {
			w = 1
		}
		out = append(out, touchSpan{x: x, w: w})
		x += w
	}
	return out
}

// centerPad 回傳把 label 置中要留幾格。
func centerPad(label string, w int) int {
	d := (w - cellWidth(label)) / 2
	if d < 0 {
		d = 0
	}
	return d
}
