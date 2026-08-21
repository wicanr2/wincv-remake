// Package markdown 把 markdown 拆成適合畫進字元格點的區塊。
//
// 為什麼自己寫而不是接一個現成的套件:現成的 markdown 套件產出的是
// HTML 或抽象語法樹,兩者都要再轉一次才能畫進格子,而中間那一層
// (行內樣式怎麼落到「第幾格是粗體」)才是這裡真正的工作量。
// 直接產出「帶樣式的行內片段」少一次轉換,也少一份要對齊的語意。
//
// 支援的範圍以「原版 CView 會拿來讀的文件」為準:標題、段落、清單、
// 引言、程式碼區塊、水平線、圖片、連結、表格、強調與行內程式碼。
// 沒有做的:HTML 內嵌、腳註、定義清單 —— 遇到會當成普通文字,不會出錯。
package markdown

import (
	"strings"
)

// Kind 是區塊的種類。
type Kind int

const (
	Para Kind = iota
	Heading
	Code
	Quote
	List
	Rule
	Image
	Table
)

// Style 是行內樣式。可以疊加,所以用位元。
type Style uint8

const (
	Bold Style = 1 << iota
	Italic
	Mono
	Link
	Strike
)

// Span 是一段同樣式的文字。
type Span struct {
	Text  string
	Style Style
	// Href 只有 Link 樣式才有意義。
	Href string
}

// Block 是一個區塊。
type Block struct {
	Kind  Kind
	Level int    // 標題階層 1-6;清單的縮排層數
	Lang  string // 程式碼區塊的語言
	Spans []Span // 文字內容(Para / Heading / Quote / List)
	Lines []string
	// 清單
	Ordered bool
	Num     int
	// 圖片
	Src, Alt string
	// 表格:第一列是表頭
	Rows   [][]string
	Aligns []byte // 'l' 'c' 'r'
}

// Parse 把 markdown 拆成區塊。
func Parse(src string) []Block {
	lines := strings.Split(strings.ReplaceAll(src, "\r\n", "\n"), "\n")
	var out []Block
	for i := 0; i < len(lines); i++ {
		ln := lines[i]
		trimmed := strings.TrimSpace(ln)

		// 圍籬程式碼區塊。內容一律原樣保留 —— 那正是它的用途。
		if fence := fenceOf(trimmed); fence != "" {
			lang := strings.TrimSpace(strings.TrimLeft(trimmed, "`~"))
			var body []string
			i++
			for ; i < len(lines); i++ {
				if strings.HasPrefix(strings.TrimSpace(lines[i]), fence) {
					break
				}
				body = append(body, lines[i])
			}
			out = append(out, Block{Kind: Code, Lang: lang, Lines: body})
			continue
		}

		if trimmed == "" {
			continue
		}

		if isRule(trimmed) {
			out = append(out, Block{Kind: Rule})
			continue
		}

		// ATX 標題
		if n := headingLevel(trimmed); n > 0 {
			text := strings.TrimSpace(strings.TrimLeft(trimmed, "#"))
			text = strings.TrimRight(text, "# ")
			out = append(out, Block{Kind: Heading, Level: n, Spans: inline(text)})
			continue
		}

		// setext 標題:下一行整行是 === 或 ---
		if i+1 < len(lines) {
			nx := strings.TrimSpace(lines[i+1])
			if len(nx) >= 2 && (allOf(nx, '=') || allOf(nx, '-')) && !isRule(nx) {
				lvl := 1
				if nx[0] == '-' {
					lvl = 2
				}
				out = append(out, Block{Kind: Heading, Level: lvl, Spans: inline(trimmed)})
				i++
				continue
			}
		}

		// 表格:這一行有 |,而且下一行是分隔列
		if strings.Contains(ln, "|") && i+1 < len(lines) && isTableSep(lines[i+1]) {
			b := Block{Kind: Table, Aligns: tableAligns(lines[i+1])}
			b.Rows = append(b.Rows, tableCells(ln))
			i += 2
			for ; i < len(lines); i++ {
				if !strings.Contains(lines[i], "|") || strings.TrimSpace(lines[i]) == "" {
					break
				}
				b.Rows = append(b.Rows, tableCells(lines[i]))
			}
			i--
			out = append(out, b)
			continue
		}

		// 引言:連續的 > 行合成一段
		if strings.HasPrefix(trimmed, ">") {
			var parts []string
			for ; i < len(lines); i++ {
				t := strings.TrimSpace(lines[i])
				if !strings.HasPrefix(t, ">") {
					break
				}
				parts = append(parts, strings.TrimSpace(strings.TrimPrefix(t, ">")))
			}
			i--
			out = append(out, Block{Kind: Quote, Spans: inline(strings.Join(parts, " "))})
			continue
		}

		// 清單
		if marker, rest, ordered, num, ok := listItem(ln); ok {
			_ = marker
			out = append(out, Block{
				Kind: List, Level: indentOf(ln) / 2,
				Ordered: ordered, Num: num, Spans: inline(rest),
			})
			continue
		}

		// 只有一張圖片的段落:獨立成一個 Image 區塊,才能整塊配版面。
		if src, alt, only := loneImage(trimmed); only {
			out = append(out, Block{Kind: Image, Src: src, Alt: alt})
			continue
		}

		// 段落:吃到空行或下一個區塊為止
		var parts []string
		for ; i < len(lines); i++ {
			t := strings.TrimSpace(lines[i])
			if t == "" || isRule(t) || headingLevel(t) > 0 ||
				strings.HasPrefix(t, ">") || fenceOf(t) != "" {
				break
			}
			if _, _, _, _, ok := listItem(lines[i]); ok && len(parts) > 0 {
				break
			}
			parts = append(parts, t)
		}
		i--
		out = append(out, Block{Kind: Para, Spans: inline(strings.Join(parts, " "))})
	}
	return out
}

func fenceOf(s string) string {
	for _, f := range []string{"```", "~~~"} {
		if strings.HasPrefix(s, f) {
			return f
		}
	}
	return ""
}

func headingLevel(s string) int {
	n := 0
	for n < len(s) && s[n] == '#' {
		n++
	}
	if n == 0 || n > 6 {
		return 0
	}
	// `#foo` 不是標題,`# foo` 才是
	if n < len(s) && s[n] != ' ' {
		return 0
	}
	return n
}

func isRule(s string) bool {
	s = strings.ReplaceAll(s, " ", "")
	if len(s) < 3 {
		return false
	}
	return allOf(s, '-') || allOf(s, '*') || allOf(s, '_')
}

func allOf(s string, c byte) bool {
	for i := 0; i < len(s); i++ {
		if s[i] != c {
			return false
		}
	}
	return len(s) > 0
}

func indentOf(s string) int {
	n := 0
	for n < len(s) && (s[n] == ' ' || s[n] == '\t') {
		if s[n] == '\t' {
			n += 4
		} else {
			n++
		}
	}
	return n
}

// listItem 判斷是不是清單項目,回傳 (記號, 內容, 是否有序, 序號, 成立與否)。
func listItem(ln string) (string, string, bool, int, bool) {
	s := strings.TrimLeft(ln, " \t")
	if len(s) >= 2 && (s[0] == '-' || s[0] == '*' || s[0] == '+') && s[1] == ' ' {
		return string(s[0]), strings.TrimSpace(s[2:]), false, 0, true
	}
	// 有序:數字 + . 或 )
	n := 0
	for n < len(s) && s[n] >= '0' && s[n] <= '9' {
		n++
	}
	if n > 0 && n+1 < len(s) && (s[n] == '.' || s[n] == ')') && s[n+1] == ' ' {
		num := 0
		for _, c := range s[:n] {
			num = num*10 + int(c-'0')
		}
		return s[:n+1], strings.TrimSpace(s[n+2:]), true, num, true
	}
	return "", "", false, 0, false
}

// loneImage 判斷整行是不是只有一張圖片。
func loneImage(s string) (src, alt string, only bool) {
	if !strings.HasPrefix(s, "![") {
		return "", "", false
	}
	sp := inline(s)
	if len(sp) != 1 || sp[0].Style&Link == 0 || sp[0].Href == "" {
		return "", "", false
	}
	// inline 把圖片存成 Link 樣式 + Href,而 Text 是 alt。
	// 這裡要的是「整行只有這一張圖」,所以長度必須剛好 1。
	return sp[0].Href, sp[0].Text, strings.HasPrefix(s, "![")
}

func isTableSep(ln string) bool {
	s := strings.TrimSpace(ln)
	if !strings.Contains(s, "-") || !strings.Contains(s, "|") {
		return false
	}
	for _, c := range s {
		if c != '-' && c != '|' && c != ':' && c != ' ' {
			return false
		}
	}
	return true
}

func tableCells(ln string) []string {
	s := strings.TrimSpace(ln)
	s = strings.TrimPrefix(s, "|")
	s = strings.TrimSuffix(s, "|")
	out := strings.Split(s, "|")
	for i := range out {
		out[i] = strings.TrimSpace(out[i])
	}
	return out
}

func tableAligns(ln string) []byte {
	var out []byte
	for _, c := range tableCells(ln) {
		switch {
		case strings.HasPrefix(c, ":") && strings.HasSuffix(c, ":"):
			out = append(out, 'c')
		case strings.HasSuffix(c, ":"):
			out = append(out, 'r')
		default:
			out = append(out, 'l')
		}
	}
	return out
}
