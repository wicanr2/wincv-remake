// Package imgview 是看圖模式。
//
// 圖片不走格點:硬塞進 8x15 的格子會失去解析度。做法是格點只負責
// 狀態列與訊息,圖片本身透過 render.Overlay 直接畫在像素層。
package imgview

import (
	"fmt"
	"github.com/wicanr2/wincv-remake/internal/i18n"
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

	// Zoom 是使用者要的顯示倍率(1 = 原尺寸)。只在 Fit 為 false 時有意義。
	Zoom float64
	// base 是目前這張 Img 是用幾倍產生的。
	//
	// 兩個數字而不是一個:PDF 這種**能重畫**的來源放大時應該用更高的
	// 解析度重畫(那才看得到更多細節),而不是把點陣圖拉大;重畫完
	// Img 本身就已經是放大的,再乘一次就會放大兩倍。一般圖片沒有
	// 重畫這回事,base 永遠是 1。
	base float64
	// Rerender 讓「能重畫的來源」以指定倍率重新產生一張圖。
	//
	// nil 表示這張圖只能點陣放大(一般圖檔就是這樣)。回錯誤時退回
	// 點陣放大 —— 放大失敗不該讓畫面停在原地什麼都沒發生。
	Rerender func(zoom float64) (image.Image, error)

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
		Fit: true, Zoom: 1, base: 1, Size: int64(len(data)), Theme: DefaultTheme(),
	}, nil
}

// FromImage 用一張已經解好的圖建 Model。
//
// markdown 內嵌的圖已經在排版時解過了,再解一次只是浪費 ——
// 而且有些來源(壓縮檔成員)第二次未必拿得到同一份位元組。
func FromImage(name, kind string, img image.Image, size int64) *Model {
	return &Model{
		Name: name, Kind: kind, Img: img,
		Fit: true, Zoom: 1, base: 1, Size: size, Theme: DefaultTheme(),
	}
}

// zoomSteps 是放大倍率的階梯。
//
// 不用固定的乘數:整數倍(1/2/3/4)時每個來源像素放大成同樣大小的方塊,
// 點陣圖才不會有的一格有的兩格 —— 與內容放大倍率是同一件事(見 pickLevel)。
// 所以階梯上要有整數,而不是 1.25 的等比級數。
var zoomSteps = []float64{0.25, 0.33, 0.5, 0.67, 1, 1.5, 2, 3, 4, 6, 8}

// Scale 是這張圖實際要放大幾倍才畫成使用者要的大小。
//
// 能重畫的來源重畫過之後,Img 本身已經是放大的,這裡就會回 1。
func (m *Model) Scale() float64 {
	if m.Zoom <= 0 || m.base <= 0 {
		return 1
	}
	return m.Zoom / m.base
}

// ZoomBy 沿著階梯移動 d 格(正數放大)。回傳倍率有沒有變。
//
// 放大會離開「縮放到視窗」——「縮放到視窗」的意思就是「不要我自己決定
// 大小」,兩者不能同時成立。
func (m *Model) ZoomBy(d int) bool {
	if m.Zoom <= 0 {
		m.Zoom = 1
	}
	i := 0
	for j, z := range zoomSteps {
		if z <= m.Zoom+1e-9 {
			i = j
		}
	}
	i += d
	if i < 0 {
		i = 0
	}
	if i >= len(zoomSteps) {
		i = len(zoomSteps) - 1
	}
	return m.SetZoom(zoomSteps[i])
}

// SetZoom 直接指定倍率。
func (m *Model) SetZoom(z float64) bool {
	if z <= 0 {
		z = 1
	}
	if !m.Fit && z == m.Zoom {
		return false
	}
	old := m.Scale()
	m.Fit, m.Zoom = false, z
	if m.base <= 0 {
		m.base = 1
	}
	// 能重畫的來源(PDF 頁面)重畫一張。點陣放大只是把同樣的像素攤開,
	// 看不到更多東西;PDF 是向量的,用更高的解析度重畫才有意義。
	if m.Rerender != nil {
		if img, err := m.Rerender(z); err == nil && img != nil {
			m.Img, m.base = img, z
		}
	}
	// 平移量跟著倍率走,不然放大之後看的會是別的地方。
	if s := m.Scale(); old > 0 && s > 0 && s != old {
		m.Pan.X = int(float64(m.Pan.X) * old / s)
		m.Pan.Y = int(float64(m.Pan.Y) * old / s)
	}
	m.clampPan()
	return true
}

// PanBy 平移(只在 1:1 模式有意義)。
func (m *Model) PanBy(dx, dy int) {
	if m.Fit {
		return
	}
	m.Pan.X += dx
	m.Pan.Y += dy
	m.clampPan()
}

func (m *Model) clampPan() {
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

// ToggleFit 在「縮放到視窗」與「使用者指定的倍率」之間切換。
func (m *Model) ToggleFit() {
	m.Fit = !m.Fit
	m.Pan = image.Point{}
	if !m.Fit && m.Zoom <= 0 {
		m.Zoom = 1
	}
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
		Zoom: m.Scale(),
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
		right = i18n.T("縮放")
	} else if z := m.Zoom; z > 0 && z != 1 {
		right = fmt.Sprintf("%g×", z)
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
		i18n.T("檔名: ") + m.Name,
		fmt.Sprintf(i18n.T("格式: %s"), m.Kind),
		fmt.Sprintf(i18n.T("尺寸: %d x %d"), b.Dx(), b.Dy()),
		fmt.Sprintf(i18n.T("大小: %s"), humanSize(m.Size)),
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
