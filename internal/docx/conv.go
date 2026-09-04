package docx

import (
	"encoding/xml"
	"fmt"
	"github.com/wicanr2/wincv-remake/internal/i18n"
	"path"
	"strings"

	"github.com/wicanr2/wincv-remake/internal/markdown"
	"github.com/wicanr2/wincv-remake/internal/ooxml"
)

// conv 是一次轉換的狀態。
//
// 產出直接累積在 out 裡而不是層層回傳:一個段落可能同時產生文字區塊、
// 圖片區塊與文字方塊的內容,層層回傳會讓每一層都要處理「我這一層可能
// 生出好幾種東西」。要把產出接到別的地方(表格儲存格)時用 capture 換掉。
type conv struct {
	d   *Doc
	out []markdown.Block

	// counters 是各串清單的計數器,鍵是 numId,索引是層級。
	counters map[string][]int

	// 欄位狀態。Word 的超連結有兩種寫法,這是舊的那一種:
	// 用 fldChar 標出起訖,網址寫在中間的 instrText 裡。
	fieldInstr strings.Builder
	fieldHref  string
	inField    int

	notesSeen []string // 依出現順序記下引用到的註腳編號
	endSeen   []string
	imgN      int
}

// capture 把 f 產出的區塊接走,不進主輸出。
func (c *conv) capture(f func()) []markdown.Block {
	saved := c.out
	c.out = nil
	f()
	got := c.out
	c.out = saved
	return got
}

func (c *conv) emit(b markdown.Block) { c.out = append(c.out, b) }

// body 走一段「區塊層級」的內容:文件本體、表格儲存格、文字方塊都是這個形狀。
func (c *conv) body(dec *xml.Decoder, owner string) error {
	return ooxml.Each(dec, func(se xml.StartElement) (bool, error) {
		switch se.Name.Local {
		case "p":
			return true, c.para(dec, owner)
		case "tbl":
			return true, c.table(dec, owner)
		case "sdt":
			// 結構化文件標籤。目錄、封面欄位、內容控制項都包在這裡面,
			// 不進去的話那些內容整段消失。
			return true, c.body(dec, owner)
		case "sdtContent":
			return true, c.body(dec, owner)
		case "AlternateContent":
			return true, c.alternate(dec, func(d *xml.Decoder) error { return c.body(d, owner) })
		case "ins":
			// 修訂:插入的內容是要留的,刪除的(w:del)整段跳過。
			return true, c.body(dec, owner)
		case "txbxContent":
			return true, c.body(dec, owner)
		}
		return false, nil
	})
}

// alternate 處理 mc:AlternateContent。
//
// [雷] Choice 與 Fallback 是**同一段內容的兩個版本**,新舊版 Word 各讀一種。
// 兩個都收會讓文字方塊的內容出現兩次,而兩次都是對的字 —— 看起來像
// 文件本身有重複,不像解析錯誤。只取第一個分支(Choice 依規格排在
// Fallback 前面,所以第一個就是最好的那一個)。
func (c *conv) alternate(dec *xml.Decoder, f func(*xml.Decoder) error) error {
	used := false
	return ooxml.Each(dec, func(se xml.StartElement) (bool, error) {
		if used {
			return false, nil
		}
		switch se.Name.Local {
		case "Choice", "Fallback":
			used = true
			return true, f(dec)
		}
		return false, nil
	})
}

type pProps struct {
	styleID string
	numID   string
	ilvl    int
	center  bool
}

// para 走一個段落。
func (c *conv) para(dec *xml.Decoder, owner string) error {
	var pr pProps
	var spans []markdown.Span
	var after []markdown.Block // 圖片、文字方塊,排在段落之後

	c.fieldHref, c.inField = "", 0
	c.fieldInstr.Reset()

	// flush 把目前累積的行內內容送出去,並清空。段落中間出現分頁或
	// 手動換行時要用 —— 一個區塊畫不出換行,只能拆成兩個。
	flush := func() {
		if len(spans) > 0 {
			c.emitPara(pr, spans)
			spans = nil
		}
	}

	err := ooxml.Each(dec, func(se xml.StartElement) (bool, error) {
		switch se.Name.Local {
		case "pPr":
			return true, c.pProps(dec, &pr)
		case "r":
			s, extra, brk := c.run(dec, owner, markdown.Style(0))
			spans = append(spans, s...)
			after = append(after, extra...)
			switch brk {
			case breakPage:
				flush()
				c.emit(markdown.Block{Kind: markdown.Rule})
			case breakLine:
				flush()
			}
			return true, nil
		case "hyperlink":
			href := c.hyperlinkHref(owner, se)
			return true, ooxml.Each(dec, func(h xml.StartElement) (bool, error) {
				if h.Name.Local != "r" {
					return false, nil
				}
				s, extra, _ := c.run(dec, owner, markdown.Style(0))
				for i := range s {
					if href != "" {
						s[i].Style |= markdown.Link
						s[i].Href = href
					}
				}
				spans = append(spans, s...)
				after = append(after, extra...)
				return true, nil
			})
		case "ins", "smartTag", "sdt", "sdtContent", "fldSimple", "bdo", "dir":
			// 這些是包在段落裡的容器,裡面還是 run。fldSimple 另外
			// 帶著欄位指令,超連結的新寫法就是它。
			if se.Name.Local == "fldSimple" {
				if h := fieldHyperlink(ooxml.Attr(se, "instr")); h != "" {
					c.fieldHref = h
				}
			}
			err := ooxml.Each(dec, func(h xml.StartElement) (bool, error) {
				if h.Name.Local != "r" {
					return false, nil
				}
				s, extra, _ := c.run(dec, owner, markdown.Style(0))
				spans = append(spans, s...)
				after = append(after, extra...)
				return true, nil
			})
			c.fieldHref = ""
			return true, err
		case "AlternateContent":
			return true, c.alternate(dec, func(d *xml.Decoder) error {
				got := c.capture(func() { _ = c.body(d, owner) })
				after = append(after, got...)
				return nil
			})
		case "del":
			return false, nil // 修訂刪掉的內容不顯示
		}
		return false, nil
	})

	// 空段落也要留一個空區塊嗎?不要 —— Word 的空段落是排版用的間距,
	// 而區塊之間本來就有間距,兩個疊起來會讓文件變得很鬆散。
	if len(spans) > 0 {
		c.emitPara(pr, spans)
	}
	c.out = append(c.out, after...)
	return err
}

// emitPara 決定一個段落要變成哪一種區塊。
func (c *conv) emitPara(pr pProps, spans []markdown.Span) {
	if lvl := c.d.headingLevel(pr.styleID); lvl > 0 {
		c.emit(markdown.Block{Kind: markdown.Heading, Level: lvl, Spans: spans})
		return
	}
	numID := pr.numID
	if numID == "" {
		if st, ok := c.d.styles[pr.styleID]; ok {
			numID = st.numID
		}
	}
	if numID != "" {
		if b, ok := c.listBlock(numID, pr.ilvl, spans); ok {
			c.emit(b)
			return
		}
	}
	if isQuoteStyle(c.d.styles[pr.styleID].name) {
		c.emit(markdown.Block{Kind: markdown.Quote, Spans: spans})
		return
	}
	c.emit(markdown.Block{Kind: markdown.Para, Spans: spans})
}

func isQuoteStyle(name string) bool {
	s := strings.ToLower(name)
	return strings.Contains(s, "quote") || strings.Contains(s, i18n.T("引文")) || strings.Contains(s, i18n.T("引言"))
}

// listBlock 把一個段落變成清單項目,並推進計數器。
func (c *conv) listBlock(numID string, ilvl int, spans []markdown.Span) (markdown.Block, bool) {
	n, ok := c.d.nums[numID]
	if !ok {
		// 編號定義找不到時仍然當成清單:段落自己已經說了它屬於某一串,
		// 那個事實比「查不到格式」更可靠。
		return markdown.Block{Kind: markdown.List, Level: ilvl + 1, Spans: spans}, true
	}
	lv, ok := n.lvl[ilvl]
	if !ok {
		return markdown.Block{Kind: markdown.List, Level: ilvl + 1, Spans: spans}, true
	}
	if lv.format == "none" {
		return markdown.Block{}, false
	}
	b := markdown.Block{Kind: markdown.List, Level: ilvl + 1, Spans: spans, Ordered: lv.ordered}
	if lv.ordered {
		b.Num = c.nextNum(numID, ilvl, lv.start)
	} else if m := bulletMarker(lv.text); m != "" {
		b.Marker = m
	}
	return b, true
}

// nextNum 推進某一層的編號,並把更深的層歸零。
//
// 歸零是編號清單的定義:1.1、1.2 之後回到第二個「1.」,下一層要從
// 2.1 重新開始而不是 2.3。
func (c *conv) nextNum(numID string, ilvl, start int) int {
	cnt := c.counters[numID]
	for len(cnt) <= ilvl {
		cnt = append(cnt, 0)
	}
	if cnt[ilvl] == 0 {
		cnt[ilvl] = start
	} else {
		cnt[ilvl]++
	}
	for i := ilvl + 1; i < len(cnt); i++ {
		cnt[i] = 0
	}
	c.counters[numID] = cnt
	return cnt[ilvl]
}

// bulletMarker 挑一個畫得出來的項目符號。
//
// Word 的預設項目符號是 Wingdings 的字碼(0xF0B7 之類),那些字碼在
// Unicode 裡是私人使用區 —— 直接畫會是一個缺字方框。畫不出來的一律
// 交回預設符號。
func bulletMarker(text string) string {
	t := strings.TrimSpace(text)
	if t == "" {
		return ""
	}
	r := []rune(t)
	if len(r) != 1 {
		return ""
	}
	switch {
	case r[0] >= 0xE000 && r[0] <= 0xF8FF: // 私人使用區
		return ""
	case r[0] < 0x20:
		return ""
	}
	return t
}

func (c *conv) pProps(dec *xml.Decoder, pr *pProps) error {
	return ooxml.Each(dec, func(se xml.StartElement) (bool, error) {
		switch se.Name.Local {
		case "pStyle":
			pr.styleID = ooxml.Attr(se, "val")
		case "jc":
			pr.center = ooxml.Attr(se, "val") == "center"
		case "numPr":
			return true, ooxml.Each(dec, func(n xml.StartElement) (bool, error) {
				switch n.Name.Local {
				case "numId":
					pr.numID = ooxml.Attr(n, "val")
				case "ilvl":
					pr.ilvl = clamp(atoiDef(ooxml.Attr(n, "val"), 0), 0, 8)
				}
				return false, nil
			})
		}
		return false, nil
	})
}

type brkKind int

const (
	breakNone brkKind = iota
	breakLine
	breakPage
)

// run 走一個文字段,回傳它產生的行內內容、要排在段落後面的區塊,
// 以及它結尾有沒有換行或分頁。
func (c *conv) run(dec *xml.Decoder, owner string, base markdown.Style) ([]markdown.Span, []markdown.Block, brkKind) {
	style := base
	var spans []markdown.Span
	var extra []markdown.Block
	brk := breakNone

	add := func(text string) {
		if text == "" {
			return
		}
		sp := markdown.Span{Text: text, Style: style}
		if c.fieldHref != "" {
			sp.Style |= markdown.Link
			sp.Href = c.fieldHref
		}
		spans = append(spans, sp)
	}

	_ = ooxml.Each(dec, func(se xml.StartElement) (bool, error) {
		switch se.Name.Local {
		case "rPr":
			return true, ooxml.Each(dec, func(p xml.StartElement) (bool, error) {
				switch p.Name.Local {
				case "b", "bCs":
					if onOff(p) {
						style |= markdown.Bold
					}
				case "i", "iCs":
					if onOff(p) {
						style |= markdown.Italic
					}
				case "strike", "dstrike":
					if onOff(p) {
						style |= markdown.Strike
					}
				}
				return false, nil
			})
		case "t":
			add(ooxml.Text(dec))
			return true, nil
		case "tab":
			add("    ")
		case "noBreakHyphen":
			add("-")
		case "br":
			if strings.EqualFold(ooxml.Attr(se, "type"), "page") {
				brk = breakPage
			} else {
				brk = breakLine
			}
		case "cr":
			brk = breakLine
		case "sym":
			add(symChar(ooxml.Attr(se, "char")))
		case "drawing", "pict", "object":
			alt := ""
			got := c.capture(func() { _ = c.drawing(dec, owner, &alt) })
			extra = append(extra, got...)
			return true, nil
		case "footnoteReference":
			id := ooxml.Attr(se, "id")
			c.notesSeen = append(c.notesSeen, id)
			add(fmt.Sprintf(i18n.T("[註 %d]"), len(c.notesSeen)))
		case "endnoteReference":
			id := ooxml.Attr(se, "id")
			c.endSeen = append(c.endSeen, id)
			add(fmt.Sprintf(i18n.T("[尾註 %d]"), len(c.endSeen)))
		case "instrText":
			c.fieldInstr.WriteString(ooxml.Text(dec))
			return true, nil
		case "fldChar":
			switch ooxml.Attr(se, "fldCharType") {
			case "begin":
				c.inField++
				c.fieldInstr.Reset()
			case "separate":
				if h := fieldHyperlink(c.fieldInstr.String()); h != "" {
					c.fieldHref = h
				}
				c.fieldInstr.Reset()
			case "end":
				if c.inField > 0 {
					c.inField--
				}
				c.fieldHref = ""
			}
		case "AlternateContent":
			return true, c.alternate(dec, func(d *xml.Decoder) error {
				alt := ""
				got := c.capture(func() { _ = c.drawing(d, owner, &alt) })
				extra = append(extra, got...)
				return nil
			})
		}
		return false, nil
	})
	return spans, extra, brk
}

// symChar 把 w:sym 的字碼換成畫得出來的字。
func symChar(hex string) string {
	n := 0
	for _, r := range strings.ToLower(strings.TrimSpace(hex)) {
		switch {
		case r >= '0' && r <= '9':
			n = n*16 + int(r-'0')
		case r >= 'a' && r <= 'f':
			n = n*16 + int(r-'a') + 10
		default:
			return ""
		}
	}
	// Wingdings 的字碼落在私人使用區。最常見的那幾個換成對應的符號,
	// 其餘的畫不出來,寧可不畫。
	switch n {
	case 0xF0B7, 0xB7, 0x2022:
		return "•"
	case 0xF0A7, 0xA7:
		return "▪"
	case 0xF0FC:
		return "✓"
	}
	if n >= 0xE000 && n <= 0xF8FF {
		return ""
	}
	if n < 0x20 {
		return ""
	}
	return string(rune(n))
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
	// 沒有引號時取第一個空白之前的部分,並丟掉 \l \o 這類旗標。
	for _, f := range strings.Fields(rest) {
		if !strings.HasPrefix(f, "\\") {
			return f
		}
	}
	return ""
}

// hyperlinkHref 查超連結的目標。
//
// 包內的目標(指向文件裡的書籤)回空字串:畫面沒有錨點可以跳,
// 給一個按了沒反應的連結比不給更糟。
func (c *conv) hyperlinkHref(owner string, se xml.StartElement) string {
	id := ooxml.RelID(se)
	if id == "" {
		return ""
	}
	r, ok := c.d.pkg.Rels(owner)[id]
	if !ok || !r.External {
		return ""
	}
	return r.Target
}

// drawing 在一段繪圖標記裡找圖片與文字方塊。
//
// 用「一路往下找」而不是照 DrawingML 的結構走:同一張圖可以包在
// inline、anchor、群組、圖表的備援裡,結構有十幾種組合,而要的東西
// 只有兩樣 —— blip 的關聯編號,與文字方塊的內容。
func (c *conv) drawing(dec *xml.Decoder, owner string, alt *string) error {
	return ooxml.Each(dec, func(se xml.StartElement) (bool, error) {
		switch se.Name.Local {
		case "docPr", "cNvPr":
			// 替代文字與 blip 分屬繪圖樹的不同層,所以整棵樹共用同一個
			// 變數 —— 每一層自己留一份的話,遞迴下去就看不到上一層讀到的名字。
			if v := ooxml.Attr(se, "descr"); v != "" {
				*alt = v
			} else if v := ooxml.Attr(se, "name"); v != "" && *alt == "" {
				*alt = v
			}
			return false, nil
		case "blip", "imagedata":
			if id := ooxml.RelID(se); id != "" {
				c.addImage(owner, id, *alt)
			}
			return false, nil
		case "txbxContent":
			return true, c.body(dec, owner)
		case "AlternateContent":
			return true, c.alternate(dec, func(d *xml.Decoder) error { return c.drawing(d, owner, alt) })
		}
		return true, c.drawing(dec, owner, alt)
	})
}

// addImage 記下一張圖並產生一個圖片區塊。
func (c *conv) addImage(owner, relID, alt string) {
	part := c.d.pkg.RelTarget(owner, relID)
	if part == "" || !c.d.pkg.Has(part) {
		return
	}
	c.imgN++
	ref := fmt.Sprintf("%d-%s", c.imgN, path.Base(part))
	c.d.imgs[ref] = part
	if alt == "" {
		alt = path.Base(part)
	}
	c.emit(markdown.Block{Kind: markdown.Image, Src: ref, Alt: alt})
}

// table 走一個表格。
//
// 儲存格壓成純文字:markdown 那一層的表格只有字串,沒有行內樣式。
// 儲存格裡的圖片改排在表格後面 —— 表格的格子畫不下一張圖,
// 而丟掉圖比挪位置糟。
func (c *conv) table(dec *xml.Decoder, owner string) error {
	var rows [][]string
	var after []markdown.Block
	err := ooxml.Each(dec, func(se xml.StartElement) (bool, error) {
		if se.Name.Local != "tr" {
			return false, nil
		}
		var cells []string
		err := ooxml.Each(dec, func(tc xml.StartElement) (bool, error) {
			if tc.Name.Local != "tc" {
				return false, nil
			}
			span := 1
			var blocks []markdown.Block
			err := ooxml.Each(dec, func(in xml.StartElement) (bool, error) {
				if in.Name.Local == "tcPr" {
					return true, ooxml.Each(dec, func(p xml.StartElement) (bool, error) {
						if p.Name.Local == "gridSpan" {
							span = clamp(atoiDef(ooxml.Attr(p, "val"), 1), 1, 64)
						}
						return false, nil
					})
				}
				got := c.capture(func() {
					switch in.Name.Local {
					case "p":
						_ = c.para(dec, owner)
					case "tbl":
						_ = c.table(dec, owner)
					case "sdt", "sdtContent":
						_ = c.body(dec, owner)
					default:
						_ = dec.Skip()
					}
				})
				text, imgs := splitImages(got)
				blocks = append(blocks, markdown.Block{Kind: markdown.Para,
					Spans: []markdown.Span{{Text: text}}})
				after = append(after, imgs...)
				return true, nil
			})
			cells = append(cells, blocksText(blocks))
			// 跨欄:後面補空格子,不然同一列的欄位會整排錯開。
			for i := 1; i < span; i++ {
				cells = append(cells, "")
			}
			return true, err
		})
		rows = append(rows, cells)
		return true, err
	})
	if len(rows) > 0 {
		c.emit(markdown.Block{Kind: markdown.Table, Rows: normalizeRows(rows)})
	}
	c.out = append(c.out, after...)
	return err
}

// normalizeRows 把每一列補到同樣的欄數。
func normalizeRows(rows [][]string) [][]string {
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
	return rows
}

func splitImages(bs []markdown.Block) (string, []markdown.Block) {
	var keep []markdown.Block
	var imgs []markdown.Block
	for _, b := range bs {
		if b.Kind == markdown.Image {
			imgs = append(imgs, b)
			continue
		}
		keep = append(keep, b)
	}
	return blocksText(keep), imgs
}

// blocksText 把一疊區塊壓成一行字。
func blocksText(bs []markdown.Block) string {
	var parts []string
	for _, b := range bs {
		var sb strings.Builder
		for _, s := range b.Spans {
			sb.WriteString(s.Text)
		}
		for _, l := range b.Lines {
			sb.WriteString(l)
		}
		for _, r := range b.Rows {
			sb.WriteString(strings.Join(r, " "))
		}
		if t := strings.TrimSpace(sb.String()); t != "" {
			parts = append(parts, t)
		}
	}
	return strings.Join(parts, " ")
}

// notes 把引用到的註腳排在文件最後。
func (c *conv) notes() {
	c.notesFrom("/footnotes", "footnote", c.notesSeen, i18n.T("註腳"))
	c.notesFrom("/endnotes", "endnote", c.endSeen, i18n.T("尾註"))
}

func (c *conv) notesFrom(relType, elem string, seen []string, title string) {
	if len(seen) == 0 {
		return
	}
	part := c.d.relPart(relType)
	if part == "" {
		return
	}
	b, err := c.d.pkg.Bytes(part)
	if err != nil {
		return
	}
	texts := map[string][]markdown.Block{}
	dec := ooxml.NewDecoder(b)
	if _, err := ooxml.Root(dec); err != nil {
		return
	}
	_ = ooxml.Each(dec, func(se xml.StartElement) (bool, error) {
		if se.Name.Local != elem {
			return false, nil
		}
		id := ooxml.Attr(se, "id")
		// separator 與 continuationSeparator 是那條分隔線,不是註腳內容。
		if t := ooxml.Attr(se, "type"); t != "" && t != "normal" {
			return false, nil
		}
		got := c.capture(func() { _ = c.body(dec, part) })
		texts[id] = got
		return true, nil
	})

	c.emit(markdown.Block{Kind: markdown.Rule})
	c.emit(markdown.Block{Kind: markdown.Heading, Level: 2,
		Spans: []markdown.Span{{Text: title}}})
	for i, id := range seen {
		body := blocksText(texts[id])
		if body == "" {
			continue
		}
		c.emit(markdown.Block{Kind: markdown.List, Ordered: true, Num: i + 1, Level: 1,
			Spans: []markdown.Span{{Text: body}}})
	}
}
