package markdown

import (
	"strings"

	"github.com/wicanr2/wincv-remake/internal/cell"
)

// width 回傳字串佔幾格(全形算兩格)。
func width(s string) int {
	n := 0
	for _, r := range s {
		if cell.IsWide(r) {
			n += 2
		} else {
			n++
		}
	}
	return n
}

func clip(s string, w int) string {
	if width(s) <= w {
		return s
	}
	var b strings.Builder
	n := 0
	for _, r := range s {
		rw := 1
		if cell.IsWide(r) {
			rw = 2
		}
		if n+rw > w {
			break
		}
		b.WriteRune(r)
		n += rw
	}
	return b.String()
}

func padTo(s string, w int, align byte) string {
	d := w - width(s)
	if d <= 0 {
		return s
	}
	switch align {
	case 'r':
		return strings.Repeat(" ", d) + s
	case 'c':
		l := d / 2
		return strings.Repeat(" ", l) + s + strings.Repeat(" ", d-l)
	}
	return s + strings.Repeat(" ", d)
}

func expandTabs(s string) string {
	var b strings.Builder
	for _, r := range s {
		if r == '\t' {
			b.WriteString(strings.Repeat(" ", 4-b.Len()%4))
		} else {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var d []byte
	for n > 0 {
		d = append([]byte{byte('0' + n%10)}, d...)
		n /= 10
	}
	return string(d)
}

// wrapSpans 把帶樣式的片段折成固定寬度的列。
//
// 折行要在**片段之間**與**片段內部**都能發生,而且樣式要跟著走 ——
// 一個粗體片段被折成兩列時,兩列都要是粗體。所以不能先把文字接起來
// 折完再套樣式。
//
// 中文不靠空白斷字:CJK 之間任何位置都能折。只按空白折的話,
// 一整段沒有空白的中文會變成一列超長的字被硬切在畫面邊緣。
func wrapSpans(spans []Span, cols int) [][]Span {
	if cols < 2 {
		cols = 2
	}
	var out [][]Span
	var cur []Span
	used := 0

	push := func() {
		if len(cur) > 0 {
			out = append(out, cur)
			cur, used = nil, 0
		}
	}
	// 相鄰且同樣式的片段要併起來。不併的話每個中文字都是一個獨立片段
	// (CJK 是逐字切的折行單位),一段千字的中文會產生上千個片段,
	// 後面每一層都得為此付代價。
	add := func(text string, st Style, href string) {
		if text == "" {
			return
		}
		if n := len(cur); n > 0 && cur[n-1].Style == st && cur[n-1].Href == href {
			cur[n-1].Text += text
		} else {
			cur = append(cur, Span{Text: text, Style: st, Href: href})
		}
		used += width(text)
	}

	for _, sp := range spans {
		for _, word := range splitWords(sp.Text) {
			w := width(word)
			// 行首的空白丟掉,不然折行處會多一格縮排。
			if used == 0 && strings.TrimSpace(word) == "" {
				continue
			}
			if used+w > cols && used > 0 {
				push()
				if strings.TrimSpace(word) == "" {
					continue
				}
			}
			// 單一「字」就超過整列寬度(超長網址、無空白的長字串):
			// 硬切,不然它會永遠放不進去而卡住。
			for width(word) > cols {
				head := clip(word, cols-used)
				if head == "" {
					push()
					continue
				}
				add(head, sp.Style, sp.Href)
				word = word[len(head):]
				push()
			}
			add(word, sp.Style, sp.Href)
		}
	}
	push()
	if len(out) == 0 {
		out = [][]Span{{}}
	}
	return out
}

// splitWords 把一段文字切成可以折行的單位。
//
// 空白後面接著下一個詞算一個單位(空白留在詞前面,折行時整個丟掉);
// CJK 每個字自己一個單位。
func splitWords(s string) []string {
	var out []string
	var b strings.Builder
	flush := func() {
		if b.Len() > 0 {
			out = append(out, b.String())
			b.Reset()
		}
	}
	for _, r := range s {
		if cell.IsWide(r) {
			flush()
			out = append(out, string(r))
			continue
		}
		if r == ' ' || r == '\t' {
			flush()
			b.WriteRune(' ')
			continue
		}
		b.WriteRune(r)
	}
	flush()
	return out
}
