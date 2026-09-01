package pdf

import (
	"image"
	"image/color"
	"image/png"
	"os"
	"testing"
)

// inkRatio 是有墨的像素佔多少比例。
//
// 用它而不是逐像素比對:兩個渲染器對反鋸齒、字型微調的處理一定不同,
// 逐像素比會永遠是紅的。要驗的是「東西有沒有畫上去、畫在哪一區」。
func inkRatio(m image.Image) float64 {
	b := m.Bounds()
	ink := 0
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			r, g, bl, _ := m.At(x, y).RGBA()
			if r < 0xC000 || g < 0xC000 || bl < 0xC000 {
				ink++
			}
		}
	}
	return float64(ink) / float64(b.Dx()*b.Dy())
}

func renderPage(t *testing.T, path string, page int, opt RenderOptions) *Rendered {
	t.Helper()
	d, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	p, err := d.Page(page)
	if err != nil {
		t.Fatal(err)
	}
	r, err := p.Render(opt)
	if err != nil {
		t.Fatal(err)
	}
	if out := os.Getenv("WINCV_RENDER_OUT"); out != "" {
		f, err := os.Create(out)
		if err == nil {
			png.Encode(f, r.Img)
			f.Close()
		}
	}
	return r
}

func TestRenderSizeAndInk(t *testing.T) {
	r := renderPage(t, "testdata/rich.pdf", 1, RenderOptions{DPI: 96})
	b := r.Img.Bounds()
	// A4 在 96 dpi 下是 794×1123。允許一點進位誤差。
	if b.Dx() < 780 || b.Dx() > 800 || b.Dy() < 1110 || b.Dy() > 1130 {
		t.Errorf("尺寸是 %d×%d,不像 96 dpi 的 A4", b.Dx(), b.Dy())
	}
	ink := inkRatio(r.Img)
	// 一頁只有幾行字的文件,墨水覆蓋率大約百分之一到幾。
	// 太低表示什麼都沒畫,太高表示畫了一片黑。
	if ink < 0.002 {
		t.Errorf("幾乎沒有東西畫上去(墨水覆蓋率 %.4f)", ink)
	}
	if ink > 0.20 {
		t.Errorf("畫得太滿,可能整片被塗黑(墨水覆蓋率 %.4f)", ink)
	}
}

// 文字要落在該落的地方:這一頁的內容集中在上半部,下半部應該是空白。
func TestRenderInkIsWhereTextIs(t *testing.T) {
	r := renderPage(t, "testdata/rich.pdf", 1, RenderOptions{DPI: 96})
	b := r.Img.Bounds()
	half := b.Min.Y + b.Dy()/2
	top := inkRatio(r.Img.SubImage(image.Rect(b.Min.X, b.Min.Y, b.Max.X, half)))
	bottom := inkRatio(r.Img.SubImage(image.Rect(b.Min.X, half, b.Max.X, b.Max.Y)))
	if top <= bottom {
		t.Errorf("上半部的墨水(%.4f)沒有比下半部(%.4f)多", top, bottom)
	}
	if bottom > 0.01 {
		t.Errorf("下半部不該有這麼多東西(%.4f)", bottom)
	}
}

// 雙欄的頁面:左右兩欄都要有字,中間的欄距要是空的。
func TestRenderTwoColumnGutterIsBlank(t *testing.T) {
	r := renderPage(t, "testdata/twocol.pdf", 1, RenderOptions{DPI: 96})
	b := r.Img.Bounds()
	w := b.Dx()
	left := inkRatio(r.Img.SubImage(image.Rect(b.Min.X+w/10, b.Min.Y, b.Min.X+w*4/10, b.Max.Y)))
	gutter := inkRatio(r.Img.SubImage(image.Rect(b.Min.X+w*49/100, b.Min.Y, b.Min.X+w*51/100, b.Max.Y)))
	right := inkRatio(r.Img.SubImage(image.Rect(b.Min.X+w*6/10, b.Min.Y, b.Min.X+w*9/10, b.Max.Y)))
	if left < 0.02 || right < 0.02 {
		t.Errorf("兩欄的墨水太少(左 %.4f、右 %.4f)", left, right)
	}
	if gutter > left/4 {
		t.Errorf("欄距不夠空白(%.4f,兩側是 %.4f / %.4f)", gutter, left, right)
	}
}

// 解析度上限:要求一個離譜的 dpi 時自動降下來,不是把記憶體吃光。
func TestRenderRespectsPixelCap(t *testing.T) {
	r := renderPage(t, "testdata/rich.pdf", 1, RenderOptions{DPI: 4000, MaxPixels: 1 << 20})
	b := r.Img.Bounds()
	if b.Dx()*b.Dy() > 1<<20 {
		t.Errorf("超過像素上限:%d×%d", b.Dx(), b.Dy())
	}
	if r.DPI >= 4000 {
		t.Errorf("解析度沒有降下來:%.0f", r.DPI)
	}
}

// 白底:沒畫到的地方要是白的,不是透明或黑的。
func TestRenderBackgroundIsWhite(t *testing.T) {
	r := renderPage(t, "testdata/rich.pdf", 1, RenderOptions{DPI: 48})
	c := color.RGBAModel.Convert(r.Img.At(r.Img.Bounds().Dx()/2, r.Img.Bounds().Dy()-3)).(color.RGBA)
	if c.R != 255 || c.G != 255 || c.B != 255 || c.A != 255 {
		t.Errorf("底色是 %+v", c)
	}
}
