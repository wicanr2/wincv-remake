package pdf

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"io"
	"math"

	"github.com/srwiley/rasterx"
	"golang.org/x/image/math/fixed"

	pdfcpulib "github.com/pdfcpu/pdfcpu/pkg/pdfcpu"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"
)

// invert 求反矩陣。畫影像時要用它把畫面上的一點換回影像裡的位置。
func (m matrix) invert() (matrix, bool) {
	det := m[0]*m[3] - m[1]*m[2]
	if det == 0 || math.IsNaN(det) || math.IsInf(det, 0) {
		return identity, false
	}
	id := 1 / det
	return matrix{
		m[3] * id, -m[1] * id,
		-m[2] * id, m[0] * id,
		(m[2]*m[5] - m[3]*m[4]) * id,
		(m[1]*m[4] - m[0]*m[5]) * id,
	}, true
}

// rasterDevice 把解譯結果畫成點陣圖。
type rasterDevice struct {
	img  *image.RGBA
	w, h int
	x    *model.XRefTable
	doc  *Doc

	scanner *rasterx.ScannerGV
	filler  *rasterx.Filler
	dasher  *rasterx.Dasher
	// offX / offY 是目前這一次繪製的座標平移量。
	offX, offY float64

	// glyphs 是字型的外框來源,查過的留著。
	glyphs *glyphCache
	// substituted 是改用系統字型補畫的字型,missing 是連補都補不出來的。
	// 兩者都要回報:畫面看起來正常,但字形不是原檔的那一套。
	substituted map[string]bool
	missing     map[string]bool
}

func newRasterDevice(d *Doc, w, h int) *rasterDevice {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	draw.Draw(img, img.Bounds(), image.NewUniform(color.White), image.Point{}, draw.Src)
	sc := rasterx.NewScannerGV(w, h, img, img.Bounds())
	dev := &rasterDevice{
		img: img, w: w, h: h, x: d.ctx.XRefTable, doc: d,
		scanner:     sc,
		filler:      rasterx.NewFiller(w, h, sc),
		dasher:      rasterx.NewDasher(w, h, sc),
		glyphs:      newGlyphCache(),
		substituted: map[string]bool{},
		missing:     map[string]bool{},
	}
	return dev
}

func (d *rasterDevice) graphics() bool { return true }

// clipRectOf 把裁剪範圍換成像素矩形。沒有裁剪時就是整張圖。
func (d *rasterDevice) clipRectOf(gs *gstate) image.Rectangle {
	if !gs.clip.set {
		return d.img.Bounds()
	}
	return d.img.Bounds().Intersect(image.Rect(
		int(math.Floor(gs.clip.x0)), int(math.Floor(gs.clip.y0)),
		int(math.Ceil(gs.clip.x1))+1, int(math.Ceil(gs.clip.y1))+1))
}

// drawPath 把一條路徑畫上去。
//
// [雷] 光柵器每次 Draw 都掃過**整張目的地影像**,而不是只掃路徑所在的
// 那一小塊。一頁有幾千個字,每個字掃一次整頁的話,一頁要畫好幾分鐘,
// 而畫出來的結果完全正確 —— 只有慢,看不出哪裡錯。
// 所以每一次都把目的地換成路徑外接矩形的子影像,並把座標一起平移過去。
func (d *rasterDevice) drawPath(p *path, col color.RGBA, clip image.Rectangle, pad float64, draw func()) {
	if p.empty() {
		return
	}
	x0, y0, x1, y1 := p.bounds()
	if math.IsInf(x0, 0) || math.IsInf(y0, 0) {
		return
	}
	r := image.Rect(
		int(math.Floor(x0-pad)), int(math.Floor(y0-pad)),
		int(math.Ceil(x1+pad))+1, int(math.Ceil(y1+pad))+1,
	).Intersect(clip)
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
	d.scanner.SetColor(col)
	d.offX, d.offY = float64(-r.Min.X), float64(-r.Min.Y)
	draw()
}

// paint 畫一條路徑。
func (d *rasterDevice) paint(p *path, gs *gstate, fill, stroke, evenOdd bool) {
	if p.empty() {
		return
	}
	clip := d.clipRectOf(gs)
	if fill && gs.fillCS != csPatternCS {
		// 奇偶填法只有在路徑不只一圈的時候才與非零繞組不同。
		// 單圈的情形走原本那條快路 —— 真實檔案裡絕大多數的填色是
		// 單一個方框,沒必要為它多配兩張遮罩。
		if evenOdd && p.subpaths() > 1 {
			d.fillEvenOdd(p, gs.fill.rgba(gs.fillAlpha), clip)
		} else {
			d.drawPath(p, gs.fill.rgba(gs.fillAlpha), clip, 1, func() {
				addPath(d.filler, p, d.offX, d.offY)
				d.filler.Draw()
				d.filler.Clear()
			})
		}
	}
	if stroke && gs.strokeCS != csPatternCS {
		w := gs.lineWidth * gs.ctm.scale()
		// 線寬 0 在 PDF 是「畫得出來的最細」,不是不畫。
		if w < 0.8 {
			w = 0.8
		}
		d.drawPath(p, gs.stroke.rgba(gs.strokeAlpha), clip, w/2+1, func() {
			d.dasher.SetStroke(
				fixed.Int26_6(w*64), fixed.Int26_6(gs.miterLimit*64),
				capOf(gs.lineCap), capOf(gs.lineCap), rasterx.RoundGap, joinOf(gs.lineJoin),
				scaleDash(gs.dash, gs.ctm.scale()), gs.dashPhase*gs.ctm.scale())
			addPath(d.dasher, p, d.offX, d.offY)
			d.dasher.Draw()
			d.dasher.Clear()
		})
	}
}

// fillEvenOdd 用奇偶規則填一條路徑。
//
// 光柵器本身只會非零繞組(它的 SetWinding 是空的),所以改用奇偶規則的
// 定義來算:一個點在裡面,等於「包住它的圈數是奇數」。把每一圈各自
// 畫進一張遮罩,再逐像素做互斥或,結果就是奇偶填法。
//
// 這個等式在「每一圈自己不交叉」時成立,而那是實際檔案裡的常態
// (字形的內外圈、甜甜圈、挖空的圖示)。自己交叉的單一圈會退化成
// 非零繞組的結果 —— 那是這個做法的邊界,不是隨機的錯。
func (d *rasterDevice) fillEvenOdd(p *path, col color.RGBA, clip image.Rectangle) {
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
		// 每一圈單獨畫。這裡用的是光柵器原本的非零繞組 —— 對單一圈
		// 來說,那就是「在這一圈裡面」。
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
			if a := row[x-r.Min.X]; a > 0 {
				c := col
				c.A = uint8(int(col.A) * int(a) / 255)
				d.blend(x, y, c, 1)
			}
		}
	}
}

// xorAlpha 把兩張覆蓋率做互斥或:a ← a + b − 2ab。
//
// 邊緣的覆蓋率是 0 到 1 之間的小數,不是非黑即白。用機率式的互斥或
// 才能讓內外圈相接的地方平順 —— 直接拿整數 XOR 會在邊緣留下雜點。
func xorAlpha(dst, src []byte) {
	for i := range dst {
		if i >= len(src) {
			return
		}
		a, b := int(dst[i]), int(src[i])
		dst[i] = byte(a + b - 2*a*b/255)
	}
}

func scaleDash(dash []float64, s float64) []float64 {
	if len(dash) == 0 {
		return nil
	}
	out := make([]float64, len(dash))
	for i, v := range dash {
		out[i] = v * s
	}
	return out
}

func capOf(n int) rasterx.CapFunc {
	switch n {
	case 1:
		return rasterx.RoundCap
	case 2:
		return rasterx.SquareCap
	}
	return rasterx.ButtCap
}

func joinOf(n int) rasterx.JoinMode {
	switch n {
	case 1:
		return rasterx.Round
	case 2:
		return rasterx.Bevel
	}
	return rasterx.Miter
}

// addPath 把一條路徑餵給光柵器,座標同時平移 (dx, dy)。
func addPath(a rasterx.Adder, p *path, dx, dy float64) {
	pt := func(x, y float64) fixed.Point26_6 {
		return fixed.Point26_6{X: fixed.Int26_6((x + dx) * 64), Y: fixed.Int26_6((y + dy) * 64)}
	}
	started := false
	for _, o := range p.ops {
		switch o.op {
		case 'm':
			if started {
				a.Stop(false)
			}
			a.Start(pt(o.p[0].x, o.p[0].y))
			started = true
		case 'l':
			if !started {
				a.Start(pt(o.p[0].x, o.p[0].y))
				started = true
				continue
			}
			a.Line(pt(o.p[0].x, o.p[0].y))
		case 'c':
			if !started {
				a.Start(pt(o.p[0].x, o.p[0].y))
				started = true
			}
			a.CubeBezier(pt(o.p[0].x, o.p[0].y), pt(o.p[1].x, o.p[1].y), pt(o.p[2].x, o.p[2].y))
		case 'h':
			if started {
				a.Stop(true)
				started = false
			}
		}
	}
	if started {
		a.Stop(false)
	}
}

// image 把一張影像畫上去。
//
// 影像本身佔的是「單位正方形」,由 CTM 決定它落在哪裡、多大、
// 有沒有翻轉或旋轉。所以畫法是反過來的:走過目的地的每一個像素,
// 用反矩陣問「這一點對應到影像裡的哪裡」。正向掃描來源會在放大時
// 留下沒填到的洞。
func (d *rasterDevice) image(sd *types.StreamDict, gs *gstate) {
	m, alpha, err := d.decodeImage(sd, gs)
	if err != nil || m == nil {
		return
	}
	inv, ok := gs.ctm.invert()
	if !ok {
		return
	}
	// 目的地範圍:單位正方形四個角變換之後的外接矩形。
	x0, y0 := math.Inf(1), math.Inf(1)
	x1, y1 := math.Inf(-1), math.Inf(-1)
	for _, c := range [4][2]float64{{0, 0}, {1, 0}, {0, 1}, {1, 1}} {
		x, y := gs.ctm.apply(c[0], c[1])
		x0, y0 = math.Min(x0, x), math.Min(y0, y)
		x1, y1 = math.Max(x1, x), math.Max(y1, y)
	}
	lo, hi := image.Pt(int(math.Floor(x0)), int(math.Floor(y0))),
		image.Pt(int(math.Ceil(x1)), int(math.Ceil(y1)))
	r := image.Rectangle{lo, hi}.Intersect(d.img.Bounds())
	if gs.clip.set {
		r = r.Intersect(image.Rect(
			int(math.Floor(gs.clip.x0)), int(math.Floor(gs.clip.y0)),
			int(math.Ceil(gs.clip.x1)), int(math.Ceil(gs.clip.y1))))
	}
	if r.Empty() {
		return
	}

	b := m.Bounds()
	fillCol := gs.fill.rgba(1)
	for py := r.Min.Y; py < r.Max.Y; py++ {
		for px := r.Min.X; px < r.Max.X; px++ {
			u, v := inv.apply(float64(px)+0.5, float64(py)+0.5)
			if u < 0 || u >= 1 || v < 0 || v >= 1 {
				continue
			}
			// 影像的第一列在單位正方形的**上緣**,所以縱向要翻過來。
			sx := b.Min.X + int(u*float64(b.Dx()))
			sy := b.Min.Y + int((1-v)*float64(b.Dy()))
			if sx >= b.Max.X {
				sx = b.Max.X - 1
			}
			if sy >= b.Max.Y {
				sy = b.Max.Y - 1
			}
			var c color.RGBA
			if alpha {
				// 遮罩影像:有墨的地方塗填色,其餘透明。
				if _, _, _, a := m.At(sx, sy).RGBA(); a < 0x8000 {
					continue
				}
				c = fillCol
			} else {
				cr, cg, cb, ca := m.At(sx, sy).RGBA()
				if ca == 0 {
					continue
				}
				c = color.RGBA{uint8(cr >> 8), uint8(cg >> 8), uint8(cb >> 8), uint8(ca >> 8)}
			}
			d.blend(px, py, c, gs.fillAlpha)
		}
	}
}

// blend 把一個像素混上去。
func (d *rasterDevice) blend(x, y int, c color.RGBA, alpha float64) {
	a := float64(c.A) / 255 * alpha
	if a <= 0 {
		return
	}
	i := d.img.PixOffset(x, y)
	p := d.img.Pix[i : i+4 : i+4]
	mix := func(dst uint8, src uint8) uint8 {
		return uint8(math.Round(float64(src)*a + float64(dst)*(1-a)))
	}
	p[0] = mix(p[0], c.R)
	p[1] = mix(p[1], c.G)
	p[2] = mix(p[2], c.B)
	p[3] = 255
}

// decodeImage 把影像串流變成可以取樣的影像。
//
// 交給 pdfcpu:色彩空間、位元深度、Decode 陣列、軟遮罩的組合有幾十種,
// 而它已經把那些收斂成「一張 png 或 jpg」。第二個回傳值表示這是不是
// 只有形狀的遮罩影像(那種要用當時的填色塗上去)。
func (d *rasterDevice) decodeImage(sd *types.StreamDict, gs *gstate) (m image.Image, isMask bool, err error) {
	defer func() {
		if r := recover(); r != nil {
			m, err = nil, fmt.Errorf("這張影像解不開(%v)", r)
		}
	}()
	if v, ok := deref(d.x, sd.Dict["ImageMask"]).(types.Boolean); ok && bool(v) {
		isMask = true
	}
	rd, _, err := pdfcpulib.RenderImage(d.x, sd, false, "img", 0)
	if err != nil || rd == nil {
		return nil, isMask, fmt.Errorf("影像取不出來")
	}
	data, err := io.ReadAll(io.LimitReader(rd, MaxImageBytes))
	if err != nil || len(data) == 0 {
		return nil, isMask, fmt.Errorf("影像是空的")
	}
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, isMask, err
	}
	return img, isMask, nil
}

// MaxImageBytes 是單張影像的上限。
const MaxImageBytes = 64 << 20
