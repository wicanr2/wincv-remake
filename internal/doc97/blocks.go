package doc97

import (
	"fmt"
	"strings"

	"github.com/wicanr2/wincv-remake/internal/markdown"
)

// 正文裡的特殊字元。它們不是文字,是結構標記。
const (
	chPictureInline = 0x01
	chFootnoteRef   = 0x02
	chAnnotationRef = 0x05
	chCellMark      = 0x07
	chDrawnObject   = 0x08
	chTab           = 0x09
	chLineBreak     = 0x0B
	chPageBreak     = 0x0C
	chColumnBreak   = 0x0E
	chParaMark      = 0x0D
	chFieldBegin    = 0x13
	chFieldSep      = 0x14
	chFieldEnd      = 0x15
	chNoBreakHyphen = 0x1E
	chOptionalHyph  = 0x1F
)

// Blocks 排出整份文件。
func (d *Doc) Blocks() []markdown.Block {
	b := &builder{d: d}
	b.paragraphs(0, d.ccpText)
	b.flushTable()
	b.footnotes()
	if len(b.out) == 0 {
		return []markdown.Block{{Kind: markdown.Para,
			Spans: []markdown.Span{{Text: "(這份文件沒有可以顯示的內容)"}}}}
	}
	return b.out
}

type builder struct {
	d   *Doc
	out []markdown.Block

	rows [][]string
	row  []string

	footN     int
	footTexts []string

	// counters 是各串清單每一層數到哪。key 是 ilfo。
	counters map[int][]int
}

// paragraphs 把一段 CP 範圍切成段落並排出來。
func (b *builder) paragraphs(from, to int) {
	start := from
	for i := from; i < to && i < len(b.d.chars); i++ {
		c := b.d.chars[i]
		if c != chParaMark && c != chCellMark {
			continue
		}
		b.paragraph(start, i)
		start = i + 1
	}
	if start < to && start < len(b.d.chars) {
		b.paragraph(start, min(to, len(b.d.chars)))
	}
}

// paragraph 排出一個段落。end 指向段落標記本身,那個位置的屬性
// 才是這一段的屬性 —— Word 把段落屬性掛在結尾的標記上。
func (b *builder) paragraph(start, end int) {
	prop := paraProp{istd: -1}
	if end < len(b.d.fcs) {
		prop = b.d.paraAt(b.d.fcs[end])
	}
	// 列結尾的標記本身沒有內容,它只是說「上面那幾格是同一列」。
	if prop.inTable && prop.ttp {
		b.endRow()
		return
	}
	spans := b.spans(start, end)
	if prop.inTable {
		b.row = append(b.row, strings.TrimSpace(flatten(spans)))
		return
	}
	b.flushTable()
	if len(spans) == 0 {
		return
	}
	switch {
	case b.headingLevel(prop) > 0:
		b.out = append(b.out, markdown.Block{Kind: markdown.Heading,
			Level: b.headingLevel(prop), Spans: spans})
	case prop.ilfo != 0:
		b.out = append(b.out, b.listBlock(prop, spans))
	default:
		b.out = append(b.out, markdown.Block{Kind: markdown.Para, Spans: spans})
	}
}

func (b *builder) headingLevel(p paraProp) int {
	if n := b.d.headingOf(p.istd); n > 0 {
		return n
	}
	if p.outline > 0 {
		return clamp(p.outline, 1, 6)
	}
	return 0
}

// spans 把一段字元換成帶樣式的行內片段。
func (b *builder) spans(start, end int) []markdown.Span {
	var out []markdown.Span
	var sb strings.Builder
	var cur charRun
	href := ""
	field := 0 // 0 一般,1 在欄位指令裡,2 在欄位結果裡
	var instr strings.Builder

	flush := func() {
		if sb.Len() == 0 {
			return
		}
		sp := markdown.Span{Text: sb.String()}
		if cur.bold {
			sp.Style |= markdown.Bold
		}
		if cur.italic {
			sp.Style |= markdown.Italic
		}
		if cur.strike {
			sp.Style |= markdown.Strike
		}
		if href != "" {
			sp.Style |= markdown.Link
			sp.Href = href
		}
		out = append(out, sp)
		sb.Reset()
	}

	for i := start; i < end && i < len(b.d.chars); i++ {
		c := b.d.chars[i]
		switch c {
		case chFieldBegin:
			flush()
			field = 1
			instr.Reset()
			continue
		case chFieldSep:
			field = 2
			if h := fieldHyperlink(instr.String()); h != "" {
				flush()
				href = h
			}
			continue
		case chFieldEnd:
			flush()
			field = 0
			href = ""
			continue
		}
		if field == 1 {
			instr.WriteRune(c)
			continue
		}

		run := charRun{}
		if i < len(b.d.fcs) {
			run = b.d.runAt(b.d.fcs[i])
		}
		if run != cur {
			flush()
			cur = run
		}

		switch c {
		case chTab:
			sb.WriteString("    ")
		case chLineBreak:
			// 段落內的手動換行。畫面上的區塊畫不出換行,拆成兩個區塊
			// 看起來一樣。
			flush()
			if len(out) > 0 {
				b.out = append(b.out, markdown.Block{Kind: markdown.Para, Spans: out})
				out = nil
			}
		case chPageBreak, chColumnBreak:
			flush()
			if len(out) > 0 {
				b.out = append(b.out, markdown.Block{Kind: markdown.Para, Spans: out})
				out = nil
			}
			b.out = append(b.out, markdown.Block{Kind: markdown.Rule})
		case chFootnoteRef:
			flush()
			b.footN++
			out = append(out, markdown.Span{Text: fmt.Sprintf("[註 %d]", b.footN)})
		case chPictureInline, chDrawnObject:
			flush()
			// 圖片的內容在 Data 串流裡,是另一套結構,這裡沒有解。
			// 標出位置比什麼都不顯示誠實。
			out = append(out, markdown.Span{Text: "(圖片)", Style: markdown.Italic})
		case chNoBreakHyphen:
			sb.WriteByte('-')
		case chOptionalHyph, chAnnotationRef, 0x00, 0x0A:
			// 選擇性連字號不顯示;註解標記與雜訊字元丟掉。
		case 0xA0:
			sb.WriteByte(' ')
		default:
			if c >= 0x20 || c == '\t' {
				sb.WriteRune(c)
			}
		}
	}
	flush()
	return out
}

func (b *builder) endRow() {
	if len(b.row) == 0 {
		return
	}
	b.rows = append(b.rows, b.row)
	b.row = nil
}

func (b *builder) flushTable() {
	b.endRow()
	if len(b.rows) == 0 {
		return
	}
	rows := b.rows
	b.rows = nil
	w := 0
	for _, r := range rows {
		if len(r) > w {
			w = len(r)
		}
	}
	for i := range rows {
		for len(rows[i]) < w {
			rows[i] = append(rows[i], "")
		}
	}
	b.out = append(b.out, markdown.Block{Kind: markdown.Table, Rows: rows})
}

// footnotes 把註腳排在文件最後。
//
// 註腳的文字接在正文後面,是同一條字元序列的下一段。每一則以
// 段落標記分隔,開頭是那個參照字元。
func (b *builder) footnotes() {
	if b.footN == 0 || b.d.ccpFtn <= 0 {
		return
	}
	from, to := b.d.ccpText, b.d.ccpText+b.d.ccpFtn
	if to > len(b.d.chars) {
		to = len(b.d.chars)
	}
	var texts []string
	start := from
	for i := from; i < to; i++ {
		if b.d.chars[i] != chParaMark && b.d.chars[i] != chCellMark {
			continue
		}
		if t := strings.TrimSpace(cleanText(b.d.chars[start:i])); t != "" {
			texts = append(texts, t)
		}
		start = i + 1
	}
	if start < to {
		if t := strings.TrimSpace(cleanText(b.d.chars[start:to])); t != "" {
			texts = append(texts, t)
		}
	}
	if len(texts) == 0 {
		return
	}
	b.out = append(b.out, markdown.Block{Kind: markdown.Rule})
	b.out = append(b.out, markdown.Block{Kind: markdown.Heading, Level: 2,
		Spans: []markdown.Span{{Text: "註腳"}}})
	for i, t := range texts {
		b.out = append(b.out, markdown.Block{Kind: markdown.List, Ordered: true,
			Num: i + 1, Level: 1, Spans: []markdown.Span{{Text: t}}})
	}
}

// cleanText 把一段字元裡的結構標記濾掉。
func cleanText(rs []rune) string {
	var sb strings.Builder
	for _, c := range rs {
		switch {
		case c == chTab:
			sb.WriteString("    ")
		case c == 0xA0:
			sb.WriteByte(' ')
		case c >= 0x20:
			sb.WriteRune(c)
		}
	}
	return sb.String()
}

func flatten(spans []markdown.Span) string {
	var sb strings.Builder
	for _, s := range spans {
		sb.WriteString(s.Text)
	}
	return sb.String()
}

// fieldHyperlink 從欄位指令裡挑出網址。
func fieldHyperlink(instr string) string {
	s := strings.TrimSpace(instr)
	if !strings.HasPrefix(strings.ToUpper(s), "HYPERLINK") {
		return ""
	}
	rest := strings.TrimSpace(s[len("HYPERLINK"):])
	if i := strings.Index(rest, "\""); i >= 0 {
		rest = rest[i+1:]
		if j := strings.Index(rest, "\""); j >= 0 {
			return rest[:j]
		}
		return ""
	}
	for _, f := range strings.Fields(rest) {
		if !strings.HasPrefix(f, "\\") {
			return f
		}
	}
	return ""
}

func clamp(n, lo, hi int) int {
	if n < lo {
		return lo
	}
	if n > hi {
		return hi
	}
	return n
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// listBlock 排一段清單。編號或項目符號要查清單格式表才知道 ——
// 段落自己只說得出「屬於第幾串、第幾層」。
func (b *builder) listBlock(prop paraProp, spans []markdown.Span) markdown.Block {
	blk := markdown.Block{Kind: markdown.List,
		Level: clamp(prop.ilvl+1, 1, 9), Spans: spans}
	lv, ok := b.d.listLevel(prop.ilfo, prop.ilvl)
	if !ok || !lv.ordered() {
		// 查不到就當項目符號。段落自己已經說了它屬於某一串,
		// 那個事實比「查不到格式」更可靠 —— 至少縮排與符號是對的。
		return blk
	}
	blk.Ordered = true
	blk.Num = b.nextNum(prop.ilfo, prop.ilvl, lv.start)
	return blk
}

// nextNum 推進某一層的編號,並把更深的層歸零。
//
// 歸零是編號清單的定義:1.1、1.2 之後回到第二個「1.」,下一層要從
// 2.1 重新開始而不是 2.3。
func (b *builder) nextNum(ilfo, ilvl, start int) int {
	if b.counters == nil {
		b.counters = map[int][]int{}
	}
	cnt := b.counters[ilfo]
	for len(cnt) <= ilvl {
		cnt = append(cnt, 0)
	}
	if cnt[ilvl] == 0 {
		if start < 1 {
			start = 1
		}
		cnt[ilvl] = start
	} else {
		cnt[ilvl]++
	}
	for i := ilvl + 1; i < len(cnt); i++ {
		cnt[i] = 0
	}
	b.counters[ilfo] = cnt
	return cnt[ilvl]
}
