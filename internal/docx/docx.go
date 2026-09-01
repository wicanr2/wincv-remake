// Package docx 讀 Word 的 .docx(WordprocessingML)。
//
// 產出的是 internal/markdown 的區塊,不是 HTML 也不是 DOM —— 畫面只有
// 字元格點,能表現的就是標題、段落、清單、表格、圖片與行內樣式那幾種,
// 多解出來的東西沒有地方畫。
//
// 走的是遞迴下降:XML 的巢狀結構(表格裡有段落,段落裡有超連結,
// 超連結裡有文字段)剛好對應函式呼叫的巢狀,不必自己維護堆疊,
// 也不必把整份文件建成樹 —— 一份幾百頁的 document.xml 建成樹會是
// 好幾百 MB,而這裡從頭到尾只留產出的區塊。
package docx

import (
	"encoding/xml"
	"fmt"
	"strconv"
	"strings"

	"github.com/wicanr2/wincv-remake/internal/markdown"
	"github.com/wicanr2/wincv-remake/internal/ooxml"
)

// Doc 是一份打開著的 .docx。用完要 Close。
type Doc struct {
	Title string

	pkg  *ooxml.Package
	main string // 主體組件的路徑,通常是 word/document.xml

	styles map[string]style
	nums   map[string]numbering // numId → 這一串的各層設定
	// imgs 是解析時遇到的圖片,鍵是給上層用的參照。
	imgs map[string]string // 參照 → 包內路徑
}

type style struct {
	id      string
	name    string
	basedOn string
	outline int // 0-8;-1 表示沒有
	bold    bool
	italic  bool
	numID   string
}

type numbering struct {
	lvl map[int]numLevel
}

type numLevel struct {
	format  string
	text    string
	start   int
	ordered bool
}

// Open 打開一份 .docx。
func Open(name string) (*Doc, error) {
	p, err := ooxml.Open(name)
	if err != nil {
		return nil, err
	}
	d, err := New(p)
	if err != nil {
		p.Close()
		return nil, err
	}
	return d, nil
}

// New 從一個已經打開的 OPC 包建 Doc。Doc 會接手它的生命週期。
func New(p *ooxml.Package) (*Doc, error) {
	main := mainPart(p)
	if main == "" {
		return nil, fmt.Errorf("這不是 Word 文件(找不到 document.xml)")
	}
	d := &Doc{
		pkg: p, main: main,
		styles: map[string]style{},
		nums:   map[string]numbering{},
		imgs:   map[string]string{},
	}
	d.Title = docTitle(p)
	d.readStyles()
	d.readNumbering()
	return d, nil
}

func (d *Doc) Close() error { return d.pkg.Close() }

// mainPart 找主體組件。
//
// 先問包的關聯而不是直接用 word/document.xml:巨集啟用的 .docm 與
// 範本 .dotx 主體叫別的名字,而關聯型別三者相同。
func mainPart(p *ooxml.Package) string {
	for _, t := range p.RelsByType("", "/officeDocument") {
		if p.Has(t) {
			return t
		}
	}
	for _, n := range []string{"word/document.xml", "word/document2.xml"} {
		if p.Has(n) {
			return n
		}
	}
	return ""
}

func docTitle(p *ooxml.Package) string {
	for _, t := range p.RelsByType("", "/metadata/core-properties") {
		b, err := p.Bytes(t)
		if err != nil {
			continue
		}
		dec := ooxml.NewDecoder(b)
		var title string
		_ = ooxml.Each(dec, func(se xml.StartElement) (bool, error) {
			if se.Name.Local == "coreProperties" {
				return true, ooxml.Each(dec, func(c xml.StartElement) (bool, error) {
					if c.Name.Local == "title" {
						title = strings.TrimSpace(ooxml.Text(dec))
						return true, nil
					}
					return false, nil
				})
			}
			return false, nil
		})
		if title != "" {
			return title
		}
	}
	return ""
}

// readStyles 讀樣式表。要的只有三件事:這個樣式是不是標題(第幾階)、
// 它預設粗體或斜體嗎、它有沒有綁一串編號。
func (d *Doc) readStyles() {
	part := d.relPart("/styles")
	if part == "" {
		return
	}
	b, err := d.pkg.Bytes(part)
	if err != nil {
		return
	}
	dec := ooxml.NewDecoder(b)
	root, err := ooxml.Root(dec)
	if err != nil || root.Name.Local != "styles" {
		return
	}
	_ = ooxml.Each(dec, func(se xml.StartElement) (bool, error) {
		if se.Name.Local != "style" {
			return false, nil
		}
		st := style{id: ooxml.Attr(se, "styleId"), outline: -1}
		err := ooxml.Each(dec, func(c xml.StartElement) (bool, error) {
			switch c.Name.Local {
			case "name":
				st.name = ooxml.Attr(c, "val")
			case "basedOn":
				st.basedOn = ooxml.Attr(c, "val")
			case "pPr":
				return true, ooxml.Each(dec, func(pp xml.StartElement) (bool, error) {
					switch pp.Name.Local {
					case "outlineLvl":
						st.outline = atoiDef(ooxml.Attr(pp, "val"), -1)
					case "numPr":
						return true, ooxml.Each(dec, func(np xml.StartElement) (bool, error) {
							if np.Name.Local == "numId" {
								st.numID = ooxml.Attr(np, "val")
							}
							return false, nil
						})
					}
					return false, nil
				})
			case "rPr":
				return true, ooxml.Each(dec, func(rp xml.StartElement) (bool, error) {
					switch rp.Name.Local {
					case "b":
						st.bold = onOff(rp)
					case "i":
						st.italic = onOff(rp)
					}
					return false, nil
				})
			}
			return false, nil
		})
		if st.id != "" {
			d.styles[st.id] = st
		}
		return true, err
	})
}

// headingLevel 算出一個樣式代表第幾階標題,0 表示不是標題。
//
// 先看 outlineLvl 再看名字:outlineLvl 是 Word 自己用來建目錄的欄位,
// 是這件事的真值;名字則會被翻譯(中文版是「標題 1」)、被使用者改。
// 兩個都沒有就沿著 basedOn 往上找 —— 自訂樣式常常只是「以標題 1 為基礎」。
func (d *Doc) headingLevel(id string) int {
	for i := 0; i < 8 && id != ""; i++ { // 上限擋住互相 basedOn 的迴圈
		st, ok := d.styles[id]
		if !ok {
			return 0
		}
		if st.outline >= 0 && st.outline <= 8 {
			return clamp(st.outline+1, 1, 6)
		}
		if n := headingByName(st.name); n > 0 {
			return n
		}
		if n := headingByName(st.id); n > 0 {
			return n
		}
		id = st.basedOn
	}
	return 0
}

func headingByName(name string) int {
	s := strings.ToLower(strings.TrimSpace(name))
	for _, pre := range []string{"heading ", "heading", "標題 ", "標題", "标题 ", "标题"} {
		if rest, ok := strings.CutPrefix(s, pre); ok {
			if n, err := strconv.Atoi(strings.TrimSpace(rest)); err == nil && n >= 1 && n <= 9 {
				return clamp(n, 1, 6)
			}
		}
	}
	if s == "title" || s == "標題" {
		return 1
	}
	if s == "subtitle" || s == "副標題" {
		return 2
	}
	return 0
}

// readNumbering 讀編號定義。
//
// 有兩層轉接:段落寫的是 numId,numId 指到 abstractNumId,層級設定在
// abstractNum 底下。中間那一層存在的理由是「同一套編號格式可以被好幾串
// 清單共用,但各自從頭數」。
func (d *Doc) readNumbering() {
	part := d.relPart("/numbering")
	if part == "" {
		return
	}
	b, err := d.pkg.Bytes(part)
	if err != nil {
		return
	}
	abstract := map[string]numbering{}
	link := map[string]string{} // numId → abstractNumId

	dec := ooxml.NewDecoder(b)
	root, err := ooxml.Root(dec)
	if err != nil || root.Name.Local != "numbering" {
		return
	}
	_ = ooxml.Each(dec, func(se xml.StartElement) (bool, error) {
		switch se.Name.Local {
		case "abstractNum":
			id := ooxml.Attr(se, "abstractNumId")
			n := numbering{lvl: map[int]numLevel{}}
			err := ooxml.Each(dec, func(c xml.StartElement) (bool, error) {
				if c.Name.Local != "lvl" {
					return false, nil
				}
				ilvl := atoiDef(ooxml.Attr(c, "ilvl"), 0)
				lv := numLevel{start: 1}
				err := ooxml.Each(dec, func(l xml.StartElement) (bool, error) {
					switch l.Name.Local {
					case "start":
						lv.start = atoiDef(ooxml.Attr(l, "val"), 1)
					case "numFmt":
						lv.format = ooxml.Attr(l, "val")
					case "lvlText":
						lv.text = ooxml.Attr(l, "val")
					}
					return false, nil
				})
				lv.ordered = lv.format != "" && lv.format != "bullet" && lv.format != "none"
				n.lvl[ilvl] = lv
				return true, err
			})
			abstract[id] = n
			return true, err
		case "num":
			id := ooxml.Attr(se, "numId")
			err := ooxml.Each(dec, func(c xml.StartElement) (bool, error) {
				if c.Name.Local == "abstractNumId" {
					link[id] = ooxml.Attr(c, "val")
				}
				return false, nil
			})
			return true, err
		}
		return false, nil
	})
	for numID, absID := range link {
		if n, ok := abstract[absID]; ok {
			d.nums[numID] = n
		}
	}
}

func (d *Doc) relPart(typ string) string {
	for _, t := range d.pkg.RelsByType(d.main, typ) {
		if d.pkg.Has(t) {
			return t
		}
	}
	return ""
}

func atoiDef(s string, def int) int {
	if n, err := strconv.Atoi(strings.TrimSpace(s)); err == nil {
		return n
	}
	return def
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

// onOff 判讀 Word 的開關屬性。
//
// [雷] `<w:b/>` 沒有 val 屬性時是**開**,不是關。照「沒有值就是 false」
// 寫的話所有粗體都會消失 —— 而畫面看起來完全正常,只是沒有粗體。
func onOff(se xml.StartElement) bool {
	v := ooxml.Attr(se, "val")
	switch strings.ToLower(v) {
	case "", "1", "true", "on":
		return true
	}
	return false
}

// Image 取一張解析時遇到的圖。
func (d *Doc) Image(ref string) ([]byte, error) {
	part, ok := d.imgs[ref]
	if !ok {
		return nil, fmt.Errorf("這份文件裡沒有 %s", ref)
	}
	return d.pkg.Bytes(part)
}

// Blocks 排出整份文件。
func (d *Doc) Blocks() []markdown.Block {
	c := &conv{d: d, counters: map[string][]int{}}
	if b, err := d.pkg.Bytes(d.main); err == nil {
		dec := ooxml.NewDecoder(b)
		if root, err := ooxml.Root(dec); err == nil && root.Name.Local == "document" {
			_ = ooxml.Each(dec, func(se xml.StartElement) (bool, error) {
				if se.Name.Local == "body" {
					return true, c.body(dec, d.main)
				}
				return false, nil
			})
		}
	}
	c.notes()
	if len(c.out) == 0 {
		c.out = append(c.out, markdown.Block{Kind: markdown.Para,
			Spans: []markdown.Span{{Text: "(這份文件沒有可以顯示的內容)"}}})
	}
	return c.out
}
