package pdf

import (
	"math"
	"strconv"

	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"
)

// Text 是頁面上的一個字。
//
// 一個字一筆而不是一段一筆:PDF 的一次「顯示字串」可以橫跨好幾個欄位,
// 也可以只有半個詞,那個切法是排版器的方便,不是文字的結構。
// 位置留在每個字上,後面才有辦法重建列與欄。
type Text struct {
	// X / Y 是字的原點,PDF 座標(由左下往右上長)。
	X, Y float64
	// W 是這個字佔多寬。
	W float64
	// Size 是換算過各層變換之後的實際字級。
	Size float64
	S    string
}

// matrix 是 PDF 的 3×2 變換矩陣:a b c d e f。
type matrix [6]float64

var identity = matrix{1, 0, 0, 1, 0, 0}

// mul 算 m × n。順序不能顛倒 —— PDF 的變換是由內而外套上去的,
// 反過來會得到一個看起來合理但位置全錯的版面。
func mul(m, n matrix) matrix {
	return matrix{
		m[0]*n[0] + m[1]*n[2],
		m[0]*n[1] + m[1]*n[3],
		m[2]*n[0] + m[3]*n[2],
		m[2]*n[1] + m[3]*n[3],
		m[4]*n[0] + m[5]*n[2] + n[4],
		m[4]*n[1] + m[5]*n[3] + n[5],
	}
}

// apply 把一個點套上變換。
func (m matrix) apply(x, y float64) (float64, float64) {
	return m[0]*x + m[2]*y + m[4], m[1]*x + m[3]*y + m[5]
}

// scale 是這個變換讓長度變成幾倍(取面積的平方根,對非等向的變換是近似)。
func (m matrix) scale() float64 {
	return math.Sqrt(math.Abs(m[0]*m[3] - m[1]*m[2]))
}

// point 是裝置座標上的一個點。
type point struct{ x, y float64 }

// pathOp 是路徑上的一段。曲線一律是三次貝茲(PDF 只有這一種)。
type pathOp struct {
	op byte // 'm' 起點,'l' 直線,'c' 曲線,'h' 收尾
	p  [3]point
}

// path 是一條建構中或已完成的路徑。點都已經套過 CTM,是裝置座標。
//
// 在建構時就套變換是規格要求的:同一條路徑中間不能改 CTM,而 CTM
// 在畫的時候可能已經被 q/Q 換掉了。留到畫的時候才套會畫到別的地方。
type path struct {
	ops []pathOp
}

func (p *path) empty() bool { return len(p.ops) == 0 }

func (p *path) moveTo(x, y float64) {
	p.ops = append(p.ops, pathOp{op: 'm', p: [3]point{{x, y}}})
}

func (p *path) lineTo(x, y float64) {
	p.ops = append(p.ops, pathOp{op: 'l', p: [3]point{{x, y}}})
}

func (p *path) curveTo(x1, y1, x2, y2, x3, y3 float64) {
	p.ops = append(p.ops, pathOp{op: 'c', p: [3]point{{x1, y1}, {x2, y2}, {x3, y3}}})
}

func (p *path) close() {
	p.ops = append(p.ops, pathOp{op: 'h'})
}

// bounds 是路徑的外接矩形。裁剪用得到。
func (p *path) bounds() (x0, y0, x1, y1 float64) {
	x0, y0 = math.Inf(1), math.Inf(1)
	x1, y1 = math.Inf(-1), math.Inf(-1)
	for _, o := range p.ops {
		n := 1
		switch o.op {
		case 'h':
			n = 0
		case 'c':
			n = 3
		}
		for i := 0; i < n; i++ {
			x0, y0 = math.Min(x0, o.p[i].x), math.Min(y0, o.p[i].y)
			x1, y1 = math.Max(x1, o.p[i].x), math.Max(y1, o.p[i].y)
		}
	}
	return
}

// gstate 是繪圖狀態。
type gstate struct {
	ctm matrix

	// 文字
	font      *Font
	fontRef   string
	size      float64
	charSpace float64
	wordSpace float64
	hscale    float64
	leading   float64
	rise      float64
	render    int

	// 顏色
	fill, stroke     rgb
	fillCS, strokeCS *colorSpace

	// 線條
	lineWidth  float64
	lineCap    int
	lineJoin   int
	miterLimit float64
	dash       []float64
	dashPhase  float64

	// 透明度
	fillAlpha, strokeAlpha float64

	// clip 是目前的裁剪範圍(裝置座標的矩形)。
	clip clipRect
}

// clipRect 是矩形的裁剪範圍。
//
// 只做矩形是刻意的取捨:PDF 的裁剪可以是任意路徑,但實際檔案裡
// 九成以上是 `re W n`(把內容框在一個方框裡)。非矩形的裁剪取它的
// 外接矩形 —— 那會多顯示一點東西,但不會少顯示;反過來(整個不畫)
// 才是使用者看得出來的錯。
type clipRect struct {
	set            bool
	x0, y0, x1, y1 float64
}

func (c clipRect) intersect(x0, y0, x1, y1 float64) clipRect {
	if !c.set {
		return clipRect{true, x0, y0, x1, y1}
	}
	return clipRect{true,
		math.Max(c.x0, x0), math.Max(c.y0, y0),
		math.Min(c.x1, x1), math.Min(c.y1, y1)}
}

// device 是解譯結果的去處。
//
// 取文字與畫頁面走的是同一條解譯路徑 —— 文字的定位是整份規格裡最容易
// 寫錯的一段(六種矩陣互相相乘),兩邊各寫一次的話,兩份會慢慢分岔,
// 而分岔的症狀是「畫出來的位置跟抽出來的位置不一樣」,很難查。
type device interface {
	// glyph 處理一個字。trm 是字的完整變換矩陣。
	glyph(g Glyph, f *Font, trm matrix, gs *gstate)
	// paint 畫一條路徑。
	paint(p *path, gs *gstate, fill, stroke, evenOdd bool)
	// image 畫一張影像。影像本身佔的是單位正方形,由 CTM 決定它到哪裡。
	image(sd *types.StreamDict, gs *gstate)
	// graphics 回答要不要送圖形指令。取文字時回 false,可以省掉路徑建構。
	graphics() bool
}

type interp struct {
	doc   *Doc
	x     *model.XRefTable
	dev   device
	gs    gstate
	stack []gstate
	tm    matrix
	tlm   matrix
	depth int

	// 路徑建構中的狀態。
	cur      path
	curX     float64
	curY     float64
	startX   float64
	startY   float64
	pendClip int // 0 沒有,1 非零繞組,2 奇偶
}

// MaxTexts 是一頁最多取幾個字。
//
// 上限不是為了記憶體,是為了「某些檔案會用一整頁的文字畫出一張圖」——
// 那種頁面有幾十萬個字,而它們排出來不是文章。
const MaxTexts = 300000

// textDevice 把解譯結果收成一串字,不畫任何東西。
type textDevice struct {
	out []Text
}

func (d *textDevice) graphics() bool                         { return false }
func (d *textDevice) paint(*path, *gstate, bool, bool, bool) {}
func (d *textDevice) image(*types.StreamDict, *gstate)       {}

func (d *textDevice) glyph(g Glyph, f *Font, trm matrix, gs *gstate) {
	if g.Text == "" || len(d.out) >= MaxTexts {
		return
	}
	d.out = append(d.out, Text{
		X: trm[4], Y: trm[5], W: g.advance,
		Size: math.Hypot(trm[2], trm[3]),
		S:    g.Text,
	})
}

// Texts 取出一頁上的所有字。
func (p *Page) Texts() []Text {
	dev := &textDevice{}
	p.interpret(dev, identity)
	return dev.out
}

// interpret 用給定的基底變換解讀一頁。
func (p *Page) interpret(dev device, base matrix) {
	in := &interp{doc: p.doc, x: p.doc.ctx.XRefTable, dev: dev}
	in.gs = gstate{ctm: base, hscale: 1, lineWidth: 1, miterLimit: 10,
		fillAlpha: 1, strokeAlpha: 1, fill: rgb{0, 0, 0}, stroke: rgb{0, 0, 0}}
	in.tm, in.tlm = identity, identity
	in.run(p.content(), p.res)
}

// run 解讀一段內容資料流。
func (in *interp) run(b []byte, res types.Dict) {
	if in.depth > 8 || len(b) == 0 {
		return
	}
	l := &lexer{b: b}
	var ops []value
	push := func(v value) {
		if len(ops) < 64 {
			ops = append(ops, v)
		}
	}
	num := func(i int) float64 {
		// 運算元由後往前數:多餘的運算元留在前面,少了的話補零。
		if i < len(ops) && ops[len(ops)-1-i].kind == vNum {
			return ops[len(ops)-1-i].num
		}
		return 0
	}
	name := func(i int) string {
		if i < len(ops) && ops[len(ops)-1-i].kind == vName {
			return ops[len(ops)-1-i].str
		}
		return ""
	}
	for {
		v, ok := l.next()
		if !ok {
			return
		}
		if v.kind != vOp {
			push(v)
			continue
		}
		switch v.str {
		// --- 繪圖狀態 ---
		case "q":
			in.stack = append(in.stack, in.gs)
		case "Q":
			if n := len(in.stack); n > 0 {
				in.gs = in.stack[n-1]
				in.stack = in.stack[:n-1]
			}
		case "cm":
			m := matrix{num(5), num(4), num(3), num(2), num(1), num(0)}
			in.gs.ctm = mul(m, in.gs.ctm)
		case "w":
			in.gs.lineWidth = num(0)
		case "J":
			in.gs.lineCap = int(num(0))
		case "j":
			in.gs.lineJoin = int(num(0))
		case "M":
			in.gs.miterLimit = num(0)
		case "d":
			in.setDash(ops)
		case "gs":
			in.extGState(res, name(0))

		// --- 路徑 ---
		case "m":
			in.pathMoveTo(num(1), num(0))
		case "l":
			in.pathLineTo(num(1), num(0))
		case "c":
			in.pathCurveTo(num(5), num(4), num(3), num(2), num(1), num(0))
		case "v":
			// 第一個控制點就是目前的點。
			in.pathCurveTo(in.curX, in.curY, num(3), num(2), num(1), num(0))
		case "y":
			// 第二個控制點就是終點。
			in.pathCurveTo(num(3), num(2), num(1), num(0), num(1), num(0))
		case "h":
			in.pathClose()
		case "re":
			in.pathRect(num(3), num(2), num(1), num(0))
		case "n":
			in.endPath(false, false, false)
		case "S":
			in.endPath(false, true, false)
		case "s":
			in.pathClose()
			in.endPath(false, true, false)
		case "f", "F":
			in.endPath(true, false, false)
		case "f*":
			in.endPath(true, false, true)
		case "B":
			in.endPath(true, true, false)
		case "B*":
			in.endPath(true, true, true)
		case "b":
			in.pathClose()
			in.endPath(true, true, false)
		case "b*":
			in.pathClose()
			in.endPath(true, true, true)
		case "W":
			in.pendClip = 1
		case "W*":
			in.pendClip = 2

		// --- 顏色 ---
		case "g":
			in.gs.fillCS, in.gs.fill = csDeviceGray, gray(num(0))
		case "G":
			in.gs.strokeCS, in.gs.stroke = csDeviceGray, gray(num(0))
		case "rg":
			in.gs.fillCS, in.gs.fill = csDeviceRGB, rgb{num(2), num(1), num(0)}
		case "RG":
			in.gs.strokeCS, in.gs.stroke = csDeviceRGB, rgb{num(2), num(1), num(0)}
		case "k":
			in.gs.fillCS, in.gs.fill = csDeviceCMYK, cmyk(num(3), num(2), num(1), num(0))
		case "K":
			in.gs.strokeCS, in.gs.stroke = csDeviceCMYK, cmyk(num(3), num(2), num(1), num(0))
		case "cs":
			in.gs.fillCS = in.colorSpace(res, name(0))
			in.gs.fill = in.gs.fillCS.initial()
		case "CS":
			in.gs.strokeCS = in.colorSpace(res, name(0))
			in.gs.stroke = in.gs.strokeCS.initial()
		case "sc", "scn":
			in.gs.fill = in.gs.fillCS.color(numbers(ops))
		case "SC", "SCN":
			in.gs.stroke = in.gs.strokeCS.color(numbers(ops))

		// --- 文字 ---
		case "BT":
			in.tm, in.tlm = identity, identity
		case "ET":
		case "Tf":
			in.gs.size = num(0)
			if len(ops) >= 2 && ops[len(ops)-2].kind == vName {
				in.gs.fontRef = ops[len(ops)-2].str
				in.gs.font = in.lookupFont(res, in.gs.fontRef)
			}
		case "Td":
			in.tlm = mul(matrix{1, 0, 0, 1, num(1), num(0)}, in.tlm)
			in.tm = in.tlm
		case "TD":
			in.gs.leading = -num(0)
			in.tlm = mul(matrix{1, 0, 0, 1, num(1), num(0)}, in.tlm)
			in.tm = in.tlm
		case "Tm":
			in.tlm = matrix{num(5), num(4), num(3), num(2), num(1), num(0)}
			in.tm = in.tlm
		case "T*":
			in.nextLine()
		case "TL":
			in.gs.leading = num(0)
		case "Tc":
			in.gs.charSpace = num(0)
		case "Tw":
			in.gs.wordSpace = num(0)
		case "Tz":
			in.gs.hscale = num(0) / 100
		case "Ts":
			in.gs.rise = num(0)
		case "Tr":
			in.gs.render = int(num(0))
		case "Tj":
			if len(ops) > 0 && ops[len(ops)-1].kind == vStr {
				in.show(ops[len(ops)-1].str)
			}
		case "'":
			in.nextLine()
			if len(ops) > 0 && ops[len(ops)-1].kind == vStr {
				in.show(ops[len(ops)-1].str)
			}
		case "\"":
			in.gs.wordSpace = num(2)
			in.gs.charSpace = num(1)
			in.nextLine()
			if len(ops) > 0 && ops[len(ops)-1].kind == vStr {
				in.show(ops[len(ops)-1].str)
			}
		case "TJ":
			if len(ops) > 0 && ops[len(ops)-1].kind == vArray {
				for _, e := range ops[len(ops)-1].arr {
					switch e.kind {
					case vStr:
						in.show(e.str)
					case vNum:
						// 負數表示往前擠。這正是「PDF 裡沒有空格字元」
						// 的來源:詞與詞之間常常就是一個這樣的調整值。
						tx := -e.num / 1000 * in.gs.size * in.gs.hscale
						in.tm = mul(matrix{1, 0, 0, 1, tx, 0}, in.tm)
					}
				}
			}

		// --- 外部物件 ---
		case "Do":
			if len(ops) > 0 && ops[len(ops)-1].kind == vName {
				in.doXObject(res, ops[len(ops)-1].str)
			}
		case "BI":
			l.skipInlineImage()
		}
		ops = ops[:0]
	}
}

// numbers 取出運算元裡的數字,依原本的順序。
func numbers(ops []value) []float64 {
	out := make([]float64, 0, len(ops))
	for _, o := range ops {
		if o.kind == vNum {
			out = append(out, o.num)
		}
	}
	return out
}

func (in *interp) setDash(ops []value) {
	in.gs.dash, in.gs.dashPhase = nil, 0
	if len(ops) < 2 {
		return
	}
	if a := ops[len(ops)-2]; a.kind == vArray {
		for _, e := range a.arr {
			if e.kind == vNum && e.num >= 0 {
				in.gs.dash = append(in.gs.dash, e.num)
			}
		}
	}
	if p := ops[len(ops)-1]; p.kind == vNum {
		in.gs.dashPhase = p.num
	}
	// 全部是 0 的虛線陣列表示實線。照字面做的話一條線都畫不出來。
	allZero := true
	for _, d := range in.gs.dash {
		if d > 0 {
			allZero = false
		}
	}
	if allZero {
		in.gs.dash = nil
	}
}

// extGState 套用具名的繪圖狀態。要的只有透明度與線寬。
func (in *interp) extGState(res types.Dict, name string) {
	all, _ := deref(in.x, res["ExtGState"]).(types.Dict)
	if all == nil {
		return
	}
	d, _ := deref(in.x, all[name]).(types.Dict)
	if d == nil {
		return
	}
	if v, ok := numOf(deref(in.x, d["ca"])); ok {
		in.gs.fillAlpha = v
	}
	if v, ok := numOf(deref(in.x, d["CA"])); ok {
		in.gs.strokeAlpha = v
	}
	if v, ok := numOf(deref(in.x, d["LW"])); ok {
		in.gs.lineWidth = v
	}
}

// --- 路徑建構 ---

func (in *interp) pathMoveTo(x, y float64) {
	in.curX, in.curY = x, y
	in.startX, in.startY = x, y
	if !in.dev.graphics() {
		return
	}
	dx, dy := in.gs.ctm.apply(x, y)
	in.cur.moveTo(dx, dy)
}

func (in *interp) pathLineTo(x, y float64) {
	in.curX, in.curY = x, y
	if !in.dev.graphics() {
		return
	}
	dx, dy := in.gs.ctm.apply(x, y)
	in.cur.lineTo(dx, dy)
}

func (in *interp) pathCurveTo(x1, y1, x2, y2, x3, y3 float64) {
	in.curX, in.curY = x3, y3
	if !in.dev.graphics() {
		return
	}
	a1, b1 := in.gs.ctm.apply(x1, y1)
	a2, b2 := in.gs.ctm.apply(x2, y2)
	a3, b3 := in.gs.ctm.apply(x3, y3)
	in.cur.curveTo(a1, b1, a2, b2, a3, b3)
}

func (in *interp) pathClose() {
	in.curX, in.curY = in.startX, in.startY
	if in.dev.graphics() {
		in.cur.close()
	}
}

func (in *interp) pathRect(x, y, w, h float64) {
	in.pathMoveTo(x, y)
	in.pathLineTo(x+w, y)
	in.pathLineTo(x+w, y+h)
	in.pathLineTo(x, y+h)
	in.pathClose()
	in.curX, in.curY = x, y
}

// endPath 畫完並清掉目前的路徑,順便處理待決的裁剪。
func (in *interp) endPath(fill, stroke, evenOdd bool) {
	if in.dev.graphics() {
		if fill || stroke {
			in.dev.paint(&in.cur, &in.gs, fill, stroke, evenOdd)
		}
		if in.pendClip != 0 && !in.cur.empty() {
			// [雷] 裁剪是在畫完之後才生效,而且影響的是**接下來**的內容。
			// 提早套用會把這條路徑自己也裁掉。
			x0, y0, x1, y1 := in.cur.bounds()
			in.gs.clip = in.gs.clip.intersect(x0, y0, x1, y1)
		}
	}
	in.pendClip = 0
	in.cur = path{}
}

func (in *interp) nextLine() {
	in.tlm = mul(matrix{1, 0, 0, 1, 0, -in.gs.leading}, in.tlm)
	in.tm = in.tlm
}

// show 畫一個字串:把每個字的位置算出來。
func (in *interp) show(s string) {
	f := in.gs.font
	if f == nil {
		return
	}
	for _, g := range f.Decode(s) {
		trm := mul(matrix{in.gs.size * in.gs.hscale, 0, 0, in.gs.size, 0, in.gs.rise},
			mul(in.tm, in.gs.ctm))

		w0 := g.Width / 1000 * in.gs.size
		adv := w0 + in.gs.charSpace
		if g.Space {
			// [雷] 字間距只加在單位元組的空白上。複合字型的兩位元組碼
			// 就算低位是 0x20 也不算 —— 加下去會讓整段中文的字距散開。
			adv += in.gs.wordSpace
		}
		in.tm = mul(matrix{1, 0, 0, 1, adv * in.gs.hscale, 0}, in.tm)

		next := mul(matrix{in.gs.size * in.gs.hscale, 0, 0, in.gs.size, 0, in.gs.rise},
			mul(in.tm, in.gs.ctm))
		g.advance = math.Abs(next[4] - trm[4])
		in.dev.glyph(g, f, trm, &in.gs)
	}
}

// doXObject 進入一個外部物件。
//
// 表單物件裡是另一段內容資料流,而且很多產生器把整頁的內容都放在
// 裡面(頁首頁尾、浮水印、被重複使用的圖表)。不進去的話那些頁面
// 會完全沒有文字,而畫面上看起來只是「這一頁沒有可取出的文字」。
func (in *interp) doXObject(res types.Dict, name string) {
	xobjs, _ := deref(in.x, res["XObject"]).(types.Dict)
	if xobjs == nil {
		return
	}
	sd, _, err := in.x.DereferenceStreamDict(xobjs[name])
	if err != nil || sd == nil {
		return
	}
	switch nameOf(deref(in.x, sd.Dict["Subtype"])) {
	case "Image":
		if in.dev.graphics() {
			in.dev.image(sd, &in.gs)
		}
		return
	case "Form":
	default:
		return
	}
	if len(sd.Content) == 0 {
		if err := sd.Decode(); err != nil {
			return
		}
	}
	saved, savedTm, savedTlm := in.gs, in.tm, in.tlm
	savedStack := len(in.stack)
	if arr, ok := deref(in.x, sd.Dict["Matrix"]).(types.Array); ok && len(arr) == 6 {
		var m matrix
		for i := 0; i < 6; i++ {
			m[i], _ = numOf(deref(in.x, arr[i]))
		}
		in.gs.ctm = mul(m, in.gs.ctm)
	}
	if bb, ok := deref(in.x, sd.Dict["BBox"]).(types.Array); ok && len(bb) == 4 && in.dev.graphics() {
		var v [4]float64
		for i := 0; i < 4; i++ {
			v[i], _ = numOf(deref(in.x, bb[i]))
		}
		x0, y0 := in.gs.ctm.apply(v[0], v[1])
		x1, y1 := in.gs.ctm.apply(v[2], v[3])
		in.gs.clip = in.gs.clip.intersect(
			math.Min(x0, x1), math.Min(y0, y1), math.Max(x0, x1), math.Max(y0, y1))
	}
	sub, _ := deref(in.x, sd.Dict["Resources"]).(types.Dict)
	if sub == nil {
		// 沒有自己的資源就沿用外層的。規格允許這樣寫,而查不到字型
		// 的後果是整段文字消失。
		sub = res
	}
	in.depth++
	in.run(sd.Content, sub)
	in.depth--
	in.gs, in.tm, in.tlm = saved, savedTm, savedTlm
	if len(in.stack) > savedStack {
		in.stack = in.stack[:savedStack]
	}
}

// lookupFont 查一個字型資源,查過的留著。
func (in *interp) lookupFont(res types.Dict, name string) *Font {
	fonts, _ := deref(in.x, res["Font"]).(types.Dict)
	if fonts == nil {
		return nil
	}
	o := fonts[name]
	key := ""
	if ir, ok := o.(types.IndirectRef); ok {
		key = strconv.Itoa(ir.ObjectNumber.Value())
		if f, ok := in.doc.fonts[key]; ok {
			return f
		}
	}
	d, _ := deref(in.x, o).(types.Dict)
	if d == nil {
		return nil
	}
	f := loadFont(in.x, d)
	if key != "" {
		in.doc.fonts[key] = f
	}
	return f
}
