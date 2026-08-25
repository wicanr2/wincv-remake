package app

import (
	"github.com/wicanr2/wincv-remake/internal/editor"
	"github.com/wicanr2/wincv-remake/internal/keys"
)

// 滑鼠與觸控的共同入口。
//
// 這一層只認**格子座標**,不認像素:像素怎麼換成格子取決於字型、倍率、
// 選單層用不用另一套字級,那些都是外殼的事。外殼把「點到內容區第幾欄
// 第幾列」「點到選單列第幾格」翻好再進來,這裡才知道格子底下是什麼。
//
// 原版是 Win32 程式,滑鼠本來就能用:點清單移游標、雙擊開檔、點選單列
// 展開。這裡做的是同一件事,不是新功能。

// Click 是內容區被點了一下。col / row 以內容畫面(Draw 拿到的那一份)
// 為準,double 表示這是連點的第二下。回傳畫面有沒有變。
//
// 單擊與雙擊的分工照 Windows 的慣例:單擊選,雙擊做 —— 檔案清單單擊
// 移游標、雙擊開啟。網頁的連結是例外:單擊就跟過去,那是瀏覽器教會
// 所有人的手感,要求雙擊反而怪。
func (a *App) Click(col, row int, double bool) bool {
	defer a.rememberPos()
	// 下拉開著時點到內容區:收起來,這一下就到此為止。
	// 點在選單外面的意思是「不要選單了」,不是「順便做那件事」。
	if a.menu.active {
		a.menu = menu{}
		return true
	}
	if a.about {
		a.about = false
		return true
	}
	if a.prompt.active {
		return false
	}
	// 觸控功能列在最底下兩列,各模式都一樣(見 Draw)。
	if a.Touch {
		if r := row - (a.screenRows - TouchRows); r >= 0 {
			if k, ok := a.TouchKeyAt(col, r, a.screenCols); ok {
				return a.HandleKey(k)
			}
			return false
		}
	}
	switch a.Mode {
	case ModeBrowser:
		return a.clickBrowser(col, row, double)
	case ModeBrowse:
		return a.clickBrowse(row)
	case ModeThumbs:
		return a.clickThumbs(col, row, double)
	case ModeEdit:
		return a.clickEditor(col, row)
	}
	return false
}

// clickBrowser:第 0 列是路徑列,清單從第 1 列起;左邊可能有磁碟窗格。
func (a *App) clickBrowser(col, row int, double bool) bool {
	b := a.Browser
	i := row - 1
	if i < 0 || i >= a.rows {
		return false
	}
	if b.DrivePane > 0 && col < b.DrivePane {
		if i >= len(b.Drives) {
			return false
		}
		a.DriveFocus = true
		b.DriveCursor = i
		if double {
			return a.driveKey(keys.Named(keys.Enter))
		}
		return true
	}
	idx := b.Top + i
	if idx >= len(b.Entries) {
		return false
	}
	a.DriveFocus = false
	b.MoveTo(idx, a.rows)
	if double {
		return a.HandleKey(keys.Named(keys.Enter))
	}
	return true
}

// clickBrowse:點到哪一列,就看那一列屬於哪個連結。點在文字上什麼都不做。
func (a *App) clickBrowse(row int) bool {
	line := a.bv.top + row
	for i, l := range a.bv.links {
		if line >= l.line && line < l.line+l.rows {
			a.bv.cur = i
			return a.browseFollow()
		}
	}
	return false
}

// clickThumbs:縮圖是一個 CellCols×CellRows 的格子陣列,由 Top 那一列排起。
func (a *App) clickThumbs(col, row int, double bool) bool {
	t := a.Thumbs
	if t.CellCols <= 0 || t.CellRows <= 0 || row >= a.rows {
		return false
	}
	per := a.thumbCols / t.CellCols
	if per <= 0 {
		return false
	}
	idx := (t.Top+row/t.CellRows)*per + col/t.CellCols
	if col/t.CellCols >= per || idx >= len(t.Items) {
		return false
	}
	t.Cursor = idx
	if double {
		return a.HandleKey(keys.Named(keys.Enter))
	}
	return true
}

// clickEditor:把游標放到點的那個字上。
func (a *App) clickEditor(col, row int) bool {
	e := a.Editor
	ln := e.Top + row
	if row >= a.rows || ln >= len(e.Lines) {
		return false
	}
	e.MoveTo(editor.Pos{Line: ln, Col: e.ScreenToCol(e.Lines[ln], col+e.Left)})
	return true
}

// Drag 是按著左鍵橫向拖了 dx 格。目前只有檔案清單用得到:
// 拖寬主檔名欄。其他模式回 false。
func (a *App) Drag(dx int) bool {
	if dx == 0 || a.Mode != ModeBrowser || a.menu.active || a.prompt.active {
		return false
	}
	return a.Browser.ResizeName(dx)
}

// Wheel 是滾輪。d 是格數,正數往下。
//
// 翻成上下鍵而不是直接動 Top:每個模式的「往下」意思不同(清單移游標、
// 文字捲一列、選單換項目),而那些差別已經全在各自的按鍵處理裡了。
func (a *App) Wheel(d int) bool {
	k := keys.Named(keys.Down)
	if d < 0 {
		k, d = keys.Named(keys.Up), -d
	}
	changed := false
	for ; d > 0; d-- {
		if a.HandleKey(k) {
			changed = true
		}
	}
	return changed
}

// MenuBarClick 是選單列被點了。col 以**選單格**為單位。
// 點到分類就展開它;已經展開同一個就收起來;點在空白處收起來。
func (a *App) MenuBarClick(col int) bool {
	i := a.menuCatAt(col)
	if i < 0 || (a.menu.active && a.menu.cat == i) {
		a.menu = menu{}
		return true
	}
	if !a.menu.active {
		a.menu = menu{active: true}
	}
	a.selectCat(i)
	return true
}

// menuCatAt 回傳選單列第 col 格是哪個分類,不是分類回 -1。
// 版面與 drawMenuBar 相同:從第 1 格起,每個標題前後各留一格。
func (a *App) menuCatAt(col int) int {
	x := 1
	for i, c := range a.menuCats() {
		w := cellWidth(" " + c.name + " ")
		if col >= x && col < x+w {
			return i
		}
		x += w
	}
	return -1
}

// MenuDropClick 是展開的下拉被點了。row 是下拉框內的第幾列(含標題列)。
// 點到項目就執行,點到分隔線或捲動提示不動。
func (a *App) MenuDropClick(row int) bool {
	m := &a.menu
	if !m.active {
		return false
	}
	if m.title != "" {
		row--
	}
	if row < 0 || row >= m.rows {
		return false
	}
	i := m.top + row
	if i >= len(m.items) || m.items[i].sep {
		return false
	}
	m.cursor = i
	return a.menuKey(keys.Named(keys.Enter))
}
