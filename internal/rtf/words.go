package rtf

import (
	"encoding/hex"
	"fmt"
	"github.com/wicanr2/wincv-remake/internal/i18n"
	"strings"
	"unicode/utf16"

	"golang.org/x/text/encoding"
	"golang.org/x/text/encoding/charmap"
	"golang.org/x/text/encoding/japanese"
	"golang.org/x/text/encoding/korean"
	"golang.org/x/text/encoding/simplifiedchinese"
	"golang.org/x/text/encoding/traditionalchinese"

	"github.com/wicanr2/wincv-remake/internal/markdown"
)

// word 處理一個控制字。
func (p *parser) word(w string, num int, hasNum bool) {
	on := !hasNum || num != 0

	switch w {
	// --- 文件層級 ---
	case "ansicpg":
		if cp := validCP(num); cp != 0 {
			p.cur.cp = cp
			p.rawCP = cp
		}
	case "mac":
		p.cur.cp = 10000
	case "pc":
		p.cur.cp = 437
	case "pca":
		p.cur.cp = 850

	// --- 要丟掉的目的地 ---
	case "colortbl", "info", "generator", "themedata", "colorschememapping",
		"latentstyles", "datastore", "xmlnstbl", "listtable", "listoverridetable",
		"rsidtbl", "mmathPr", "objdata", "result", "nonshppict", "pnseclvl",
		"header", "footer", "headerl", "headerr", "headerf", "footerl", "footerr", "footerf",
		"filetbl", "revtbl", "upr", "annotation", "atnid", "atnauthor":
		p.cur.dest = destSkip
	case "fonttbl":
		p.cur.dest = destFontTbl
		p.fontCP = map[int]int{}
	case "stylesheet":
		p.cur.dest = destStyleSheet
		p.styles = map[int]string{}
	case "pict":
		p.cur.dest = destPict
		p.picHex.Reset()
		p.picBin = nil
		p.picKind = ""
	case "listtext", "pntext":
		p.cur.dest = destListText
		p.pushSink()
	case "fldinst":
		p.cur.dest = destFldInst
		p.instr.Reset()
	case "footnote":
		p.footN++
		p.emitText(fmt.Sprintf(i18n.T("[註 %d]"), p.footN))
		p.cur.dest = destFootnote
		p.pushSink()

	// --- 字型與字碼頁 ---
	case "f":
		if p.cur.dest == destFontTbl {
			p.curFont = num
			return
		}
		if cp, ok := p.fontCP[num]; ok && cp != 0 {
			p.cur.cp = cp
		}
	case "fcharset":
		if p.cur.dest == destFontTbl {
			if p.fontCP == nil {
				p.fontCP = map[int]int{}
			}
			p.fontCP[p.curFont] = charsetCP(num)
		}
	case "cpg":
		if p.cur.dest == destFontTbl {
			if cp := validCP(num); cp != 0 {
				p.fontCP[p.curFont] = cp
			}
		}
	case "uc":
		if num >= 0 && num <= 32 {
			p.cur.uc = num
		}
	case "u":
		p.unicode(num)

	// --- 字元格式 ---
	case "plain":
		p.cur.style = 0
	case "b":
		p.setStyle(markdown.Bold, on)
	case "i":
		p.setStyle(markdown.Italic, on)
	case "strike", "striked":
		p.setStyle(markdown.Strike, on)
	case "v":
		// 隱藏文字。索引項目與目錄欄位靠它藏東西,顯示出來是雜訊。
		if on {
			p.cur.dest = destSkip
		}

	// --- 段落 ---
	case "par":
		p.endPara()
	case "line":
		p.endPara()
	case "pard":
		p.cur.styleNum = 0
		p.cur.outline = 0
		p.cur.inTable = false
		p.listMark, p.inList, p.listLevel = "", false, 0
	case "page":
		p.endPara()
		p.emitBlock(markdown.Block{Kind: markdown.Rule})
	case "sect":
		p.endPara()
	case "s":
		if p.cur.dest == destStyleSheet {
			p.styleCur = num
			return
		}
		p.cur.styleNum = num
	case "ls":
		p.inList = true
	case "ilvl":
		p.listLevel = clamp(num, 0, 8)
	case "outlinelevel":
		if hasNum && num >= 0 && num <= 8 {
			p.cur.outline = num + 1
		}

	// --- 表格 ---
	case "intbl":
		p.cur.inTable = true
	case "cell", "nestcell":
		p.endCell()
	case "row", "nestrow":
		p.endRow()
	case "trowd":
		p.cur.inTable = true

	// --- 特殊字元 ---
	case "tab":
		p.emitText("    ")
	case "emdash":
		p.emitText("—")
	case "endash":
		p.emitText("–")
	case "lquote":
		p.emitText("‘")
	case "rquote":
		p.emitText("’")
	case "ldblquote":
		p.emitText("“")
	case "rdblquote":
		p.emitText("”")
	case "bullet":
		p.emitText("•")
	case "chftn":
		// 註腳編號的自動欄位。標記已經由 \footnote 那邊放好了。
	case "bin":
		p.binary(num)

	// --- 圖片格式 ---
	case "pngblip":
		p.picKind = "png"
	case "jpegblip":
		p.picKind = "jpg"
	case "emfblip":
		p.picKind = "emf"
	case "wmetafile":
		p.picKind = "wmf"
	case "dibitmap", "wbitmap":
		p.picKind = "bmp"
	}
}

func (p *parser) setStyle(s markdown.Style, on bool) {
	if on {
		p.cur.style |= s
	} else {
		p.cur.style &^= s
	}
}

// unicode 處理 \uN。
//
// [雷] 數值大於 32767 的字碼是用負數寫的(16 位元有號數溢位)。
// 不還原的話會拿到一個負的 rune,而 Go 的 string(rune) 會安靜地
// 給出一個替換字元 —— 每個罕用字都變成問號,但不會有任何錯誤。
func (p *parser) unicode(n int) {
	if n < 0 {
		n += 0x10000
	}
	r := rune(n)
	switch {
	case utf16.IsSurrogate(r) && p.highSurrogate == 0:
		p.highSurrogate = r
	case p.highSurrogate != 0:
		p.emitText(string(utf16.DecodeRune(p.highSurrogate, r)))
		p.highSurrogate = 0
	default:
		p.emitText(string(r))
	}
	p.skip = p.cur.uc
}

// binary 跳過 \binN 後面那一段原始位元組。
func (p *parser) binary(n int) {
	if n <= 0 || p.i+n > len(p.src) {
		return
	}
	data := p.src[p.i : p.i+n]
	p.i += n
	if p.cur.dest == destPict {
		p.picBin = append(p.picBin, data...)
	}
}

// --- 文字 ---

// putByte 把一個原始位元組放進待解碼的緩衝區。
//
// 不是逐個位元組解碼,因為 Big5、GBK、Shift_JIS 的一個字是兩個位元組,
// 而它們在 RTF 裡是兩個各自獨立的 `\'hh`。要湊齊了才解得出字。
func (p *parser) putByte(c byte) {
	if p.cur.dest == destPict {
		if isHexDigit(c) {
			p.picHex.WriteByte(c)
		}
		return
	}
	if p.rawCP != p.cur.cp {
		p.flushRaw()
		p.rawCP = p.cur.cp
	}
	p.raw = append(p.raw, c)
}

func isHexDigit(c byte) bool {
	return c >= '0' && c <= '9' || c >= 'a' && c <= 'f' || c >= 'A' && c <= 'F'
}

// flushRaw 把待解碼的位元組解成字。
func (p *parser) flushRaw() {
	if len(p.raw) == 0 {
		return
	}
	b := p.raw
	p.raw = p.raw[:0]
	p.emitText(decode(b, p.rawCP))
}

func (p *parser) emitText(s string) {
	if s == "" {
		return
	}
	switch p.cur.dest {
	case destSkip, destFontTbl, destPict:
		return
	case destStyleSheet:
		p.styleBuf.WriteString(s)
		return
	case destFldInst:
		p.instr.WriteString(s)
		return
	}
	// 同樣式的相鄰片段併起來:一個中文字常常被拆成好幾個 `\'hh`,
	// 不併的話一句話會變成幾十個片段,排版那一層要多做很多次工。
	if n := len(p.spans); n > 0 && p.spans[n-1].Style == p.cur.style && p.spans[n-1].Href == p.cur.href {
		p.spans[n-1].Text += s
		return
	}
	sp := markdown.Span{Text: s, Style: p.cur.style}
	if p.cur.href != "" {
		sp.Style |= markdown.Link
		sp.Href = p.cur.href
	}
	p.spans = append(p.spans, sp)
}

// --- 區塊 ---

func (p *parser) endPara() {
	p.flushRaw()
	if p.cur.inTable {
		// 儲存格裡的換行:內容之後會被壓成一行,用空白接起來。
		p.emitText(" ")
		return
	}
	if len(p.spans) == 0 {
		return
	}
	spans := p.spans
	p.spans = nil

	if lvl := p.headingLevel(); lvl > 0 {
		p.emitBlock(markdown.Block{Kind: markdown.Heading, Level: lvl, Spans: spans})
	} else if p.inList || p.listMark != "" {
		b := markdown.Block{Kind: markdown.List, Level: p.listLevel + 1, Spans: spans}
		b.Marker = safeMarker(p.listMark)
		p.emitBlock(b)
	} else {
		p.emitBlock(markdown.Block{Kind: markdown.Para, Spans: spans})
	}
	p.listMark = ""
}

// headingLevel 看目前段落的樣式是不是標題。
func (p *parser) headingLevel() int {
	if p.cur.outline > 0 {
		return clamp(p.cur.outline, 1, 6)
	}
	name := p.styles[p.cur.styleNum]
	return headingByName(name)
}

func headingByName(name string) int {
	s := strings.ToLower(strings.TrimSpace(name))
	for _, pre := range []string{"heading ", "heading", i18n.T("標題 "), i18n.T("標題"), i18n.T("标题 "), i18n.T("标题")} {
		if rest, ok := strings.CutPrefix(s, pre); ok {
			rest = strings.TrimSpace(rest)
			if len(rest) == 1 && rest[0] >= '1' && rest[0] <= '9' {
				return clamp(int(rest[0]-'0'), 1, 6)
			}
		}
	}
	return 0
}

// safeMarker 擋掉畫不出來的項目符號。
//
// 舊式清單的符號常常是 Symbol 或 Wingdings 字型裡的一個位元組,
// 照字碼頁解出來會是 · 或落在私人使用區。畫不出來的一律交回預設符號。
func safeMarker(s string) string {
	t := strings.TrimSpace(s)
	if t == "" {
		return ""
	}
	r := []rune(t)
	if len(r) == 1 {
		if r[0] >= 0xE000 && r[0] <= 0xF8FF || r[0] < 0x20 {
			return ""
		}
		if r[0] == 0xB7 { // 中間點:Symbol 字型的項目符號解出來的樣子
			return "•"
		}
	}
	if len(r) > 8 {
		return ""
	}
	return t
}

// emitBlock 送出一個區塊。表格要先收尾 —— 表格是靠一列一列累積出來的,
// 中間插進別的區塊表示那個表格已經結束了。
func (p *parser) emitBlock(b markdown.Block) {
	p.flushTable()
	p.out = append(p.out, b)
}

func (p *parser) endCell() {
	p.flushRaw()
	p.row = append(p.row, strings.TrimSpace(flatten(p.spans)))
	p.spans = nil
}

func (p *parser) endRow() {
	p.flushRaw()
	if len(p.spans) > 0 {
		p.endCell()
	}
	if len(p.row) > 0 {
		p.rows = append(p.rows, p.row)
		p.row = nil
	}
	// 一列結束就離開表格狀態。照規格是要等 \pard 才重設,而真實的
	// 檔案也都會寫 —— 但少了那一句的話,表格後面的整篇內文會被
	// 當成同一個儲存格吃掉,而畫面上只會看到表格變長,不像出錯。
	p.cur.inTable = false
}

func (p *parser) flushTable() {
	if len(p.rows) == 0 {
		return
	}
	rows := p.rows
	p.rows = nil
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
	p.out = append(p.out, markdown.Block{Kind: markdown.Table, Rows: rows})
}

// blocksText 把一疊區塊壓成一行字。
func blocksText(bs []markdown.Block) string {
	var parts []string
	for _, b := range bs {
		if t := strings.TrimSpace(flatten(b.Spans)); t != "" {
			parts = append(parts, t)
		}
	}
	return strings.Join(parts, " ")
}

func flatten(spans []markdown.Span) string {
	var sb strings.Builder
	for _, s := range spans {
		sb.WriteString(s.Text)
	}
	return sb.String()
}

// --- 巢狀輸出 ---

// pushSink 把產出接到一個新的緩衝區。註腳與清單符號的內容要另外收,
// 不能混進正文。
func (p *parser) pushSink() {
	p.sinkOut = append(p.sinkOut, p.out)
	p.sinkSpans = append(p.sinkSpans, p.spans)
	p.out, p.spans = nil, nil
}

func (p *parser) popSink() []markdown.Block {
	p.flushRaw()
	if len(p.spans) > 0 {
		p.out = append(p.out, markdown.Block{Kind: markdown.Para, Spans: p.spans})
	}
	got := p.out
	n := len(p.sinkOut) - 1
	p.out, p.spans = p.sinkOut[n], p.sinkSpans[n]
	p.sinkOut, p.sinkSpans = p.sinkOut[:n], p.sinkSpans[:n]
	return got
}

// --- 圖片 ---

func (p *parser) endPict() {
	data := p.picBin
	if len(data) == 0 {
		if b, err := hex.DecodeString(p.picHex.String()); err == nil {
			data = b
		}
	}
	p.picHex.Reset()
	p.picBin = nil
	kind := p.picKind
	p.picKind = ""
	if len(data) == 0 {
		return
	}
	// 只交出畫得出來的格式。WMF / EMF 是向量指令,這裡沒有解譯器,
	// 交出去只會變成一個解不開的檔案。
	if kind != "png" && kind != "jpg" {
		p.emitBlock(markdown.Block{Kind: markdown.Para,
			Spans: []markdown.Span{{Text: i18n.T("(內嵌圖片:") + kindName(kind) + i18n.T(",無法顯示)"), Style: markdown.Italic}}})
		return
	}
	p.picN++
	ref := fmt.Sprintf("%d.%s", p.picN, kind)
	p.imgs[ref] = data
	p.emitBlock(markdown.Block{Kind: markdown.Image, Src: ref, Alt: ref})
}

func kindName(k string) string {
	switch k {
	case "wmf":
		return "WMF"
	case "emf":
		return "EMF"
	case "bmp":
		return "DIB"
	}
	return i18n.T("未知格式")
}

// --- 樣式表 ---

func (p *parser) endStyleEntry() {
	name := strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(p.styleBuf.String()), ";"))
	p.styleBuf.Reset()
	if name != "" {
		if p.styles == nil {
			p.styles = map[int]string{}
		}
		p.styles[p.styleCur] = name
	}
}

// --- 收尾 ---

func (p *parser) finish() {
	p.flushRaw()
	p.endRow()
	if len(p.spans) > 0 {
		p.endPara()
	}
	p.flushTable()
	if len(p.footnotes) > 0 {
		p.out = append(p.out, markdown.Block{Kind: markdown.Rule})
		p.out = append(p.out, markdown.Block{Kind: markdown.Heading, Level: 2,
			Spans: []markdown.Span{{Text: i18n.T("註腳")}}})
		p.out = append(p.out, p.footnotes...)
	}
}

// --- 字碼頁 ---

// charsetCP 把 \fcharset 的字集編號換成字碼頁。
//
// 這張表是這一包能不能讀中文的關鍵:一份繁體中文的 RTF 只在字型表裡
// 寫 `\fcharset136`,正文全部是 `\'hh`。查不到就會照 cp1252 解,
// 得到一串拉丁字母 —— 那是合法的字串,不會有任何錯誤訊息。
func charsetCP(n int) int {
	switch n {
	case 0:
		return 1252
	case 2:
		return 0 // Symbol:沒有對應的字碼頁,交回預設
	case 77:
		return 10000
	case 128:
		return 932 // 日文
	case 129:
		return 949 // 韓文
	case 130:
		return 1361
	case 134:
		return 936 // 簡體中文
	case 136:
		return 950 // 繁體中文
	case 161:
		return 1253
	case 162:
		return 1254
	case 163:
		return 1258
	case 177:
		return 1255
	case 178:
		return 1256
	case 186:
		return 1257
	case 204:
		return 1251
	case 222:
		return 874
	case 238:
		return 1250
	case 254:
		return 437
	case 255:
		return 850
	}
	return 0
}

func validCP(n int) int {
	if encFor(n) != nil || n == 65001 {
		return n
	}
	return 0
}

// encFor 查一個字碼頁的解碼器。回傳 nil 表示照 Latin-1 逐位元組解。
func encFor(cp int) encoding.Encoding {
	switch cp {
	case 950:
		return traditionalchinese.Big5
	case 936:
		return simplifiedchinese.GBK
	case 932:
		return japanese.ShiftJIS
	case 949:
		return korean.EUCKR
	case 1250:
		return charmap.Windows1250
	case 1251:
		return charmap.Windows1251
	case 1252:
		return charmap.Windows1252
	case 1253:
		return charmap.Windows1253
	case 1254:
		return charmap.Windows1254
	case 1255:
		return charmap.Windows1255
	case 1256:
		return charmap.Windows1256
	case 1257:
		return charmap.Windows1257
	case 1258:
		return charmap.Windows1258
	case 874:
		return charmap.Windows874
	case 437:
		return charmap.CodePage437
	case 850:
		return charmap.CodePage850
	case 10000:
		return charmap.Macintosh
	}
	return nil
}

func decode(b []byte, cp int) string {
	if cp == 65001 {
		return string(b)
	}
	e := encFor(cp)
	if e == nil {
		e = charmap.Windows1252
	}
	s, err := e.NewDecoder().Bytes(b)
	if err != nil {
		// 解不動的位元組照 Latin-1 逐個換過去。丟掉整段比留下幾個
		// 錯字更糟 —— 錯的是那幾個位元組,不是整個段落。
		var sb strings.Builder
		for _, c := range b {
			sb.WriteRune(rune(c))
		}
		return sb.String()
	}
	return string(s)
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
