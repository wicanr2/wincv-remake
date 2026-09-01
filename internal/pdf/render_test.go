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

// 內嵌影像(BI / ID / EI):4×4 的紅藍棋盤放大貼在頁面上。
//
// testdata/inline.pdf 是手寫的最小檔案。棋盤的用意是「位置錯了看得出來」——
// 顏色平均的圖就算貼歪了、上下顛倒了也看不出來。
func TestRenderInlineImage(t *testing.T) {
	r := renderPage(t, "testdata/inline.pdf", 1, RenderOptions{DPI: 96})
	const pageH = 792
	// 影像佔 PDF 的 (60,500)–(260,700),4×4 格,每格 50 點。
	// 左上角那一格是紅的,它右邊那一格是藍的。
	at := func(col, row int) color.RGBA {
		return pixelAt(t, r, 60+float64(col)*50+25, 700-float64(row)*50-25, pageH)
	}
	red := func(name string, c color.RGBA) {
		if c.R < 200 || c.G > 80 || c.B > 80 {
			t.Errorf("%s 應該是紅的,拿到 %+v", name, c)
		}
	}
	blue := func(name string, c color.RGBA) {
		if c.B < 200 || c.R > 80 || c.G > 80 {
			t.Errorf("%s 應該是藍的,拿到 %+v", name, c)
		}
	}
	// 棋盤的第一列:紅藍紅藍。畫反了(上下顛倒)的話這一列會變成藍紅藍紅。
	red("第一列第一格", at(0, 0))
	blue("第一列第二格", at(1, 0))
	red("第一列第三格", at(2, 0))
	// 第二列相位相反。
	blue("第二列第一格", at(0, 1))
	red("第二列第二格", at(1, 1))
}

// 漸層。testdata/shading.pdf 是手寫的最小檔案,四塊各盯一件事:
//
//	A 三角形裁剪 + 軸向漸層 —— `sh` 的形狀完全來自裁剪路徑
//	B 放射漸層當填色圖樣(PatternType 2 + ShadingType 3)
//	C 接合函式(第 3 型):綠 → 黃 → 綠
//	D 取樣函式(第 0 型):紅 → 藍 → 綠 → 黃
//
// C 與 D 的顏色順序是刻意挑的:兩端相同(C)或中間繞一圈(D),
// 兩種都不可能由「兩個端點線性內插」生出來,所以函式沒讀對就一定紅。
func TestRenderShading(t *testing.T) {
	r := renderPage(t, "testdata/shading.pdf", 1, RenderOptions{DPI: 96})
	const pageH = 792

	// 主色:哪一個分量明顯大。用「誰大」而不是比對確切數值,
	// 因為反鋸齒與取樣位置會讓邊緣差幾個階。
	want := func(name string, c color.RGBA, wr, wg, wb bool) {
		t.Helper()
		got := func(v uint8) bool { return v > 200 }
		lo := func(v uint8) bool { return v < 60 }
		ok := true
		for _, p := range []struct {
			on bool
			v  uint8
		}{{wr, c.R}, {wg, c.G}, {wb, c.B}} {
			if p.on && !got(p.v) || !p.on && !lo(p.v) {
				ok = false
			}
		}
		if !ok {
			t.Errorf("%s 顏色不對,拿到 %+v(期待 R=%v G=%v B=%v)", name, c, wr, wg, wb)
		}
	}

	// A:三角形內部照漸層走,三角形外(但仍在外接矩形內)必須是白的。
	want("A 左下角", pixelAt(t, r, 80, 570, pageH), true, false, false)
	want("A 右下角", pixelAt(t, r, 240, 565, pageH), false, false, true)
	want("A 右上角(三角形外)", pixelAt(t, r, 240, 660, pageH), true, true, true)

	// B:放射漸層的圓心是紅的,角落被 Extend 拉成藍的。
	want("B 圓心", pixelAt(t, r, 400, 620, pageH), true, false, false)
	want("B 角落", pixelAt(t, r, 310, 570, pageH), false, false, true)

	// C:接合函式。兩端同色、中間不同 —— 線性內插做不出這個。
	want("C 左", pixelAt(t, r, 65, 440, pageH), false, true, false)
	want("C 中", pixelAt(t, r, 160, 440, pageH), true, true, false)
	want("C 右", pixelAt(t, r, 255, 440, pageH), false, true, false)

	// D:取樣函式的四個取樣點。
	want("D 取樣 0", pixelAt(t, r, 302, 440, pageH), true, false, false)
	want("D 取樣 1", pixelAt(t, r, 367, 440, pageH), false, false, true)
	want("D 取樣 2", pixelAt(t, r, 433, 440, pageH), false, true, false)
	want("D 取樣 3", pixelAt(t, r, 498, 440, pageH), true, true, false)
}

// 取文字不該被漸層影響:textDevice 對 shade 是空實作,而 shading.pdf
// 一個字都沒有。這條擋的是「圖形路徑意外把取文字弄掛」。
func TestShadingDoesNotBreakTextExtraction(t *testing.T) {
	d, err := Open("testdata/shading.pdf")
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	p, err := d.Page(1)
	if err != nil {
		t.Fatal(err)
	}
	if got := len(p.Texts()); got != 0 {
		t.Errorf("這一頁沒有文字,卻取出 %d 段", got)
	}
}
