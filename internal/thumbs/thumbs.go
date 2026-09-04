// Package thumbs 是縮圖列表。
//
// 做法是把整個網格畫進**一張**合成圖,再交給 render.Overlay 一次貼上,
// 而不是每張縮圖各自疊一層。這樣格點層只要負責檔名與狀態列。
//
// 解碼是背景進行的:一個目錄裡幾十張 JPEG 解起來要好幾秒,
// 卡在那裡等會讓整個程式看起來當掉。已解好的先畫,還沒好的畫佔位框。
package thumbs

import (
	"fmt"
	"github.com/wicanr2/wincv-remake/internal/i18n"
	"image"
	"image/color"
	"sync"

	"github.com/wicanr2/wincv-remake/internal/cell"
	"github.com/wicanr2/wincv-remake/internal/imgfmt"
	"github.com/wicanr2/wincv-remake/internal/render"
)

// Item 是一張縮圖。
type Item struct {
	Name string
	Img  image.Image // 解好之前是 nil
	Err  error
	W, H int // 原圖尺寸
}

// Loader 取一個檔案的內容。縮圖列表不該知道檔案從哪來
// (真實目錄或壓縮檔內都可能)。
type Loader func(name string) ([]byte, error)

// Theme 是配色。
type Theme struct {
	BG                 cell.Color
	NameFG             cell.Color
	SelFG, SelBG       cell.Color
	StatusFG, StatusBG cell.Color
}

func DefaultTheme() Theme {
	return Theme{
		BG:     cell.Black,
		NameFG: cell.LtGray,
		SelFG:  cell.Black, SelBG: cell.LtGray,
		StatusFG: cell.Black, StatusBG: cell.LtGray,
	}
}

// Model 是縮圖列表的狀態。
type Model struct {
	Items  []Item
	Cursor int
	Top    int // 畫面最上面那一列是第幾列縮圖

	// CellCols / CellRows 是一格縮圖佔幾個字格。含底下的檔名那一列。
	CellCols, CellRows int
	Theme              Theme

	load   Loader
	mu     sync.Mutex
	canvas *image.RGBA
	done   map[int]bool
}

// New 建一個縮圖列表。names 應該只含圖檔。
func New(names []string, load Loader) *Model {
	m := &Model{
		CellCols: 14, CellRows: 8,
		Theme: DefaultTheme(),
		load:  load,
		done:  map[int]bool{},
	}
	for _, n := range names {
		m.Items = append(m.Items, Item{Name: n})
	}
	return m
}

// Decode 解一張縮圖。可以在別的 goroutine 呼叫。
func (m *Model) Decode(i int) {
	m.mu.Lock()
	if i < 0 || i >= len(m.Items) || m.done[i] {
		m.mu.Unlock()
		return
	}
	m.done[i] = true
	name := m.Items[i].Name
	m.mu.Unlock()

	data, err := m.load(name)
	var img image.Image
	if err == nil {
		img, _, err = imgfmt.Decode(name, data)
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	m.Items[i].Err = err
	if img != nil {
		b := img.Bounds()
		m.Items[i].W, m.Items[i].H = b.Dx(), b.Dy()
		m.Items[i].Img = img
	}
}

// DecodeVisible 解目前畫面上看得到的那些。呼叫端可以在背景做。
func (m *Model) DecodeVisible(cols, rows int) {
	perRow := m.perRow(cols)
	if perRow <= 0 {
		return
	}
	visRows := rows / m.CellRows
	first := m.Top * perRow
	last := first + visRows*perRow + perRow
	for i := first; i < last && i < len(m.Items); i++ {
		if i >= 0 {
			m.Decode(i)
		}
	}
}

func (m *Model) perRow(cols int) int { return cols / m.CellCols }

func (m *Model) clamp(cols, rows int) {
	if m.Cursor < 0 {
		m.Cursor = 0
	}
	if m.Cursor >= len(m.Items) {
		m.Cursor = len(m.Items) - 1
	}
	if m.Cursor < 0 {
		m.Cursor = 0
	}
	perRow := m.perRow(cols)
	if perRow <= 0 {
		return
	}
	visRows := rows / m.CellRows
	if visRows <= 0 {
		visRows = 1
	}
	row := m.Cursor / perRow
	if row < m.Top {
		m.Top = row
	}
	if row >= m.Top+visRows {
		m.Top = row - visRows + 1
	}
	if m.Top < 0 {
		m.Top = 0
	}
}

// MoveBy 依格數移動游標。dx 是左右,dy 是上下(一次一整列)。
func (m *Model) MoveBy(dx, dy, cols, rows int) {
	perRow := m.perRow(cols)
	if perRow <= 0 {
		perRow = 1
	}
	m.Cursor += dx + dy*perRow
	m.clamp(cols, rows)
}

// Current 回傳游標所在的那一張。
func (m *Model) Current() *Item {
	if m.Cursor < 0 || m.Cursor >= len(m.Items) {
		return nil
	}
	return &m.Items[m.Cursor]
}

// Draw 畫檔名與狀態列,回傳縮圖要疊在哪。
func (m *Model) Draw(s *cell.Screen, cellW, cellH int) *render.Overlay {
	t := m.Theme
	s.Clear(t.NameFG, t.BG)
	rows := s.Rows - 1
	if rows < 0 {
		rows = 0
	}
	m.clamp(s.Cols, rows)

	perRow := m.perRow(s.Cols)
	if perRow <= 0 {
		m.drawStatus(s)
		return nil
	}
	visRows := rows / m.CellRows

	// 合成圖:一格縮圖的圖片區是 CellRows-1 列高(最後一列放檔名)。
	imgW, imgH := s.Cols*cellW, rows*cellH
	if m.canvas == nil || m.canvas.Rect.Dx() != imgW || m.canvas.Rect.Dy() != imgH {
		m.canvas = image.NewRGBA(image.Rect(0, 0, imgW, imgH))
	}
	clear(m.canvas.Pix)

	m.mu.Lock()
	defer m.mu.Unlock()

	for r := 0; r < visRows; r++ {
		for c := 0; c < perRow; c++ {
			idx := (m.Top+r)*perRow + c
			if idx >= len(m.Items) {
				break
			}
			it := m.Items[idx]
			cx, cy := c*m.CellCols, r*m.CellRows
			// 圖片區
			px := image.Rect(cx*cellW+2, cy*cellH+2,
				(cx+m.CellCols)*cellW-2, (cy+m.CellRows-1)*cellH-2)
			if it.Img != nil {
				blitFit(m.canvas, it.Img, px)
			}
			// 檔名那一列
			ny := cy + m.CellRows - 1
			name := truncate(it.Name, m.CellCols)
			fg, bg := t.NameFG, t.BG
			if idx == m.Cursor {
				fg, bg = t.SelFG, t.SelBG
				s.Fill(cx, cy, m.CellCols, m.CellRows, ' ', fg, bg)
			}
			s.Fill(cx, ny, m.CellCols, 1, ' ', fg, bg)
			s.Print(cx, ny, name, fg, bg)
			if it.Img == nil && it.Err != nil {
				s.Print(cx+1, cy+1, i18n.T("解不開"), cell.LtRed, bg)
			}
		}
	}
	m.drawStatus(s)
	return &render.Overlay{Img: m.canvas, Rect: image.Rect(0, 0, imgW, imgH)}
}

func (m *Model) drawStatus(s *cell.Screen) {
	t := m.Theme
	y := s.Rows - 1
	s.Fill(0, y, s.Cols, 1, ' ', t.StatusFG, t.StatusBG)
	left := i18n.T("縮圖列表")
	if it := m.Current(); it != nil {
		left = it.Name
		if it.W > 0 {
			left += fmt.Sprintf("  %dx%d", it.W, it.H)
		}
	}
	s.Print(0, y, left, t.StatusFG, t.StatusBG)
	right := fmt.Sprintf("%d / %d", m.Cursor+1, len(m.Items))
	if x := s.Cols - len(right); x >= 0 {
		s.Print(x, y, right, t.StatusFG, t.StatusBG)
	}
}

// blitFit 把一張圖等比例縮放後置中畫進 dst 的 rect。
func blitFit(dst *image.RGBA, src image.Image, rect image.Rectangle) {
	sb := src.Bounds()
	sw, sh := sb.Dx(), sb.Dy()
	if sw <= 0 || sh <= 0 || rect.Dx() <= 0 || rect.Dy() <= 0 {
		return
	}
	num, den := rect.Dx(), sw
	if rect.Dy()*sw < rect.Dx()*sh {
		num, den = rect.Dy(), sh
	}
	ow, oh := sw*num/den, sh*num/den
	if ow <= 0 || oh <= 0 {
		return
	}
	ox := rect.Min.X + (rect.Dx()-ow)/2
	oy := rect.Min.Y + (rect.Dy()-oh)/2
	for y := 0; y < oh; y++ {
		dy := oy + y
		if dy < dst.Rect.Min.Y || dy >= dst.Rect.Max.Y {
			continue
		}
		syy := sb.Min.Y + y*sh/oh
		for x := 0; x < ow; x++ {
			dx := ox + x
			if dx < dst.Rect.Min.X || dx >= dst.Rect.Max.X {
				continue
			}
			sxx := sb.Min.X + x*sw/ow
			r, g, b, a := src.At(sxx, syy).RGBA()
			if a == 0 {
				continue
			}
			o := dst.PixOffset(dx, dy)
			dst.Pix[o+0] = uint8(r >> 8)
			dst.Pix[o+1] = uint8(g >> 8)
			dst.Pix[o+2] = uint8(b >> 8)
			dst.Pix[o+3] = 0xFF
		}
	}
}

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

var _ = color.RGBA{}
