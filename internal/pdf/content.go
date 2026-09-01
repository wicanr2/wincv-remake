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

// gstate 是繪圖狀態裡與文字有關的部分。
type gstate struct {
	ctm       matrix
	font      *Font
	size      float64
	charSpace float64
	wordSpace float64
	hscale    float64
	leading   float64
	rise      float64
	render    int
}

type interp struct {
	doc   *Doc
	x     *model.XRefTable
	out   []Text
	gs    gstate
	stack []gstate
	tm    matrix
	tlm   matrix
	depth int
}

// MaxTexts 是一頁最多取幾個字。
//
// 上限不是為了記憶體,是為了「某些檔案會用一整頁的文字畫出一張圖」——
// 那種頁面有幾十萬個字,而它們排出來不是文章。
const MaxTexts = 300000

// Texts 取出一頁上的所有字。
func (p *Page) Texts() []Text {
	in := &interp{doc: p.doc, x: p.doc.ctx.XRefTable}
	in.gs = gstate{ctm: identity, hscale: 1}
	in.tm, in.tlm = identity, identity
	in.run(p.content(), p.res)
	return in.out
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
		case "BT":
			in.tm, in.tlm = identity, identity
		case "ET":
		case "Tf":
			in.gs.size = num(0)
			if len(ops) >= 2 && ops[len(ops)-2].kind == vName {
				in.gs.font = in.lookupFont(res, ops[len(ops)-2].str)
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

func (in *interp) nextLine() {
	in.tlm = mul(matrix{1, 0, 0, 1, 0, -in.gs.leading}, in.tlm)
	in.tm = in.tlm
}

// show 畫一個字串:把每個字的位置算出來。
func (in *interp) show(s string) {
	f := in.gs.font
	if f == nil || len(in.out) >= MaxTexts {
		return
	}
	for _, g := range f.Decode(s) {
		trm := mul(matrix{in.gs.size * in.gs.hscale, 0, 0, in.gs.size, 0, in.gs.rise},
			mul(in.tm, in.gs.ctm))
		x0, y0 := trm[4], trm[5]

		w0 := g.Width / 1000 * in.gs.size
		adv := w0 + in.gs.charSpace
		if g.Space {
			// [雷] 字間距只加在單位元組的空白上。複合字型的兩位元組碼
			// 就算低位是 0x20 也不算 —— 加下去會讓整段中文的字距散開。
			adv += in.gs.wordSpace
		}
		in.tm = mul(matrix{1, 0, 0, 1, adv * in.gs.hscale, 0}, in.tm)

		if g.Text == "" {
			continue
		}
		next := mul(matrix{in.gs.size * in.gs.hscale, 0, 0, in.gs.size, 0, in.gs.rise},
			mul(in.tm, in.gs.ctm))
		w := next[4] - x0
		if w < 0 {
			w = -w
		}
		in.out = append(in.out, Text{
			X: x0, Y: y0, W: w,
			Size: math.Hypot(trm[2], trm[3]),
			S:    g.Text,
		})
		if len(in.out) >= MaxTexts {
			return
		}
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
	if nameOf(deref(in.x, sd.Dict["Subtype"])) != "Form" {
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
