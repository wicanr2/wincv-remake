package imgfmt

import (
	"encoding/xml"
	"image/color"
	"io"
	"math"
	"strconv"
	"strings"
)

// SVG 的 <text> 由這一份自己畫。
//
// 底層的 oksvg 只認得路徑,碰到 <text> 就整個跳過。對圖示來說沒差,
// 對圖表來說差很多:標題、座標軸刻度、資料標籤全部不見,剩下一堆
// 無從解讀的長條 —— 而且畫面看起來是完整的,不像出錯。
//
// 所以這裡自己走一遍 XML,把文字的位置、字級、顏色與對齊算出來,
// 在 oksvg 畫完路徑之後疊上去。字形來源是 internal/vecfont(系統或內嵌
// 字型的外框),不是原檔指名的那一套 —— 位置與內容是對的,字形不是。

// svgPiece 是一段共用樣式的文字。
type svgPiece struct {
	text    string
	size    float64
	spacing float64
	bold    bool
	italic  bool
	fill    color.RGBA
	visible bool
}

// svgChunk 是 SVG 所謂的「文字塊」:一個絕對定位點加上跟在後面的文字。
// 對齊(text-anchor)是以塊為單位算的,所以塊必須是繪製的單位 ——
// 逐段對齊會讓每一段各自置中。
type svgChunk struct {
	x, y     float64
	anchor   string
	baseline string
	tm       matrix2D
	pieces   []svgPiece
}

// svgStyle 是會被子元素繼承的那些屬性。
type svgStyle struct {
	fill     color.RGBA
	hasFill  bool
	fillNone bool
	opacity  float64
	fillOp   float64
	size     float64
	weight   int
	italic   bool
	spacing  string
	anchor   string
	baseline string
	hidden   bool
}

func defaultSVGStyle() svgStyle {
	return svgStyle{
		fill: color.RGBA{0, 0, 0, 255}, hasFill: true,
		opacity: 1, fillOp: 1, size: 16, weight: 400, anchor: "start",
	}
}

// svgSkip 是不該畫出來的子樹。<defs> 之類的是「備用零件」,
// 由別處引用才會出現;<title>/<desc> 是說明文字,不是畫面內容。
var svgSkip = map[string]bool{
	"defs": true, "clipPath": true, "mask": true, "symbol": true,
	"marker": true, "pattern": true, "title": true, "desc": true,
	"style": true, "script": true, "metadata": true, "switch": true,
}

// parseSVGText 走一遍 SVG,收集所有要畫的文字塊。座標都在使用者空間,
// 換到畫面上由呼叫端負責。
func parseSVGText(d []byte) []svgChunk {
	dec := xml.NewDecoder(strings.NewReader(string(d)))
	dec.Strict = false
	dec.AutoClose = xml.HTMLAutoClose
	dec.Entity = xml.HTMLEntity

	var (
		chunks []svgChunk
		styles = []svgStyle{defaultSVGStyle()}
		ctms   = []matrix2D{identity2D}
		// depth 由零開始;skipAt 記下從哪一層起整個子樹都跳過。
		depth  int
		skipAt = -1
		// inText 記下 <text> 從哪一層開始,-1 表示不在文字裡。
		inText = -1
		cur    *svgChunk
		penX   float64
		penY   float64
	)

	flush := func() {
		if cur != nil && len(cur.pieces) > 0 {
			chunks = append(chunks, *cur)
		}
		cur = nil
	}

	for {
		tok, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			break
		}
		switch t := tok.(type) {
		case xml.StartElement:
			depth++
			parent := styles[len(styles)-1]
			pctm := ctms[len(ctms)-1]
			st := inheritSVGStyle(parent, t.Attr)
			ctm := pctm.mult(parseSVGTransform(attrOf(t.Attr, "transform")))
			styles = append(styles, st)
			ctms = append(ctms, ctm)
			if skipAt >= 0 {
				continue
			}
			if svgSkip[t.Name.Local] || st.hidden {
				skipAt = depth
				continue
			}
			switch t.Name.Local {
			case "text":
				flush()
				inText = depth
				penX = firstNum(attrOf(t.Attr, "x")) + firstNum(attrOf(t.Attr, "dx"))
				penY = firstNum(attrOf(t.Attr, "y")) + firstNum(attrOf(t.Attr, "dy"))
				cur = &svgChunk{x: penX, y: penY, anchor: st.anchor, baseline: st.baseline, tm: ctm}
			case "tspan":
				if inText < 0 {
					continue
				}
				ax, ay := attrOf(t.Attr, "x"), attrOf(t.Attr, "y")
				if ax != "" || ay != "" {
					// 絕對定位開一個新塊:對齊要從這裡重算。
					flush()
					if ax != "" {
						penX = firstNum(ax)
					}
					if ay != "" {
						penY = firstNum(ay)
					}
					cur = &svgChunk{x: penX, y: penY, anchor: st.anchor, baseline: st.baseline, tm: ctm}
				}
				if cur != nil {
					cur.x += firstNum(attrOf(t.Attr, "dx"))
					cur.y += firstNum(attrOf(t.Attr, "dy"))
				}
			}
		case xml.EndElement:
			if skipAt == depth {
				skipAt = -1
			}
			if inText == depth {
				flush()
				inText = -1
			}
			depth--
			if len(styles) > 1 {
				styles = styles[:len(styles)-1]
				ctms = ctms[:len(ctms)-1]
			}
		case xml.CharData:
			if skipAt >= 0 || inText < 0 || cur == nil {
				continue
			}
			s := collapseSpace(string(t))
			if s == "" {
				continue
			}
			st := styles[len(styles)-1]
			cur.pieces = append(cur.pieces, svgPiece{
				text:    s,
				size:    st.size,
				spacing: parseLength(st.spacing, st.size, 0),
				bold:    st.weight >= 600,
				italic:  st.italic,
				fill:    svgFillColor(st),
				visible: !st.fillNone,
			})
		}
	}
	flush()
	return trimChunks(chunks)
}

// trimChunks 去掉塊頭尾的空白。SVG 預設會把排版用的換行與縮排折起來,
// 折完剩下的頭尾空白不該佔位置 —— 留著的話置中會偏。
func trimChunks(in []svgChunk) []svgChunk {
	out := in[:0]
	for _, c := range in {
		if len(c.pieces) == 0 {
			continue
		}
		c.pieces[0].text = strings.TrimLeft(c.pieces[0].text, " ")
		last := len(c.pieces) - 1
		c.pieces[last].text = strings.TrimRight(c.pieces[last].text, " ")
		n := 0
		for _, p := range c.pieces {
			if p.text != "" {
				n++
			}
		}
		if n == 0 {
			continue
		}
		out = append(out, c)
	}
	return out
}

func svgFillColor(st svgStyle) color.RGBA {
	a := st.opacity * st.fillOp
	if a < 0 {
		a = 0
	}
	if a > 1 {
		a = 1
	}
	c := st.fill
	return color.RGBA{c.R, c.G, c.B, uint8(math.Round(float64(c.A) * a))}
}

// inheritSVGStyle 把一個元素的屬性疊到繼承來的樣式上。
// 表現屬性與 style="" 都要看 —— 產生器兩種都用,而 style 優先。
func inheritSVGStyle(parent svgStyle, attrs []xml.Attr) svgStyle {
	st := parent
	apply := func(k, v string) {
		v = strings.TrimSpace(v)
		if v == "" {
			return
		}
		switch k {
		case "fill":
			if strings.EqualFold(v, "none") {
				st.fillNone = true
				return
			}
			if strings.EqualFold(v, "currentColor") || strings.EqualFold(v, "inherit") {
				return
			}
			if c, ok := parseSVGColor(v); ok {
				st.fill, st.hasFill, st.fillNone = c, true, false
			}
		case "fill-opacity":
			if f, err := strconv.ParseFloat(strings.TrimSuffix(v, "%"), 64); err == nil {
				if strings.HasSuffix(v, "%") {
					f /= 100
				}
				st.fillOp = f
			}
		case "opacity":
			if f, err := strconv.ParseFloat(strings.TrimSuffix(v, "%"), 64); err == nil {
				if strings.HasSuffix(v, "%") {
					f /= 100
				}
				st.opacity *= f
			}
		case "font-size":
			st.size = parseLength(v, parent.size, parent.size)
		case "font-weight":
			st.weight = parseWeight(v, parent.weight)
		case "font-style":
			st.italic = v == "italic" || v == "oblique"
		case "letter-spacing":
			st.spacing = v
		case "text-anchor":
			st.anchor = v
		case "dominant-baseline", "alignment-baseline":
			st.baseline = v
		case "display":
			if v == "none" {
				st.hidden = true
			}
		case "visibility":
			if v == "hidden" || v == "collapse" {
				st.hidden = true
			}
		}
	}
	for _, a := range attrs {
		if a.Name.Local == "style" {
			continue
		}
		apply(a.Name.Local, a.Value)
	}
	for _, a := range attrs {
		if a.Name.Local != "style" {
			continue
		}
		for _, decl := range strings.Split(a.Value, ";") {
			k, v, ok := strings.Cut(decl, ":")
			if !ok {
				continue
			}
			apply(strings.TrimSpace(strings.ToLower(k)), v)
		}
	}
	return st
}

func parseWeight(v string, parent int) int {
	switch strings.ToLower(v) {
	case "normal":
		return 400
	case "bold":
		return 700
	case "bolder":
		return parent + 300
	case "lighter":
		return parent - 300
	}
	if n, err := strconv.Atoi(strings.TrimSpace(v)); err == nil {
		return n
	}
	return parent
}

// parseLength 解一個長度。em / rem / % 相對於 rel,其餘當像素。
func parseLength(v string, rel, def float64) float64 {
	v = strings.TrimSpace(strings.ToLower(v))
	if v == "" || v == "normal" || v == "inherit" {
		return def
	}
	mul := 1.0
	switch {
	case strings.HasSuffix(v, "em"):
		v, mul = strings.TrimSuffix(v, "em"), rel
	case strings.HasSuffix(v, "rem"):
		v, mul = strings.TrimSuffix(v, "rem"), rel
	case strings.HasSuffix(v, "%"):
		v, mul = strings.TrimSuffix(v, "%"), rel/100
	case strings.HasSuffix(v, "px"):
		v = strings.TrimSuffix(v, "px")
	case strings.HasSuffix(v, "pt"):
		v, mul = strings.TrimSuffix(v, "pt"), 96.0/72
	case strings.HasSuffix(v, "pc"):
		v, mul = strings.TrimSuffix(v, "pc"), 16
	case strings.HasSuffix(v, "mm"):
		v, mul = strings.TrimSuffix(v, "mm"), 96.0/25.4
	case strings.HasSuffix(v, "cm"):
		v, mul = strings.TrimSuffix(v, "cm"), 96.0/2.54
	case strings.HasSuffix(v, "in"):
		v, mul = strings.TrimSuffix(v, "in"), 96
	}
	f, err := strconv.ParseFloat(strings.TrimSpace(v), 64)
	if err != nil {
		return def
	}
	return f * mul
}

// firstNum 取屬性裡的第一個數。x 可以寫成一串(逐字定位),
// 那種用法在圖表裡幾乎看不到,取第一個當整段的起點。
func firstNum(v string) float64 {
	v = strings.TrimSpace(v)
	if v == "" {
		return 0
	}
	if i := strings.IndexAny(v, " ,\t\n"); i > 0 {
		v = v[:i]
	}
	return parseLength(v, 0, 0)
}

func attrOf(attrs []xml.Attr, name string) string {
	for _, a := range attrs {
		if a.Name.Local == name {
			return a.Value
		}
	}
	return ""
}

// collapseSpace 把連續空白折成一個空格,換行與縮排一併吃掉。
// 這是 SVG 預設的 xml:space 行為。
func collapseSpace(s string) string {
	var b strings.Builder
	space := false
	for _, r := range s {
		if r == ' ' || r == '\t' || r == '\n' || r == '\r' {
			space = true
			continue
		}
		if space && b.Len() >= 0 {
			b.WriteByte(' ')
		}
		space = false
		b.WriteRune(r)
	}
	if space {
		b.WriteByte(' ')
	}
	return b.String()
}
