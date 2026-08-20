package imgfmt

import (
	"fmt"
	"image"
	"image/color"
)

// c64Palette 是 C64 的 16 色。取自 Pepto 量測 VIC-II 訊號得到的那一組,
// 是模擬器與轉檔工具最常用的版本。
var c64Palette = [16]color.RGBA{
	{0x00, 0x00, 0x00, 0xFF}, // 0 黑
	{0xFF, 0xFF, 0xFF, 0xFF}, // 1 白
	{0x68, 0x37, 0x2B, 0xFF}, // 2 紅
	{0x70, 0xA4, 0xB2, 0xFF}, // 3 青
	{0x6F, 0x3D, 0x86, 0xFF}, // 4 紫
	{0x58, 0x8D, 0x43, 0xFF}, // 5 綠
	{0x35, 0x28, 0x79, 0xFF}, // 6 藍
	{0xB8, 0xC7, 0x6F, 0xFF}, // 7 黃
	{0x6F, 0x4F, 0x25, 0xFF}, // 8 橘
	{0x43, 0x39, 0x00, 0xFF}, // 9 褐
	{0x9A, 0x67, 0x59, 0xFF}, // 10 淺紅
	{0x44, 0x44, 0x44, 0xFF}, // 11 深灰
	{0x6C, 0x6C, 0x6C, 0xFF}, // 12 灰
	{0x9A, 0xD2, 0x84, 0xFF}, // 13 淺綠
	{0x6C, 0x5E, 0xB5, 0xFF}, // 14 淺藍
	{0x95, 0x95, 0x95, 0xFF}, // 15 淺灰
}

// DecodeKOA 解 Koala Painter 的 C64 圖檔。
//
// 版面固定,沒有壓縮:載入位址(2)+ 點陣圖(8000)+ 螢幕記憶體(1000)
// + 色彩記憶體(1000)+ 背景色(1)= 10003 個位元組。有些檔案沒有前面的
// 載入位址,所以 10001 也收。
//
// 多色點陣模式下,每個像素佔 2 個位元,而那 2 個位元決定**去哪裡取顏色**:
//
//	00  背景色(整張圖共用一個)
//	01  螢幕記憶體的高 4 位
//	10  螢幕記憶體的低 4 位
//	11  色彩記憶體的低 4 位
//
// 所以同一個字元格(8x8)裡最多只有 4 種顏色,而且四種來源各不相同 ——
// 這是這個格式唯一需要小心的地方,四個來源接錯任何一個,
// 圖看起來仍然「有東西」,只是顏色亂掉。
//
// 每個像素在螢幕上是兩倍寬(160x200 的資料畫成 320x200),
// 這裡照著加倍,不然圖會被壓扁。
func DecodeKOA(data []byte) (image.Image, error) {
	const (
		bitmapLen = 8000
		screenLen = 1000
		colorLen  = 1000
	)
	switch len(data) {
	case 10003:
		data = data[2:] // 跳過載入位址
	case 10001:
		// 沒有載入位址的變體
	default:
		return nil, fmt.Errorf("KOA: 長度是 %d,應該是 10003 或 10001", len(data))
	}

	bitmap := data[:bitmapLen]
	screen := data[bitmapLen : bitmapLen+screenLen]
	colram := data[bitmapLen+screenLen : bitmapLen+screenLen+colorLen]
	bg := data[bitmapLen+screenLen+colorLen] & 0x0F

	img := image.NewRGBA(image.Rect(0, 0, 320, 200))
	for y := 0; y < 200; y++ {
		cellY, rowInCell := y/8, y%8
		for x := 0; x < 160; x++ {
			cellX, pixInCell := x/4, x%4
			cell := cellY*40 + cellX
			b := bitmap[cell*8+rowInCell]
			// 最左邊的像素在最高的兩個位元
			bits := (b >> uint(6-2*pixInCell)) & 0x03

			var idx byte
			switch bits {
			case 0:
				idx = bg
			case 1:
				idx = screen[cell] >> 4
			case 2:
				idx = screen[cell] & 0x0F
			case 3:
				idx = colram[cell] & 0x0F
			}
			c := c64Palette[idx]
			img.SetRGBA(x*2, y, c)
			img.SetRGBA(x*2+1, y, c)
		}
	}
	return img, nil
}
