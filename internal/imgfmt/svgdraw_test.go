package imgfmt

import (
	"image"
	"testing"

	"github.com/wicanr2/wincv-remake/internal/vecfont"
)

// ink 回一塊區域裡有多少像素不是背景色。
func ink(img *image.RGBA, r image.Rectangle) int {
	bg := img.RGBAAt(0, 0)
	n := 0
	for y := r.Min.Y; y < r.Max.Y; y++ {
		for x := r.Min.X; x < r.Max.X; x++ {
			if img.RGBAAt(x, y) != bg {
				n++
			}
		}
	}
	return n
}

func decodeRGBA(t *testing.T, src string) *image.RGBA {
	t.Helper()
	m, err := DecodeSVG([]byte(src))
	if err != nil {
		t.Fatalf("解不開:%v", err)
	}
	img, ok := m.(*image.RGBA)
	if !ok {
		t.Fatalf("型別 %T", m)
	}
	return img
}

// <text> 是圖表的意義所在:少了它剩下一堆無從解讀的長條,
// 而畫面看起來是完整的 —— 所以這條要盯著「字真的畫出來了」。
func TestDecodeSVGDrawsText(t *testing.T) {
	if !vecfont.Default().Ready() {
		t.Skip("這台機器沒有可用的字型")
	}
	src := `<svg width="200" height="60" viewBox="0 0 200 60">
	  <rect width="200" height="60" fill="#ffffff"/>
	  <text x="10" y="40" font-size="24" fill="#000000">Hi</text>
	</svg>`
	img := decodeRGBA(t, src)
	// 字在左半邊的下半部,右半邊應該是空的。
	left := ink(img, image.Rect(0, 0, 100, 60))
	right := ink(img, image.Rect(100, 0, 200, 60))
	if left == 0 {
		t.Fatal("完全沒有畫出文字")
	}
	if right != 0 {
		t.Errorf("右半邊不該有東西,卻有 %d 個像素", right)
	}
}

// text-anchor 決定文字往哪一邊長。算錯的話標籤會整排偏到另一側,
// 而每一個標籤自己看起來都很正常。
func TestDecodeSVGTextAnchor(t *testing.T) {
	if !vecfont.Default().Ready() {
		t.Skip("這台機器沒有可用的字型")
	}
	tmpl := func(anchor string) string {
		return `<svg width="200" height="60" viewBox="0 0 200 60">
		  <rect width="200" height="60" fill="#ffffff"/>
		  <text x="100" y="40" font-size="24" fill="#000000" text-anchor="` + anchor + `">Hi</text>
		</svg>`
	}
	start := decodeRGBA(t, tmpl("start"))
	end := decodeRGBA(t, tmpl("end"))

	if ink(start, image.Rect(0, 0, 100, 60)) != 0 {
		t.Error("text-anchor=start 不該畫到 x=100 的左邊")
	}
	if ink(start, image.Rect(100, 0, 200, 60)) == 0 {
		t.Error("text-anchor=start 應該畫在 x=100 的右邊")
	}
	if ink(end, image.Rect(100, 0, 200, 60)) != 0 {
		t.Error("text-anchor=end 不該畫到 x=100 的右邊")
	}
	if ink(end, image.Rect(0, 0, 100, 60)) == 0 {
		t.Error("text-anchor=end 應該畫在 x=100 的左邊")
	}
}

// 文字要跟著 viewBox 一起縮放,而且用的是 oksvg 算的那份變換 ——
// 用另一份的話字與長條會各自對齊到不同的地方。
func TestDecodeSVGTextScalesWithViewBox(t *testing.T) {
	if !vecfont.Default().Ready() {
		t.Skip("這台機器沒有可用的字型")
	}
	// viewBox 遠大於上限,會被縮到 SVGMaxSide 之內。
	src := `<svg viewBox="0 0 8000 4000">
	  <rect width="8000" height="4000" fill="#ffffff"/>
	  <text x="400" y="2000" font-size="800" fill="#000000">Hi</text>
	</svg>`
	img := decodeRGBA(t, src)
	b := img.Bounds()
	if b.Dx() > SVGMaxSide || b.Dy() > SVGMaxSide {
		t.Fatalf("尺寸 %v 超過上限", b)
	}
	// 字在左半、垂直置中附近;右下角應該是空的。
	if ink(img, image.Rect(0, 0, b.Dx()/2, b.Dy())) == 0 {
		t.Error("縮放後文字不見了")
	}
	if ink(img, image.Rect(b.Dx()/2, b.Dy()*3/4, b.Dx(), b.Dy())) != 0 {
		t.Error("右下角不該有東西")
	}
}

// fill="none" 的文字是看不見的(常見於只拿來量尺寸的輔助元素)。
func TestDecodeSVGTextFillNone(t *testing.T) {
	if !vecfont.Default().Ready() {
		t.Skip("這台機器沒有可用的字型")
	}
	src := `<svg width="200" height="60" viewBox="0 0 200 60">
	  <rect width="200" height="60" fill="#ffffff"/>
	  <text x="10" y="40" font-size="24" fill="none">Hi</text>
	</svg>`
	img := decodeRGBA(t, src)
	if n := ink(img, img.Bounds()); n != 0 {
		t.Errorf("fill=none 不該畫出來,卻有 %d 個像素", n)
	}
}
