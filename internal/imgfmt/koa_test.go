package imgfmt

import (
	"image/color"
	"testing"
)

// 找不到可以產生或解讀 .koa 的參考工具,所以這裡自己照規格組一個檔案,
// 把**四種顏色來源各用一次**。這測的是「兩個位元決定去哪裡取色」
// 這個對應關係 —— 那正是這個格式唯一會弄錯的地方,而且接錯任何一路,
// 圖看起來仍然有東西,只是顏色亂掉,肉眼不容易發現。
func buildKOA(bg, hi, lo, col byte) []byte {
	d := make([]byte, 10003)
	d[0], d[1] = 0x00, 0x60 // 載入位址 $6000

	bitmap := d[2 : 2+8000]
	screen := d[2+8000 : 2+9000]
	colram := d[2+9000 : 2+10000]

	// 第 0 個字元格的第 0 列:四個像素分別用 00 01 10 11
	bitmap[0] = 0<<6 | 1<<4 | 2<<2 | 3
	screen[0] = hi<<4 | lo
	colram[0] = col
	d[2+10000] = bg
	return d
}

func TestKOAColorSources(t *testing.T) {
	const bg, hi, lo, col = 6, 1, 2, 5 // 藍、白、紅、綠
	img, err := DecodeKOA(buildKOA(bg, hi, lo, col))
	if err != nil {
		t.Fatal(err)
	}
	if b := img.Bounds(); b.Dx() != 320 || b.Dy() != 200 {
		t.Fatalf("尺寸 %v,期望 320x200(每個像素兩倍寬)", b)
	}
	want := []struct {
		x   int
		idx byte
		src string
	}{
		{0, bg, "背景色"},
		{2, hi, "螢幕記憶體高 4 位"},
		{4, lo, "螢幕記憶體低 4 位"},
		{6, col, "色彩記憶體低 4 位"},
	}
	for _, w := range want {
		got := color.RGBAModel.Convert(img.At(w.x, 0)).(color.RGBA)
		if got != c64Palette[w.idx] {
			t.Errorf("x=%d(%s):拿到 %v,期望 %v", w.x, w.src, got, c64Palette[w.idx])
		}
		// 每個像素兩倍寬,右邊那一格要一樣
		if next := color.RGBAModel.Convert(img.At(w.x+1, 0)).(color.RGBA); next != got {
			t.Errorf("x=%d 沒有加倍寬度", w.x)
		}
	}
}

func TestKOARejectsWrongLength(t *testing.T) {
	if _, err := DecodeKOA(make([]byte, 1234)); err == nil {
		t.Error("長度不對應該報錯")
	}
	// 沒有載入位址的變體也要收
	d := buildKOA(0, 1, 2, 3)[2:]
	if _, err := DecodeKOA(d); err != nil {
		t.Errorf("10001 位元組的變體應該也能解: %v", err)
	}
}
