package editor

import (
	"fmt"

	"github.com/wicanr2/wincv-remake/internal/cell"
	"github.com/wicanr2/wincv-remake/internal/textenc"
	"golang.org/x/text/encoding/japanese"
	"golang.org/x/text/encoding/korean"
	"golang.org/x/text/encoding/simplifiedchinese"
	"golang.org/x/text/encoding/traditionalchinese"
)

func encode(s string, e textenc.Enc) ([]byte, error) {
	switch e {
	case textenc.Big5:
		return traditionalchinese.Big5.NewEncoder().Bytes([]byte(s))
	case textenc.GBK:
		return simplifiedchinese.GBK.NewEncoder().Bytes([]byte(s))
	case textenc.ShiftJIS:
		return japanese.ShiftJIS.NewEncoder().Bytes([]byte(s))
	case textenc.EUCKR:
		return korean.EUCKR.NewEncoder().Bytes([]byte(s))
	}
	return []byte(s), nil
}

// dispWidth 回傳一個 rune 佔幾格。Tab 依 TabWidth 對齊。
func (m *Model) dispWidth(r rune, atCol int) int {
	if r == '\t' {
		w := m.TabWidth - atCol%m.TabWidth
		if w == 0 {
			w = m.TabWidth
		}
		return w
	}
	if cell.IsWide(r) {
		return 2
	}
	return 1
}

// ColToScreen 把 rune 索引換成顯示格位置。Tab 與全形字都要算進去。
func (m *Model) ColToScreen(line []rune, col int) int {
	x := 0
	for i := 0; i < col && i < len(line); i++ {
		x += m.dispWidth(line[i], x)
	}
	return x
}

// ScreenToCol 是 ColToScreen 的反向:畫面上第 x 格(已加回 Left)落在
// 第幾個 rune 上。全形字佔兩格,點到後半格也算那個字;超出行尾就是行尾。
func (m *Model) ScreenToCol(line []rune, x int) int {
	sx := 0
	for i, r := range line {
		w := m.dispWidth(r, sx)
		if x < sx+w {
			return i
		}
		sx += w
	}
	return len(line)
}

// commentStateAt 回傳第 ln 行**開始**時是不是在跨行註解裡。
//
// 跨行註解的狀態要從檔頭累積,所以整份算一次並快取起來;
// 內容改動時才重算。不快取的話,每次重畫都要從第 0 行掃到畫面頂端。
func (m *Model) commentStateAt(ln int) bool {
	if m.Syntax == nil || m.Syntax.BlockStart == "" {
		return false
	}
	if m.cmtDirty || len(m.cmtState) != len(m.Lines)+1 {
		m.cmtState = make([]bool, len(m.Lines)+1)
		in := false
		for i, l := range m.Lines {
			m.cmtState[i] = in
			_, in = m.Syntax.HighlightState(string(l), in)
		}
		m.cmtState[len(m.Lines)] = in
		m.cmtDirty = false
	}
	if ln < 0 || ln >= len(m.cmtState) {
		return false
	}
	return m.cmtState[ln]
}

// Draw 把編輯器畫進 s,回傳內容區有幾列。
func (m *Model) Draw(s *cell.Screen) int {
	t := m.Theme
	s.Clear(t.FG, t.BG)
	rows := s.Rows - 1
	if rows < 0 {
		rows = 0
	}
	m.scrollIntoView(s.Cols, rows)

	for y := 0; y < rows; y++ {
		ln := m.Top + y
		if ln >= len(m.Lines) {
			break
		}
		m.drawLine(s, y, ln)
	}
	m.drawCursor(s, rows)
	m.drawStatus(s)
	return rows
}

// scrollIntoView 讓游標留在畫面內。
func (m *Model) scrollIntoView(cols, rows int) {
	if rows <= 0 {
		return
	}
	if m.Cur.Line < m.Top {
		m.Top = m.Cur.Line
	}
	if m.Cur.Line >= m.Top+rows {
		m.Top = m.Cur.Line - rows + 1
	}
	if m.Top < 0 {
		m.Top = 0
	}
	x := m.ColToScreen(m.Lines[m.Cur.Line], m.Cur.Col)
	if x < m.Left {
		m.Left = x
	}
	if x >= m.Left+cols {
		m.Left = x - cols + 1
	}
	if m.Left < 0 {
		m.Left = 0
	}
}

func (m *Model) drawLine(s *cell.Screen, y, ln int) {
	t := m.Theme
	line := m.Lines[ln]

	// 先算好每個 rune 的顏色,再一次畫出去。
	colors := make([]cell.Color, len(line))
	colored := make([]bool, len(line))
	if m.Syntax != nil {
		toks, _ := m.Syntax.HighlightState(string(line), m.commentStateAt(ln))
		for _, tok := range toks {
			for i := tok.Start; i < tok.End && i < len(line); i++ {
				colors[i], colored[i] = tok.Color, tok.Colored
			}
		}
	}

	x := 0
	for i, r := range line {
		w := m.dispWidth(r, x)
		sx := x - m.Left
		fg, bg := t.FG, t.BG
		if colored[i] {
			fg = colors[i]
		}
		if m.inBlock(ln, i) {
			fg, bg = t.BlockFG, t.BlockBG
		}
		if r == '\t' {
			for k := 0; k < w; k++ {
				if sx+k >= 0 && sx+k < s.Cols {
					s.Set(sx+k, y, ' ', fg, bg)
				}
			}
		} else if sx >= 0 && sx < s.Cols {
			s.Print(sx, y, string(r), fg, bg)
		}
		x += w
	}
	// 整列區塊要把行尾之後也反白,才看得出整列被選起來。
	if m.Block.Kind == BlockLine && m.inBlockLine(ln) {
		for sx := x - m.Left; sx < s.Cols; sx++ {
			if sx >= 0 {
				s.Set(sx, y, ' ', t.BlockFG, t.BlockBG)
			}
		}
	}
}

func (m *Model) inBlockLine(ln int) bool {
	if m.Block.Kind == BlockNone {
		return false
	}
	top, bot, _, _ := m.Block.Norm()
	return ln >= top && ln <= bot
}

func (m *Model) inBlock(ln, col int) bool {
	if !m.inBlockLine(ln) {
		return false
	}
	if m.Block.Kind == BlockLine {
		return true
	}
	_, _, left, right := m.Block.Norm()
	return col >= left && col < right
}

// drawCursor 把游標那一格反白。
func (m *Model) drawCursor(s *cell.Screen, rows int) {
	y := m.Cur.Line - m.Top
	if y < 0 || y >= rows {
		return
	}
	x := m.ColToScreen(m.Lines[m.Cur.Line], m.Cur.Col) - m.Left
	if x < 0 || x >= s.Cols {
		return
	}
	c := s.At(x, y)
	if c == nil {
		return
	}
	c.FG, c.BG = c.BG, c.FG
}

func (m *Model) drawStatus(s *cell.Screen) {
	t := m.Theme
	y := s.Rows - 1
	s.Fill(0, y, s.Cols, 1, ' ', t.StatusFG, t.StatusBG)

	mark := ""
	switch m.Block.Kind {
	case BlockRect:
		mark = "  區塊"
	case BlockLine:
		mark = "  整列"
	}
	mode := "插入"
	if !m.Insert {
		mode = "覆蓋"
	}
	dirty := ""
	if m.Dirty {
		dirty = " *"
	}
	left := fmt.Sprintf("%s%s  [%s]  %s%s", m.Name, dirty, m.Enc, mode, mark)
	s.Print(0, y, left, t.StatusFG, t.StatusBG)

	right := fmt.Sprintf("%d 行 %d 欄 / %d", m.Cur.Line+1, m.Cur.Col+1, len(m.Lines))
	if x := s.Cols - len(right); x >= 0 {
		s.Print(x, y, right, t.StatusFG, t.StatusBG)
	}
}
