// Package app 把各個模式接起來:檔案瀏覽器、文字檢視器,
// 以及它們之間的切換與按鍵分派。
//
// 這一層不依賴 Ebiten,所以整個互動流程可以 headless 測。
package app

import (
	"fmt"
	"io"
	"path/filepath"
	"strings"

	"github.com/wicanr2/wincv-remake/internal/browser"
	"github.com/wicanr2/wincv-remake/internal/cell"
	"github.com/wicanr2/wincv-remake/internal/keys"
	"github.com/wicanr2/wincv-remake/internal/textenc"
	"github.com/wicanr2/wincv-remake/internal/vfs"
	"github.com/wicanr2/wincv-remake/internal/viewer"
)

// Mode 是目前在哪一個畫面。
type Mode int

const (
	ModeBrowser Mode = iota
	ModeViewer
)

// MaxViewBytes 是檢視器一次讀進來的上限。
// 原版能順順地看大檔;先設一個保守上限,之後改成串流。
const MaxViewBytes = 64 << 20

// App 是整個程式的狀態。
type App struct {
	FS      vfs.FS
	Browser *browser.Model
	Viewer  *viewer.Model
	Mode    Mode

	// Message 是暫時顯示在狀態列的訊息(錯誤、提示)。
	Message string

	// Quit 被設成 true 時,外層應該結束程式。
	Quit bool

	// rows 是最近一次繪製時內容區的列數,按鍵處理要用來算翻頁。
	rows int

	// viewRaw 留著原始解碼文字,切換 ANSI 時要重新解析。
	viewRaw string
}

func New(fsys vfs.FS, dir string) *App {
	return &App{FS: fsys, Browser: browser.New(fsys, dir)}
}

// Draw 依目前模式畫出畫面。
func (a *App) Draw(s *cell.Screen) {
	switch a.Mode {
	case ModeViewer:
		a.rows = a.Viewer.Draw(s)
	default:
		a.rows = a.Browser.Draw(s)
	}
	if a.Message != "" {
		y := s.Rows - 1
		s.Fill(0, y, s.Cols, 1, ' ', cell.White, cell.Red)
		s.Print(0, y, a.Message, cell.White, cell.Red)
	}
}

// HandleKey 分派一次按鍵。回傳是否有東西改變(需要重畫)。
func (a *App) HandleKey(k keys.Key) bool {
	a.Message = ""
	switch a.Mode {
	case ModeViewer:
		return a.viewerKey(k)
	default:
		return a.browserKey(k)
	}
}

// --- 檔案瀏覽器 -----------------------------------------------------------

func (a *App) browserKey(k keys.Key) bool {
	b := a.Browser
	rows := a.rows
	if rows <= 0 {
		rows = 1
	}

	switch k.Code {
	case keys.Up:
		b.MoveBy(-1, rows)
		return true
	case keys.Down:
		b.MoveBy(1, rows)
		return true
	case keys.PgUp:
		b.MoveBy(-rows, rows)
		return true
	case keys.PgDn:
		b.MoveBy(rows, rows)
		return true
	case keys.Home:
		b.Home(rows)
		return true
	case keys.End:
		b.End(rows)
		return true
	case keys.Enter:
		return a.enter()
	case keys.Backspace:
		return a.goParent()
	}

	if k.Code == keys.Rune && k.R == ' ' && !k.Alt && !k.Ctrl {
		b.ToggleMark(rows)
		return true
	}

	switch k.Letter() {
	case 'T':
		// T 只標檔案,Alt-T 連目錄一起標(0.5 版起的行為)。
		b.MarkAll(k.Alt)
		return true
	case 'U':
		if !k.Alt && !k.Ctrl {
			b.UnmarkAll()
			return true
		}
	case 'S':
		if !k.Alt && !k.Ctrl {
			// 依序切換排序欄位。原版的 '5' 是「改變檔案列表的方式」,
			// 語意還沒實測清楚(見 docs/ui/keymap.md 待實測第 4 項),
			// 先給一個明確可用的排序切換。
			b.SortKey = (b.SortKey + 1) % 4
			b.Resort()
			a.Message = "排序:" + sortName(b.SortKey)
			return true
		}
	}
	return false
}

func sortName(k vfs.SortKey) string {
	switch k {
	case vfs.ByExt:
		return "副檔名"
	case vfs.BySize:
		return "大小"
	case vfs.ByTime:
		return "時間"
	}
	return "檔名"
}

// enter 進入目錄或開啟檔案。
func (a *App) enter() bool {
	e := a.Browser.Current()
	if e == nil {
		return false
	}
	if e.IsDir {
		dir := a.Browser.Dir
		if e.Up {
			dir = vfs.Parent(dir)
		} else {
			dir = filepath.Join(dir, e.Name)
		}
		if err := a.Browser.Load(dir); err != nil {
			a.Message = "無法進入 " + dir + ": " + err.Error()
		}
		return true
	}
	return a.openViewer(e.Name)
}

func (a *App) goParent() bool {
	p := vfs.Parent(a.Browser.Dir)
	if p == a.Browser.Dir {
		return false
	}
	prev := filepath.Base(a.Browser.Dir)
	if err := a.Browser.Load(p); err != nil {
		a.Message = "無法回上一層: " + err.Error()
		return true
	}
	// 回到上一層時,游標停在剛才離開的那個目錄上 —— 一路往回走時
	// 游標每次都跳回第一筆會很難用。
	for i, e := range a.Browser.Entries {
		if e.Name == prev {
			a.Browser.MoveTo(i, a.rows)
			break
		}
	}
	return true
}

func (a *App) openViewer(name string) bool {
	full := filepath.Join(a.Browser.Dir, name)
	rc, err := a.FS.Open(full)
	if err != nil {
		a.Message = "開不了 " + name + ": " + err.Error()
		return true
	}
	defer rc.Close()
	data, err := io.ReadAll(io.LimitReader(rc, MaxViewBytes))
	if err != nil {
		a.Message = "讀取失敗: " + err.Error()
		return true
	}
	m := viewer.Load(name, data, textenc.Unknown)
	if m.Enc == textenc.Binary {
		a.Message = fmt.Sprintf("%s 看起來是二進位檔(%s bytes)", name, comma(int64(len(data))))
		return true
	}
	a.viewRaw = textenc.Decode(data, m.Enc)
	a.Viewer = m
	a.Mode = ModeViewer
	return true
}

// --- 文字檢視器 -----------------------------------------------------------

func (a *App) viewerKey(k keys.Key) bool {
	v := a.Viewer
	rows := a.rows
	if rows <= 0 {
		rows = 1
	}

	switch k.Code {
	case keys.Up:
		v.ScrollBy(-1, rows)
		return true
	case keys.Down:
		v.ScrollBy(1, rows)
		return true
	case keys.PgUp:
		v.ScrollBy(-rows, rows)
		return true
	case keys.PgDn:
		v.ScrollBy(rows, rows)
		return true
	case keys.Home:
		v.Home(rows)
		return true
	case keys.End:
		v.End(rows)
		return true
	case keys.Left:
		if !v.Wrap && v.Left > 0 {
			v.Left -= 8
			if v.Left < 0 {
				v.Left = 0
			}
			return true
		}
	case keys.Right:
		if !v.Wrap {
			v.Left += 8
			return true
		}
	case keys.Esc, keys.Enter, keys.Backspace:
		// Esc 是回上一層,不是離開程式。
		// 見 knowledge-base/retro-cht/esc-cancel-f10-quit-autosave。
		a.Mode = ModeBrowser
		return true
	}

	switch k.Letter() {
	case 'W':
		if !k.Ctrl && !k.Alt {
			v.Wrap = !v.Wrap
			v.Left = 0
			return true
		}
	case 'A':
		if !k.Ctrl && !k.Alt {
			v.SetAnsi(!v.Ansi, a.viewRaw)
			return true
		}
	case 'N':
		if k.Ctrl {
			v.NextHit(rows)
			return true
		}
	}
	return false
}

// --- 小工具 ---------------------------------------------------------------

func comma(v int64) string {
	s := fmt.Sprintf("%d", v)
	var sb strings.Builder
	for i, c := range s {
		if i > 0 && (len(s)-i)%3 == 0 {
			sb.WriteByte(',')
		}
		sb.WriteRune(c)
	}
	return sb.String()
}
