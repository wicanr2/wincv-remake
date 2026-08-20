// Package imgview 是看圖模式。
//
// 圖片不走格點:硬塞進 8x15 的格子會失去解析度。做法是格點只負責
// 狀態列與訊息,圖片本身透過 render.Overlay 直接畫在像素層。
package imgview

import (
	"fmt"
	"image"

	"github.com/wicanr2/wincv-remake/internal/cell"
	"github.com/wicanr2/wincv-remake/internal/imgfmt"
	"github.com/wicanr2/wincv-remake/internal/render"
)

// Theme 是配色。
type Theme struct {
	BG                 cell.Color
	StatusFG, StatusBG cell.Color
	InfoFG, InfoBG     cell.Color
}

func DefaultTheme() Theme {
	return Theme{
		BG:       cell.Black,
		StatusFG: cell.Black, StatusBG: cell.LtGray,
		InfoFG: cell.LtCyan, InfoBG: cell.Blue,
	}
}

// Model 是看圖的狀態。
type Model struct {
	Name string
	Kind string
	Img  image.Image

	Fit  bool // true = 縮放到視窗;false = 1:1
	Pan  image.Point
	Info bool // 顯示檔案資訊(原版看圖時按 I)

	Size  int64 // 原始檔案大小,顯示在狀態列
	Theme Theme
}

// Load 解一張圖。
func Load(name string, data []byte) (*Model, error) {
	img, kind, err := imgfmt.Decode(name, data)
	if err != nil {
		return nil, err
	}
	return &Model{
		Name: name, Kind: kind, Img: img,
		Fit: true, Size: int64(len(data)), Theme: DefaultTheme(),
	}, nil
}

// PanBy 平移(只在 1:1 模式有意義)。
func (m *Model) PanBy(dx, dy int) {
	if m.Fit {
		return
	}
	m.Pan.X += dx
	m.Pan.Y += dy
	b := m.Img.Bounds()
	if m.Pan.X > b.Dx() {
		m.Pan.X = b.Dx()
	}
	if m.Pan.Y > b.Dy() {
		m.Pan.Y = b.Dy()
	}
	if m.Pan.X < -b.Dx() {
		m.Pan.X = -b.Dx()
	}
	if m.Pan.Y < -b.Dy() {
		m.Pan.Y = -b.Dy()
	}
}

// ToggleFit 在「縮放到視窗」與「1:1」之間切換。
func (m *Model) ToggleFit() {
	m.Fit = !m.Fit
	m.Pan = image.Point{}
}

// Draw 畫狀態列與資訊框,回傳圖片要疊在哪。
//
// cellW/cellH 是格子的像素尺寸 —— imgview 需要知道它才能把
// 「格點座標」換算成「像素矩形」。
func (m *Model) Draw(s *cell.Screen, cellW, cellH int) *render.Overlay {
	t := m.Theme
	s.Clear(cell.LtGray, t.BG)

	rows := s.Rows - 1
	if rows < 0 {
		rows = 0
	}
	m.drawStatus(s)
	if m.Info {
		m.drawInfo(s)
	}

	return &render.Overlay{
		Img:  m.Img,
		Rect: image.Rect(0, 0, s.Cols*cellW, rows*cellH),
		Fit:  m.Fit,
		Pan:  m.Pan,
	}
}

func (m *Model) drawStatus(s *cell.Screen) {
	t := m.Theme
	y := s.Rows - 1
	s.Fill(0, y, s.Cols, 1, ' ', t.StatusFG, t.StatusBG)
	b := m.Img.Bounds()
	left := fmt.Sprintf("%s  [%s]  %dx%d", m.Name, m.Kind, b.Dx(), b.Dy())
	s.Print(0, y, left, t.StatusFG, t.StatusBG)

	right := "1:1"
	if m.Fit {
		right = "縮放"
	}
	right = fmt.Sprintf("%s  %s", humanSize(m.Size), right)
	if x := s.Cols - width(right); x >= 0 {
		s.Print(x, y, right, t.StatusFG, t.StatusBG)
	}
}

// drawInfo 畫左上角的資訊框(原版看圖時按 I)。
func (m *Model) drawInfo(s *cell.Screen) {
	t := m.Theme
	b := m.Img.Bounds()
	lines := []string{
		"檔名: " + m.Name,
		fmt.Sprintf("格式: %s", m.Kind),
		fmt.Sprintf("尺寸: %d x %d", b.Dx(), b.Dy()),
		fmt.Sprintf("大小: %s", humanSize(m.Size)),
	}
	w := 0
	for _, l := range lines {
		if n := width(l); n > w {
			w = n
		}
	}
	w += 2
	if w > s.Cols {
		w = s.Cols
	}
	s.Fill(0, 0, w, len(lines)+2, ' ', t.InfoFG, t.InfoBG)
	for i, l := range lines {
		s.Print(1, i+1, l, t.InfoFG, t.InfoBG)
	}
}

func humanSize(n int64) string {
	switch {
	case n >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(n)/(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%.1f KB", float64(n)/(1<<10))
	}
	return fmt.Sprintf("%d bytes", n)
}

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
