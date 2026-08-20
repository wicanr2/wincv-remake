// Package app 把各個模式接起來:檔案瀏覽器、文字檢視器,
// 以及它們之間的切換與按鍵分派。
//
// 這一層不依賴 Ebiten,所以整個互動流程可以 headless 測。
package app

import (
	"fmt"
	"io"
	"path"
	"path/filepath"
	"strings"

	"github.com/wicanr2/wincv-remake/internal/archive"
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

	// stack 記錄「進入壓縮檔之前在哪」。進壓縮檔時 push,
	// 從壓縮檔最上層再往上時 pop —— 使用者感覺就只是進出目錄。
	stack []layer
}

type layer struct {
	fs     vfs.FS
	dir    string
	cursor int
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
		if e.Up {
			return a.goParent()
		}
		dir := a.joinDir(e.Name)
		if err := a.Browser.Load(dir); err != nil {
			a.Message = "無法進入 " + dir + ": " + err.Error()
		}
		return true
	}
	if archive.IsArchive(e.Name) {
		return a.enterArchive(e.Name)
	}
	return a.openViewer(e.Name)
}

// joinDir 接出子目錄的路徑。壓縮檔內用 / 分隔,不能用 filepath.Join
// (Windows 上會變成反斜線,而壓縮檔裡的路徑一律是斜線)。
func (a *App) joinDir(name string) string {
	if af, ok := a.Browser.FS.(*archive.FS); ok {
		_, inner := archiveSplit(a.Browser.Dir)
		if inner == "" {
			return af.Path(name)
		}
		return af.Path(inner + "/" + name)
	}
	return filepath.Join(a.Browser.Dir, name)
}

func archiveSplit(p string) (string, string) {
	if i := strings.Index(p, "!"); i >= 0 {
		return p[:i], strings.Trim(p[i+1:], "/")
	}
	return p, ""
}

// enterArchive 把壓縮檔當目錄進去。
func (a *App) enterArchive(name string) bool {
	full := filepath.Join(a.Browser.Dir, name)
	af, err := archive.Open(full)
	if err != nil {
		a.Message = err.Error()
		return true
	}
	a.stack = append(a.stack, layer{fs: a.Browser.FS, dir: a.Browser.Dir, cursor: a.Browser.Cursor})
	a.Browser.FS = af
	if err := a.Browser.Load(af.Root()); err != nil {
		a.Message = err.Error()
		a.popLayer()
	}
	return true
}

func (a *App) popLayer() bool {
	if len(a.stack) == 0 {
		return false
	}
	l := a.stack[len(a.stack)-1]
	a.stack = a.stack[:len(a.stack)-1]
	a.Browser.FS = l.fs
	if err := a.Browser.Load(l.dir); err != nil {
		a.Message = err.Error()
		return true
	}
	a.Browser.MoveTo(l.cursor, a.rows)
	return true
}

func (a *App) goParent() bool {
	// 在壓縮檔最上層再往上,就是離開這個壓縮檔。
	if af, ok := a.Browser.FS.(*archive.FS); ok {
		if af.IsRoot(a.Browser.Dir) {
			return a.popLayer()
		}
		_, inner := archiveSplit(a.Browser.Dir)
		up := path.Dir(inner)
		if up == "." {
			up = ""
		}
		prev := path.Base(inner)
		if err := a.Browser.Load(af.Path(up)); err != nil {
			a.Message = err.Error()
			return true
		}
		a.focusOn(prev)
		return true
	}

	p := vfs.Parent(a.Browser.Dir)
	if p == a.Browser.Dir {
		return false
	}
	prev := filepath.Base(a.Browser.Dir)
	if err := a.Browser.Load(p); err != nil {
		a.Message = "無法回上一層: " + err.Error()
		return true
	}
	a.focusOn(prev)
	return true
}

// focusOn 把游標移到指定名稱那一筆。回上一層時游標停在剛才離開的
// 目錄上 —— 一路往回走時每次都跳回第一筆會很難用。
func (a *App) focusOn(name string) {
	for i, e := range a.Browser.Entries {
		if e.Name == name {
			a.Browser.MoveTo(i, a.rows)
			return
		}
	}
}

func (a *App) openViewer(name string) bool {
	full := a.joinDir(name)
	rc, err := a.Browser.FS.Open(full)
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
