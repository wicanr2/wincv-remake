package pdf

import (
	"image"
	"image/color"
	"math"

	"github.com/srwiley/rasterx"

	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"
)

// 漸層(shading)有七種型別,這裡做的是最常見的兩種:沿一條線變化的
// 軸向漸層(第 2 型),與從一個圓長到另一個圓的放射漸層(第 3 型)。
// 其餘幾種(自由三角網格、Coons 曲面)在一般文件裡幾乎看不到。
//
// 顏色由 PDF 的函式算:位置換成 0–1 的參數 t,函式把 t 換成一組顏色分量。

// shading 是一份解析好的漸層。座標都在「漸層空間」,由呼叫端負責把
// 畫面座標換過來。
type shading struct {
	typ    int
	cs     *colorSpace
	coords []float64
	fn     pdfFunc
	t0, t1 float64
	extend [2]bool
}

// loadShading 讀一個漸層物件。看不懂的回 nil,上層就不畫 ——
// 留白比畫出一片猜來的顏色好。
func (in *interp) loadShading(o types.Object) *shading {
	d, ok := deref(in.x, o).(types.Dict)
	if !ok {
		if sd, _, err := in.x.DereferenceStreamDict(o); err == nil && sd != nil {
			d = sd.Dict
		} else {
			return nil
		}
	}
	sh := &shading{
		typ:    int(numOrZero(in.x, d["ShadingType"])),
		coords: floatsOf(in.x, d["Coords"]),
		t0:     0, t1: 1,
	}
	if sh.typ != 2 && sh.typ != 3 {
		return nil
	}
	sh.cs = in.parseColorSpace(deref(in.x, d["ColorSpace"]), 0)
	sh.fn = loadFunc(in.x, d["Function"])
	if sh.fn == nil {
		return nil
	}
	if dm := floatsOf(in.x, d["Domain"]); len(dm) >= 2 {
		sh.t0, sh.t1 = dm[0], dm[1]
	}
	if ext, ok := deref(in.x, d["Extend"]).(types.Array); ok && len(ext) >= 2 {
		b0, _ := deref(in.x, ext[0]).(types.Boolean)
		b1, _ := deref(in.x, ext[1]).(types.Boolean)
		sh.extend[0], sh.extend[1] = bool(b0), bool(b1)
	}
	switch sh.typ {
	case 2:
		if len(sh.coords) < 4 {
			return nil
		}
	case 3:
		if len(sh.coords) < 6 {
			return nil
		}
	}
	return sh
}

// colorAt 算漸層空間上一點的顏色。第二個回傳值為假表示這一點不在漸層上,
// 不該塗任何東西(沒有延伸的兩端就是這種情形)。
func (sh *shading) colorAt(x, y float64) (rgb, bool) {
	s, ok := 0.0, false
	if sh.typ == 2 {
		s, ok = sh.axialParam(x, y)
	} else {
		s, ok = sh.radialParam(x, y)
	}
	if !ok {
		return rgb{}, false
	}
	t := sh.t0 + s*(sh.t1-sh.t0)
	return sh.cs.color(sh.fn.eval(t)), true
}

// axialParam 把一點投影到漸層軸上,換成 0–1 的位置。
func (sh *shading) axialParam(x, y float64) (float64, bool) {
	x0, y0, x1, y1 := sh.coords[0], sh.coords[1], sh.coords[2], sh.coords[3]
	dx, dy := x1-x0, y1-y0
	den := dx*dx + dy*dy
	if den == 0 {
		return 0, sh.extend[0] || sh.extend[1]
	}
	s := ((x-x0)*dx + (y-y0)*dy) / den
	return sh.clampParam(s)
}

// radialParam 解「這一點落在哪一個圓上」。
//
// 兩個圓之間的每一個中間圓是 c(s)、r(s) 的線性內插,要找的是滿足
// |p − c(s)| = r(s) 的 s。展開之後是一個二次式,取**較大**的那個根 ——
// 兩個圓重疊的地方,後畫的(s 較大的)蓋住先畫的。
func (sh *shading) radialParam(px, py float64) (float64, bool) {
	x0, y0, r0 := sh.coords[0], sh.coords[1], sh.coords[2]
	x1, y1, r1 := sh.coords[3], sh.coords[4], sh.coords[5]
	dx, dy, dr := x1-x0, y1-y0, r1-r0
	fx, fy := px-x0, py-y0

	a := dx*dx + dy*dy - dr*dr
	b := fx*dx + fy*dy + r0*dr
	c := fx*fx + fy*fy - r0*r0

	if math.Abs(a) < 1e-9 {
		if b == 0 {
			return 0, false
		}
		s := c / (2 * b)
		if r0+s*dr < 0 {
			return 0, false
		}
		return sh.clampParam(s)
	}
	disc := b*b - a*c
	if disc < 0 {
		return 0, false
	}
	sq := math.Sqrt(disc)
	for _, s := range [2]float64{(b + sq) / a, (b - sq) / a} {
		if r0+s*dr < 0 {
			continue
		}
		if v, ok := sh.clampParam(s); ok {
			return v, true
		}
	}
	return 0, false
}

// clampParam 處理兩端:有延伸就夾住,沒有就不畫。
func (sh *shading) clampParam(s float64) (float64, bool) {
	switch {
	case s < 0:
		if !sh.extend[0] {
			return 0, false
		}
		return 0, true
	case s > 1:
		if !sh.extend[1] {
			return 0, false
		}
		return 1, true
	}
	return s, true
}

// shadeFunc 做出一個「問畫面上這一點該是什麼顏色」的函式。
// m 是漸層空間到畫面的變換。
func shadeFunc(sh *shading, m matrix, alpha float64) (rasterx.ColorFunc, bool) {
	inv, ok := m.invert()
	if !ok {
		return nil, false
	}
	return func(x, y int) color.Color {
		sx, sy := inv.apply(float64(x)+0.5, float64(y)+0.5)
		c, ok := sh.colorAt(sx, sy)
		if !ok {
			return color.RGBA{}
		}
		return c.rgba(alpha)
	}, true
}

// shade 把一個漸層塗滿某個範圍(`sh` 運算子:範圍就是目前的裁剪)。
//
// 有裁剪路徑就照那條路徑填,沒有才退回外接矩形。這裡不能只用矩形:
// 別的運算子有自己的形狀,裁剪只是保險;`sh` 的形狀完全來自裁剪。
// 實測(085 第 29 頁)那一頁的漸層條是箭頭形,只用矩形會畫成方塊。
func (d *rasterDevice) shade(sh *shading, gs *gstate) {
	r := d.clipRectOf(gs)
	if r.Empty() {
		return
	}
	if gs.clipPath != nil && !gs.clipPath.empty() {
		d.fillShading(gs.clipPath, sh, gs.ctm, gs, gs.clipEvenOdd)
		return
	}
	fn, ok := shadeFunc(sh, gs.ctm, gs.fillAlpha)
	if !ok {
		return
	}
	for y := r.Min.Y; y < r.Max.Y; y++ {
		for x := r.Min.X; x < r.Max.X; x++ {
			c, isRGBA := fn(x, y).(color.RGBA)
			if !isRGBA || c.A == 0 {
				continue
			}
			d.blend(x, y, c, 1)
		}
	}
}

// fillShading 用漸層填一條路徑(以漸層當填色的圖樣)。
//
// 交給光柵器的是一個「逐點問顏色」的函式,所以路徑的邊緣照樣有反鋸齒 ——
// 自己逐像素填的話,邊緣會是鋸齒狀的。
func (d *rasterDevice) fillShading(p *path, sh *shading, m matrix, gs *gstate, evenOdd bool) {
	fn, ok := shadeFunc(sh, m, gs.fillAlpha)
	if !ok {
		return
	}
	clip := d.clipRectOf(gs)
	if evenOdd && p.subpaths() > 1 {
		// 奇偶填法走遮罩那條路,顏色再逐像素問。
		d.fillEvenOddFunc(p, fn, clip)
		return
	}
	d.drawPathFunc(p, fn, clip, func() {
		addPath(d.filler, p, d.offX, d.offY)
		d.filler.Draw()
		d.filler.Clear()
	})
}

// drawPathFunc 與 drawPath 相同,只是顏色改成逐點問。
func (d *rasterDevice) drawPathFunc(p *path, fn rasterx.ColorFunc, clip image.Rectangle, draw func()) {
	if p.empty() {
		return
	}
	x0, y0, x1, y1 := p.bounds()
	if math.IsInf(x0, 0) || math.IsInf(y0, 0) {
		return
	}
	r := image.Rect(
		int(math.Floor(x0-1)), int(math.Floor(y0-1)),
		int(math.Ceil(x1+1))+1, int(math.Ceil(y1+1))+1).Intersect(clip)
	if r.Empty() {
		return
	}
	sub, ok := d.img.SubImage(r).(*image.RGBA)
	if !ok {
		return
	}
	d.scanner.Dest = sub
	d.scanner.SetBounds(r.Dx(), r.Dy())
	d.scanner.Clear()
	// [雷] 顏色函式收到的座標是「來源影像的座標」,而來源的取樣起點是
	// Offset。不把 Offset 設成子影像的左上角,顏色會整片位移到別的地方,
	// 而畫出來仍然是一片漸層,看不出位移。
	d.scanner.Offset = r.Min
	d.scanner.SetColor(fn)
	d.offX, d.offY = float64(-r.Min.X), float64(-r.Min.Y)
	draw()
	d.scanner.Offset = image.Point{}
}

// fillEvenOddFunc 是奇偶填法加上逐點顏色。
func (d *rasterDevice) fillEvenOddFunc(p *path, fn rasterx.ColorFunc, clip image.Rectangle) {
	x0, y0, x1, y1 := p.bounds()
	if math.IsInf(x0, 0) || math.IsInf(y0, 0) {
		return
	}
	r := image.Rect(
		int(math.Floor(x0-1)), int(math.Floor(y0-1)),
		int(math.Ceil(x1+1))+1, int(math.Ceil(y1+1))+1).Intersect(clip)
	if r.Empty() {
		return
	}
	acc := image.NewAlpha(r)
	one := image.NewAlpha(r)
	opaque := image.NewUniform(color.Alpha{A: 255})
	for _, sp := range p.split() {
		for i := range one.Pix {
			one.Pix[i] = 0
		}
		d.scanner.Dest = one
		d.scanner.SetBounds(r.Dx(), r.Dy())
		d.scanner.Clear()
		d.scanner.SetColor(opaque.C)
		addPath(d.filler, &sp, float64(-r.Min.X), float64(-r.Min.Y))
		d.filler.Draw()
		d.filler.Clear()
		xorAlpha(acc.Pix, one.Pix)
	}
	for y := r.Min.Y; y < r.Max.Y; y++ {
		row := acc.Pix[(y-r.Min.Y)*acc.Stride:]
		for x := r.Min.X; x < r.Max.X; x++ {
			a := row[x-r.Min.X]
			if a == 0 {
				continue
			}
			c, ok := fn(x, y).(color.RGBA)
			if !ok || c.A == 0 {
				continue
			}
			c.A = uint8(int(c.A) * int(a) / 255)
			d.blend(x, y, c, 1)
		}
	}
}
