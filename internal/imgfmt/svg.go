package imgfmt

import (
	"bytes"
	"fmt"
	"image"

	"github.com/srwiley/oksvg"
	"github.com/srwiley/rasterx"
)

// SVGMaxSide 是光柵化時的最長邊上限。
//
// SVG 沒有固有的像素大小,可以無限放大。畫進格點畫面時只需要
// 幾百個像素,而 viewBox 寫著幾萬單位的檔案並不少見 ——
// 不設上限的話會為了一張圖配置好幾 GB。
const SVGMaxSide = 2048

// DecodeSVG 把 SVG 光柵化成點陣圖。
//
// 尺寸取自 width/height 屬性,沒有就取 viewBox。兩個都沒有的話
// 給一個預設值 —— 那種檔案在瀏覽器裡也是靠 CSS 決定大小,
// 沒有「正確答案」可言。
func DecodeSVG(d []byte) (image.Image, error) {
	icon, err := oksvg.ReadIconStream(bytes.NewReader(d), oksvg.WarnErrorMode)
	if err != nil {
		return nil, fmt.Errorf("SVG 解析失敗: %w", err)
	}
	w, h := icon.ViewBox.W, icon.ViewBox.H
	if w <= 0 || h <= 0 {
		w, h = 256, 256
	}
	// 等比例縮到上限之內。
	if w > SVGMaxSide || h > SVGMaxSide {
		k := SVGMaxSide / w
		if kh := SVGMaxSide / h; kh < k {
			k = kh
		}
		w, h = w*k, h*k
	}
	iw, ih := int(w+0.5), int(h+0.5)
	if iw < 1 {
		iw = 1
	}
	if ih < 1 {
		ih = 1
	}
	icon.SetTarget(0, 0, float64(iw), float64(ih))

	img := image.NewRGBA(image.Rect(0, 0, iw, ih))
	scanner := rasterx.NewScannerGV(iw, ih, img, img.Bounds())
	icon.Draw(rasterx.NewDasher(iw, ih, scanner), 1.0)
	return img, nil
}
