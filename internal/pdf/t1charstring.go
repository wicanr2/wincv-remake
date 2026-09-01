package pdf

// Type1 的 charstring 與 Type2 長得像但不是同一套:運算元的個數是固定的
// (Type2 允許重複),數字的編碼也不同(255 後面是整數,不是定點數)。
//
// [雷] 曲線的「flex」與提示替換不是獨立的運算子,而是用 callothersubr
// 呼叫外部子程式做的。不處理的話,flex 中間那七個 rmoveto 會被當成
// 七次「開新的一圈」—— 字形會裂成一堆碎片,而每一片都是合法的形狀。
type t1 struct {
	f     *type1Font
	stack []float64
	out   []gseg
	x, y  float64
	depth int
	done  bool

	// flex 的狀態。
	inFlex  bool
	flexPts []point
	// ps 是 callothersubr 回推、等著被 pop 取走的值。
	ps []float64
	// sbx 是這個字形自己的左邊界(hsbw 的第一個運算元)。seac 要用它
	// 算重音的位置,而那時候 x 早就被移到別的地方了。
	sbx float64
}

func (t *t1) moveTo(x, y float64) {
	t.out = append(t.out, gseg{op: 'm', x: [3]float64{x}, y: [3]float64{y}})
}

func (t *t1) lineTo(x, y float64) {
	t.out = append(t.out, gseg{op: 'l', x: [3]float64{x}, y: [3]float64{y}})
}

func (t *t1) curveTo(x1, y1, x2, y2, x3, y3 float64) {
	t.out = append(t.out, gseg{op: 'c',
		x: [3]float64{x1, x2, x3}, y: [3]float64{y1, y2, y3}})
}

func (t *t1) clear() { t.stack = t.stack[:0] }

func (t *t1) run(cs []byte) {
	if t.depth > MaxCharstringDepth || t.done {
		return
	}
	for i := 0; i < len(cs) && !t.done; {
		b0 := int(cs[i])
		if b0 >= 32 || b0 == 255 {
			v, n := t1Number(cs[i:])
			if n == 0 {
				return
			}
			if len(t.stack) < 48 {
				t.stack = append(t.stack, v)
			}
			i += n
			continue
		}
		i++
		s := t.stack
		switch b0 {
		case 13: // hsbw:左邊界與字寬
			if len(s) >= 1 {
				t.x = s[0]
				t.sbx = s[0]
			}
			t.clear()
		case 9: // closepath
			// 每一圈的收尾由上層在開新的一圈時處理,這裡不必做事。
			t.clear()
		case 1, 3: // hstem vstem
			t.clear()
		case 21: // rmoveto
			if len(s) >= 2 {
				t.x += s[0]
				t.y += s[1]
			}
			t.emitMove()
			t.clear()
		case 22: // hmoveto
			if len(s) >= 1 {
				t.x += s[0]
			}
			t.emitMove()
			t.clear()
		case 4: // vmoveto
			if len(s) >= 1 {
				t.y += s[0]
			}
			t.emitMove()
			t.clear()
		case 5: // rlineto
			if len(s) >= 2 {
				t.x += s[0]
				t.y += s[1]
				t.lineTo(t.x, t.y)
			}
			t.clear()
		case 6: // hlineto
			if len(s) >= 1 {
				t.x += s[0]
				t.lineTo(t.x, t.y)
			}
			t.clear()
		case 7: // vlineto
			if len(s) >= 1 {
				t.y += s[0]
				t.lineTo(t.x, t.y)
			}
			t.clear()
		case 8: // rrcurveto
			if len(s) >= 6 {
				t.rrcurve(s[:6])
			}
			t.clear()
		case 30: // vhcurveto
			if len(s) >= 4 {
				t.rrcurve([]float64{0, s[0], s[1], s[2], s[3], 0})
			}
			t.clear()
		case 31: // hvcurveto
			if len(s) >= 4 {
				t.rrcurve([]float64{s[0], 0, s[1], s[2], 0, s[3]})
			}
			t.clear()
		case 10: // callsubr
			if len(s) == 0 {
				break
			}
			idx := int(s[len(s)-1])
			t.stack = t.stack[:len(t.stack)-1]
			if idx >= 0 && idx < len(t.f.subrs) && t.f.subrs[idx] != nil {
				t.depth++
				t.run(t.f.subrs[idx])
				t.depth--
			}
		case 11: // return
			return
		case 14: // endchar
			t.done = true
			return
		case 12:
			if i >= len(cs) {
				return
			}
			b1 := int(cs[i])
			i++
			t.escape(b1)
		default:
			t.clear()
		}
	}
}

// emitMove 處理一次移動。flex 進行中的話,那是曲線上的取樣點而不是新的一圈。
func (t *t1) emitMove() {
	if t.inFlex {
		t.flexPts = append(t.flexPts, point{t.x, t.y})
		return
	}
	t.moveTo(t.x, t.y)
}

func (t *t1) rrcurve(a []float64) {
	x1 := t.x + a[0]
	y1 := t.y + a[1]
	x2 := x1 + a[2]
	y2 := y1 + a[3]
	t.x = x2 + a[4]
	t.y = y2 + a[5]
	t.curveTo(x1, y1, x2, y2, t.x, t.y)
}

func (t *t1) escape(b1 int) {
	s := t.stack
	switch b1 {
	case 12: // div
		if len(s) >= 2 && s[len(s)-1] != 0 {
			v := s[len(s)-2] / s[len(s)-1]
			t.stack = append(t.stack[:len(t.stack)-2], v)
		}
		return
	case 16: // callothersubr
		t.otherSubr()
		return
	case 17: // pop
		v := 0.0
		if n := len(t.ps); n > 0 {
			v = t.ps[n-1]
			t.ps = t.ps[:n-1]
		}
		t.stack = append(t.stack, v)
		return
	case 33: // setcurrentpoint
		if len(s) >= 2 {
			t.x, t.y = s[0], s[1]
		}
	case 7: // sbw
		if len(s) >= 2 {
			t.x, t.y = s[0], s[1]
		}
	case 6: // seac:用兩個標準字形拼出重音字。
		if len(s) >= 5 {
			t.seac(s[0], s[1], s[2], int(s[3]), int(s[4]))
		}
	case 0, 1, 2: // dotsection vstem3 hstem3:提示,對畫面沒有影響
	}
	t.clear()
}

// otherSubr 處理 callothersubr。堆疊上是「參數… 參數個數 子程式編號」。
func (t *t1) otherSubr() {
	if len(t.stack) < 2 {
		t.clear()
		return
	}
	idx := int(t.stack[len(t.stack)-1])
	n := int(t.stack[len(t.stack)-2])
	t.stack = t.stack[:len(t.stack)-2]
	var args []float64
	if n > 0 && n <= len(t.stack) {
		args = append(args, t.stack[len(t.stack)-n:]...)
		t.stack = t.stack[:len(t.stack)-n]
	}
	switch idx {
	case 1: // flex 開始
		t.inFlex = true
		t.flexPts = t.flexPts[:0]
	case 2: // flex 進行中,每一個點呼叫一次
	case 0: // flex 結束:收集到的七個點變成兩段曲線
		t.inFlex = false
		if len(t.flexPts) >= 7 {
			p := t.flexPts[len(t.flexPts)-7:]
			t.curveTo(p[1].x, p[1].y, p[2].x, p[2].y, p[3].x, p[3].y)
			t.curveTo(p[4].x, p[4].y, p[5].x, p[5].y, p[6].x, p[6].y)
			t.x, t.y = p[6].x, p[6].y
		}
		// 後面固定跟著兩個 pop 與一個 setcurrentpoint。
		t.ps = append(t.ps[:0], t.y, t.x)
	case 3: // 提示替換:後面跟著一個 pop
		t.ps = append(t.ps[:0], 3)
	default:
		// 不認得的:參數原樣回推,讓後面的 pop 取回去。
		t.ps = t.ps[:0]
		for i := len(args) - 1; i >= 0; i-- {
			t.ps = append(t.ps, args[i])
		}
	}
}

// t1Number 讀一個運算元。與 Type2 的差別在 255:這裡後面是 32 位元整數。
func t1Number(b []byte) (float64, int) {
	c := int(b[0])
	switch {
	case c >= 32 && c <= 246:
		return float64(c - 139), 1
	case c >= 247 && c <= 250:
		if len(b) < 2 {
			return 0, 0
		}
		return float64((c-247)*256 + int(b[1]) + 108), 2
	case c >= 251 && c <= 254:
		if len(b) < 2 {
			return 0, 0
		}
		return float64(-(c-251)*256 - int(b[1]) - 108), 2
	case c == 255:
		if len(b) < 5 {
			return 0, 0
		}
		v := int32(uint32(b[1])<<24 | uint32(b[2])<<16 | uint32(b[3])<<8 | uint32(b[4]))
		return float64(v), 5
	}
	return 0, 0
}

// seac 把兩個標準字形拼成一個重音字。
//
// 運算元是「重音的左邊界、水平位移、垂直位移、基底字、重音字」,而後兩個
// 是 **StandardEncoding 的字碼**,不是字形編號 —— 拿它們當編號用會取到
// 完全無關的字,而畫出來仍然是一個像樣的字形。
//
// 位移要扣掉重音自己的左邊界再補上這個字的:兩個字形各自的座標都是從
// 自己的左邊界起算的,不校正的話重音會偏掉一個邊界的寬度。
func (t *t1) seac(asb, adx, ady float64, bchar, achar int) {
	base, ok1 := standardName(bchar)
	accent, ok2 := standardName(achar)
	if !ok1 || !ok2 {
		return
	}
	t.out = t.out[:0]
	t.appendGlyph(base, 0, 0)
	t.appendGlyph(accent, t.sbx-asb+adx, ady)
	t.done = true
}

// appendGlyph 把另一個字形的外框平移後接到這一條上。
func (t *t1) appendGlyph(name string, dx, dy float64) {
	cs, ok := t.f.chars[name]
	if !ok || len(cs) == 0 {
		return
	}
	sub := &t1{f: t.f, depth: t.depth + 1}
	sub.run(cs)
	for _, g := range sub.out {
		n := 1
		if g.op == 'c' {
			n = 3
		}
		for i := 0; i < n; i++ {
			g.x[i] += dx
			g.y[i] += dy
		}
		t.out = append(t.out, g)
	}
}
