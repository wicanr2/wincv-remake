package pdf

import (
	"bytes"
	"fmt"
	"github.com/wicanr2/wincv-remake/internal/i18n"
	"image"
	"image/color"
	"image/draw"
	"image/jpeg"
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

	// mask 是目前生效的軟遮罩(裝置座標的 alpha 圖),nil 表示沒有。
	// 畫之前由各個繪製入口從 gstate 取過來 —— blend 拿不到 gstate。
	mask *image.Alpha
	// layers 是暫時畫布的堆疊。透明群組與軟遮罩都要先畫到別的地方,
	// 再整批合成回去。
	layers []*image.RGBA

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
	if d.mask == nil {
		d.scanner.SetColor(col)
	} else {
		// [雷] 顏色函式收到的是**來源影像的座標**,而來源的取樣起點是
		// Offset。不設成子影像的左上角,遮罩會整片位移到別的地方,
		// 而畫出來仍然是一塊有濃淡的東西,看不出位移。
		d.scanner.SetColor(d.maskFunc(func(int, int) color.Color { return col }))
		d.scanner.Offset = r.Min
	}
	d.offX, d.offY = float64(-r.Min.X), float64(-r.Min.Y)
	draw()
	d.scanner.Offset = image.Point{}
}

// paint 畫一條路徑。
func (d *rasterDevice) paint(p *path, gs *gstate, fill, stroke, evenOdd bool) {
	d.use(gs)
	if p.empty() {
		return
	}
	clip := d.clipRectOf(gs)
	if fill && gs.fillCS == csPatternCS && gs.fillShade != nil {
		d.fillShading(p, gs.fillShade, gs.fillShadeM, gs, evenOdd)
	}
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
				// 覆蓋率當係數傳進去 —— 顏色是預乘的,只改 A 會讓
				// 各通道與 alpha 對不起來。
				d.blend(x, y, col, float64(a)/255)
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
	m, isMask, err := d.decodeImage(sd, gs)
	if err != nil || m == nil {
		return
	}
	d.paintImage(m, isMask, gs)
}

// inline 畫一張內嵌影像。解碼在解譯器那一層做完了(它才拿得到頁面資源),
// 擺放的規則與一般影像完全相同,所以走同一段。
func (d *rasterDevice) inline(m image.Image, isMask bool, gs *gstate) {
	d.paintImage(m, isMask, gs)
}

// paintImage 把一張已經解好的影像貼到頁面上。
func (d *rasterDevice) paintImage(m image.Image, alpha bool, gs *gstate) {
	d.use(gs)
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

// blend 把一個像素混上去。c 是**預乘**的顏色,k 是再乘上去的係數
// (覆蓋率、常數透明度、軟遮罩)。
func (d *rasterDevice) blend(x, y int, c color.RGBA, k float64) {
	if m := d.mask; m != nil {
		if !(image.Point{x, y}).In(m.Rect) {
			return
		}
		k *= float64(m.Pix[m.PixOffset(x, y)]) / 255
	}
	if k <= 0 || c.A == 0 {
		return
	}
	// 蓋掉多少 = 來源的 alpha 再乘上係數。預乘的來源各通道也要乘同一個係數。
	a := float64(c.A) / 255 * k
	i := d.img.PixOffset(x, y)
	p := d.img.Pix[i : i+4 : i+4]
	mix := func(dst uint8, src uint8) uint8 {
		return uint8(math.Round(math.Min(float64(src)*k+float64(dst)*(1-a), 255)))
	}
	p[0] = mix(p[0], c.R)
	p[1] = mix(p[1], c.G)
	p[2] = mix(p[2], c.B)
	// 目的地不一定是不透明的:暫時畫布(群組與遮罩)一開始是全透明的,
	// 而「畫了什麼」正是靠這個通道記下來的。頁面本身是白底不透明,
	// 這條算式在那裡的結果仍然是 255。
	p[3] = mix(p[3], c.A)
}

// decodeImage 把影像串流變成可以取樣的影像。
//
// 交給 pdfcpu:色彩空間、位元深度、Decode 陣列、軟遮罩的組合有幾十種,
// 而它已經把那些收斂成「一張 png 或 jpg」。第二個回傳值表示這是不是
// 只有形狀的遮罩影像(那種要用當時的填色塗上去)。
func (d *rasterDevice) decodeImage(sd *types.StreamDict, gs *gstate) (m image.Image, isMask bool, err error) {
	defer func() {
		if r := recover(); r != nil {
			m, err = nil, fmt.Errorf(i18n.T("這張影像解不開(%v)"), r)
		}
	}()
	if v, ok := deref(d.x, sd.Dict["ImageMask"]).(types.Boolean); ok && bool(v) {
		isMask = true
	}
	rd, _, err := pdfcpulib.RenderImage(d.x, sd, false, "img", 0)
	if err == nil && rd != nil {
		data, err := io.ReadAll(io.LimitReader(rd, MaxImageBytes))
		if err == nil && len(data) > 0 {
			if img, _, err := image.Decode(bytes.NewReader(data)); err == nil {
				return img, isMask, nil
			}
		}
	}
	// 退路:自己解。
	//
	// [雷] 物件層對某些組合(實測:ICCBased 色彩空間,不論 DCTDecode 或
	// FlateDecode)會**回空但不報錯**,而那張圖就整塊不見 —— 版面其餘部分
	// 完全正常,看起來像那一頁本來就沒有圖。arXiv 論文那一類的插圖全是這種。
	if img := d.rawImage(sd, isMask); img != nil {
		return img, isMask, nil
	}
	return nil, isMask, fmt.Errorf(i18n.T("影像取不出來"))
}

// rawImage 自己把影像串流解成影像。
//
// 兩條路:只經過 DCTDecode 的串流本身就是一份完整的 JPEG;其餘的濾鏡
// 物件層會解掉,剩下的是原始取樣值,那一段與內嵌影像走同一個解碼器。
func (d *rasterDevice) rawImage(sd *types.StreamDict, isMask bool) image.Image {
	w := int(numOrZero(d.x, sd.Dict["Width"]))
	h := int(numOrZero(d.x, sd.Dict["Height"]))
	if w <= 0 || h <= 0 || w > 1<<15 || h > 1<<15 || w*h > MaxImagePixels {
		return nil
	}
	var img image.Image
	if onlyDCT(d.x, sd.Dict["Filter"]) {
		if len(sd.Raw) == 0 {
			return nil
		}
		m, err := jpeg.Decode(bytes.NewReader(sd.Raw))
		if err != nil {
			return nil
		}
		img = m
	} else {
		data := streamContent(sd)
		if len(data) == 0 {
			return nil
		}
		if isMask {
			img = maskImage(data, w, h, imageDecodeFlipped(d.x, sd.Dict))
		} else {
			bpc := int(numOrZero(d.x, sd.Dict["BitsPerComponent"]))
			if bpc <= 0 || bpc > 16 {
				return nil
			}
			cs := parseColorSpace(d.x, deref(d.x, sd.Dict["ColorSpace"]), 0)
			img = samplesImage(data, w, h, bpc, cs)
		}
	}
	if isMask {
		return img
	}
	return d.applyImageSMask(img, sd)
}

// applyImageSMask 把影像自己的 /SMask 當成透明度貼上去。
//
// 沒有這一步的話,去背的插圖會帶著一整塊不透明的方形背景蓋住底下的東西 ——
// 而圖本身是對的,所以看起來像「排版擠掉了」而不是「透明度沒做」。
func (d *rasterDevice) applyImageSMask(img image.Image, sd *types.StreamDict) image.Image {
	msd, _, err := d.x.DereferenceStreamDict(sd.Dict["SMask"])
	if err != nil || msd == nil {
		return img
	}
	mw := int(numOrZero(d.x, msd.Dict["Width"]))
	mh := int(numOrZero(d.x, msd.Dict["Height"]))
	if mw <= 0 || mh <= 0 || mw*mh > MaxImagePixels {
		return img
	}
	var mask image.Image
	if onlyDCT(d.x, msd.Dict["Filter"]) && len(msd.Raw) > 0 {
		m, err := jpeg.Decode(bytes.NewReader(msd.Raw))
		if err != nil {
			return img
		}
		mask = m
	} else {
		data := streamContent(msd)
		bpc := int(numOrZero(d.x, msd.Dict["BitsPerComponent"]))
		if len(data) == 0 || bpc <= 0 || bpc > 16 {
			return img
		}
		mask = samplesImage(data, mw, mh, bpc, csDeviceGray)
	}
	b := img.Bounds()
	out := image.NewRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
	for y := 0; y < b.Dy(); y++ {
		// 遮罩的尺寸不一定與影像相同,規格允許,要各自縮放。
		my := y * mh / b.Dy()
		for x := 0; x < b.Dx(); x++ {
			mx := x * mw / b.Dx()
			a, _, _, _ := mask.At(mx, my).RGBA()
			k := float64(a>>8) / 255
			cr, cg, cb, _ := img.At(b.Min.X+x, b.Min.Y+y).RGBA()
			// 預乘。
			out.SetRGBA(x, y, color.RGBA{
				uint8(float64(cr>>8) * k), uint8(float64(cg>>8) * k),
				uint8(float64(cb>>8) * k), uint8(255 * k),
			})
		}
	}
	return out
}

// streamContent 取串流解過濾鏡之後的位元組。
func streamContent(sd *types.StreamDict) []byte {
	if len(sd.Content) == 0 {
		if err := sd.Decode(); err != nil {
			return nil
		}
	}
	return sd.Content
}

// imageDecodeFlipped 看遮罩影像的 Decode 陣列有沒有把黑白對調。
func imageDecodeFlipped(x *model.XRefTable, d types.Dict) bool {
	dec := floatsOf(x, d["Decode"])
	return len(dec) >= 2 && dec[0] == 1
}

func onlyDCT(x *model.XRefTable, o types.Object) bool {
	switch v := deref(x, o).(type) {
	case types.Name:
		return v.Value() == "DCTDecode"
	case types.Array:
		return len(v) == 1 && nameOf(deref(x, v[0])) == "DCTDecode"
	}
	return false
}

// MaxImageBytes 是單張影像的上限。
const MaxImageBytes = 64 << 20

// MaxImagePixels 是自己解影像時的像素上限。標頭裡的寬高是檔案說了算的,
// 不設限的話一個宣告 40000×40000 的檔案就會吃掉幾 GB。
const MaxImagePixels = 64 << 20
