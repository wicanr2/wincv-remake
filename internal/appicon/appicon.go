// Package appicon 畫 WinCV Remake 的應用程式圖示。
//
// 圖示是重製版自己畫的,不取用原版的 WinCV.ico(原作者的著作)。
// 畫面元素取自 WinCV 自己的介面:藍底黃字的標題、紅色的游標列、
// 黑底的檔案清單與藍色狀態列,顏色用 internal/render 的 DefaultPalette
// 同一組值。
//
// 用像素畫而不是向量圖:這個專案存在的理由之一是與原版逐像素對齊,
// 圖示也沿用點陣的樣子。16 與 32 各有一張原生設計,較大的尺寸是
// 32 那張整數倍放大,較小的尺寸不縮放(縮放會把一像素寬的線糊掉)。
//
// 執行時的視窗圖示(ebiten.SetWindowIcon)與打包用的圖檔
// (tools/mkicon 產生 .ico、Android 的 mipmap)都從這裡來,只有一份設計。
package appicon

import (
	"image"
	"image/color"
)

// 顏色與 internal/render.DefaultPalette 相同(不引用它,免得這個小套件
// 依賴整個光柵器)。
var (
	clear  = color.RGBA{}
	frame  = color.RGBA{0x40, 0x40, 0x40, 0xFF} // dkgray
	navy   = color.RGBA{0x00, 0x00, 0x80, 0xFF} // blue
	black  = color.RGBA{0x00, 0x00, 0x00, 0xFF}
	yellow = color.RGBA{0xFF, 0xFF, 0x00, 0xFF} // ltyellow
	red    = color.RGBA{0x80, 0x00, 0x00, 0xFF} // red(游標列)
	white  = color.RGBA{0xFF, 0xFF, 0xFF, 0xFF}
	gray   = color.RGBA{0xC0, 0xC0, 0xC0, 0xFF} // ltgray
	cyan   = color.RGBA{0x00, 0xFF, 0xFF, 0xFF} // ltcyan
	green  = color.RGBA{0x00, 0xFF, 0x00, 0xFF} // ltgreen
)

// 5×7 的「C」與「V」。
var glyphC = []string{
	".###.",
	"#...#",
	"#....",
	"#....",
	"#....",
	"#...#",
	".###.",
}

var glyphV = []string{
	"#...#",
	"#...#",
	"#...#",
	"#...#",
	".#.#.",
	".#.#.",
	"..#..",
}

type canvas struct {
	img *image.RGBA
}

func newCanvas(n int) *canvas {
	return &canvas{image.NewRGBA(image.Rect(0, 0, n, n))}
}

func (c *canvas) rect(x0, y0, x1, y1 int, col color.RGBA) {
	for y := y0; y <= y1; y++ {
		for x := x0; x <= x1; x++ {
			c.img.SetRGBA(x, y, col)
		}
	}
}

func (c *canvas) glyph(g []string, x0, y0, scale int, col color.RGBA) {
	for y, row := range g {
		for x, ch := range row {
			if ch == '#' {
				c.rect(x0+x*scale, y0+y*scale, x0+(x+1)*scale-1, y0+(y+1)*scale-1, col)
			}
		}
	}
}

// corners 把外框的四個角削圓。r 是削掉的像素數。
func (c *canvas) corners(r int) {
	n := c.img.Bounds().Dx()
	for i := 0; i < r; i++ {
		for j := 0; j < r-i; j++ {
			c.img.SetRGBA(i, j, clear)
			c.img.SetRGBA(n-1-i, j, clear)
			c.img.SetRGBA(i, n-1-j, clear)
			c.img.SetRGBA(n-1-i, n-1-j, clear)
		}
	}
}

// draw16 是 16×16 的原生設計。
func draw16() *image.RGBA {
	c := newCanvas(16)
	c.rect(0, 0, 15, 15, frame)
	c.rect(1, 1, 14, 8, navy)
	c.glyph(glyphC, 2, 1, 1, yellow)
	c.glyph(glyphV, 9, 1, 1, yellow)
	c.rect(1, 9, 14, 14, black)
	c.rect(1, 10, 14, 11, red)
	c.rect(3, 10, 6, 10, white)
	c.rect(2, 13, 5, 13, cyan)
	c.rect(8, 13, 13, 13, gray)
	c.corners(2)
	return c.img
}

// draw32 是 32×32 的原生設計。
func draw32() *image.RGBA {
	c := newCanvas(32)
	c.rect(0, 0, 31, 31, frame)
	// 上半:藍底黃字。
	c.rect(1, 1, 30, 17, navy)
	c.glyph(glyphC, 4, 2, 2, yellow)
	c.glyph(glyphV, 17, 2, 2, yellow)
	// 下半:黑底的檔案清單,第一列是紅色游標列。
	c.rect(1, 18, 30, 30, black)
	c.rect(1, 19, 30, 21, red)
	c.rect(3, 20, 10, 20, white)
	c.rect(13, 20, 17, 20, white)
	c.rect(20, 20, 28, 20, white)
	for i, y := range []int{23, 25} {
		c.rect(3, y, 9-i*2, y, cyan)
		c.rect(13, y, 17, y, gray)
		c.rect(20, y, 28-i*3, y, green)
	}
	// 狀態列。
	c.rect(1, 27, 30, 30, navy)
	c.rect(3, 28, 12, 28, yellow)
	c.rect(15, 28, 22, 28, white)
	c.corners(3)
	return c.img
}

// Image 回傳 n×n 的圖示。16 與 32 是原生設計;其他尺寸用整數倍放大,
// 32 那張放得下兩倍以上(或剛好整除)就用它,否則用 16 那張(48 = 16×3)。
// 放不滿的部分置中留透明。
func Image(n int) image.Image {
	switch {
	case n <= 16:
		return draw16()
	case n%32 == 0 || n >= 64:
		return place(draw32(), n)
	default:
		return place(draw16(), n)
	}
}

func place(src *image.RGBA, n int) *image.RGBA {
	s := src.Bounds().Dx()
	k := n / s
	off := (n - s*k) / 2
	dst := image.NewRGBA(image.Rect(0, 0, n, n))
	for y := 0; y < s*k; y++ {
		for x := 0; x < s*k; x++ {
			dst.SetRGBA(off+x, off+y, src.RGBAAt(x/k, y/k))
		}
	}
	return dst
}

// Sizes 是視窗與 .ico 要提供的尺寸。
var Sizes = []int{16, 32, 48, 64, 128, 256}

// All 依 Sizes 回傳每一個尺寸的圖示。
func All() []image.Image {
	out := make([]image.Image, len(Sizes))
	for i, n := range Sizes {
		out[i] = Image(n)
	}
	return out
}
