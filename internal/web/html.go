package web

import (
	"io"
	"net/url"
	"strings"

	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"

	"github.com/wicanr2/wincv-remake/internal/markdown"
)

// ParseHTML 把一份 HTML 拆成排版用的區塊,回傳標題與區塊。
//
// 產出的是 markdown 那一套區塊,不是 DOM —— 這一層做的就是「丟掉」:
// 樣式表、腳本、版面用的巢狀容器全部不留,剩下標題、段落、清單、
// 引言、預格式化、表格、連結與圖片。畫面只有字元格點,那些丟掉的
// 東西本來也畫不出來。
//
// base 用來把相對位址接成絕對位址。傳 nil 的話相對位址原樣留著。
func ParseHTML(base *url.URL, r io.Reader) (string, []markdown.Block) {
	p := &htmlParser{base: base, z: html.NewTokenizer(r)}
	p.run()
	return p.title, p.out
}

// htmlParser 是一個一路往前掃的狀態機。
//
// 不建 DOM 樹的理由:要的東西是線性的(一段接一段),而 HTML 的樹狀
// 結構在這裡只有兩個用處——決定樣式範圍與決定何時換段——兩個都可以
// 用堆疊處理。不建樹就不必擔心巢狀錯亂的頁面把記憶體吃光。
type htmlParser struct {
	base *url.URL
	z    *html.Tokenizer

	title string
	out   []markdown.Block

	// cur 是正在累積的段落。
	cur     []markdown.Span
	kind    markdown.Kind
	level   int    // 標題階層,或清單縮排層數
	marker  string // 清單項目的符號
	ordered bool
	num     int

	style markdown.Style
	href  string

	// skip 大於 0 表示正在一個「整個丟掉」的子樹裡(script / style / …)。
	skip int
	// inPre 大於 0 表示在預格式化區塊裡,空白要原樣保留。
	inPre int
	pre   strings.Builder

	// lists 是清單的巢狀狀態。每進一層 ul/ol 推一個。
	lists []listState
	// inTitle 表示正在 <title> 裡面。
	inTitle bool
}

type listState struct {
	ordered bool
	n       int
}

// skipped 是整個子樹都不要的元素。
//
// script/style 的內容不是給人看的;svg/canvas 畫不出來;
// iframe/object 是另一份文件,這個客戶端不會去取;
// select/textarea 是表單控制項,沒有互動就只剩雜訊。
var skipped = map[atom.Atom]bool{
	atom.Script: true, atom.Style: true, atom.Noscript: true,
	atom.Svg: true, atom.Canvas: true, atom.Iframe: true,
	atom.Object: true, atom.Select: true, atom.Textarea: true,
	atom.Template: true, atom.Video: true, atom.Audio: true,
}

// blockLevel 是「到這裡要換一段」的元素。
var blockLevel = map[atom.Atom]bool{
	atom.P: true, atom.Div: true, atom.Section: true, atom.Article: true,
	atom.Header: true, atom.Footer: true, atom.Nav: true, atom.Aside: true,
	atom.Main: true, atom.Form: true, atom.Fieldset: true, atom.Figure: true,
	atom.Figcaption: true, atom.Address: true, atom.Details: true,
	atom.Summary: true, atom.Dl: true, atom.Dt: true, atom.Dd: true,
	atom.Tr: true, atom.Caption: true, atom.Center: true,
}

func (p *htmlParser) run() {
	for {
		switch p.z.Next() {
		case html.ErrorToken:
			p.flush()
			return
		case html.TextToken:
			p.text(string(p.z.Text()))
		case html.StartTagToken, html.SelfClosingTagToken:
			t := p.z.Token()
			p.start(t)
			// 自閉合的標籤不會有結束標籤,立刻收尾。
			// br/hr/img 本來就沒有內容,不必等。
			if t.Type == html.SelfClosingTagToken && !voidElement(t.DataAtom) {
				p.end(t.DataAtom)
			}
		case html.EndTagToken:
			t := p.z.Token()
			p.end(t.DataAtom)
		}
	}
}

// voidElement 是規格上就沒有結束標籤的元素。start 已經處理完它們,
// 再送一次 end 會把樣式堆疊弄亂。
func voidElement(a atom.Atom) bool {
	switch a {
	case atom.Br, atom.Hr, atom.Img, atom.Input, atom.Meta, atom.Link:
		return true
	}
	return false
}

func (p *htmlParser) start(t html.Token) {
	if p.skip > 0 {
		if skipped[t.DataAtom] {
			p.skip++
		}
		return
	}
	if skipped[t.DataAtom] {
		p.skip = 1
		return
	}

	switch t.DataAtom {
	case atom.Title:
		p.inTitle = true
	case atom.Br:
		// 換行但不換段:br 在段落中間很常見,拆成兩段會多出一個空行。
		p.text("\n")
	case atom.Hr:
		p.flush()
		p.out = append(p.out, markdown.Block{Kind: markdown.Rule})
	case atom.H1, atom.H2, atom.H3, atom.H4, atom.H5, atom.H6:
		p.flush()
		p.kind = markdown.Heading
		p.level = int(t.Data[1] - '0')
	case atom.Pre:
		p.flush()
		p.inPre++
		p.pre.Reset()
	case atom.Blockquote:
		p.flush()
		p.kind = markdown.Quote
	case atom.Ul, atom.Ol:
		p.flush()
		p.lists = append(p.lists, listState{ordered: t.DataAtom == atom.Ol})
	case atom.Li:
		p.flush()
		p.kind = markdown.List
		p.level = len(p.lists)
		if p.level < 1 {
			// 沒有 ul/ol 就冒出來的 li。頁面壞掉不是不看它的理由。
			p.level = 1
		}
		if n := len(p.lists); n > 0 {
			p.lists[n-1].n++
			p.ordered = p.lists[n-1].ordered
			p.num = p.lists[n-1].n
		}
	case atom.Td, atom.Th:
		// 表格不另外排版,同一列的格子之間插一個分隔就好。
		// 保住行內的連結比排出格線重要 —— 連結是這個畫面唯一的導覽方式。
		if len(p.cur) > 0 {
			p.addText("  ")
		}
	case atom.A:
		if h := attr(t, "href"); h != "" {
			p.href = p.resolve(h)
			p.style |= markdown.Link
		}
	case atom.B, atom.Strong:
		p.style |= markdown.Bold
	case atom.I, atom.Em, atom.Cite:
		p.style |= markdown.Italic
	case atom.Code, atom.Kbd, atom.Samp, atom.Tt:
		p.style |= markdown.Mono
	case atom.S, atom.Del, atom.Strike:
		p.style |= markdown.Strike
	case atom.Img:
		p.image(attr(t, "src"), attr(t, "alt"))
	default:
		if blockLevel[t.DataAtom] {
			p.flush()
		}
	}
}

func (p *htmlParser) end(a atom.Atom) {
	if p.skip > 0 {
		if skipped[a] {
			p.skip--
		}
		return
	}
	switch a {
	case atom.Title:
		p.inTitle = false
	case atom.Pre:
		if p.inPre > 0 {
			p.inPre--
			if p.inPre == 0 {
				p.flushPre()
			}
		}
	case atom.Ul, atom.Ol:
		p.flush()
		if n := len(p.lists); n > 0 {
			p.lists = p.lists[:n-1]
		}
	case atom.H1, atom.H2, atom.H3, atom.H4, atom.H5, atom.H6,
		atom.Li, atom.Blockquote:
		p.flush()
	case atom.A:
		p.href = ""
		p.style &^= markdown.Link
	case atom.B, atom.Strong:
		p.style &^= markdown.Bold
	case atom.I, atom.Em, atom.Cite:
		p.style &^= markdown.Italic
	case atom.Code, atom.Kbd, atom.Samp, atom.Tt:
		p.style &^= markdown.Mono
	case atom.S, atom.Del, atom.Strike:
		p.style &^= markdown.Strike
	default:
		if blockLevel[a] {
			p.flush()
		}
	}
}

func (p *htmlParser) text(s string) {
	if p.skip > 0 {
		return
	}
	if p.inTitle {
		p.title += s
		return
	}
	if p.inPre > 0 {
		p.pre.WriteString(s)
		return
	}
	if s = collapse(s); s == "" {
		return
	}
	p.addText(s)
}

// addText 把一段文字接到目前的段落上。
//
// 樣式相同就併進上一個 span:每個文字節點各開一個 span 會讓
// 排版時的折行點多到不合理,而且同樣式的相鄰片段本來就是同一段字。
func (p *htmlParser) addText(s string) {
	// 段落開頭的空白是上一個標籤留下來的,不是內容。
	if len(p.cur) == 0 && strings.TrimSpace(s) == "" {
		return
	}
	if n := len(p.cur); n > 0 &&
		p.cur[n-1].Style == p.style && p.cur[n-1].Href == p.href {
		p.cur[n-1].Text += s
		return
	}
	p.cur = append(p.cur, markdown.Span{Text: s, Style: p.style, Href: p.href})
}

// image 產生一個圖片區塊。
//
// 圖片自己成一段,不留在段落中間:排版引擎是以「一張圖佔幾列」在算
// 後面內容的行號的,夾在文字裡沒有位置可以放。
func (p *htmlParser) image(src, alt string) {
	if src == "" {
		return
	}
	p.flush()
	if alt == "" {
		alt = "圖片"
	}
	p.out = append(p.out, markdown.Block{
		Kind: markdown.Image, Src: p.resolve(src), Alt: alt,
	})
}

// flush 把累積中的段落收成一個區塊。
func (p *htmlParser) flush() {
	defer func() {
		p.cur, p.kind, p.level = nil, markdown.Para, 0
		p.marker, p.ordered, p.num = "", false, 0
	}()
	if len(p.cur) == 0 {
		return
	}
	// 整段都是空白的話不算一段。
	empty := true
	for _, sp := range p.cur {
		if strings.TrimSpace(sp.Text) != "" {
			empty = false
			break
		}
	}
	if empty {
		return
	}
	// 段尾的空白會在排版時變成一個看得見的縮排。
	last := len(p.cur) - 1
	p.cur[last].Text = strings.TrimRight(p.cur[last].Text, " \t\n")
	p.cur[0].Text = strings.TrimLeft(p.cur[0].Text, " \t\n")

	b := markdown.Block{Kind: p.kind, Level: p.level, Spans: p.cur,
		Marker: p.marker, Ordered: p.ordered, Num: p.num}
	if b.Kind == markdown.Heading && (b.Level < 1 || b.Level > 6) {
		b.Level = 1
	}
	p.out = append(p.out, b)
}

// flushPre 把 <pre> 的內容收成一個原樣區塊。
func (p *htmlParser) flushPre() {
	s := strings.ReplaceAll(p.pre.String(), "\r\n", "\n")
	s = strings.Trim(s, "\n")
	p.pre.Reset()
	if strings.TrimSpace(s) == "" {
		return
	}
	p.out = append(p.out, markdown.Block{
		Kind: markdown.Pre, Lines: strings.Split(s, "\n"),
	})
}

// resolve 把相對位址接成絕對位址。
func (p *htmlParser) resolve(ref string) string {
	ref = strings.TrimSpace(ref)
	if ref == "" || p.base == nil {
		return ref
	}
	u, err := url.Parse(ref)
	if err != nil {
		return ref
	}
	return p.base.ResolveReference(u).String()
}

func attr(t html.Token, name string) string {
	for _, a := range t.Attr {
		if strings.EqualFold(a.Key, name) {
			return a.Val
		}
	}
	return ""
}

// collapse 把連續的空白(含換行)壓成一個空格。
//
// HTML 的原始碼裡到處是為了可讀性而換的行,原樣送進排版引擎的話
// 每一個縮排都會變成內容的一部分。<pre> 不走這裡。
func collapse(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	space := false
	for _, r := range s {
		switch r {
		case ' ', '\t', '\n', '\r', '\f', '\v', ' ':
			space = true
		default:
			// 開頭的空白也留一個:「</b> 文字」的那個空格是有意義的,
			// 段落真正的前導空白由 flush 那邊統一修掉。
			if space {
				b.WriteByte(' ')
			}
			space = false
			b.WriteRune(r)
		}
	}
	if space && b.Len() > 0 {
		b.WriteByte(' ')
	}
	return b.String()
}
