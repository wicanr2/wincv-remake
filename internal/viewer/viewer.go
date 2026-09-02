// Package viewer 是文字檢視器 —— CView 的招牌功能。
//
// 要處理的事:編碼判讀(Big5/GB/SJIS/KOR/UTF)、ANSI 彩色控制碼(BBS 簽名檔)、
// 自動換行切換、行距、搜尋。
package viewer

import (
	"strconv"
	"strings"

	"github.com/wicanr2/wincv-remake/internal/cell"
	"github.com/wicanr2/wincv-remake/internal/textenc"
)

// Span 是一行裡屬性相同的一段文字。
type Span struct {
	Text string
	FG   cell.Color
	BG   cell.Color
}

// Line 是解析後的一行。
type Line struct {
	Spans []Span
}

// Text 回傳這一行的純文字(去掉屬性)。
func (l Line) Text() string {
	var sb strings.Builder
	for _, s := range l.Spans {
		sb.WriteString(s.Text)
	}
	return sb.String()
}

// Width 回傳這一行佔幾格(全形算兩格)。
func (l Line) Width() int {
	n := 0
	for _, s := range l.Spans {
		for _, r := range s.Text {
			if cell.IsWide(r) {
				n += 2
			} else {
				n++
			}
		}
	}
	return n
}

// Theme 是檢視器的顏色。
type Theme struct {
	FG, BG             cell.Color
	StatusFG, StatusBG cell.Color
	HitFG, HitBG       cell.Color // 搜尋命中
	BarFG, BarBG       cell.Color // 游標所在那一列(光棒)
}

func DefaultTheme() Theme {
	return Theme{
		FG: cell.LtGray, BG: cell.Black,
		StatusFG: cell.Black, StatusBG: cell.LtGray,
		HitFG: cell.Black, HitBG: cell.Yellow,
		// 與檔案清單的游標同一組顏色。整個 UI 只有一種「游標在這裡」的
		// 長相,換個畫面就換一種的話,使用者要重新學一次。
		BarFG: cell.White, BarBG: cell.Blue,
	}
}

// Model 是檢視器的狀態。
type Model struct {
	Name  string
	Enc   textenc.Enc
	Lines []Line

	Top  int // 畫面第一列對應的行號
	Left int // 不換行時的水平捲動量(格)
	// Cur 是游標停在第幾行。原版的工具列右邊就在報這個(「1 字 1 行/ 626」),
	// 只是它沒有把那一列畫出來。
	Cur int

	Wrap bool // 自動換行(原版 W)
	Ansi bool // 解讀 ANSI 色碼(原版 A)
	// Bar 決定要不要把游標那一列整列反白(光棒)。
	//
	// 預設開著:長檔案捲到一半時,「我剛才看到哪裡」是靠一條線記住的。
	// 可以關掉是因為 ANSI 彩色簽名檔的底色本來就有意義,蓋掉會失真。
	Bar bool

	Theme Theme

	// Hits 是搜尋結果的行號,用於「一次列出所有結果、上下鍵瀏覽」
	// (原版 0.51 版的 Ctrl-Shift-F)。
	Hits    []int
	HitIdx  int
	Pattern string
}

// Load 從位元組建立檢視內容。encHint 為 textenc.Unknown 時自動判讀。
func Load(name string, data []byte, encHint textenc.Enc) *Model {
	e := encHint
	if e == textenc.Unknown {
		e = textenc.Detect(data)
	}
	m := &Model{
		Name:  name,
		Enc:   e,
		Ansi:  true,
		Bar:   true,
		Theme: DefaultTheme(),
	}
	m.Lines = parse(textenc.Decode(data, e), m.Ansi, m.Theme)
	return m
}

// SetAnsi 切換是否解讀 ANSI 色碼,並重新解析。
func (m *Model) SetAnsi(on bool, raw string) {
	m.Ansi = on
	m.Lines = parse(raw, on, m.Theme)
}

// parse 把解碼後的文字切成行,順便處理 ANSI SGR。
//
// 換行同時吃 CRLF 與 LF —— 原版有「UNIX ↔ PC 文字檔轉換」功能,
// 表示兩種檔案都會遇到,顯示時不該因此差一個字元。
func parse(s string, ansi bool, th Theme) []Line {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "\n")
	raw := strings.Split(s, "\n")
	// 檔案以換行結尾時,Split 會多出一個空字串,那不是真的一行。
	if n := len(raw); n > 0 && raw[n-1] == "" {
		raw = raw[:n-1]
	}

	out := make([]Line, 0, len(raw))
	fg, bg := th.FG, th.BG
	for _, r := range raw {
		var line Line
		if !ansi {
			line.Spans = []Span{{Text: stripAnsi(r), FG: th.FG, BG: th.BG}}
			out = append(out, line)
			continue
		}
		var sb strings.Builder
		i := 0
		flush := func() {
			if sb.Len() > 0 {
				line.Spans = append(line.Spans, Span{Text: sb.String(), FG: fg, BG: bg})
				sb.Reset()
			}
		}
		for i < len(r) {
			if r[i] == 0x1B && i+1 < len(r) && r[i+1] == '[' {
				j := i + 2
				for j < len(r) && !isFinal(r[j]) {
					j++
				}
				if j < len(r) {
					if r[j] == 'm' {
						flush()
						fg, bg = applySGR(r[i+2:j], fg, bg, th)
					}
					i = j + 1
					continue
				}
			}
			sb.WriteByte(r[i])
			i++
		}
		flush()
		if len(line.Spans) == 0 {
			line.Spans = []Span{{Text: "", FG: fg, BG: bg}}
		}
		out = append(out, line)
	}
	return out
}

func isFinal(c byte) bool { return c >= 0x40 && c <= 0x7E }

func stripAnsi(s string) string {
	var sb strings.Builder
	i := 0
	for i < len(s) {
		if s[i] == 0x1B && i+1 < len(s) && s[i+1] == '[' {
			j := i + 2
			for j < len(s) && !isFinal(s[j]) {
				j++
			}
			i = j + 1
			continue
		}
		sb.WriteByte(s[i])
		i++
	}
	return sb.String()
}

// ansiDim / ansiBright 把 ANSI 的顏色編號(SGR 30-37)對到 WinCV 的
// 具名顏色。不能用算的:WinCV 的 29 色不是 ANSI 那種位元順序,
// 也不是 IBM PC 的屬性順序,它有自己的一套名字與排列。
var ansiDim = [8]cell.Color{
	cell.Black, cell.Red, cell.Green, cell.Yellow,
	cell.Blue, cell.Magenta, cell.Cyan, cell.LtGray,
}

var ansiBright = [8]cell.Color{
	cell.DkGray, cell.LtRed, cell.LtGreen, cell.LtYellow,
	cell.LtBlue, cell.LtMagenta, cell.LtCyan, cell.White,
}

func ansiToCell(n int, bright bool) cell.Color {
	if n < 0 || n > 7 {
		return cell.LtGray
	}
	if bright {
		return ansiBright[n]
	}
	return ansiDim[n]
}

// applySGR 套用一組 SGR 參數,回傳新的前景與背景。
//
// BBS 簽名檔的慣例是「1 代表亮色」而不是粗體,所以 1 要把前景加上
// 亮度位元 —— 用現代終端機的「粗體」語意去解會失去原味。
func applySGR(params string, fg, bg cell.Color, th Theme) (cell.Color, cell.Color) {
	if params == "" {
		params = "0"
	}
	base, bright := -1, false
	for _, p := range strings.Split(params, ";") {
		n, err := strconv.Atoi(p)
		if err != nil {
			continue
		}
		switch {
		case n == 0:
			base, bright, bg = -1, false, th.BG
			fg = th.FG
		case n == 1:
			bright = true
		case n == 22:
			bright = false
		case n >= 30 && n <= 37:
			base = n - 30
		case n == 39:
			base, bright = -1, false
			fg = th.FG
		case n >= 40 && n <= 47:
			bg = ansiToCell(n-40, false)
		case n == 49:
			bg = th.BG
		case n >= 90 && n <= 97:
			base, bright = n-90, true
		case n >= 100 && n <= 107:
			bg = ansiToCell(n-100, true)
		}
	}
	if base >= 0 {
		fg = ansiToCell(base, bright)
	} else if bright {
		// 只給了 ESC[1m,沒指定顏色:把目前的顏色提亮。
		for i, c := range ansiDim {
			if c == fg {
				fg = ansiBright[i]
				break
			}
		}
	}
	return fg, bg
}

// --- 捲動 -----------------------------------------------------------------

func (m *Model) clamp(rows int) {
	max := len(m.Lines) - 1
	if max < 0 {
		max = 0
	}
	if m.Top > max {
		m.Top = max
	}
	if m.Top < 0 {
		m.Top = 0
	}
	if m.Cur > max {
		m.Cur = max
	}
	if m.Cur < 0 {
		m.Cur = 0
	}
	if m.Left < 0 {
		m.Left = 0
	}
}

// reveal 把畫面捲到游標看得見的地方。
//
// 只在游標跑出畫面時才捲:游標還在畫面裡的時候硬把它拉到正中間,
// 會讓每按一次方向鍵整頁都在動,眼睛跟不上。
func (m *Model) reveal(rows int) {
	if rows < 1 {
		rows = 1
	}
	if m.Cur < m.Top {
		m.Top = m.Cur
	}
	if m.Cur > m.Top+rows-1 {
		m.Top = m.Cur - rows + 1
	}
	if m.Top < 0 {
		m.Top = 0
	}
}

// MoveBy 移動游標,需要的時候才捲動畫面。
func (m *Model) MoveBy(d, rows int) {
	m.Cur += d
	m.clamp(rows)
	m.reveal(rows)
}

// PageBy 翻頁:游標與畫面一起走,游標在畫面上的相對位置不變。
//
// 與「游標往下移一整頁」不同:那會讓游標從畫面頂端跑到底端,而畫面
// 只往下捲一列。翻頁翻的是畫面,游標只是跟著它走。
func (m *Model) PageBy(pages, rows int) {
	if rows < 1 {
		rows = 1
	}
	off := m.Cur - m.Top
	m.Top += pages * rows
	if m.Top < 0 {
		m.Top = 0
	}
	m.Cur = m.Top + off
	m.clamp(rows)
	m.reveal(rows)
}

// ScrollBy 捲動畫面,游標跟著留在畫面內。
//
// 捲動與移動游標是兩件事:滑鼠滾輪、捲軸是捲畫面,方向鍵是移游標。
// 兩者都要讓「游標在畫面裡」這件事成立,不然光棒會消失。
func (m *Model) ScrollBy(d, rows int) {
	m.Top += d
	m.clamp(rows)
	if rows < 1 {
		rows = 1
	}
	if m.Cur < m.Top {
		m.Cur = m.Top
	}
	if m.Cur > m.Top+rows-1 {
		m.Cur = m.Top + rows - 1
	}
	m.clamp(rows)
}

func (m *Model) Home(rows int) {
	m.Top, m.Left, m.Cur = 0, 0, 0
	m.clamp(rows)
}

func (m *Model) End(rows int) {
	m.Cur = len(m.Lines) - 1
	m.Top = len(m.Lines) - rows
	m.clamp(rows)
	m.reveal(rows)
}

// GoToLine 把游標放到某一行,並讓它出現在畫面上,盡量置中。
func (m *Model) GoToLine(n, rows int) {
	m.Cur = n
	m.Top = n - rows/2
	m.clamp(rows)
	m.reveal(rows)
}

// --- 搜尋 -----------------------------------------------------------------

// Search 找出所有含 pattern 的行,結果放進 Hits。
// 大小寫不分 —— 原版有 CAPS-SEARCH 一系列 word,行為是不分大小寫。
func (m *Model) Search(pattern string, rows int) int {
	m.Pattern, m.Hits, m.HitIdx = pattern, nil, 0
	if pattern == "" {
		return 0
	}
	low := strings.ToLower(pattern)
	for i, l := range m.Lines {
		if strings.Contains(strings.ToLower(l.Text()), low) {
			m.Hits = append(m.Hits, i)
		}
	}
	if len(m.Hits) > 0 {
		m.GoToLine(m.Hits[0], rows)
	}
	return len(m.Hits)
}

// NextHit / PrevHit 在搜尋結果之間移動(原版 Ctrl-N 續找)。
func (m *Model) NextHit(rows int) {
	if len(m.Hits) == 0 {
		return
	}
	m.HitIdx = (m.HitIdx + 1) % len(m.Hits)
	m.GoToLine(m.Hits[m.HitIdx], rows)
}

func (m *Model) PrevHit(rows int) {
	if len(m.Hits) == 0 {
		return
	}
	m.HitIdx = (m.HitIdx - 1 + len(m.Hits)) % len(m.Hits)
	m.GoToLine(m.Hits[m.HitIdx], rows)
}

// --- 繪製 -----------------------------------------------------------------

// Draw 把檢視器畫進 s,回傳內容區有幾列。
func (m *Model) Draw(s *cell.Screen) int {
	t := m.Theme
	s.Clear(t.FG, t.BG)
	rows := s.Rows - 1 // 最後一列是狀態列
	if rows < 0 {
		rows = 0
	}
	m.clamp(rows)

	y := 0
	for i := m.Top; i < len(m.Lines) && y < rows; i++ {
		used := m.drawLine(s, y, rows, m.Lines[i])
		// 光棒在文字之後補畫:先鋪底色會被下面那一層的空白蓋掉,
		// 而自動換行時一行佔幾列要畫完才知道。
		if m.Bar && i == m.Cur {
			m.drawBar(s, y, used, rows)
		}
		y += used
	}
	m.drawStatus(s)
	return rows
}

// drawBar 把游標那一行整列反白。自動換行時那一行可能佔好幾列,
// 每一列都要蓋 —— 只蓋第一列的話,長行的光棒看起來像斷掉。
func (m *Model) drawBar(s *cell.Screen, y, used, rows int) {
	for dy := 0; dy < used && y+dy < rows; dy++ {
		for x := 0; x < s.Cols; x++ {
			// 只換顏色,字與全形的左右半都留著 —— 重畫一次的話,
			// 全形字的兩格會拆開,而拆開之後畫面上看到的是半個字。
			if c := s.At(x, y+dy); c != nil {
				c.FG, c.BG = m.Theme.BarFG, m.Theme.BarBG
			}
		}
	}
}

// drawLine 畫一行,回傳它佔掉幾列(不換行時永遠是 1)。
func (m *Model) drawLine(s *cell.Screen, y, rows int, l Line) int {
	if !m.Wrap {
		x := -m.Left
		for _, sp := range l.Spans {
			for _, r := range sp.Text {
				w := 1
				if cell.IsWide(r) {
					w = 2
				}
				if x >= 0 && x+w <= s.Cols {
					s.Print(x, y, string(r), sp.FG, sp.BG)
				}
				x += w
				if x >= s.Cols {
					break
				}
			}
		}
		return 1
	}

	x, used := 0, 1
	for _, sp := range l.Spans {
		for _, r := range sp.Text {
			w := 1
			if cell.IsWide(r) {
				w = 2
			}
			if x+w > s.Cols {
				x = 0
				y++
				used++
				if y >= rows {
					return used
				}
			}
			s.Print(x, y, string(r), sp.FG, sp.BG)
			x += w
		}
	}
	return used
}

func (m *Model) drawStatus(s *cell.Screen) {
	t := m.Theme
	y := s.Rows - 1
	s.Fill(0, y, s.Cols, 1, ' ', t.StatusFG, t.StatusBG)

	pct := 100
	if n := len(m.Lines); n > 0 {
		pct = (m.Cur + 1) * 100 / n
	}
	left := m.Name + "  [" + m.Enc.String() + "]"
	if m.Wrap {
		left += "  自動換行"
	}
	if !m.Ansi {
		left += "  ANSI off"
	}
	s.Print(0, y, left, t.StatusFG, t.StatusBG)

	// 報游標在第幾行,不是畫面捲到第幾行 —— 原版的工具列也是報游標
	// (「1 字 1 行/ 626」)。
	right := strconv.Itoa(m.Cur+1) + "/" + strconv.Itoa(len(m.Lines)) + "  " +
		strconv.Itoa(pct) + "%"
	if len(m.Hits) > 0 {
		right = strconv.Itoa(m.HitIdx+1) + "/" + strconv.Itoa(len(m.Hits)) +
			" 命中  " + right
	}
	x := s.Cols - len(right)
	if x >= 0 {
		s.Print(x, y, right, t.StatusFG, t.StatusBG)
	}
}
