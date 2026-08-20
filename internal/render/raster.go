// Package render 把 cell.Screen 畫成像素。
//
// 這一檔是純 CPU 光柵器,不 import Ebiten:同一份程式碼可以在沒有顯示器的
// 環境下產生 PNG,驗收(與原版截圖做格點比對)因此不需要開視窗。
// Ebiten 的部分在 game.go,只負責把這裡產出的像素貼上去。
package render

import (
	"image"
	"image/color"

	"github.com/wicanr2/wincv-remake/internal/cell"
	"github.com/wicanr2/wincv-remake/internal/fnt"
)

// DefaultPalette 是 Windows 主控台的 16 色。
//
// WinCV 的配色可由使用者在主選單的「顏色」裡改,原版截圖中同時出現
// 0x80 與 0xC0 兩種強度,表示它另有自己的色表。在把 image 裡的色表
// 逆向出來之前,先用這組當預設。
var DefaultPalette = [16]color.RGBA{
	{0x00, 0x00, 0x00, 0xFF}, // 0  黑
	{0x00, 0x00, 0x80, 0xFF}, // 1  藍
	{0x00, 0x80, 0x00, 0xFF}, // 2  綠
	{0x00, 0x80, 0x80, 0xFF}, // 3  青
	{0x80, 0x00, 0x00, 0xFF}, // 4  紅
	{0x80, 0x00, 0x80, 0xFF}, // 5  洋紅
	{0x80, 0x80, 0x00, 0xFF}, // 6  棕
	{0xC0, 0xC0, 0xC0, 0xFF}, // 7  淺灰
	{0x80, 0x80, 0x80, 0xFF}, // 8  深灰
	{0x00, 0x00, 0xFF, 0xFF}, // 9  亮藍
	{0x00, 0xFF, 0x00, 0xFF}, // 10 亮綠
	{0x00, 0xFF, 0xFF, 0xFF}, // 11 亮青
	{0xFF, 0x00, 0x00, 0xFF}, // 12 亮紅
	{0xFF, 0x00, 0xFF, 0xFF}, // 13 亮洋紅
	{0xFF, 0xFF, 0x00, 0xFF}, // 14 黃
	{0xFF, 0xFF, 0xFF, 0xFF}, // 15 白
}

// CJKSource 提供全形字的點陣圖,寬度必須是半形的兩倍、高度相同。
//
// 原版的全形中文是 Windows GDI 用系統字型(image 裡指名「新細明體」)畫的,
// 不在隨附的 .FON 內。也就是說「原版的中文字形」本來就隨使用者的 Windows
// 而異,不是單一固定答案 —— 所以這裡做成可抽換的來源。
type CJKSource interface {
	Glyph(r rune) *fnt.Glyph
}

// Rasterizer 把 Screen 畫進一張 RGBA。
type Rasterizer struct {
	Half    *fnt.Font // 半形字型(cvga / cvga1018 / cvga1224)
	CJK     CJKSource // 全形字型來源,可為 nil
	Palette [16]color.RGBA

	CellW, CellH int
	buf          *image.RGBA
}

func New(half *fnt.Font, cjk CJKSource) *Rasterizer {
	return &Rasterizer{
		Half:    half,
		CJK:     cjk,
		Palette: DefaultPalette,
		CellW:   half.PixWidth,
		CellH:   half.PixHeight,
	}
}

// Size 回傳畫一個 cols x rows 的畫面需要多少像素。
func (r *Rasterizer) Size(cols, rows int) (int, int) {
	return cols * r.CellW, rows * r.CellH
}

// Draw 把 s 畫成一張 RGBA。回傳的緩衝區會被下一次呼叫重用。
func (r *Rasterizer) Draw(s *cell.Screen) *image.RGBA {
	w, h := r.Size(s.Cols, s.Rows)
	if r.buf == nil || r.buf.Rect.Dx() != w || r.buf.Rect.Dy() != h {
		r.buf = image.NewRGBA(image.Rect(0, 0, w, h))
	}
	// 兩趟:先把所有底色鋪完,再畫所有字模。
	//
	// 不能一格畫完再畫下一格 —— 全形字的字模橫跨兩格,
	// 右半格(Cont)接著鋪自己的底色時會把字的右半抹掉。
	for y := 0; y < s.Rows; y++ {
		for x := 0; x < s.Cols; x++ {
			r.fillBG(s, x, y)
		}
	}
	for y := 0; y < s.Rows; y++ {
		for x := 0; x < s.Cols; x++ {
			r.drawGlyph(s, x, y)
		}
	}
	return r.buf
}

func (r *Rasterizer) fillBG(s *cell.Screen, cx, cy int) {
	c := s.At(cx, cy)
	if c == nil {
		return
	}
	px, py := cx*r.CellW, cy*r.CellH
	bg := r.Palette[c.BG&0x0F]
	for y := 0; y < r.CellH; y++ {
		row := r.buf.PixOffset(px, py+y)
		for x := 0; x < r.CellW; x++ {
			o := row + x*4
			r.buf.Pix[o+0] = bg.R
			r.buf.Pix[o+1] = bg.G
			r.buf.Pix[o+2] = bg.B
			r.buf.Pix[o+3] = 0xFF
		}
	}
}

func (r *Rasterizer) drawGlyph(s *cell.Screen, cx, cy int) {
	c := s.At(cx, cy)
	if c == nil || c.Cont {
		return // 全形字在左半格一次畫完,右半格不再畫
	}
	var g *fnt.Glyph
	if c.Wide {
		if r.CJK == nil {
			return
		}
		g = r.CJK.Glyph(c.Ch)
	} else {
		b, ok := toCP950Byte(c.Ch)
		if !ok {
			return
		}
		g = r.Half.Glyph(b)
	}
	if g == nil {
		return
	}

	px, py := cx*r.CellW, cy*r.CellH
	fg := r.Palette[c.FG&0x0F]
	for y := 0; y < g.H && y < r.CellH; y++ {
		row := r.buf.PixOffset(px, py+y)
		for x := 0; x < g.W; x++ {
			if !g.At(x, y) {
				continue
			}
			o := row + x*4
			if o < 0 || o+3 >= len(r.buf.Pix) {
				continue
			}
			r.buf.Pix[o+0] = fg.R
			r.buf.Pix[o+1] = fg.G
			r.buf.Pix[o+2] = fg.B
			r.buf.Pix[o+3] = 0xFF
		}
	}
}

// toCP950Byte 把一個半形 rune 對回 .FON 的字碼。
//
// 半形字型是 0x00-0xFF 的單位元組字型,0x00-0x7F 就是 ASCII;
// 0x80-0xFF 在 Big5 環境下是雙位元組字的前導位元組,不會單獨出現在
// 一個半形格裡,所以只有 U+0000-U+00FF 之間直接對應的才給。
func toCP950Byte(r rune) (byte, bool) {
	if r >= 0 && r <= 0xFF {
		return byte(r), true
	}
	return 0, false
}
