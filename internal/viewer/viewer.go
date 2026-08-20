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
}

func DefaultTheme() Theme {
	return Theme{
		FG: cell.LightGray, BG: cell.Black,
		StatusFG: cell.Black, StatusBG: cell.LightGray,
		HitFG: cell.Black, HitBG: cell.Yellow,
	}
}

// Model 是檢視器的狀態。
type Model struct {
	Name  string
	Enc   textenc.Enc
	Lines []Line

	Top  int // 畫面第一列對應的行號
	Left int // 不換行時的水平捲動量(格)

	Wrap bool // 自動換行(原版 W)
	Ansi bool // 解讀 ANSI 色碼(原版 A)

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

// ansiToCell 把 ANSI 的顏色編號換成 cell 的調色盤索引。
//
// 兩邊的位元順序不同,不能直接相減:
//   ANSI(SGR 30-37)  值 = R*1 + G*2 + B*4   → 1 是紅、4 是藍
//   IBM PC 文字屬性   值 = R*4 + G*2 + B*1   → 4 是紅、1 是藍
// 也就是紅與藍的位元對調。cell.Color 用的是後者(DOS 時代程式的色表就是這個順序)。
func ansiToCell(n int) cell.Color {
	return cell.Color(((n & 1) << 2) | (n & 2) | ((n & 4) >> 2))
}

// applySGR 套用一組 SGR 參數,回傳新的前景與背景。
//
// BBS 簽名檔的慣例是「1 代表亮色」而不是粗體,所以 1 要把前景加上
// 亮度位元 —— 用現代終端機的「粗體」語意去解會失去原味。
func applySGR(params string, fg, bg cell.Color, th Theme) (cell.Color, cell.Color) {
	if params == "" {
		params = "0"
	}
	base := fg & 7
	bright := fg&8 != 0
	for _, p := range strings.Split(params, ";") {
		n, err := strconv.Atoi(p)
		if err != nil {
			continue
		}
		switch {
		case n == 0:
			base, bright, bg = th.FG&7, th.FG&8 != 0, th.BG
		case n == 1:
			bright = true
		case n == 22:
			bright = false
		case n >= 30 && n <= 37:
			base = ansiToCell(n - 30)
		case n == 39:
			base, bright = th.FG&7, th.FG&8 != 0
		case n >= 40 && n <= 47:
			bg = ansiToCell(n - 40)
		case n == 49:
			bg = th.BG
		case n >= 90 && n <= 97:
			base, bright = ansiToCell(n-90), true
		case n >= 100 && n <= 107:
			bg = ansiToCell(n-100) | 8
		}
	}
	fg = base
	if bright {
		fg |= 8
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
	if m.Left < 0 {
		m.Left = 0
	}
}

func (m *Model) ScrollBy(d, rows int) { m.Top += d; m.clamp(rows) }
func (m *Model) Home(rows int)        { m.Top = 0; m.Left = 0; m.clamp(rows) }

func (m *Model) End(rows int) {
	m.Top = len(m.Lines) - rows
	m.clamp(rows)
}

// GoToLine 讓某一行出現在畫面上,盡量置中。
func (m *Model) GoToLine(n, rows int) {
	m.Top = n - rows/2
	m.clamp(rows)
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
		y += m.drawLine(s, y, rows, m.Lines[i])
	}
	m.drawStatus(s)
	return rows
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
		pct = (m.Top + 1) * 100 / n
	}
	left := m.Name + "  [" + m.Enc.String() + "]"
	if m.Wrap {
		left += "  自動換行"
	}
	if !m.Ansi {
		left += "  ANSI off"
	}
	s.Print(0, y, left, t.StatusFG, t.StatusBG)

	right := strconv.Itoa(m.Top+1) + "/" + strconv.Itoa(len(m.Lines)) + "  " +
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
