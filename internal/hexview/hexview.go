// Package hexview 是 16 進位檢視。
//
// 原版每列 16 bytes,左邊位移、中間十六進位、右邊字元。
// 字元欄要用 Big5 判讀 —— 這是中文工具,右邊那一欄看得懂中文才有用。
package hexview

import (
	"fmt"

	"github.com/wicanr2/wincv-remake/internal/cell"
	"github.com/wicanr2/wincv-remake/internal/textenc"
)

// BytesPerLine 是每列幾個位元組。原版是 16。
const BytesPerLine = 16

// Theme 是配色。
type Theme struct {
	OffsetFG           cell.Color
	HexFG, HexAltFG    cell.Color // 交替兩色,方便數位置
	AsciiFG, WideFG    cell.Color
	BG                 cell.Color
	StatusFG, StatusBG cell.Color
}

func DefaultTheme() Theme {
	return Theme{
		OffsetFG: cell.BrightCyan,
		HexFG:    cell.LightGray, HexAltFG: cell.White,
		AsciiFG: cell.BrightGreen, WideFG: cell.Yellow,
		BG:       cell.Black,
		StatusFG: cell.Black, StatusBG: cell.LightGray,
	}
}

// Model 是 HEX 檢視的狀態。
type Model struct {
	Name  string
	Data  []byte
	Top   int // 畫面第一列對應的列號
	Theme Theme

	// Big5 為 true 時,右邊字元欄把雙位元組組成中文字。
	Big5 bool
}

func Load(name string, data []byte) *Model {
	return &Model{Name: name, Data: data, Theme: DefaultTheme(), Big5: true}
}

// Lines 回傳總共有幾列。
func (m *Model) Lines() int {
	return (len(m.Data) + BytesPerLine - 1) / BytesPerLine
}

func (m *Model) clamp(rows int) {
	max := m.Lines() - 1
	if max < 0 {
		max = 0
	}
	if m.Top > max {
		m.Top = max
	}
	if m.Top < 0 {
		m.Top = 0
	}
}

func (m *Model) ScrollBy(d, rows int) { m.Top += d; m.clamp(rows) }
func (m *Model) Home(rows int)        { m.Top = 0; m.clamp(rows) }
func (m *Model) End(rows int)         { m.Top = m.Lines() - rows; m.clamp(rows) }

// Draw 畫出來,回傳內容區有幾列。
func (m *Model) Draw(s *cell.Screen) int {
	t := m.Theme
	s.Clear(t.HexFG, t.BG)
	rows := s.Rows - 1
	if rows < 0 {
		rows = 0
	}
	m.clamp(rows)

	for y := 0; y < rows; y++ {
		ln := m.Top + y
		if ln >= m.Lines() {
			break
		}
		m.drawLine(s, y, ln)
	}
	m.drawStatus(s)
	return rows
}

func (m *Model) drawLine(s *cell.Screen, y, ln int) {
	t := m.Theme
	off := ln * BytesPerLine
	end := off + BytesPerLine
	if end > len(m.Data) {
		end = len(m.Data)
	}
	row := m.Data[off:end]

	x := s.Print(0, y, fmt.Sprintf("%08X", off), t.OffsetFG, t.BG)
	x += 2

	for i := 0; i < BytesPerLine; i++ {
		fg := t.HexFG
		if i%2 == 1 {
			fg = t.HexAltFG
		}
		txt := "  "
		if i < len(row) {
			txt = fmt.Sprintf("%02X", row[i])
		}
		s.Print(x, y, txt, fg, t.BG)
		x += 2
		x++ // 位元組之間空一格
		if i == 7 {
			x++ // 中間再空一格,一眼看出前後半
		}
	}

	m.drawChars(s, x, y, row)
}

// drawChars 畫右邊的字元欄。
//
// Big5 模式下,合法的雙位元組要合成一個全形字並佔兩格 —— 這樣
// 字元欄的每一格才跟左邊的十六進位一一對應,不會錯位。
func (m *Model) drawChars(s *cell.Screen, x, y int, row []byte) {
	t := m.Theme
	i := 0
	for i < len(row) {
		if m.Big5 && i+1 < len(row) && isBig5Lead(row[i]) && isBig5Trail(row[i+1]) {
			str := textenc.Decode(row[i:i+2], textenc.Big5)
			r := []rune(str)
			if len(r) == 1 && cell.IsWide(r[0]) {
				s.Print(x+i, y, string(r[0]), t.WideFG, t.BG)
				i += 2
				continue
			}
		}
		c := row[i]
		ch := '.'
		if c >= 0x20 && c < 0x7F {
			ch = rune(c)
		}
		s.Set(x+i, y, ch, t.AsciiFG, t.BG)
		i++
	}
}

func isBig5Lead(b byte) bool  { return b >= 0xA1 && b <= 0xF9 }
func isBig5Trail(b byte) bool { return (b >= 0x40 && b <= 0x7E) || (b >= 0xA1 && b <= 0xFE) }

func (m *Model) drawStatus(s *cell.Screen) {
	t := m.Theme
	y := s.Rows - 1
	s.Fill(0, y, s.Cols, 1, ' ', t.StatusFG, t.StatusBG)
	left := fmt.Sprintf("%s  [HEX]  %d bytes", m.Name, len(m.Data))
	if m.Big5 {
		left += "  Big5"
	}
	s.Print(0, y, left, t.StatusFG, t.StatusBG)

	pct := 100
	if n := m.Lines(); n > 0 {
		pct = (m.Top + 1) * 100 / n
	}
	right := fmt.Sprintf("%08X  %d%%", m.Top*BytesPerLine, pct)
	if x := s.Cols - len(right); x >= 0 {
		s.Print(x, y, right, t.StatusFG, t.StatusBG)
	}
}
