package app

import (
	"github.com/wicanr2/wincv-remake/internal/cell"
	"github.com/wicanr2/wincv-remake/internal/i18n"
	"github.com/wicanr2/wincv-remake/internal/keys"
)

// aboutLines 是「關於」畫面的內容。
//
// 原作者列在前面、重製列在後面 —— 署名的是重製這件事,不是原作。
//
// 做成函式而不是套件層級的 var:var 在 init 時求值,那時語言還沒設定,
// 翻譯會被凍在啟動當下的語言,之後換語言這一頁不會變。
func aboutLines() []struct {
	text string
	fg   cell.Color
} {
	return []struct {
		text string
		fg   cell.Color
	}{
		{"WinCV Remake", cell.LtCyan},
		{"", cell.LtGray},
		{i18n.T("原作   WinCV 0.52(CView for Windows)"), cell.LtGray},
		{i18n.T("       Lcc Wizard(林健總)   cview.com.tw"), cell.LtGray},
		{i18n.T("       最後更新 2011-11-24"), cell.Gray},
		{"", cell.LtGray},
		{i18n.T("重製   王俊又"), cell.LtYellow},
		{i18n.T("       為保存台灣中文軟體盡一份心力"), cell.LtYellow},
		{"", cell.LtGray},
		{"Go + Ebiten", cell.Gray},
		{"Linux / Windows / macOS / Android", cell.Gray},
		{i18n.T("半形 cvga.fon + 全形倚天點陣字"), cell.Gray},
		{i18n.T("Android 沒有這兩份,改用系統字型"), cell.Gray},
		{"", cell.LtGray},
		{i18n.T("按任意鍵關閉"), cell.DkGray},
	}
}

// Abouting 回傳現在是不是開著「關於」。
func (a *App) Abouting() bool { return a.about }

func (a *App) openAbout() bool {
	a.about = true
	return true
}

func (a *App) aboutKey(keys.Key) bool {
	a.about = false
	return true
}

func (a *App) drawAbout(s *cell.Screen) {
	lines := aboutLines()
	w := 0
	for _, l := range lines {
		if n := cellWidth(l.text) + 6; n > w {
			w = n
		}
	}
	if w > s.Cols {
		w = s.Cols
	}
	h := len(lines) + 2
	if h > s.Rows {
		h = s.Rows
	}
	x0, y0 := (s.Cols-w)/2, (s.Rows-h)/2
	if x0 < 0 {
		x0 = 0
	}
	if y0 < 0 {
		y0 = 0
	}
	s.Fill(x0, y0, w, h, ' ', cell.LtGray, cell.Blue)
	for i, l := range lines {
		y := y0 + 1 + i
		if y >= y0+h {
			break
		}
		if l.text == "" {
			continue
		}
		x := x0 + (w-cellWidth(l.text))/2
		if i > 1 {
			x = x0 + 3 // 前兩列置中,其餘靠左對齊比較好讀
		}
		if x < x0 {
			x = x0
		}
		s.Print(x, y, l.text, l.fg, cell.Blue)
	}
}
