package render

import (
	"image"
	"image/color"
	"testing"

	"github.com/wicanr2/wincv-remake/internal/cell"
)

// Overlay.Zoom 是 1:1 模式下的放大倍率:放大 2 倍時,來源的一個像素
// 要佔目標的 2×2。用最近鄰,理由同 Fit —— 點陣風格的介面不做平滑縮放。
func TestOverlayZoom(t *testing.T) {
	src := image.NewRGBA(image.Rect(0, 0, 2, 2))
	src.Set(0, 0, color.RGBA{255, 0, 0, 255})
	src.Set(1, 0, color.RGBA{0, 255, 0, 255})

	r := New(&stubHalf{w: 8, h: 15}, nil)
	s := cell.New(4, 2)
	img := r.DrawWith(s, &Overlay{
		Img:  src,
		Rect: image.Rect(0, 0, 8, 8),
		Zoom: 2,
	})
	// (0,0)-(1,1) 都該是來源的 (0,0) 紅色;(2,0) 起是來源的 (1,0) 綠色。
	for _, c := range []struct {
		x, y int
		want color.RGBA
	}{
		{0, 0, color.RGBA{255, 0, 0, 255}},
		{1, 1, color.RGBA{255, 0, 0, 255}},
		{2, 0, color.RGBA{0, 255, 0, 255}},
		{3, 1, color.RGBA{0, 255, 0, 255}},
	} {
		got := img.RGBAAt(c.x, c.y)
		if got != c.want {
			t.Errorf("(%d,%d) = %v,預期 %v", c.x, c.y, got, c.want)
		}
	}
}
