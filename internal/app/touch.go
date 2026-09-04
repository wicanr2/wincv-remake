package app

import (
	"github.com/wicanr2/wincv-remake/internal/cell"
	"github.com/wicanr2/wincv-remake/internal/i18n"
	"github.com/wicanr2/wincv-remake/internal/keys"
)

// TouchRows 是觸控功能列佔的列數。
//
// 三列:最上面一列是隨模式換的動作,底下兩列是固定的按鍵 HUD
// (方向鍵、PgUp/PgDn、Home/End、Esc、Enter)。分列而不是擠成一列,
// 因為手指的目標至少要三格寬才點得準。
const TouchRows = 3

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
			{label: i18n.T("尋找"), key: keys.Named(keys.F7)},
			{label: i18n.T("編碼"), key: k('T')},
			{label: i18n.T("中英"), key: keys.Named(keys.F8)},
			{label: i18n.T("放大"), key: keys.CtrlCh('+')},
			{label: i18n.T("縮小"), key: keys.CtrlCh('-')},
			{label: i18n.T("返回"), key: keys.Named(keys.Esc), wide: true},
		}
	case ModeImage:
		// 標籤一律兩個字:功能列的格寬是 cols/units,30 欄時只有四格,
		// 三個字的標籤畫不完 —— 而畫不完的症狀是「按鈕看起來少一個字」,
		// 不是任何錯誤。放大縮小放在這裡是因為 PDF 的整頁圖也走看圖模式,
		// 在手機上那是主要的操作。
		//
		// 五顆是上限:30 欄(手機直立)時格寬是 cols/units,再扣掉一欄
		// 分隔線,兩個中文字剛好填滿。第六顆會讓每個標籤只剩一個字。
		// 「資訊」因此讓位給放大縮小 —— `I` 鍵仍然可用。
		return []touchButton{
			{label: i18n.T("上張"), key: keys.Named(keys.PgUp)},
			{label: i18n.T("下張"), key: keys.Named(keys.PgDn)},
			{label: i18n.T("縮小"), key: k('-')},
			{label: i18n.T("放大"), key: k('+')},
			{label: i18n.T("原寸"), key: k('1')},
			{label: i18n.T("返回"), key: keys.Named(keys.Esc), wide: true},
		}
	case ModeHex:
		return []touchButton{
			{label: i18n.T("尋找"), key: keys.Named(keys.F7)},
			{label: i18n.T("返回"), key: keys.Named(keys.Esc), wide: true},
		}
	default:
		// 「標記」放這裡而不是 HUD:HUD 只放真的鍵,而 Space 在讀文件時
		// 是翻頁,標記只在清單才有意義。預視窗格不放 —— 手機螢幕沒有
		// 分兩欄的餘裕,而 30 欄寬時多一顆鍵標籤就擠不下。
		return []touchButton{
			{label: i18n.T("標記"), key: k(' ')},
			{label: i18n.T("拷貝"), key: k('C')},
			{label: i18n.T("移動"), key: k('M')},
			{label: i18n.T("更名"), key: k('R')},
			{label: i18n.T("磁碟"), key: keys.AltCh('D')},
			{label: i18n.T("選單"), key: keys.Named(keys.F9), wide: true},
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

	// 底下兩列:按鍵 HUD。每一格都是一顆真的鍵,**每個模式都一樣**。
	//
	// 中間三欄是方向鍵排成十字(▲ 在上、◀ ▼ ▶ 在下),兩側是 Esc / Enter
	// 與 PgUp/PgDn、Home/End。位置固定是為了讓拇指記得住;標籤就是
	// 鍵名,所以不會有「這個模式下按了沒反應」的按鈕 —— 模式相關的
	// 動作在上面那一列。
	for _, nav := range a.touchNav() {
		y++
		s.Fill(0, y, s.Cols, 1, ' ', cell.LtGray2, cell.Blue)
		x := 0
		for i, n := range nav {
			w := s.Cols / len(nav)
			if i == len(nav)-1 {
				w = s.Cols - x
			}
			fg := cell.LtGray2
			if n.key.Code == keys.Esc || n.key.Code == keys.Enter {
				fg = cell.Yellow // 兩顆「決定」鍵顏色不同,閉著眼也分得出兩端
			}
			s.Print(x+centerPad(n.label, w), y, n.label, fg, cell.Blue)
			if i < len(nav)-1 {
				s.Set(x+w-1, y, cell.VLine, cell.LtBlue, cell.Blue)
			}
			x += w
		}
	}
}

// navKey 是 HUD 上的一顆鍵。
type navKey struct {
	label string
	key   keys.Key
}

// touchNav 是 HUD 的兩列、各五格。
//
//	Esc  | PgUp | ▲ | PgDn | Enter
//	Home | ◀    | ▼ | ▶    | End
func (a *App) touchNav() [][]navKey {
	return [][]navKey{
		{
			{"Esc", keys.Named(keys.Esc)},
			{"PgUp", keys.Named(keys.PgUp)},
			{"▲", keys.Named(keys.Up)},
			{"PgDn", keys.Named(keys.PgDn)},
			{"Enter", keys.Named(keys.Enter)},
		},
		{
			{"Home", keys.Named(keys.Home)},
			{"◀", keys.Named(keys.Left)},
			{"▼", keys.Named(keys.Down)},
			{"▶", keys.Named(keys.Right)},
			{"End", keys.Named(keys.End)},
		},
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
// row 是功能列裡的第幾列(0 = 動作列,1–2 = HUD),cols 是畫面寬度。
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
	case 1, 2:
		nav := a.touchNav()[row-1]
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
