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
	"github.com/wicanr2/wincv-remake/internal/hexview"
	"github.com/wicanr2/wincv-remake/internal/imgfmt"
	"github.com/wicanr2/wincv-remake/internal/imgview"
	"github.com/wicanr2/wincv-remake/internal/render"
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
	ModeHex
	ModeImage
)

// MaxViewBytes 是檢視器一次讀進來的上限。
// 原版能順順地看大檔;先設一個保守上限,之後改成串流。
const MaxViewBytes = 64 << 20

// App 是整個程式的狀態。
type App struct {
	FS      vfs.FS
	Browser *browser.Model
	Viewer  *viewer.Model
	Hex     *hexview.Model
	Image   *imgview.Model
	Mode    Mode

	// CellW / CellH 是格子的像素尺寸。看圖模式要用它把格點座標
	// 換算成像素矩形;外層(cmd/wincv 或 celldump)要在建立後設好。
	CellW, CellH int

	// Message 是暫時顯示在狀態列的訊息(錯誤、提示)。
	Message string

	// Quit 被設成 true 時,外層應該結束程式。
	Quit bool

	// rows 是最近一次繪製時內容區的列數,按鍵處理要用來算翻頁。
	rows int

	// viewRaw 留著原始解碼文字,切換 ANSI 時要重新解析。
	viewRaw string
	// viewData 是目前檢視中的原始位元組,文字與 16 進位之間切換要用。
	viewData []byte

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
	return &App{FS: fsys, Browser: browser.New(fsys, dir), CellW: 8, CellH: 15}
}

// Draw 依目前模式畫出畫面。回傳的 overlay 不為 nil 時,
// 呼叫端要把它交給 render.Rasterizer.DrawWith。
func (a *App) Draw(s *cell.Screen) *render.Overlay {
	var ov *render.Overlay
	switch a.Mode {
	case ModeViewer:
		a.rows = a.Viewer.Draw(s)
	case ModeHex:
		a.rows = a.Hex.Draw(s)
	case ModeImage:
		ov = a.Image.Draw(s, a.CellW, a.CellH)
		a.rows = s.Rows - 1
	default:
		a.rows = a.Browser.Draw(s)
	}
	if a.Message != "" {
		y := s.Rows - 1
		s.Fill(0, y, s.Cols, 1, ' ', cell.White, cell.Red)
		s.Print(0, y, a.Message, cell.White, cell.Red)
	}
	return ov
}

// HandleKey 分派一次按鍵。回傳是否有東西改變(需要重畫)。
func (a *App) HandleKey(k keys.Key) bool {
	a.Message = ""
	switch a.Mode {
	case ModeViewer:
		return a.viewerKey(k)
	case ModeHex:
		return a.hexKey(k)
	case ModeImage:
		return a.imageKey(k)
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
	if imgfmt.IsImage(e.Name) {
		return a.openImage(e.Name)
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

// readCurrent 讀目前目錄下的一個檔案(壓縮檔內也適用)。
func (a *App) readCurrent(name string) ([]byte, error) {
	rc, err := a.Browser.FS.Open(a.joinDir(name))
	if err != nil {
		return nil, fmt.Errorf("開不了 %s: %w", name, err)
	}
	defer rc.Close()
	data, err := io.ReadAll(io.LimitReader(rc, MaxViewBytes))
	if err != nil {
		return nil, fmt.Errorf("讀取失敗: %w", err)
	}
	return data, nil
}

func (a *App) openViewer(name string) bool {
	data, err := a.readCurrent(name)
	if err != nil {
		a.Message = err.Error()
		return true
	}
	// 判成二進位就直接開 16 進位檢視 —— 原版 0.5 版起就是這個行為
	// (「按 enter 看檔時自動將可能為執行檔的檔案以 16 進位方式看檔」)。
	m := viewer.Load(name, data, textenc.Unknown)
	if m.Enc == textenc.Binary {
		a.openHex(name, data)
		return true
	}
	a.viewData = data
	a.viewRaw = textenc.Decode(data, m.Enc)
	a.Viewer = m
	a.Mode = ModeViewer
	return true
}

func (a *App) openImage(name string) bool {
	data, err := a.readCurrent(name)
	if err != nil {
		a.Message = err.Error()
		return true
	}
	m, err := imgview.Load(name, data)
	if err != nil {
		// 解不開就退回 16 進位 —— 至少看得到內容,而不是什麼都沒有。
		a.Message = err.Error()
		a.openHex(name, data)
		return true
	}
	a.Image = m
	a.Mode = ModeImage
	return true
}

// imageNeighbours 回傳目前目錄裡所有圖檔的索引,用於看上一張/下一張。
func (a *App) imageNeighbours() []int {
	var out []int
	for i, e := range a.Browser.Entries {
		if !e.IsDir && imgfmt.IsImage(e.Name) {
			out = append(out, i)
		}
	}
	return out
}

// stepImage 換到上一張或下一張圖(原版 Enter / BackSpace)。
func (a *App) stepImage(d int) bool {
	idx := a.imageNeighbours()
	if len(idx) == 0 {
		return false
	}
	cur := -1
	for i, v := range idx {
		if v == a.Browser.Cursor {
			cur = i
			break
		}
	}
	if cur < 0 {
		cur = 0
	}
	next := (cur + d + len(idx)) % len(idx)
	a.Browser.MoveTo(idx[next], a.rows)
	e := a.Browser.Current()
	if e == nil {
		return false
	}
	return a.openImage(e.Name)
}

func (a *App) imageKey(k keys.Key) bool {
	m := a.Image
	switch k.Code {
	case keys.Enter:
		return a.stepImage(1)
	case keys.Backspace:
		return a.stepImage(-1)
	case keys.Esc:
		a.Mode = ModeBrowser
		return true
	case keys.Up:
		m.PanBy(0, -32)
		return true
	case keys.Down:
		m.PanBy(0, 32)
		return true
	case keys.Left:
		m.PanBy(-32, 0)
		return true
	case keys.Right:
		m.PanBy(32, 0)
		return true
	}
	if k.Code == keys.Rune && k.R == ' ' {
		// 標記此檔,換看下一張(原版行為)。
		if e := a.Browser.Current(); e != nil && !e.Up {
			e.Marked = !e.Marked
		}
		return a.stepImage(1)
	}
	switch k.Letter() {
	case 'I':
		m.Info = !m.Info
		return true
	case 'F':
		m.ToggleFit()
		return true
	}
	return false
}

func (a *App) openHex(name string, data []byte) {
	a.Hex = hexview.Load(name, data)
	a.viewData = data
	a.Mode = ModeHex
}

// --- 16 進位檢視 ----------------------------------------------------------

func (a *App) hexKey(k keys.Key) bool {
	h := a.Hex
	rows := a.rows
	if rows <= 0 {
		rows = 1
	}
	switch k.Code {
	case keys.Up:
		h.ScrollBy(-1, rows)
		return true
	case keys.Down:
		h.ScrollBy(1, rows)
		return true
	case keys.PgUp:
		h.ScrollBy(-rows, rows)
		return true
	case keys.PgDn:
		h.ScrollBy(rows, rows)
		return true
	case keys.Home:
		h.Home(rows)
		return true
	case keys.End:
		h.End(rows)
		return true
	case keys.Esc, keys.Enter, keys.Backspace:
		a.Mode = ModeBrowser
		return true
	}
	switch k.Letter() {
	case 'H':
		// 從 16 進位切回文字 —— 只有本來就是文字檔才切得回去。
		if a.Viewer != nil {
			a.Mode = ModeViewer
			return true
		}
	case 'F':
		if !k.Ctrl && !k.Alt {
			h.Big5 = !h.Big5
			return true
		}
	}
	return false
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
	case 'H':
		// H 切到 16 進位(image 內的標籤是「&Hex模式」)。
		if !k.Ctrl && !k.Alt {
			a.openHex(v.Name, a.viewData)
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
