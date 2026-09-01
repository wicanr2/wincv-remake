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

// 這一份的字型全部嵌在檔案裡(拉丁文是 TrueType、中文是 Type1),
// 所以不該有任何一個字需要用系統字型補畫 —— 補畫代表某一種嵌入格式
// 解不開,而畫面上看起來只是字形換了一套。
func TestRenderUsesEmbeddedFonts(t *testing.T) {
	r := renderPage(t, "testdata/rich.pdf", 1, RenderOptions{DPI: 96})
	if len(r.Substituted) > 0 {
		t.Errorf("這些字型沒有用檔案裡嵌的那一份:%v", r.Substituted)
	}
	if len(r.Missing) > 0 {
		t.Errorf("這些字型的字沒有畫上去:%v", r.Missing)
	}
}

// pixelAt 取畫面上某個 PDF 座標對應的顏色。dpi 換算與 Y 軸翻轉都在這裡做,
// 測試裡就只要寫文件裡的座標。
func pixelAt(t *testing.T, r *Rendered, pdfX, pdfY, pageH float64) color.RGBA {
	t.Helper()
	s := r.DPI / 72
	x := int(pdfX * s)
	y := int((pageH - pdfY) * s)
	b := r.Img.Bounds()
	if x < b.Min.X || x >= b.Max.X || y < b.Min.Y || y >= b.Max.Y {
		t.Fatalf("座標 (%.0f, %.0f) 落在畫面外", pdfX, pdfY)
	}
	return color.RGBAModel.Convert(r.Img.At(x, y)).(color.RGBA)
}

// 奇偶填法:同方向的兩個同心方框,f* 中間要是洞,f 要是實心。
//
// testdata/evenodd.pdf 是手寫的最小檔案(未壓縮,打開就看得到內容),
// 對照組是 LibreOffice 對同一份檔案的算繪結果。
func TestRenderEvenOddFill(t *testing.T) {
	r := renderPage(t, "testdata/evenodd.pdf", 1, RenderOptions{DPI: 96})
	const pageH = 792

	white := func(name string, c color.RGBA) {
		if c.R < 200 || c.G < 200 || c.B < 200 {
			t.Errorf("%s 應該是白的,拿到 %+v", name, c)
		}
	}
	black := func(name string, c color.RGBA) {
		if c.R > 60 || c.G > 60 || c.B > 60 {
			t.Errorf("%s 應該是黑的,拿到 %+v", name, c)
		}
	}
	// 左邊那一個用 f*:外圈與內圈同方向,中間應該被挖空。
	black("左邊方框的環", pixelAt(t, r, 75, 600, pageH))
	white("左邊方框的洞", pixelAt(t, r, 150, 600, pageH))
	// 右邊那一個用 f:同樣的兩圈,但非零繞組會把中間填實。
	black("右邊方框的環", pixelAt(t, r, 325, 600, pageH))
	black("右邊方框的中間", pixelAt(t, r, 400, 600, pageH))
}

// 只有一圈的路徑,兩種填法的結果一樣 —— 那條快路不能因為 f* 就被繞開。
func TestSingleSubpathSameEitherRule(t *testing.T) {
	p := &path{}
	p.moveTo(10, 10)
	p.lineTo(50, 10)
	p.lineTo(50, 50)
	p.close()
	if n := p.subpaths(); n != 1 {
		t.Errorf("這條路徑有 %d 圈,應該是 1 圈", n)
	}
	p.moveTo(60, 60)
	p.lineTo(80, 60)
	p.close()
	if n := p.subpaths(); n != 2 {
		t.Errorf("加一圈之後是 %d 圈,應該是 2 圈", n)
	}
	if got := len(p.split()); got != 2 {
		t.Errorf("拆出 %d 圈", got)
	}
}
