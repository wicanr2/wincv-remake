package imgfmt

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"os"
	"testing"
)

func rgbaAt(t *testing.T, img image.Image, x, y int) color.RGBA {
	t.Helper()
	r, g, b, a := img.At(x, y).RGBA()
	return color.RGBA{uint8(r >> 8), uint8(g >> 8), uint8(b >> 8), uint8(a >> 8)}
}

// PBM 的 1 是黑、0 是白 —— 反了整張圖就反相。
func TestPNMAsciiPBMPolarity(t *testing.T) {
	src := "P1\n# 註解\n2 2\n1 0\n0 1\n"
	img, err := DecodePNM([]byte(src))
	if err != nil {
		t.Fatal(err)
	}
	if got := rgbaAt(t, img, 0, 0); got.R != 0 {
		t.Errorf("(0,0) 的 1 應為黑,得到 %v", got)
	}
	if got := rgbaAt(t, img, 1, 0); got.R != 255 {
		t.Errorf("(1,0) 的 0 應為白,得到 %v", got)
	}
}

func TestPNMBinaryPPM(t *testing.T) {
	// P6 2x1 maxval 255,兩個像素:紅、綠
	src := append([]byte("P6\n2 1\n255\n"), 255, 0, 0, 0, 255, 0)
	img, err := DecodePNM(src)
	if err != nil {
		t.Fatal(err)
	}
	if got := rgbaAt(t, img, 0, 0); got.R != 255 || got.G != 0 {
		t.Errorf("第一個像素 = %v, 應為紅", got)
	}
	if got := rgbaAt(t, img, 1, 0); got.G != 255 || got.R != 0 {
		t.Errorf("第二個像素 = %v, 應為綠", got)
	}
}

func TestPNMScalesMaxval(t *testing.T) {
	src := append([]byte("P5\n1 1\n15\n"), 15)
	img, err := DecodePNM(src)
	if err != nil {
		t.Fatal(err)
	}
	if got := rgbaAt(t, img, 0, 0); got.R != 255 {
		t.Errorf("maxval=15 的 15 應放大成 255,得到 %d", got.R)
	}
}

// TGA 預設原點在左下,忘了翻轉會整張上下顛倒 ——
// 而顛倒的圖看起來「有解出來」,最容易漏掉。
func TestTGABottomUpDefault(t *testing.T) {
	// 2x2 未壓縮 24bpp,descriptor=0(bottom-up)
	// 檔案裡的第一列是畫面的**最後一列**
	hdr := make([]byte, 18)
	hdr[2] = 2 // uncompressed RGB
	hdr[12], hdr[13] = 2, 0
	hdr[14], hdr[15] = 2, 0
	hdr[16] = 24
	body := []byte{
		0, 0, 255, 0, 0, 255, // 檔案第 1 列 = 畫面第 2 列:紅紅 (BGR)
		255, 0, 0, 255, 0, 0, // 檔案第 2 列 = 畫面第 1 列:藍藍
	}
	img, err := DecodeTGA(append(hdr, body...))
	if err != nil {
		t.Fatal(err)
	}
	if got := rgbaAt(t, img, 0, 0); got.B != 255 {
		t.Errorf("畫面第一列應為藍(檔案的最後一列),得到 %v", got)
	}
	if got := rgbaAt(t, img, 0, 1); got.R != 255 {
		t.Errorf("畫面第二列應為紅,得到 %v", got)
	}

	// 設了 top-down 旗標就不翻轉
	hdr[17] = 0x20
	img2, err := DecodeTGA(append(hdr, body...))
	if err != nil {
		t.Fatal(err)
	}
	if got := rgbaAt(t, img2, 0, 0); got.R != 255 {
		t.Errorf("top-down 時畫面第一列應為紅,得到 %v", got)
	}
}

func TestTGARLE(t *testing.T) {
	hdr := make([]byte, 18)
	hdr[2] = 10 // RLE RGB
	hdr[12] = 4
	hdr[14] = 1
	hdr[16] = 24
	hdr[17] = 0x20
	// 一個 RLE packet:重複 4 次的藍色 (BGR = FF 00 00)
	body := []byte{0x83, 255, 0, 0}
	img, err := DecodeTGA(append(hdr, body...))
	if err != nil {
		t.Fatal(err)
	}
	for x := 0; x < 4; x++ {
		if got := rgbaAt(t, img, x, 0); got.B != 255 {
			t.Errorf("x=%d = %v, 應為藍", x, got)
		}
	}
}

// PCX 的 256 色調色盤在檔尾,前面有一個 0x0C 標記。
func TestPCX8bppWithTailPalette(t *testing.T) {
	w, h := 4, 2
	hdr := make([]byte, 128)
	hdr[0] = 0x0A
	hdr[2] = 1 // RLE
	hdr[3] = 8 // bpp
	hdr[8], hdr[9] = byte(w-1), 0
	hdr[10], hdr[11] = byte(h-1), 0
	hdr[0x41] = 1 // planes
	hdr[0x42] = byte(w)

	// 每列四個索引 1;RLE:0xC4 表示重複 4 次
	body := []byte{0xC4, 1, 0xC4, 1}
	pal := make([]byte, 769)
	pal[0] = 0x0C
	pal[1+3*1+0] = 10 // 索引 1 = (10,20,30)
	pal[1+3*1+1] = 20
	pal[1+3*1+2] = 30

	img, err := DecodePCX(append(append(hdr, body...), pal...))
	if err != nil {
		t.Fatal(err)
	}
	if img.Bounds().Dx() != w || img.Bounds().Dy() != h {
		t.Fatalf("尺寸 = %v, 應為 %dx%d", img.Bounds(), w, h)
	}
	got := rgbaAt(t, img, 0, 0)
	if got.R != 10 || got.G != 20 || got.B != 30 {
		t.Errorf("像素 = %v, 應為 (10,20,30) —— 檔尾調色盤沒讀到", got)
	}
}

// ICO 目錄項的 0 表示 256,照字面讀會挑錯張。
func TestICOPicksLargest(t *testing.T) {
	// 造一個兩張圖的 ICO:16x16 與 "0x0"(其實是 256x256)。
	// 兩張都用 PNG 內嵌,方便產生。
	mk := func(n int, c color.RGBA) []byte {
		im := image.NewRGBA(image.Rect(0, 0, n, n))
		for y := 0; y < n; y++ {
			for x := 0; x < n; x++ {
				im.SetRGBA(x, y, c)
			}
		}
		var buf bytes.Buffer
		png.Encode(&buf, im)
		return buf.Bytes()
	}
	small := mk(16, color.RGBA{255, 0, 0, 255})
	big := mk(64, color.RGBA{0, 255, 0, 255})

	var out bytes.Buffer
	out.Write([]byte{0, 0, 1, 0, 2, 0}) // reserved, type=1, count=2
	dirSize := 6 + 2*16
	writeEntry := func(w byte, data []byte, off int) {
		e := make([]byte, 16)
		e[0], e[1] = w, w
		e[8] = byte(len(data))
		e[9] = byte(len(data) >> 8)
		e[10] = byte(len(data) >> 16)
		e[11] = byte(len(data) >> 24)
		e[12] = byte(off)
		e[13] = byte(off >> 8)
		e[14] = byte(off >> 16)
		e[15] = byte(off >> 24)
		out.Write(e)
	}
	writeEntry(16, small, dirSize)
	writeEntry(0, big, dirSize+len(small)) // 0 = 256
	out.Write(small)
	out.Write(big)

	img, err := DecodeICO(out.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	got := rgbaAt(t, img, 0, 0)
	if got.G != 255 {
		t.Errorf("挑到的是紅色那張(16x16),應該挑標成 0(=256)的那張: %v", got)
	}
}

// 用原版自己的 .ico 跑一遍。
func TestRealIcoFiles(t *testing.T) {
	for _, n := range []string{
		"../../original/app/WinCV.ico",
		"../../original/app/wincv1.ico",
		"../../original/app/wincvbusy.ico",
	} {
		d, err := os.ReadFile(n)
		if err != nil {
			t.Skipf("找不到 %s", n)
		}
		img, kind, err := Decode(n, d)
		if err != nil {
			t.Errorf("%s: %v", n, err)
			continue
		}
		if kind != "ICO" {
			t.Errorf("%s 判成 %s", n, kind)
		}
		if img.Bounds().Dx() == 0 {
			t.Errorf("%s 解出零寬圖", n)
		}
	}
}

func TestFormatCoverage(t *testing.T) {
	done := 0
	for _, f := range Formats {
		if f.Supported {
			done++
		}
		if !f.Supported && f.Note == "" {
			t.Errorf("%s 還沒支援,但沒寫下一步", f.Name)
		}
	}
	t.Logf("圖檔格式支援 %d/%d", done, len(Formats))
	if done < 10 {
		t.Errorf("已支援 %d 種,主要格式應該都在", done)
	}
}
