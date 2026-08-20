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
	MaskFG             cell.Color // 路徑列尾巴的 "*.*"
	CountFG            cell.Color // 路徑列右邊的「第幾筆 / 共幾筆」
	MarkStatFG         cell.Color // 「標記:」後面的兩個數字
	TotalFG            cell.Color // 目錄總位元組
	DirFG, FileFG      cell.Color
	DateFG             cell.Color
	LinkFG             cell.Color
	MarkFG             cell.Color
	MarkColBG          cell.Color // 最左邊那一欄(游標與標記的指示欄)的底色
	CursorFG, CursorBG cell.Color
	StatusFG, StatusBG cell.Color
	BG                 cell.Color
}

// DefaultTheme 的每個值都是從原版截圖上量的,不是配的。
// 量測對象 docs/ui/oracle-window.png,重跑:
//
//	tools/celldiff.py docs/ui/oracle-window.png <重製版.png> --ox 34 --oy 40
//
// 幾個一眼看不出來、但量得到的:
//   - 最左邊的指示欄是 #000080,而且**游標列也不變**(游標的底色只從第 1 欄開始)。
//   - 日期固定 #00FF00,連游標列上都不變;其餘每一欄(含時間)都跟著
//     檔案自己的顏色走。
//   - 目錄是 #14BE00,不是一般的 ltgreen #00FF00 —— image 裡是另一個 word。
//   - 游標列是白字配 #800000,不是反白。
//   - 路徑列不是單一顏色:路徑本身 #FFFF00、尾巴的 "*.*" #00FF00、
//     右邊的筆數 #C5C5C5、標記數 #FFFF00、目錄總位元組 #FFFFFF。
func DefaultTheme() Theme {
	return Theme{
		PathFG: cell.LtYellow, PathBG: cell.Blue,
		MaskFG: cell.LtGreen, CountFG: cell.LtGray2,
		MarkStatFG: cell.LtYellow, TotalFG: cell.White,
		DirFG: cell.DirGreen, FileFG: cell.LtGray,
		DateFG: cell.LtGreen,
		LinkFG: cell.LtGray,
		MarkFG: cell.Yellow,
		MarkColBG: cell.Blue,
		CursorFG: cell.White, CursorBG: cell.Red,
		StatusFG: cell.LtGray2, StatusBG: cell.Blue,
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

	// Notes 是這個目錄的檔案註解(原版的 dir.doc),沒有就是 nil。
	// 由 NoteLoader 在每次 Load 之後填。
	Notes map[string]string
	// NoteLoader 讀某個目錄的註解。設成 nil 就不顯示註解 ——
	// 壓縮檔裡面沒有真的目錄可讀,呼叫端要自己判斷後回 nil。
	NoteLoader func(dir string) map[string]string

	// ColorOf 決定一列用什麼顏色(原版依副檔名分類上色)。
	// 設成 nil 就全部用 Theme.FileFG。判斷規則需要知道哪些副檔名是
	// 壓縮檔、哪些是圖檔,那是上層才有的知識,所以做成 hook。
	ColorOf func(e Entry) cell.Color

	// ReserveBottom 是畫面底部要留給別人用的列數(原版的預視窗格)。
	// 狀態列會往上移到留白之上,列表也跟著變短。
	ReserveBottom int
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
	m.Notes = nil
	if m.NoteLoader != nil {
		m.Notes = m.NoteLoader(dir)
	}
	return nil
}

// Reload 重讀目前的目錄,並讓游標留在原本那一筆上。
//
// 檔案操作(拷貝、刪除、轉換)之後都要重讀,但**不該把游標丟回最上面** ——
// 在幾百個檔案的目錄裡連續做兩次操作時,每次都要重新捲回去很惱人。
func (m *Model) Reload() error {
	cur, top := "", m.Top
	if e := m.Current(); e != nil {
		cur = e.Name
	}
	if err := m.Load(m.Dir); err != nil {
		return err
	}
	if cur == "" {
		return nil
	}
	for i, e := range m.Entries {
		if e.Name == cur {
			m.Cursor, m.Top = i, top
			return nil
		}
	}
	// 原本那一筆不見了(例如剛被刪掉),游標留在同一個位置,
	// 這樣連續刪除時會自然停在下一個檔案上。
	if m.Cursor >= len(m.Entries) {
		m.Cursor = len(m.Entries) - 1
	}
	if m.Cursor < 0 {
		m.Cursor = 0
	}
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
	rows := s.Rows - 2 - m.ReserveBottom // 扣掉路徑列、狀態列與底部保留區
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
	// 指示欄的底色鋪滿整個清單區,空白列也要有 —— 原版量到的就是一整條。
	s.Fill(0, 1, 1, rows, ' ', t.FileFG, t.MarkColBG)
	m.drawStatus(s)
	return rows
}

func (m *Model) drawPathBar(s *cell.Screen) {
	t := m.Theme
	s.Fill(0, 0, s.Cols, 1, ' ', t.PathFG, t.PathBG)
	x := s.Print(0, 0, m.FS.Label(m.Dir)+string(filepath.Separator), t.PathFG, t.PathBG)
	s.Print(x, 0, "*.*", t.MaskFG, t.PathBG)

	// 分母不含「..」—— 原版顯示的是這個目錄裡實際有幾筆,
	// 分子則是含「..」在內的第幾列(原版在游標停在第二列時顯示 "2/ 62")。
	n, bytes := m.MarkedStats()
	segs := []struct {
		text string
		fg   cell.Color
	}{
		{fmt.Sprintf("%d/%4d  ", m.Cursor+1, m.countReal()), t.CountFG},
		{"標記: ", t.PathFG},
		{fmt.Sprintf("%d / %s / ", n, comma(bytes)), t.MarkStatFG},
		{comma(m.TotalBytes), t.TotalFG},
	}
	total := 0
	for _, sg := range segs {
		total += width(sg.text)
	}
	x = s.Cols - total
	if x < 0 {
		x = 0
	}
	for _, sg := range segs {
		x += s.Print(x, 0, sg.text, sg.fg, t.PathBG)
	}
}

func (m *Model) drawRow(s *cell.Screen, y int, e Entry, cursor bool) {
	t := m.Theme
	bg := t.BG
	if cursor {
		// 游標列從第 1 欄開始鋪底色,第 0 欄(指示欄)不動。
		bg = t.CursorBG
		s.Fill(1, y, s.Cols-1, 1, ' ', t.CursorFG, bg)
	}
	nameFG := t.FileFG
	switch {
	case e.Marked:
		nameFG = t.MarkFG
	case e.IsDir:
		nameFG = t.DirFG
	case m.ColorOf != nil:
		nameFG = m.ColorOf(e)
	}
	if cursor {
		nameFG = t.CursorFG
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
	x += s.Print(x, y, pad(base, colBase), nameFG, bg)
	x++
	x += s.Print(x, y, pad(ext, colExt), nameFG, bg)
	x++

	var size string
	if e.IsDir {
		size = "<DIR>"
	} else {
		size = comma(e.Size)
	}
	x += s.Print(x, y, lpad(size, colSize), nameFG, bg)
	x++

	if !e.ModTime.IsZero() {
		// 日期在游標列上也保持原色,時間則跟著變白 —— 原版量到的就是這樣。
		x += s.Print(x, y, e.ModTime.Format("01-02-06"), t.DateFG, bg)
		x++
		// 時間跟著檔案自己的顏色走(量到:magenta 的檔案連時間也是
		// magenta),只有日期是固定綠色。
		x += s.Print(x, y, e.ModTime.Format("15:04"), nameFG, bg)
		x++
	} else {
		x += colDate + colTime + 2
	}

	linkFG := t.LinkFG
	if cursor {
		linkFG = t.CursorFG
	}
	if long != "" && x < s.Cols {
		// 長檔名欄原版是加底線的 c0c0c0,不是換色 —— 量到底線就在
		// 格子的最後一條掃描線上(docs/ui/oracle-window.png)。
		n := s.Print(x, y, long, linkFG, bg)
		s.Underline(x, y, n, true)
	}
}

// statusY 是狀態列的位置。有底部保留區時要往上讓。
func (m *Model) statusY(s *cell.Screen) int {
	y := s.Rows - 1 - m.ReserveBottom
	if y < 1 {
		y = s.Rows - 1
	}
	return y
}

func (m *Model) drawStatus(s *cell.Screen) {
	t := m.Theme
	y := m.statusY(s)
	s.Fill(0, y, s.Cols, 1, ' ', t.StatusFG, t.StatusBG)
	if e := m.Current(); e != nil {
		line := e.Name
		if !e.IsDir {
			line = fmt.Sprintf("%s  %s", e.Name, comma(e.Size))
		}
		if !e.ModTime.IsZero() {
			line += "  " + e.ModTime.Format("2006-01-02 15:04")
		}
		if n := m.Notes[e.Name]; n != "" {
			line += "  ; " + n
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
