package imgfmt

import (
	"image/color"
	"os"
	"testing"
)

// SVG 是向量的,尺寸要從 viewBox 來,而且畫出來的顏色要對 ——
// 「有回傳一張圖」不等於「畫對了」,全白的圖也會通過只檢查尺寸的測試。
func TestDecodeSVG(t *testing.T) {
	d, err := os.ReadFile("testdata/tri.svg")
	if err != nil {
		t.Fatal(err)
	}
	img, name, err := Decode("tri.svg", d)
	if err != nil {
		t.Fatal(err)
	}
	if name != "SVG" {
		t.Errorf("格式名 = %q", name)
	}
	b := img.Bounds()
	if b.Dx() != 40 || b.Dy() != 20 {
		t.Fatalf("尺寸 = %d×%d, 想要 40×20", b.Dx(), b.Dy())
	}
	near := func(x, y int, want color.RGBA) {
		t.Helper()
		r, g, bl, _ := img.At(x, y).RGBA()
		gr, gg, gb := uint32(want.R)<<8, uint32(want.G)<<8, uint32(want.B)<<8
		const tol = 0x1800
		if diff(r, gr) > tol || diff(g, gg) > tol || diff(bl, gb) > tol {
			t.Errorf("(%d,%d) = %v, 想要 %v", x, y, img.At(x, y), want)
		}
	}
	near(5, 10, color.RGBA{0xFF, 0, 0, 0xFF})        // 左半紅
	near(30, 10, color.RGBA{0xFF, 0xFF, 0xFF, 0xFF}) // 右半白
}

func diff(a, b uint32) uint32 {
	if a > b {
		return a - b
	}
	return b - a
}

// 壞掉的 SVG 要回錯誤,不能回一張空白圖 —— 空白圖看起來像「這張圖就是白的」。
func TestDecodeSVGBroken(t *testing.T) {
	if _, _, err := Decode("x.svg", []byte("<svg><this is not xml")); err == nil {
		t.Error("壞掉的 SVG 沒有回錯誤")
	}
}

// viewBox 很大的檔案不能配置到爆。
func TestDecodeSVGClampsHugeViewBox(t *testing.T) {
	huge := []byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 100000 100000"></svg>`)
	img, _, err := Decode("h.svg", huge)
	if err != nil {
		t.Fatal(err)
	}
	if b := img.Bounds(); b.Dx() > SVGMaxSide || b.Dy() > SVGMaxSide {
		t.Errorf("尺寸 %d×%d 超過上限 %d", b.Dx(), b.Dy(), SVGMaxSide)
	}
}
