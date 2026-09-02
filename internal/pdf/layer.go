package pdf

import (
	"image"
	"image/color"
	"image/draw"

	"github.com/srwiley/rasterx"
)

// PDF 的透明度有兩件事要先畫到別的地方、再整批合成回來:
//
//   - **透明群組**:一個表單物件標了 `/Group`,它裡面的內容要當成一個整體
//     算完再套外層的透明度。直接畫在同一張畫布上的話,群組裡自己設的
//     透明度會蓋掉外層的 —— 該半透明的一塊會變成不透明。
//   - **軟遮罩**:`/SMask` 指的是另一段內容,把它畫出來之後拿亮度(或
//     alpha)當遮罩。漸層條、羽化的陰影、淡出的色塊都是這樣做的。
//
// 兩件事都靠同一組東西:一張暫時的畫布。

// maxLayers 是暫時畫布的巢狀上限。一層一張整頁的 RGBA(A4 96 dpi 約 3.4 MB),
// 而畸形或惡意的檔案可以無限巢狀。
const maxLayers = 8

// pushLayer 開一張暫時畫布。fill 是底色,nil 表示全透明。
func (d *rasterDevice) pushLayer(fill *rgb) bool {
	if len(d.layers) >= maxLayers {
		return false
	}
	img := image.NewRGBA(image.Rect(0, 0, d.w, d.h))
	if fill != nil {
		draw.Draw(img, img.Bounds(), image.NewUniform(fill.rgba(1)), image.Point{}, draw.Src)
	}
	d.layers = append(d.layers, d.img)
	d.img = img
	return true
}

// popLayer 取回最上面那張畫布,並把目的地換回下一層。
func (d *rasterDevice) popLayer() *image.RGBA {
	if len(d.layers) == 0 {
		return nil
	}
	top := d.img
	d.img = d.layers[len(d.layers)-1]
	d.layers = d.layers[:len(d.layers)-1]
	return top
}

// popLayerAsMask 把最上面那張畫布收起來當成遮罩。
//
// 亮度遮罩取顏色的亮度,alpha 遮罩取透明度。亮度用的是 Rec. 601 的權重,
// 與規格一致 —— 用平均值的話彩色的遮罩會偏掉,而畫出來仍然是一片合理的
// 漸變,看不出是錯的。
func (d *rasterDevice) popLayerAsMask(luminosity bool) *image.Alpha {
	img := d.popLayer()
	if img == nil {
		return nil
	}
	m := image.NewAlpha(image.Rect(0, 0, d.w, d.h))
	for y := 0; y < d.h; y++ {
		for x := 0; x < d.w; x++ {
			i := img.PixOffset(x, y)
			p := img.Pix[i : i+4 : i+4]
			var v float64
			if luminosity {
				// 沒畫到的地方 alpha 是 0,亮度也就當成 0(完全遮住)。
				v = (0.299*float64(p[0]) + 0.587*float64(p[1]) + 0.114*float64(p[2])) *
					float64(p[3]) / 255
			} else {
				v = float64(p[3])
			}
			m.Pix[m.PixOffset(x, y)] = uint8(v + 0.5)
		}
	}
	return m
}

// popLayerComposite 把最上面那張畫布依透明度與遮罩合成回下一層。
func (d *rasterDevice) popLayerComposite(alpha float64, mask *image.Alpha) {
	img := d.popLayer()
	if img == nil {
		return
	}
	saved := d.mask
	d.mask = nil // 遮罩在這裡自己套,不要在 blend 裡再套一次
	for y := 0; y < d.h; y++ {
		for x := 0; x < d.w; x++ {
			i := img.PixOffset(x, y)
			p := img.Pix[i : i+4 : i+4]
			if p[3] == 0 {
				continue
			}
			a := alpha
			if mask != nil {
				a *= float64(mask.Pix[mask.PixOffset(x, y)]) / 255
			}
			d.blend(x, y, color.RGBA{p[0], p[1], p[2], p[3]}, a)
		}
	}
	d.mask = saved
}

// use 把目前的軟遮罩接過來。blend 拿不到 gstate,所以每個繪製入口
// 進來的時候要先過這一手。
func (d *rasterDevice) use(gs *gstate) { d.mask = gs.softMask }

// maskFunc 把一個顏色函式接上目前的軟遮罩。
//
// [雷] 光柵器是直接寫進目的地影像的,不經過 blend —— 只在 blend 裡套遮罩的話,
// 影像與漸層會受遮罩控制,而路徑、描邊、文字**完全不受影響**。畫面上看起來
// 是「有些東西淡出了、有些沒有」,不像遮罩沒生效。
func (d *rasterDevice) maskFunc(fn rasterx.ColorFunc) rasterx.ColorFunc {
	m := d.mask
	if m == nil {
		return fn
	}
	scale := func(v uint8, k float64) uint8 { return uint8(float64(v)*k + 0.5) }
	return func(x, y int) color.Color {
		if !(image.Point{X: x, Y: y}).In(m.Rect) {
			return color.RGBA{}
		}
		c, ok := fn(x, y).(color.RGBA)
		if !ok {
			return color.RGBA{}
		}
		// 顏色是預乘的,四個通道要一起縮。
		k := float64(m.Pix[m.PixOffset(x, y)]) / 255
		return color.RGBA{scale(c.R, k), scale(c.G, k), scale(c.B, k), scale(c.A, k)}
	}
}
