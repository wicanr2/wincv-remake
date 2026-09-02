package imgfmt

import (
	"image"
	"image/color"
	"math"
	"strconv"
	"strings"

	"github.com/srwiley/rasterx"
	"golang.org/x/image/colornames"
	"golang.org/x/image/math/fixed"

	"github.com/wicanr2/wincv-remake/internal/vecfont"
)

// matrix2D 是二維仿射變換。與 rasterx 的同構,自己留一份是為了讓
// 解析那一段不必碰繪圖套件(才測得動)。
type matrix2D struct{ a, b, c, d, e, f float64 }

var identity2D = matrix2D{1, 0, 0, 1, 0, 0}

// mult 回 m·n:先套 n 再套 m。
func (m matrix2D) mult(n matrix2D) matrix2D {
	return matrix2D{
		a: m.a*n.a + m.c*n.b,
		b: m.b*n.a + m.d*n.b,
		c: m.a*n.c + m.c*n.d,
		d: m.b*n.c + m.d*n.d,
		e: m.a*n.e + m.c*n.f + m.e,
		f: m.b*n.e + m.d*n.f + m.f,
	}
}

func (m matrix2D) apply(x, y float64) (float64, float64) {
	return x*m.a + y*m.c + m.e, x*m.b + y*m.d + m.f
}

// scale 是這個變換平均放大多少倍。畫粗體時的筆畫寬度要照它換算。
func (m matrix2D) scale() float64 { return math.Sqrt(math.Abs(m.a*m.d - m.b*m.c)) }

func (m matrix2D) translate(x, y float64) matrix2D {
	return m.mult(matrix2D{1, 0, 0, 1, x, y})
}

func (m matrix2D) scaled(x, y float64) matrix2D {
	return m.mult(matrix2D{x, 0, 0, y, 0, 0})
}

// parseSVGTransform 解 transform 屬性。寫在前面的先套用在座標上,
// 所以照書寫順序依序右乘。
func parseSVGTransform(s string) matrix2D {
	m := identity2D
	rest := strings.TrimSpace(s)
	for rest != "" {
		open := strings.Index(rest, "(")
		if open < 0 {
			break
		}
		name := strings.TrimSpace(strings.Trim(rest[:open], " ,\t\n"))
		close := strings.Index(rest[open:], ")")
		if close < 0 {
			break
		}
		args := numList(rest[open+1 : open+close])
		rest = strings.TrimSpace(rest[open+close+1:])
		switch name {
		case "translate":
			if len(args) >= 2 {
				m = m.translate(args[0], args[1])
			} else if len(args) == 1 {
				m = m.translate(args[0], 0)
			}
		case "scale":
			switch len(args) {
			case 1:
				m = m.scaled(args[0], args[0])
			default:
				if len(args) >= 2 {
					m = m.scaled(args[0], args[1])
				}
			}
		case "rotate":
			if len(args) >= 1 {
				th := args[0] * math.Pi / 180
				r := matrix2D{math.Cos(th), math.Sin(th), -math.Sin(th), math.Cos(th), 0, 0}
				if len(args) >= 3 {
					m = m.translate(args[1], args[2]).mult(r).translate(-args[1], -args[2])
				} else {
					m = m.mult(r)
				}
			}
		case "skewX":
			if len(args) >= 1 {
				m = m.mult(matrix2D{1, 0, math.Tan(args[0] * math.Pi / 180), 1, 0, 0})
			}
		case "skewY":
			if len(args) >= 1 {
				m = m.mult(matrix2D{1, math.Tan(args[0] * math.Pi / 180), 0, 1, 0, 0})
			}
		case "matrix":
			if len(args) >= 6 {
				m = m.mult(matrix2D{args[0], args[1], args[2], args[3], args[4], args[5]})
			}
		}
	}
	return m
}

func numList(s string) []float64 {
	fields := strings.FieldsFunc(s, func(r rune) bool {
		return r == ' ' || r == ',' || r == '\t' || r == '\n' || r == '\r'
	})
	out := make([]float64, 0, len(fields))
	for _, f := range fields {
		if v, err := strconv.ParseFloat(f, 64); err == nil {
			out = append(out, v)
		}
	}
	return out
}

// parseSVGColor 解 #rgb / #rrggbb / rgb() / rgba() / 具名顏色。
// 漸層(url(#...))解不出來,回假 —— 那時文字沿用繼承來的顏色,
// 總比畫成黑色好。
func parseSVGColor(s string) (color.RGBA, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return color.RGBA{}, false
	}
	if s[0] == '#' {
		h := s[1:]
		switch len(h) {
		case 3, 4:
			v, err := strconv.ParseUint(h[:3], 16, 32)
			if err != nil {
				return color.RGBA{}, false
			}
			r := uint8(v>>8) & 0xf
			g := uint8(v>>4) & 0xf
			b := uint8(v) & 0xf
			return color.RGBA{r * 17, g * 17, b * 17, 255}, true
		case 6, 8:
			v, err := strconv.ParseUint(h[:6], 16, 32)
			if err != nil {
				return color.RGBA{}, false
			}
			return color.RGBA{uint8(v >> 16), uint8(v >> 8), uint8(v), 255}, true
		}
		return color.RGBA{}, false
	}
	low := strings.ToLower(s)
	if strings.HasPrefix(low, "rgb(") || strings.HasPrefix(low, "rgba(") {
		inner := low[strings.Index(low, "(")+1:]
		inner = strings.TrimSuffix(strings.TrimSpace(inner), ")")
		parts := strings.FieldsFunc(inner, func(r rune) bool { return r == ',' || r == ' ' || r == '/' })
		if len(parts) < 3 {
			return color.RGBA{}, false
		}
		ch := func(i int) uint8 {
			p := strings.TrimSpace(parts[i])
			pct := strings.HasSuffix(p, "%")
			f, err := strconv.ParseFloat(strings.TrimSuffix(p, "%"), 64)
			if err != nil {
				return 0
			}
			if pct {
				f = f * 255 / 100
			}
			return uint8(math.Round(math.Min(math.Max(f, 0), 255)))
		}
		c := color.RGBA{ch(0), ch(1), ch(2), 255}
		if len(parts) >= 4 {
			if f, err := strconv.ParseFloat(strings.TrimSuffix(strings.TrimSpace(parts[3]), "%"), 64); err == nil {
				if strings.HasSuffix(parts[3], "%") {
					f /= 100
				}
				c.A = uint8(math.Round(math.Min(math.Max(f, 0), 1) * 255))
			}
		}
		return c, true
	}
	if c, ok := colornames.Map[low]; ok {
		return color.RGBA{c.R, c.G, c.B, c.A}, true
	}
	return color.RGBA{}, false
}

// drawSVGText 把文字塊畫到已經有路徑的圖上。m 是使用者座標到畫面的變換,
// 取自 oksvg 自己算的那一份 —— 用同一份才會與長條、格線對齊。
//
// 回傳畫了幾個字塊、以及有幾個字沒有字形。
func drawSVGText(img *image.RGBA, chunks []svgChunk, m matrix2D) (drawn, missing int) {
	if len(chunks) == 0 {
		return 0, 0
	}
	set := vecfont.Default()
	if !set.Ready() {
		// 一個字型都沒有就畫不了。回報出去,不要靜靜地留白。
		for _, c := range chunks {
			for _, p := range c.pieces {
				missing += len([]rune(p.text))
			}
		}
		return 0, missing
	}
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	scanner := rasterx.NewScannerGV(w, h, img, b)
	filler := rasterx.NewFiller(w, h, scanner)
	stroker := rasterx.NewStroker(w, h, scanner)

	for _, c := range chunks {
		base := m.mult(c.tm)
		width := 0.0
		for _, p := range c.pieces {
			width += pieceWidth(set, p)
		}
		x := c.x
		switch c.anchor {
		case "middle":
			x -= width / 2
		case "end":
			x -= width
		}
		y := c.y + baselineShift(c)
		any := false
		for _, p := range c.pieces {
			for _, r := range p.text {
				segs, adv, ok := set.Glyph(r)
				if !ok {
					missing++
					// 沒有字形也要留一格,不然後面的字會擠上來。
					adv = float64(vecfont.Em) * 0.5
				}
				k := p.size / vecfont.Em
				if len(segs) > 0 && p.visible && p.fill.A > 0 {
					gm := base.translate(x, y).scaled(k, k)
					if p.italic {
						gm = gm.mult(matrix2D{1, 0, math.Tan(12 * math.Pi / 180), 1, 0, 0})
					}
					scanner.SetColor(p.fill)
					addGlyph(filler, segs, gm)
					filler.Draw()
					filler.Clear()
					if p.bold {
						// 沒有真正的粗體字檔就描一圈邊。字重不會與原檔
						// 完全相同,但「這一行是標題」看得出來。
						sw := 0.045 * p.size * base.scale()
						if sw < 0.6 {
							sw = 0.6
						}
						stroker.SetStroke(fixed.Int26_6(sw*64), 4,
							rasterx.RoundCap, rasterx.RoundCap, rasterx.RoundGap, rasterx.Round)
						addGlyph(stroker, segs, gm)
						stroker.Draw()
						stroker.Clear()
					}
					any = true
				}
				x += adv*k + p.spacing
			}
		}
		if any {
			drawn++
		}
	}
	return drawn, missing
}

// baselineShift 把基線挪到 dominant-baseline 指的位置。
// 係數是常見的近似值:字型自己的量測要載入 OS/2 表,而各家字型
// 給的值本來就不一致,差別小於這裡的用途所需的精度。
func baselineShift(c svgChunk) float64 {
	size := 0.0
	if len(c.pieces) > 0 {
		size = c.pieces[0].size
	}
	switch c.baseline {
	case "middle", "central":
		return 0.35 * size
	case "hanging", "text-before-edge":
		return 0.8 * size
	case "text-after-edge", "ideographic":
		return -0.2 * size
	}
	return 0
}

func pieceWidth(set *vecfont.Set, p svgPiece) float64 {
	w := 0.0
	for _, r := range p.text {
		_, adv, ok := set.Glyph(r)
		if !ok {
			adv = float64(vecfont.Em) * 0.5
		}
		w += adv*p.size/vecfont.Em + p.spacing
	}
	return w
}

// addGlyph 把一個字的外框餵給光柵器。
func addGlyph(r rasterx.Adder, segs []vecfont.Seg, m matrix2D) {
	pt := func(x, y float64) fixed.Point26_6 {
		dx, dy := m.apply(x, y)
		return fixed.Point26_6{X: fixed.Int26_6(dx * 64), Y: fixed.Int26_6(dy * 64)}
	}
	open := false
	for _, s := range segs {
		switch s.Op {
		case 'm':
			if open {
				r.Stop(true)
			}
			r.Start(pt(s.Args[0], s.Args[1]))
			open = true
		case 'l':
			r.Line(pt(s.Args[0], s.Args[1]))
		case 'q':
			r.QuadBezier(pt(s.Args[0], s.Args[1]), pt(s.Args[2], s.Args[3]))
		case 'c':
			r.CubeBezier(pt(s.Args[0], s.Args[1]), pt(s.Args[2], s.Args[3]), pt(s.Args[4], s.Args[5]))
		}
	}
	if open {
		r.Stop(true)
	}
}
