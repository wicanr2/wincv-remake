// Package browser 是檔案瀏覽器主畫面 —— WinCV 所有功能的容器。
//
// 版面照原版:上方路徑列(左邊路徑、右邊計數),中間檔案列表,下方狀態列。
// 列表的欄位是 主檔名 / 副檔名 / 大小 / 日期 / 時間 / 補充,
// 這是 DOS 時代 8.3 的排法,原版把長檔名放在最右邊那一欄。
package browser

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/wicanr2/wincv-remake/internal/cell"
	"github.com/wicanr2/wincv-remake/internal/vfs"
)

// 欄位寬度(以半形格為單位)。取自原版畫面的欄位對齊。
const (
	colBase = 8  // 主檔名
	colExt  = 3  // 副檔名
	colSize = 11 // 大小,靠右
	colDate = 8  // MM-DD-YY
	colTime = 5  // HH:MM
)

// Theme 是畫面用到的顏色。原版可由「顏色」選單設定,
// 所以集中在這裡,不要散在繪圖程式碼裡。
type Theme struct {
	PathFG, PathBG     cell.Color
	DirFG, FileFG      cell.Color
	SizeFG, DateFG     cell.Color
	LinkFG             cell.Color
	MarkFG             cell.Color
	CursorFG, CursorBG cell.Color
	StatusFG, StatusBG cell.Color
	BG                 cell.Color
}

func DefaultTheme() Theme {
	return Theme{
		PathFG: cell.BrightCyan, PathBG: cell.Blue,
		DirFG: cell.BrightGreen, FileFG: cell.LightGray,
		SizeFG: cell.White, DateFG: cell.DarkGray,
		LinkFG: cell.BrightCyan,
		MarkFG: cell.Yellow,
		CursorFG: cell.Black, CursorBG: cell.LightGray,
		StatusFG: cell.Black, StatusBG: cell.LightGray,
		BG: cell.Black,
	}
}

// Model 是瀏覽器的狀態。純資料 + 純函式,不碰畫面也不碰鍵盤,
// 所以可以完全用測試驅動。
type Model struct {
	FS      vfs.FS
	Dir     string
	Entries []Entry

	Cursor int // 游標所在的 index
	Top    int // 畫面第一列對應的 index

	SortKey  vfs.SortKey
	SortDesc bool

	Theme Theme

	// TotalBytes 是這個目錄所有檔案的大小總和,顯示在路徑列右邊。
	TotalBytes int64
	// FreeBytes / DiskBytes 顯示在狀態列。0 表示不知道。
	FreeBytes, DiskBytes int64
}

// Entry 是 Model 持有的一筆,比 vfs.Entry 多了標記狀態。
type Entry struct {
	vfs.Entry
	Marked bool
	// Up 為 true 表示這是往上一層的 ".." 那一筆。
	Up bool
}

func New(fsys vfs.FS, dir string) *Model {
	m := &Model{FS: fsys, Theme: DefaultTheme()}
	m.Load(dir)
	return m
}

// Load 讀入一個目錄。讀不到就保持原狀並回傳錯誤。
func (m *Model) Load(dir string) error {
	es, err := m.FS.ReadDir(dir)
	if err != nil {
		return err
	}
	vfs.Sort(es, m.SortKey, m.SortDesc)

	out := make([]Entry, 0, len(es)+1)
	if p := vfs.Parent(dir); p != dir {
		out = append(out, Entry{Entry: vfs.Entry{Name: "..", IsDir: true}, Up: true})
	}
	var total int64
	for _, e := range es {
		if !e.IsDir {
			total += e.Size
		}
		out = append(out, Entry{Entry: e})
	}
	m.Dir, m.Entries, m.TotalBytes = dir, out, total
	m.Cursor, m.Top = 0, 0
	return nil
}

// Resort 依目前的排序設定重排,並讓游標跟著原本那一筆走。
func (m *Model) Resort() {
	var cur string
	if e := m.Current(); e != nil {
		cur = e.Name
	}
	rest := m.Entries
	var up []Entry
	if len(rest) > 0 && rest[0].Up {
		up, rest = rest[:1], rest[1:]
	}
	plain := make([]vfs.Entry, len(rest))
	for i, e := range rest {
		plain[i] = e.Entry
	}
	marked := map[string]bool{}
	for _, e := range rest {
		if e.Marked {
			marked[e.Name] = true
		}
	}
	vfs.Sort(plain, m.SortKey, m.SortDesc)
	out := append([]Entry{}, up...)
	for _, e := range plain {
		out = append(out, Entry{Entry: e, Marked: marked[e.Name]})
	}
	m.Entries = out
	for i, e := range m.Entries {
		if e.Name == cur {
			m.Cursor = i
			break
		}
	}
}

func (m *Model) Current() *Entry {
	if m.Cursor < 0 || m.Cursor >= len(m.Entries) {
		return nil
	}
	return &m.Entries[m.Cursor]
}

// countReal 回傳不含「..」的筆數。
func (m *Model) countReal() int {
	n := len(m.Entries)
	if n > 0 && m.Entries[0].Up {
		n--
	}
	return n
}

// MarkedStats 回傳已標記的筆數與位元組總和。
func (m *Model) MarkedStats() (n int, bytes int64) {
	for _, e := range m.Entries {
		if e.Marked {
			n++
			bytes += e.Size
		}
	}
	return
}

// --- 游標移動 -------------------------------------------------------------

func (m *Model) clamp(rows int) {
	if m.Cursor < 0 {
		m.Cursor = 0
	}
	if m.Cursor >= len(m.Entries) {
		m.Cursor = len(m.Entries) - 1
	}
	if m.Cursor < 0 {
		m.Cursor = 0
	}
	if rows <= 0 {
		return
	}
	if m.Cursor < m.Top {
		m.Top = m.Cursor
	}
	if m.Cursor >= m.Top+rows {
		m.Top = m.Cursor - rows + 1
	}
	if m.Top < 0 {
		m.Top = 0
	}
}

func (m *Model) MoveBy(d, rows int) { m.Cursor += d; m.clamp(rows) }
func (m *Model) MoveTo(i, rows int) { m.Cursor = i; m.clamp(rows) }
func (m *Model) Home(rows int)      { m.MoveTo(0, rows) }
func (m *Model) End(rows int)       { m.MoveTo(len(m.Entries)-1, rows) }

// --- 標記 -----------------------------------------------------------------

// ToggleMark 切換游標這一筆的標記,然後往下移一格 —— 原版 Space 的行為。
// ".." 不能標記。
func (m *Model) ToggleMark(rows int) {
	if e := m.Current(); e != nil && !e.Up {
		e.Marked = !e.Marked
	}
	m.MoveBy(1, rows)
}

// MarkAll 標記所有檔案。withDirs 為 true 時連目錄一起標記
// (原版 'T' 只標檔案,Alt-T 連目錄)。
func (m *Model) MarkAll(withDirs bool) {
	for i := range m.Entries {
		e := &m.Entries[i]
		if e.Up || (e.IsDir && !withDirs) {
			continue
		}
		e.Marked = true
	}
}

// UnmarkAll 解除所有標記(原版 'U')。
func (m *Model) UnmarkAll() {
	for i := range m.Entries {
		m.Entries[i].Marked = false
	}
}

// --- 繪製 -----------------------------------------------------------------

// Draw 把整個瀏覽器畫進 s。回傳列表區能顯示幾列,呼叫端移動游標時要用。
func (m *Model) Draw(s *cell.Screen) int {
	t := m.Theme
	s.Clear(t.FileFG, t.BG)

	m.drawPathBar(s)
	rows := s.Rows - 2 // 扣掉路徑列與狀態列
	if rows < 0 {
		rows = 0
	}
	m.clamp(rows)
	for i := 0; i < rows; i++ {
		idx := m.Top + i
		if idx >= len(m.Entries) {
			break
		}
		m.drawRow(s, 1+i, m.Entries[idx], idx == m.Cursor)
	}
	m.drawStatus(s)
	return rows
}

func (m *Model) drawPathBar(s *cell.Screen) {
	t := m.Theme
	s.Fill(0, 0, s.Cols, 1, ' ', t.PathFG, t.PathBG)
	s.Print(0, 0, m.FS.Label(m.Dir)+string(filepath.Separator)+"*.*", t.PathFG, t.PathBG)

	// 分母不含「..」—— 原版顯示的是這個目錄裡實際有幾筆,
	// 分子則是含「..」在內的第幾列(原版在游標停在第二列時顯示 "2/ 62")。
	n, bytes := m.MarkedStats()
	right := fmt.Sprintf("%d/%4d  標記: %d / %s / %s",
		m.Cursor+1, m.countReal(), n, comma(bytes), comma(m.TotalBytes))
	x := s.Cols - width(right)
	if x < 0 {
		x = 0
	}
	s.Print(x, 0, right, t.PathFG, t.PathBG)
}

func (m *Model) drawRow(s *cell.Screen, y int, e Entry, cursor bool) {
	t := m.Theme
	nameFG := t.FileFG
	switch {
	case e.Marked:
		nameFG = t.MarkFG
	case e.IsDir:
		nameFG = t.DirFG
	}

	x := 1
	base, ext := e.Base(), e.Ext()
	long := e.Link
	// 放不進 8.3 的名字,主欄位放截斷版,完整名字丟到最右欄 —— 原版
	// 在 Windows 上是「短名在左、長名在右」,這裡是同一個版面的等價作法。
	if width(base) > colBase || width(ext) > colExt {
		if long == "" {
			long = e.Name
		}
		base = truncate(base, colBase)
		ext = truncate(ext, colExt)
	}
	x += s.Print(x, y, pad(base, colBase), nameFG, t.BG)
	x++
	x += s.Print(x, y, pad(ext, colExt), nameFG, t.BG)
	x++

	var size string
	if e.IsDir {
		size = "<DIR>"
	} else {
		size = comma(e.Size)
	}
	x += s.Print(x, y, lpad(size, colSize), t.SizeFG, t.BG)
	x++

	if !e.ModTime.IsZero() {
		x += s.Print(x, y, e.ModTime.Format("01-02-06"), t.DateFG, t.BG)
		x++
		x += s.Print(x, y, e.ModTime.Format("15:04"), t.DateFG, t.BG)
		x++
	} else {
		x += colDate + colTime + 2
	}

	if long != "" && x < s.Cols {
		s.Print(x, y, long, t.LinkFG, t.BG)
	}

	if cursor {
		s.SetAttr(0, y, s.Cols, t.CursorFG, t.CursorBG)
	}
}

func (m *Model) drawStatus(s *cell.Screen) {
	t := m.Theme
	y := s.Rows - 1
	s.Fill(0, y, s.Cols, 1, ' ', t.StatusFG, t.StatusBG)
	if e := m.Current(); e != nil {
		line := e.Name
		if !e.IsDir {
			line = fmt.Sprintf("%s  %s", e.Name, comma(e.Size))
		}
		if !e.ModTime.IsZero() {
			line += "  " + e.ModTime.Format("2006-01-02 15:04")
		}
		s.Print(0, y, line, t.StatusFG, t.StatusBG)
	}
	if m.DiskBytes > 0 {
		right := fmt.Sprintf("剩餘: %sMB / %sMB",
			comma(m.FreeBytes>>20), comma(m.DiskBytes>>20))
		x := s.Cols - width(right)
		if x >= 0 {
			s.Print(x, y, right, t.StatusFG, t.StatusBG)
		}
	}
}

// --- 小工具 ---------------------------------------------------------------

// width 回傳一個字串佔幾格(全形算兩格)。
func width(s string) int {
	n := 0
	for _, r := range s {
		if cell.IsWide(r) {
			n += 2
		} else {
			n++
		}
	}
	return n
}

func pad(s string, w int) string {
	if d := w - width(s); d > 0 {
		return s + strings.Repeat(" ", d)
	}
	return s
}

func lpad(s string, w int) string {
	if d := w - width(s); d > 0 {
		return strings.Repeat(" ", d) + s
	}
	return s
}

// truncate 依顯示格數截斷,不會把全形字切成一半。
func truncate(s string, w int) string {
	n := 0
	for i, r := range s {
		cw := 1
		if cell.IsWide(r) {
			cw = 2
		}
		if n+cw > w {
			return s[:i]
		}
		n += cw
	}
	return s
}

// comma 把數字加上千位逗號 —— 原版的大小欄就是這個格式。
func comma(v int64) string {
	neg := v < 0
	if neg {
		v = -v
	}
	s := fmt.Sprintf("%d", v)
	var sb strings.Builder
	for i, c := range s {
		if i > 0 && (len(s)-i)%3 == 0 {
			sb.WriteByte(',')
		}
		sb.WriteRune(c)
	}
	if neg {
		return "-" + sb.String()
	}
	return sb.String()
}

var _ = time.Time{}
