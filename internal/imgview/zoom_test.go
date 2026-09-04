package imgview

import (
	"image"
	"testing"
)

func solid(w, h int) image.Image {
	return image.NewRGBA(image.Rect(0, 0, w, h))
}

// ZoomBy 沿階梯走,而且會離開「縮放到視窗」——
// 兩者不能同時成立:縮放到視窗的意思就是「不要我自己決定大小」。
func TestZoomBySteps(t *testing.T) {
	m := FromImage("a.png", "PNG", solid(100, 100), 0)
	if !m.Fit {
		t.Fatal("預設應該是縮放到視窗")
	}
	m.ZoomBy(1)
	if m.Fit {
		t.Error("放大之後還停在縮放到視窗")
	}
	if m.Zoom != 1.5 {
		t.Errorf("從 1 放大一階是 %g,預期 1.5", m.Zoom)
	}
	m.ZoomBy(1)
	if m.Zoom != 2 {
		t.Errorf("再一階是 %g,預期 2", m.Zoom)
	}
	m.ZoomBy(-2)
	if m.Zoom != 1 {
		t.Errorf("退兩階是 %g,預期 1", m.Zoom)
	}
	// 階梯的兩端要夾住,不是繞回去。
	for i := 0; i < 20; i++ {
		m.ZoomBy(1)
	}
	if m.Zoom != zoomSteps[len(zoomSteps)-1] {
		t.Errorf("一直放大停在 %g,預期 %g", m.Zoom, zoomSteps[len(zoomSteps)-1])
	}
	for i := 0; i < 20; i++ {
		m.ZoomBy(-1)
	}
	if m.Zoom != zoomSteps[0] {
		t.Errorf("一直縮小停在 %g,預期 %g", m.Zoom, zoomSteps[0])
	}
}

// 沒有 Rerender 的圖(一般圖檔)靠點陣放大:Scale 就是使用者要的倍率。
func TestScaleWithoutRerender(t *testing.T) {
	m := FromImage("a.png", "PNG", solid(100, 100), 0)
	m.SetZoom(2)
	if got := m.Scale(); got != 2 {
		t.Errorf("Scale=%g,預期 2", got)
	}
	if m.Img.Bounds().Dx() != 100 {
		t.Error("沒有 Rerender 的圖不該被換掉")
	}
}

// 能重畫的來源(PDF 頁面)要用更高的解析度重畫,重畫完就不再點陣放大。
func TestRerenderReplacesImage(t *testing.T) {
	m := FromImage("p.pdf", "PDF", solid(100, 100), 0)
	var asked []float64
	m.Rerender = func(z float64) (image.Image, error) {
		asked = append(asked, z)
		return solid(int(100*z), int(100*z)), nil
	}
	m.SetZoom(2)
	if len(asked) != 1 || asked[0] != 2 {
		t.Fatalf("Rerender 收到 %v,預期 [2]", asked)
	}
	if m.Img.Bounds().Dx() != 200 {
		t.Errorf("圖沒有被換成重畫的那張:%v", m.Img.Bounds())
	}
	if got := m.Scale(); got != 1 {
		t.Errorf("重畫過之後 Scale=%g,預期 1(不要再乘一次)", got)
	}
	// 再放大一階,要拿累積的倍率去重畫,不是相對值。
	m.ZoomBy(1)
	if asked[len(asked)-1] != 3 {
		t.Errorf("第二次重畫用 %g 倍,預期 3", asked[len(asked)-1])
	}
}

// 重畫失敗要退回點陣放大,不能停在原地什麼都沒發生。
func TestRerenderFailureFallsBack(t *testing.T) {
	m := FromImage("p.pdf", "PDF", solid(100, 100), 0)
	m.Rerender = func(z float64) (image.Image, error) {
		return nil, errTooBig
	}
	m.SetZoom(4)
	if got := m.Scale(); got != 4 {
		t.Errorf("重畫失敗後 Scale=%g,預期 4(退回點陣放大)", got)
	}
}

var errTooBig = &tooBig{}

type tooBig struct{}

func (*tooBig) Error() string { return "太大" }

// 按 1 回到原尺寸。
func TestSetZoomOne(t *testing.T) {
	m := FromImage("a.png", "PNG", solid(100, 100), 0)
	m.SetZoom(4)
	m.SetZoom(1)
	if m.Zoom != 1 || m.Fit {
		t.Errorf("Zoom=%g Fit=%v,預期 1 / false", m.Zoom, m.Fit)
	}
}
