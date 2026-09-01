package pdf

// Type2 charstring 是一台堆疊機:運算元先推進去,遇到運算子才動作。
// 除了畫線與貝茲曲線,還有子程式呼叫與「提示」(hint)。
//
// [雷] 提示指令對畫面沒有影響(那是給低解析度的字形微調用的),但它們的
// 運算元一定要正確消化掉,而且 hintmask 後面還跟著一段位元遮罩,長度由
// 前面宣告過幾組 stem 決定。少吃一個位元組,後面整串座標就錯位 ——
// 而錯位之後畫出來仍然是某種形狀,不是空白。
//
// [雷] 每個字形的第一個「清空堆疊」的運算子可能多帶一個前導運算元,
// 那是字寬。多出來的那一個不丟掉的話,整個字形會平移一段距離。

// gseg 是字形外框上的一段,座標是千分之一字身、Y 軸向上。
//
// 兩種字型格式(TrueType 與 CFF)解出來的東西都換成這一種,
// 上層畫的時候就不必知道它是從哪一種來的。
type gseg struct {
	op   byte // 'm' 起點,'l' 直線,'c' 三次貝茲
	x, y [3]float64
}

// MaxCharstringDepth 是子程式呼叫的巢狀上限。
const MaxCharstringDepth = 10

type t2 struct {
	f     *cffFont
	subrs [][]byte

	stack  []float64
	out    []gseg
	x, y   float64
	nStems int
	width  bool // 字寬那一個運算元處理過了沒
	depth  int
	done   bool
}

// glyph 解出一個字形的外框。
func (f *cffFont) glyph(gid int) ([]gseg, bool) {
	if gid < 0 || gid >= len(f.charStrings) {
		return nil, false
	}
	t := &t2{f: f, subrs: f.subrs}
	if f.isCID && gid < len(f.fdSelect) {
		if fd := int(f.fdSelect[gid]); fd < len(f.fdSubrs) {
			t.subrs = f.fdSubrs[fd]
		}
	}
	t.run(f.charStrings[gid])
	if len(t.out) == 0 {
		return nil, false
	}
	// 字形座標換算到千分之一字身。多數字型的 FontMatrix 就是 1/1000,
	// 這時候是恆等變換;不是的(例如 1/2048)靠這一步扶正。
	m := f.matrix
	if m[0] == 0.001 && m[1] == 0 && m[2] == 0 && m[3] == 0.001 && m[4] == 0 && m[5] == 0 {
		return t.out, true
	}
	for i := range t.out {
		for j := 0; j < 3; j++ {
			x, y := t.out[i].x[j], t.out[i].y[j]
			t.out[i].x[j] = (m[0]*x + m[2]*y + m[4]) * 1000
			t.out[i].y[j] = (m[1]*x + m[3]*y + m[5]) * 1000
		}
	}
	return t.out, true
}

func bias(n int) int {
	switch {
	case n < 1240:
		return 107
	case n < 33900:
		return 1131
	}
	return 32768
}

func (t *t2) moveTo(x, y float64) {
	t.out = append(t.out, gseg{op: 'm', x: [3]float64{x}, y: [3]float64{y}})
}

func (t *t2) lineTo(x, y float64) {
	t.out = append(t.out, gseg{op: 'l', x: [3]float64{x}, y: [3]float64{y}})
}

func (t *t2) curveTo(x1, y1, x2, y2, x3, y3 float64) {
	t.out = append(t.out, gseg{op: 'c',
		x: [3]float64{x1, x2, x3}, y: [3]float64{y1, y2, y3}})
}

// takeWidth 丟掉第一個「清空堆疊」的運算子多帶的字寬運算元。
// even 為真表示這個運算子的運算元本來應該是偶數個。
func (t *t2) takeWidth(want int, even bool) {
	if t.width {
		return
	}
	t.width = true
	n := len(t.stack)
	if (even && n%2 == 1) || (!even && n > want) {
		t.stack = t.stack[1:]
	}
}

func (t *t2) clear() { t.stack = t.stack[:0] }

func (t *t2) run(cs []byte) {
	if t.depth > MaxCharstringDepth || t.done {
		return
	}
	for i := 0; i < len(cs) && !t.done; {
		b0 := int(cs[i])
		if b0 >= 32 || b0 == 28 {
			v, n := t2Number(cs[i:])
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
		switch b0 {
		case 1, 3, 18, 23: // hstem vstem hstemhm vstemhm
			t.takeWidth(0, true)
			t.nStems += len(t.stack) / 2
			t.clear()
		case 19, 20: // hintmask cntrmask
			t.takeWidth(0, true)
			t.nStems += len(t.stack) / 2
			t.clear()
			i += (t.nStems + 7) / 8
		case 21: // rmoveto
			t.takeWidth(2, false)
			if len(t.stack) >= 2 {
				t.x += t.stack[0]
				t.y += t.stack[1]
			}
			t.moveTo(t.x, t.y)
			t.clear()
		case 22: // hmoveto
			t.takeWidth(1, false)
			if len(t.stack) >= 1 {
				t.x += t.stack[0]
			}
			t.moveTo(t.x, t.y)
			t.clear()
		case 4: // vmoveto
			t.takeWidth(1, false)
			if len(t.stack) >= 1 {
				t.y += t.stack[0]
			}
			t.moveTo(t.x, t.y)
			t.clear()
		case 5: // rlineto
			for j := 0; j+1 < len(t.stack); j += 2 {
				t.x += t.stack[j]
				t.y += t.stack[j+1]
				t.lineTo(t.x, t.y)
			}
			t.clear()
		case 6, 7: // hlineto vlineto:橫豎交替
			horiz := b0 == 6
			for _, d := range t.stack {
				if horiz {
					t.x += d
				} else {
					t.y += d
				}
				t.lineTo(t.x, t.y)
				horiz = !horiz
			}
			t.clear()
		case 8: // rrcurveto
			for j := 0; j+5 < len(t.stack); j += 6 {
				t.rrcurve(t.stack[j : j+6])
			}
			t.clear()
		case 24: // rcurveline
			j := 0
			for ; j+5 < len(t.stack)-2; j += 6 {
				t.rrcurve(t.stack[j : j+6])
			}
			if j+1 < len(t.stack) {
				t.x += t.stack[j]
				t.y += t.stack[j+1]
				t.lineTo(t.x, t.y)
			}
			t.clear()
		case 25: // rlinecurve
			j := 0
			for ; len(t.stack)-j > 6; j += 2 {
				t.x += t.stack[j]
				t.y += t.stack[j+1]
				t.lineTo(t.x, t.y)
			}
			if j+5 < len(t.stack) {
				t.rrcurve(t.stack[j : j+6])
			}
			t.clear()
		case 26: // vvcurveto
			j := 0
			dx := 0.0
			if len(t.stack)%4 == 1 {
				dx = t.stack[0]
				j = 1
			}
			for ; j+3 < len(t.stack); j += 4 {
				x1 := t.x + dx
				y1 := t.y + t.stack[j]
				x2 := x1 + t.stack[j+1]
				y2 := y1 + t.stack[j+2]
				t.x = x2
				t.y = y2 + t.stack[j+3]
				t.curveTo(x1, y1, x2, y2, t.x, t.y)
				dx = 0
			}
			t.clear()
		case 27: // hhcurveto
			j := 0
			dy := 0.0
			if len(t.stack)%4 == 1 {
				dy = t.stack[0]
				j = 1
			}
			for ; j+3 < len(t.stack); j += 4 {
				x1 := t.x + t.stack[j]
				y1 := t.y + dy
				x2 := x1 + t.stack[j+1]
				y2 := y1 + t.stack[j+2]
				t.x = x2 + t.stack[j+3]
				t.y = y2
				t.curveTo(x1, y1, x2, y2, t.x, t.y)
				dy = 0
			}
			t.clear()
		case 30, 31: // vhcurveto hvcurveto:每一段換一個方向
			horiz := b0 == 31
			j := 0
			for j+3 < len(t.stack) {
				last := j+8 > len(t.stack)
				extra := 0.0
				if last && j+5 == len(t.stack) {
					extra = t.stack[j+4]
				}
				var x1, y1, x2, y2 float64
				if horiz {
					x1, y1 = t.x+t.stack[j], t.y
					x2, y2 = x1+t.stack[j+1], y1+t.stack[j+2]
					t.y = y2 + t.stack[j+3]
					t.x = x2 + extra
				} else {
					x1, y1 = t.x, t.y+t.stack[j]
					x2, y2 = x1+t.stack[j+1], y1+t.stack[j+2]
					t.x = x2 + t.stack[j+3]
					t.y = y2 + extra
				}
				t.curveTo(x1, y1, x2, y2, t.x, t.y)
				horiz = !horiz
				j += 4
			}
			t.clear()
		case 10, 29: // callsubr callgsubr
			list := t.subrs
			if b0 == 29 {
				list = t.f.gsubrs
			}
			if len(t.stack) == 0 {
				t.clear()
				break
			}
			idx := int(t.stack[len(t.stack)-1]) + bias(len(list))
			t.stack = t.stack[:len(t.stack)-1]
			if idx >= 0 && idx < len(list) {
				t.depth++
				t.run(list[idx])
				t.depth--
			}
		case 11: // return
			return
		case 14: // endchar
			t.takeWidth(0, false)
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

// rrcurve 依六個相對量畫一段曲線。
func (t *t2) rrcurve(a []float64) {
	x1 := t.x + a[0]
	y1 := t.y + a[1]
	x2 := x1 + a[2]
	y2 := y1 + a[3]
	t.x = x2 + a[4]
	t.y = y2 + a[5]
	t.curveTo(x1, y1, x2, y2, t.x, t.y)
}

// escape 處理兩位元組的運算子。要的只有四個 flex —— 那是「用兩段曲線
// 表示一段很平的曲線」,不畫的話字形會缺一塊。
func (t *t2) escape(b1 int) {
	s := t.stack
	switch b1 {
	case 35: // flex
		if len(s) >= 13 {
			t.rrcurve(s[0:6])
			t.rrcurve(s[6:12])
		}
	case 34: // hflex
		if len(s) >= 7 {
			y0 := t.y
			t.rrcurve([]float64{s[0], 0, s[1], s[2], s[3], 0})
			x1 := t.x + s[4]
			y1 := t.y
			x2 := x1 + s[5]
			y2 := y0
			t.x = x2 + s[6]
			t.y = y0
			t.curveTo(x1, y1, x2, y2, t.x, t.y)
		}
	case 36: // hflex1
		if len(s) >= 9 {
			y0 := t.y
			t.rrcurve([]float64{s[0], s[1], s[2], s[3], s[4], 0})
			x1 := t.x + s[5]
			y1 := t.y
			x2 := x1 + s[6]
			y2 := y1 + s[7]
			t.x = x2 + s[8]
			t.y = y0
			t.curveTo(x1, y1, x2, y2, t.x, t.y)
		}
	case 37: // flex1
		if len(s) >= 11 {
			x0, y0 := t.x, t.y
			dx := s[0] + s[2] + s[4] + s[6] + s[8]
			dy := s[1] + s[3] + s[5] + s[7] + s[9]
			t.rrcurve(s[0:6])
			x1 := t.x + s[6]
			y1 := t.y + s[7]
			x2 := x1 + s[8]
			y2 := y1 + s[9]
			// 最後一個點只給一個座標,另一個由「整段的位移」反推。
			if absf(dx) > absf(dy) {
				t.x = x2 + s[10]
				t.y = y0
			} else {
				t.x = x0
				t.y = y2 + s[10]
			}
			t.curveTo(x1, y1, x2, y2, t.x, t.y)
		}
	}
	t.clear()
}

func absf(v float64) float64 {
	if v < 0 {
		return -v
	}
	return v
}

// t2Number 讀一個運算元,回傳值與吃掉幾個位元組。
func t2Number(b []byte) (float64, int) {
	c := int(b[0])
	switch {
	case c == 28:
		if len(b) < 3 {
			return 0, 0
		}
		return float64(int16(uint16(b[1])<<8 | uint16(b[2]))), 3
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
		// 16.16 的定點數。
		v := int32(uint32(b[1])<<24 | uint32(b[2])<<16 | uint32(b[3])<<8 | uint32(b[4]))
		return float64(v) / 65536, 5
	}
	return 0, 0
}
