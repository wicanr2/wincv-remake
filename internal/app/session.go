package app

import (
	"github.com/wicanr2/wincv-remake/internal/browser"
	"os"
	"path/filepath"

	"github.com/wicanr2/wincv-remake/internal/archive"
	"github.com/wicanr2/wincv-remake/internal/session"
)

// Snapshot 取一份「回到原處要知道的事」。
//
// 壓縮檔內部不存:那個路徑對下一次啟動沒有意義(要先把壓縮檔開回來
// 才走得到),存了反而會讓還原失敗。存它外面的那一層目錄就好。
func (a *App) Snapshot() session.State {
	st := session.State{
		Dir:      a.Browser.Dir,
		Cols:     a.thumbCols,
		Rows:     a.rows,
		Zoom:     a.Zoom,
		NameW:    a.Browser.NameW,
		Scale:    a.Scale,
		MenuBar:  session.Bool(a.MenuBar),
		MenuZoom: session.Int(a.MenuZoom),
	}
	a.rememberPos()
	st.Positions = a.positions
	if e := a.Browser.Current(); e != nil {
		st.Cursor = e.Name
	}
	// 壓縮檔內部的位置不存:那條路徑下次啟動走不到(要先把壓縮檔
	// 開回來)。改存壓縮檔本身所在的目錄,並讓游標停在那個壓縮檔上。
	if _, ok := a.Browser.FS.(*archive.FS); ok {
		outer, _ := archiveSplit(a.Browser.Dir)
		st.Dir, st.Cursor = filepath.Dir(outer), filepath.Base(outer)
		st.Mode, st.File, st.Top = "", "", 0
		return st
	}

	switch a.Mode {
	case ModeViewer:
		if a.Viewer != nil {
			st.Mode, st.File, st.Top = "viewer", a.Viewer.Name, a.Viewer.Top
			st.Cur = a.Viewer.Cur
		}
	case ModeHex:
		if a.Hex != nil {
			st.Mode, st.File, st.Top = "hex", a.Hex.Name, a.Hex.Top
		}
	case ModeEdit:
		if a.Editor != nil {
			st.Mode, st.File, st.Top = "edit", a.Editor.Name, a.Editor.Top
		}
	case ModeMarkdown:
		st.Mode, st.File, st.Top = "markdown", a.md.name, a.md.top
	case ModeImage:
		if a.Image != nil {
			st.Mode, st.File = "image", a.Image.Name
		}
	case ModeBrowse:
		st.Mode, st.URL = "browse", a.bv.url
	}
	// 說明是內嵌的,不是檔案 —— 還原時會找不到那個「檔名」。
	if st.File == helpName {
		st.Mode, st.File, st.Top = "", "", 0
	}
	return st
}

// Restore 回到上次的位置。
//
// 每一步都可以失敗而不影響下一步:目錄不見了就留在原處、檔案不見了
// 就只回到目錄。**還原是錦上添花,不能讓程式開不起來** ——
// 上次看的那個檔案很可能正是這次被刪掉的那一個。
func (a *App) Restore(st session.State) {
	if st.Dir != "" {
		if fi, err := os.Stat(st.Dir); err == nil && fi.IsDir() {
			a.Browser.Dir = st.Dir
			a.Browser.Reload()
		}
	}
	if st.Zoom > 0 && st.Zoom <= a.MaxZoom {
		a.Zoom = st.Zoom
	}
	if st.NameW >= browser.MinNameW && st.NameW <= browser.MaxNameW {
		a.Browser.NameW = st.NameW
	}
	if st.Scale >= MinScale && st.Scale <= MaxScale {
		a.Scale = st.Scale
	}
	if st.MenuBar != nil {
		a.MenuBar = *st.MenuBar
	}
	if st.MenuZoom != nil && *st.MenuZoom >= -1 && *st.MenuZoom <= a.MaxZoom {
		a.MenuZoom = *st.MenuZoom
	}
	if len(st.Positions) > 0 {
		a.positions = st.Positions
	}
	if st.Cursor != "" {
		a.focusOn(st.Cursor)
	}

	if st.Mode == "browse" {
		// 書與 Office 文件都是本機檔案,直接開回去。網頁不行 —— 上次停在哪一頁
		// 不值得讓程式一啟動就對外發出請求,位址留著等使用者按 Enter。
		_, _, isBook := parseBookURL(st.URL)
		_, _, isPDF := parsePDFURL(st.URL)
		_, _, isDoc := parseDocURL(st.URL)
		if isBook || isPDF || isDoc {
			a.OpenURL(st.URL)
			return
		}
		a.bv.url = st.URL
		return
	}
	if st.Mode == "" || st.File == "" {
		return
	}
	full := filepath.Join(a.Browser.Dir, st.File)
	if fi, err := os.Stat(full); err != nil || fi.IsDir() {
		return
	}
	a.focusOn(st.File)
	switch st.Mode {
	case "edit":
		a.openEditor()
		if a.Editor != nil {
			a.Editor.Top = clampTop(st.Top, len(a.Editor.Lines))
		}
	case "image":
		a.openImage(st.File)
	case "hex":
		data, err := a.readCurrent(st.File)
		if err != nil {
			return
		}
		a.openHex(st.File, data)
		if a.Hex != nil {
			a.Hex.Top = clampTop(st.Top, len(a.Hex.Data)/16+1)
		}
	default: // viewer / markdown:openViewer 會依內容自己選一個
		a.openViewer(st.File)
		switch {
		case a.Viewer != nil && a.Mode == ModeViewer:
			a.Viewer.Top = clampTop(st.Top, len(a.Viewer.Lines))
			a.Viewer.Cur = clampTop(st.Cur, len(a.Viewer.Lines))
		case a.Mode == ModeMarkdown:
			a.md.top = clampTop(st.Top, len(a.md.blocks)*4)
		}
	}
}

// clampTop 把存下來的捲動位置夾回合理範圍。
//
// 檔案在兩次執行之間會變:上次捲到第 900 列,這次那個檔只剩 10 列。
// 不夾的話畫面會是一片空白,而使用者只會覺得「檔案不見了」。
func clampTop(top, n int) int {
	if top < 0 || n <= 0 {
		return 0
	}
	if top >= n {
		return 0
	}
	return top
}
