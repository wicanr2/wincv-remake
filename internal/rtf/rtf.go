// Package rtf 讀 Rich Text Format。
//
// RTF 是一種標記語言而不是容器格式:一份 .rtf 就是一長串
// `{` 群組、`\控制字` 與純文字。難的地方不在語法而在**文字怎麼變回字**:
//
//   - 一個位元組是什麼字,取決於當時選到的字型是哪一個字集。
//     同一份檔案裡可以有 Big5 的段落與 cp1252 的段落。
//   - `\uN` 帶著一個 Unicode 字碼,後面跟著給舊版讀取器看的替代寫法,
//     那幾個位元組要跳過 —— 跳幾個由 `\ucN` 決定。不跳的話每個中文字
//     後面都會多出兩個問號。
//   - 清單的項目符號不在段落裡,而在一個 `\listtext` 群組裡,那是
//     「不懂清單的讀取器請直接印這個」的備援。拿它當符號用,就不必
//     解整張清單表。
//
// 圖片是內嵌的十六進位文字,取出來就能直接解碼。
package rtf

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/wicanr2/wincv-remake/internal/markdown"
)

// MaxBytes 是檔案大小上限。
const MaxBytes = 64 << 20

// Doc 是一份解析完的 RTF。
type Doc struct {
	blocks []markdown.Block
	imgs   map[string][]byte
}

// Open 讀一份 .rtf。
func Open(name string) (*Doc, error) {
	st, err := os.Stat(name)
	if err != nil {
		return nil, err
	}
	if st.Size() > MaxBytes {
		return nil, fmt.Errorf("這份 RTF 太大(%d 位元組)", st.Size())
	}
	b, err := os.ReadFile(name)
	if err != nil {
		return nil, err
	}
	return Parse(b)
}

// Parse 解析一份 RTF。
func Parse(src []byte) (*Doc, error) {
	if i := skipBOM(src); i > 0 {
		src = src[i:]
	}
	if !strings.HasPrefix(strings.TrimLeft(string(firstBytes(src, 16)), " \r\n\t"), "{\\rt") {
		return nil, fmt.Errorf("這不是 RTF 檔")
	}
	p := &parser{src: src, imgs: map[string][]byte{}}
	p.cur = state{cp: 1252, uc: 1}
	p.run()
	p.finish()
	d := &Doc{blocks: p.out, imgs: p.imgs}
	if len(d.blocks) == 0 {
		d.blocks = []markdown.Block{{Kind: markdown.Para,
			Spans: []markdown.Span{{Text: "(這份文件沒有可以顯示的內容)"}}}}
	}
	return d, nil
}

func firstBytes(b []byte, n int) []byte {
	if len(b) < n {
		return b
	}
	return b[:n]
}

func skipBOM(b []byte) int {
	if len(b) >= 3 && b[0] == 0xEF && b[1] == 0xBB && b[2] == 0xBF {
		return 3
	}
	return 0
}

// Blocks 回傳整份文件的區塊。
func (d *Doc) Blocks() []markdown.Block { return d.blocks }

// Image 取一張內嵌的圖。
func (d *Doc) Image(ref string) ([]byte, error) {
	b, ok := d.imgs[ref]
	if !ok {
		return nil, fmt.Errorf("這份文件裡沒有 %s", ref)
	}
	return b, nil
}

func (d *Doc) Close() error { return nil }

// --- 解析 ---

type dest int

const (
	destBody dest = iota
	destSkip      // 整個群組丟掉
	destFontTbl
	destStyleSheet
	destPict
	destListText
	destFldInst
	destFootnote
)

type state struct {
	dest  dest
	cp    int // 目前的字碼頁
	uc    int // \uN 之後要跳過幾個替代字元
	style markdown.Style
	href  string
	// outline 是 \outlinelevel + 1,0 表示沒有指定。
	outline int
	// 段落層級的屬性。放在 state 裡是因為它們同樣受群組範圍限制。
	styleNum int
	ilvl     int
	inTable  bool
}

type parser struct {
	src []byte
	i   int

	cur   state
	stack []state

	out   []markdown.Block
	spans []markdown.Span

	raw   []byte // 還沒解碼的位元組
	rawCP int

	skip int // \uN 之後還要跳過幾個字元

	// 字型表與樣式表。
	fontCP   map[int]int
	curFont  int
	curChars int
	styles   map[int]string
	styleBuf strings.Builder
	styleCur int

	// 清單:目前段落的項目符號(來自 \listtext 或 \pntext)。
	listMark  string
	listLevel int
	inList    bool

	// 表格。
	rows [][]string
	row  []string

	// 欄位。
	instr strings.Builder

	// 圖片。
	picHex  strings.Builder
	picBin  []byte
	picKind string
	picN    int
	imgs    map[string][]byte

	// 註腳。
	footN     int
	footnotes []markdown.Block
	sinkOut   [][]markdown.Block
	sinkSpans [][]markdown.Span

	highSurrogate rune
}

func (p *parser) run() {
	for p.i < len(p.src) {
		c := p.src[p.i]
		switch c {
		case '{':
			p.i++
			p.flushRaw()
			// 群組繼承目前的狀態,離開時整個還原 —— RTF 的所有格式
			// 設定都受群組範圍限制,這是它唯一的作用域機制。
			p.stack = append(p.stack, p.cur)
		case '}':
			p.i++
			p.flushRaw()
			p.closeGroup()
		case '\\':
			p.i++
			p.control()
		case '\r', '\n':
			// RTF 原始碼裡的換行沒有意義,換行一律由 \par 表示。
			p.i++
		default:
			p.i++
			if p.skip > 0 {
				p.skip--
				continue
			}
			p.putByte(c)
		}
	}
}

func (p *parser) closeGroup() {
	popped := p.cur
	if len(p.stack) > 0 {
		p.cur = p.stack[len(p.stack)-1]
		p.stack = p.stack[:len(p.stack)-1]
	} else {
		p.cur = state{cp: 1252, uc: 1}
	}
	// 樣式表的每一筆是一個群組,而且它們的目的地跟外層一樣,
	// 所以要在比對目的地之前先收 —— 比對會把它們全部濾掉。
	if popped.dest == destStyleSheet {
		p.endStyleEntry()
	}
	if popped.dest == p.cur.dest {
		return
	}
	// 一個特殊目的地的群組結束了,把它累積的東西收掉。
	switch popped.dest {
	case destPict:
		p.endPict()
	case destListText:
		p.listMark = strings.TrimSpace(blocksText(p.popSink()))
	case destFldInst:
		// 這時候 p.cur 已經還原成 field 群組本身,所以網址設在它身上,
		// 接下來的 fldrslt 群組會繼承到。
		if h := fieldHyperlink(p.instr.String()); h != "" {
			p.cur.href = h
		}
		p.instr.Reset()
	case destStyleSheet:
		p.endStyleEntry()
	case destFootnote:
		p.footnotes = append(p.footnotes, p.popSink()...)
	}
}

// control 讀一個控制字或控制符號。
func (p *parser) control() {
	if p.i >= len(p.src) {
		return
	}
	c := p.src[p.i]
	// 控制符號:反斜線後面接一個非字母。
	if !isAlpha(c) {
		p.i++
		switch c {
		case '\'':
			p.hexByte()
		case '\\', '{', '}':
			if p.skip > 0 {
				p.skip--
				return
			}
			p.putByte(c)
		case '*':
			// \* 表示「不認得後面那個目的地就整組丟掉」。
			p.flushRaw()
			p.markOptional()
		case '~':
			p.putByte(' ')
		case '_', '-':
			// 不斷行連字號與選擇性連字號:畫面上就是一個連字號或什麼都沒有。
		case '\r', '\n':
			p.endPara()
		}
		return
	}
	// 控制字。
	start := p.i
	for p.i < len(p.src) && isAlpha(p.src[p.i]) {
		p.i++
	}
	word := string(p.src[start:p.i])
	num, hasNum := 0, false
	neg := false
	if p.i < len(p.src) && p.src[p.i] == '-' {
		neg = true
		p.i++
	}
	ns := p.i
	for p.i < len(p.src) && p.src[p.i] >= '0' && p.src[p.i] <= '9' {
		p.i++
	}
	if p.i > ns {
		num, _ = strconv.Atoi(string(p.src[ns:p.i]))
		hasNum = true
		if neg {
			num = -num
		}
	}
	// 控制字後面的一個空白是分隔符,不是內容。
	if p.i < len(p.src) && p.src[p.i] == ' ' {
		p.i++
	}
	// [雷] `\'hh` 不能觸發清空。Big5 的一個字是連續兩個 `\'hh`,
	// 中間清空就會拿半個字去解碼,解出來是兩個問號。
	p.flushRaw()
	p.word(word, num, hasNum)
}

func isAlpha(c byte) bool { return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' }

func (p *parser) hexByte() {
	if p.i+1 >= len(p.src) {
		return
	}
	v := hexVal(p.src[p.i])*16 + hexVal(p.src[p.i+1])
	p.i += 2
	if p.skip > 0 {
		p.skip--
		return
	}
	if p.cur.dest == destPict {
		p.picHex.WriteByte(p.src[p.i-2])
		p.picHex.WriteByte(p.src[p.i-1])
		return
	}
	p.putByte(byte(v))
}

func hexVal(c byte) int {
	switch {
	case c >= '0' && c <= '9':
		return int(c - '0')
	case c >= 'a' && c <= 'f':
		return int(c-'a') + 10
	case c >= 'A' && c <= 'F':
		return int(c-'A') + 10
	}
	return 0
}

// markOptional 處理 `\*`:看下一個控制字是不是我們認得的目的地,
// 不認得就整組丟掉。
func (p *parser) markOptional() {
	j := p.i
	if j < len(p.src) && p.src[j] == '\\' {
		j++
		s := j
		for j < len(p.src) && isAlpha(p.src[j]) {
			j++
		}
		switch string(p.src[s:j]) {
		case "fldinst", "listtext", "pntext", "shppict", "footnote":
			return // 這幾個要進去
		}
	}
	p.cur.dest = destSkip
}
