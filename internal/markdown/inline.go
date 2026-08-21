package markdown

import "strings"

// inline 把一行文字拆成帶樣式的片段。
//
// 圖片與連結都存成 Link 樣式 + Href;圖片另外把 Text 設成 alt。
// 兩者在格點畫面上的差別只有顏色,不值得為此多一個樣式位元。
//
// 這是手寫的掃描器而不是正則:markdown 的強調是「配對」問題
// (`**a* b**` 這種交錯要能收斂),正則寫得出來但看不懂。
func inline(s string) []Span {
	var out []Span
	var buf strings.Builder
	var style Style

	flush := func() {
		if buf.Len() > 0 {
			out = append(out, Span{Text: buf.String(), Style: style})
			buf.Reset()
		}
	}

	for i := 0; i < len(s); {
		// 跳脫:\* 就是一個星號
		if s[i] == '\\' && i+1 < len(s) {
			buf.WriteByte(s[i+1])
			i += 2
			continue
		}
		// 行內程式碼:內容原樣保留,不再解析
		if s[i] == '`' {
			n := 1
			for i+n < len(s) && s[i+n] == '`' {
				n++
			}
			mark := s[i : i+n]
			if end := strings.Index(s[i+n:], mark); end >= 0 {
				flush()
				out = append(out, Span{Text: s[i+n : i+n+end], Style: style | Mono})
				i += n + end + n
				continue
			}
		}
		// 圖片 ![alt](src) 與連結 [text](href)
		if s[i] == '!' && i+1 < len(s) && s[i+1] == '[' {
			if text, href, n, ok := linkAt(s[i+1:]); ok {
				flush()
				out = append(out, Span{Text: text, Style: style | Link, Href: href})
				i += 1 + n
				continue
			}
		}
		if s[i] == '[' {
			if text, href, n, ok := linkAt(s[i:]); ok {
				flush()
				out = append(out, Span{Text: text, Style: style | Link, Href: href})
				i += n
				continue
			}
		}
		// 強調
		if s[i] == '*' || s[i] == '_' {
			c := s[i]
			n := 1
			for i+n < len(s) && s[i+n] == c {
				n++
			}
			// 只認 * 與 **,更多的當文字
			if n <= 2 {
				want := Italic
				if n == 2 {
					want = Bold
				}
				// 有沒有配對的收尾?沒有就當普通字元,不然
				// 一個落單的星號會讓後面整段都變成斜體。
				if style&want != 0 || strings.Contains(s[i+n:], strings.Repeat(string(c), n)) {
					flush()
					style ^= want
					i += n
					continue
				}
			}
			buf.WriteString(s[i : i+n])
			i += n
			continue
		}
		if strings.HasPrefix(s[i:], "~~") {
			if style&Strike != 0 || strings.Contains(s[i+2:], "~~") {
				flush()
				style ^= Strike
				i += 2
				continue
			}
		}
		buf.WriteByte(s[i])
		i++
	}
	flush()
	return out
}

// linkAt 解析 s 開頭的 [text](href),回傳吃掉幾個位元組。
//
// 中括號要配對算 —— [![img](a)](b) 這種巢狀在 README 裡很常見,
// 只找第一個 ] 會切在錯的地方。
func linkAt(s string) (text, href string, n int, ok bool) {
	if len(s) == 0 || s[0] != '[' {
		return
	}
	depth, i := 0, 0
	for ; i < len(s); i++ {
		if s[i] == '\\' {
			i++
			continue
		}
		if s[i] == '[' {
			depth++
		} else if s[i] == ']' {
			depth--
			if depth == 0 {
				break
			}
		}
	}
	if i >= len(s) || depth != 0 {
		return
	}
	text = s[1:i]
	j := i + 1
	if j >= len(s) || s[j] != '(' {
		return
	}
	depth = 0
	k := j
	for ; k < len(s); k++ {
		if s[k] == '(' {
			depth++
		} else if s[k] == ')' {
			depth--
			if depth == 0 {
				break
			}
		}
	}
	if k >= len(s) {
		return
	}
	href = strings.TrimSpace(s[j+1 : k])
	// 連結標題:[x](url "標題") —— 標題丟掉,格點畫面沒地方放
	if sp := strings.IndexAny(href, " \t"); sp >= 0 {
		href = href[:sp]
	}
	// 巢狀圖片連結:[![alt](img)](url) 的 text 還是 markdown,
	// 取裡層的 alt 當文字。
	if strings.HasPrefix(text, "![") {
		if inner := inline(text); len(inner) == 1 {
			text = inner[0].Text
		}
	}
	return text, href, k + 1, true
}
