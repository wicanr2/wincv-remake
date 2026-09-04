// Package pptx 讀 PowerPoint 的 .pptx(PresentationML)。
//
// 一份簡報在字元格點上就是「一張投影片一頁」:標題變成標題區塊,
// 內文的條列保留階層,表格照表格畫,圖片貼在後面,備忘稿附在最後。
//
// 三件事決定了它跟 Word 的解析不一樣:
//
//   - **閱讀順序不是文件順序。** 投影片裡的圖形是照 z 順序存的,
//     跟人的閱讀順序沒有關係。標題一律提到最前面,其餘依版面上的
//     位置由上而下、由左而右 —— 但只在每個圖形都寫明位置時才這樣排,
//     少了位置資訊的圖形會全部擠到左上角,那比原本的順序更糟。
//   - **文字可能不在投影片裡。** 佔位圖形的型別要回頭問版面配置,
//     沒有型別就分不出標題與內文。
//   - **備忘稿是另一個組件。** 靠關聯連過去。
package pptx

import (
	"encoding/xml"
	"fmt"
	"github.com/wicanr2/wincv-remake/internal/i18n"
	"path"
	"sort"
	"strconv"
	"strings"

	"github.com/wicanr2/wincv-remake/internal/markdown"
	"github.com/wicanr2/wincv-remake/internal/ooxml"
)

// MaxSlides 是張數上限。
const MaxSlides = 5000

// Deck 是一份打開著的簡報。用完要 Close。
type Deck struct {
	Title  string
	Slides []Slide

	pkg  *ooxml.Package
	imgs map[string]string
	imgN int
}

// Slide 是一張投影片。
type Slide struct {
	Title  string
	Part   string
	Blocks []markdown.Block
}

// Open 打開一份 .pptx。
func Open(name string) (*Deck, error) {
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

// New 從一個已經打開的 OPC 包建 Deck。
func New(p *ooxml.Package) (*Deck, error) {
	main := presPart(p)
	if main == "" {
		return nil, fmt.Errorf(i18n.T("這不是 PowerPoint 簡報(找不到 presentation.xml)"))
	}
	d := &Deck{pkg: p, imgs: map[string]string{}}
	d.Title = coreTitle(p)
	for i, part := range slideParts(p, main) {
		s := Slide{Part: part}
		s.Title, s.Blocks = d.slide(part, i+1)
		d.Slides = append(d.Slides, s)
	}
	if len(d.Slides) == 0 {
		return nil, fmt.Errorf(i18n.T("這份簡報沒有投影片"))
	}
	return d, nil
}

func (d *Deck) Close() error { return d.pkg.Close() }

// Image 取一張圖。
func (d *Deck) Image(ref string) ([]byte, error) {
	part, ok := d.imgs[ref]
	if !ok {
		return nil, fmt.Errorf(i18n.T("這份簡報裡沒有 %s"), ref)
	}
	return d.pkg.Bytes(part)
}

func presPart(p *ooxml.Package) string {
	for _, t := range p.RelsByType("", "/officeDocument") {
		if p.Has(t) {
			return t
		}
	}
	if p.Has("ppt/presentation.xml") {
		return "ppt/presentation.xml"
	}
	return ""
}

func coreTitle(p *ooxml.Package) string {
	for _, t := range p.RelsByType("", "/metadata/core-properties") {
		b, err := p.Bytes(t)
		if err != nil {
			continue
		}
		dec := ooxml.NewDecoder(b)
		var title string
		_ = ooxml.Each(dec, func(se xml.StartElement) (bool, error) {
			if se.Name.Local != "coreProperties" {
				return false, nil
			}
			return true, ooxml.Each(dec, func(c xml.StartElement) (bool, error) {
				if c.Name.Local == "title" {
					title = strings.TrimSpace(ooxml.Text(dec))
					return true, nil
				}
				return false, nil
			})
		})
		if title != "" {
			return title
		}
	}
	return ""
}

// slideParts 依放映順序列出投影片組件。
//
// [雷] 順序在 p:sldIdLst,不是關聯表也不是檔名。關聯表沒有順序,
// 而檔名的數字是建立順序 —— 搬動過投影片的簡報兩者就對不上,
// 而且看起來完全正常,只是順序錯了。
func slideParts(p *ooxml.Package, main string) []string {
	b, err := p.Bytes(main)
	if err != nil {
		return nil
	}
	var out []string
	dec := ooxml.NewDecoder(b)
	root, err := ooxml.Root(dec)
	if err != nil || root.Name.Local != "presentation" {
		return nil
	}
	_ = ooxml.Each(dec, func(se xml.StartElement) (bool, error) {
		if se.Name.Local != "sldIdLst" {
			return false, nil
		}
		return true, ooxml.Each(dec, func(s xml.StartElement) (bool, error) {
			if s.Name.Local != "sldId" || len(out) >= MaxSlides {
				return false, nil
			}
			if t := p.RelTarget(main, ooxml.RelID(s)); t != "" && p.Has(t) {
				out = append(out, t)
			}
			return false, nil
		})
	})
	return out
}

// phKind 是佔位圖形的角色。
type phKind int

const (
	phNone phKind = iota
	phTitle
	phBody
)

// shape 是投影片上的一個圖形,連同它排出來的區塊。
type shape struct {
	kind   phKind
	x, y   int64
	hasPos bool
	blocks []markdown.Block
	title  string
	// plain 是這個圖形的文字壓成一行,paras 是它有幾段。
	// 沒有佔位資訊的簡報要靠這兩個值認出哪一個是標題。
	plain string
	paras int
}

func (d *Deck) slide(part string, num int) (string, []markdown.Block) {
	layout := d.layoutOf(part)
	shapes := d.spTreeOf(part, layout)

	var title string
	var rest []markdown.Block
	var body []shape
	for _, s := range shapes {
		if s.kind == phTitle && title == "" && s.title != "" {
			title = s.title
			continue
		}
		body = append(body, s)
	}
	// 只有在每個圖形都寫明位置時才依位置排。少一個就整批照原順序 ——
	// 沒有位置的圖形會被當成 (0,0) 擠到最前面,那比 z 順序更難讀。
	allPos := true
	for _, s := range body {
		if !s.hasPos {
			allPos = false
			break
		}
	}
	if allPos && len(body) > 1 {
		sort.SliceStable(body, func(i, j int) bool {
			if body[i].y != body[j].y {
				return body[i].y < body[j].y
			}
			return body[i].x < body[j].x
		})
	}
	// 沒有佔位資訊的簡報(很多轉檔工具產出的都是)整張都是普通文字方塊。
	// 這時候取排在最前面、只有一段、又短的那一個當標題 —— 那就是
	// 人看投影片時當成標題的東西。條件下得緊一點:猜錯的代價是
	// 一段內文被抬成標題,而漏猜只是少一個標題。
	if title == "" && len(body) > 0 {
		if s := body[0]; s.paras == 1 && s.plain != "" && len([]rune(s.plain)) <= 60 {
			title = s.plain
			body = body[1:]
		}
	}
	for _, s := range body {
		rest = append(rest, s.blocks...)
	}

	out := []markdown.Block{}
	if title != "" {
		out = append(out, markdown.Block{Kind: markdown.Heading, Level: 1,
			Spans: []markdown.Span{{Text: title}}})
	} else {
		out = append(out, markdown.Block{Kind: markdown.Heading, Level: 1,
			Spans: []markdown.Span{{Text: fmt.Sprintf(i18n.T("第 %d 張"), num)}}})
	}
	out = append(out, rest...)
	out = append(out, d.notes(part)...)
	if len(out) == 1 {
		out = append(out, markdown.Block{Kind: markdown.Para,
			Spans: []markdown.Span{{Text: i18n.T("(這一張沒有文字)")}}})
	}
	return title, out
}

// layoutOf 找出投影片用的版面配置,並回傳「佔位編號 → 角色」的對照。
//
// 需要這一層轉接是因為投影片自己的佔位圖形常常只寫 idx 不寫 type,
// 型別留在版面配置裡。少了它就分不出標題與內文,整張投影片會變成
// 一堆沒有階層的段落。
func (d *Deck) layoutOf(part string) map[string]phKind {
	out := map[string]phKind{}
	var lay string
	for _, t := range d.pkg.RelsByType(part, "/slideLayout") {
		if d.pkg.Has(t) {
			lay = t
			break
		}
	}
	if lay == "" {
		return out
	}
	b, err := d.pkg.Bytes(lay)
	if err != nil {
		return out
	}
	dec := ooxml.NewDecoder(b)
	var walk func(*xml.Decoder) error
	walk = func(dd *xml.Decoder) error {
		return ooxml.Each(dd, func(se xml.StartElement) (bool, error) {
			if se.Name.Local == "ph" {
				idx := ooxml.Attr(se, "idx")
				if idx == "" {
					idx = "0"
				}
				out[idx] = phOf(ooxml.Attr(se, "type"))
				return false, nil
			}
			return true, walk(dd)
		})
	}
	_ = walk(dec)
	return out
}

func phOf(typ string) phKind {
	switch typ {
	case "title", "ctrTitle":
		return phTitle
	case "":
		// 沒寫型別時,規格的預設值是內文。
		return phBody
	default:
		return phBody
	}
}

// spTreeOf 走一張投影片的圖形樹。
func (d *Deck) spTreeOf(part string, layout map[string]phKind) []shape {
	b, err := d.pkg.Bytes(part)
	if err != nil {
		return nil
	}
	dec := ooxml.NewDecoder(b)
	root, err := ooxml.Root(dec)
	if err != nil || (root.Name.Local != "sld" && root.Name.Local != "notes") {
		return nil
	}
	var out []shape
	var walk func(*xml.Decoder) error
	walk = func(dd *xml.Decoder) error {
		return ooxml.Each(dd, func(se xml.StartElement) (bool, error) {
			switch se.Name.Local {
			case "spTree", "grpSp", "cSld":
				return true, walk(dd)
			case "sp", "cxnSp":
				s := d.shapeOf(dd, part, layout)
				if len(s.blocks) > 0 || s.title != "" {
					out = append(out, s)
				}
				return true, nil
			case "pic":
				s := d.picOf(dd, part)
				if len(s.blocks) > 0 {
					out = append(out, s)
				}
				return true, nil
			case "graphicFrame":
				s := d.frameOf(dd, part)
				if len(s.blocks) > 0 {
					out = append(out, s)
				}
				return true, nil
			case "AlternateContent":
				used := false
				return true, ooxml.Each(dd, func(a xml.StartElement) (bool, error) {
					if used {
						return false, nil
					}
					switch a.Name.Local {
					case "Choice", "Fallback":
						used = true
						return true, walk(dd)
					}
					return false, nil
				})
			}
			return false, nil
		})
	}
	_ = walk(dec)
	return out
}

// shapeOf 走一個一般圖形:佔位角色、位置、文字。
func (d *Deck) shapeOf(dec *xml.Decoder, part string, layout map[string]phKind) shape {
	s := shape{kind: phNone}
	haveKind := false
	_ = ooxml.Each(dec, func(se xml.StartElement) (bool, error) {
		switch se.Name.Local {
		case "nvSpPr", "nvCxnSpPr", "nvPr":
			return true, ooxml.Each(dec, func(n xml.StartElement) (bool, error) {
				if n.Name.Local == "nvPr" {
					return true, ooxml.Each(dec, func(p xml.StartElement) (bool, error) {
						if p.Name.Local == "ph" {
							typ := ooxml.Attr(p, "type")
							if typ != "" {
								s.kind, haveKind = phOf(typ), true
							} else if k, ok := layout[phIdx(p)]; ok {
								s.kind, haveKind = k, true
							} else {
								s.kind, haveKind = phBody, true
							}
						}
						return false, nil
					})
				}
				return false, nil
			})
		case "spPr":
			return true, ooxml.Each(dec, func(p xml.StartElement) (bool, error) {
				if p.Name.Local != "xfrm" {
					return false, nil
				}
				return true, ooxml.Each(dec, func(x xml.StartElement) (bool, error) {
					if x.Name.Local == "off" {
						s.x = atoi64(ooxml.Attr(x, "x"))
						s.y = atoi64(ooxml.Attr(x, "y"))
						s.hasPos = true
					}
					return false, nil
				})
			})
		case "txBody", "txbxContent":
			blocks, plain := d.textBody(dec, part, s.kind)
			s.blocks = append(s.blocks, blocks...)
			s.plain, s.paras = plain, len(blocks)
			if s.kind == phTitle {
				s.title = plain
			}
			return true, nil
		}
		return false, nil
	})
	if !haveKind {
		s.kind = phNone
	}
	// 標題佔位的內容已經抽成 title,區塊不重複留一份。
	if s.kind == phTitle && s.title != "" {
		s.blocks = nil
	}
	return s
}

func phIdx(se xml.StartElement) string {
	if v := ooxml.Attr(se, "idx"); v != "" {
		return v
	}
	return "0"
}

func atoi64(s string) int64 {
	n, _ := strconv.ParseInt(strings.TrimSpace(s), 10, 64)
	return n
}

// textBody 走一個文字方塊,回傳區塊與壓成一行的純文字。
func (d *Deck) textBody(dec *xml.Decoder, part string, kind phKind) ([]markdown.Block, string) {
	var out []markdown.Block
	var plain []string
	_ = ooxml.Each(dec, func(se xml.StartElement) (bool, error) {
		if se.Name.Local != "p" {
			return false, nil
		}
		blocks, text := d.paragraph(dec, part, kind)
		out = append(out, blocks...)
		if text != "" {
			plain = append(plain, text)
		}
		return true, nil
	})
	return out, strings.Join(plain, " ")
}

// paragraph 走一個段落。
func (d *Deck) paragraph(dec *xml.Decoder, part string, kind phKind) ([]markdown.Block, string) {
	lvl := 0
	bullet := kind != phTitle // 內文預設有項目符號,標題沒有
	marker := ""
	ordered := false
	var spans []markdown.Span
	var out []markdown.Block

	flush := func() {
		if len(spans) == 0 {
			return
		}
		switch {
		case kind == phTitle:
			out = append(out, markdown.Block{Kind: markdown.Para, Spans: spans})
		case bullet:
			b := markdown.Block{Kind: markdown.List, Level: lvl + 1, Spans: spans, Ordered: ordered}
			if !ordered && marker != "" {
				b.Marker = marker
			}
			out = append(out, b)
		default:
			out = append(out, markdown.Block{Kind: markdown.Para, Spans: spans})
		}
		spans = nil
	}

	_ = ooxml.Each(dec, func(se xml.StartElement) (bool, error) {
		switch se.Name.Local {
		case "pPr":
			lvl = clamp(atoiDef(ooxml.Attr(se, "lvl"), 0), 0, 8)
			return true, ooxml.Each(dec, func(p xml.StartElement) (bool, error) {
				switch p.Name.Local {
				case "buNone":
					bullet = false
				case "buChar":
					bullet = true
					marker = safeMarker(ooxml.Attr(p, "char"))
				case "buAutoNum":
					bullet, ordered = true, true
				}
				return false, nil
			})
		case "r", "fld":
			// a:fld 是自動更新的欄位(頁碼、日期),裡面存著上次算出來
			// 的值。那個值就是簡報放映時看到的字,照收。
			spans = append(spans, d.run(dec, part)...)
			return true, nil
		case "br":
			flush()
		}
		return false, nil
	})
	flush()

	var sb strings.Builder
	for _, b := range out {
		for _, s := range b.Spans {
			sb.WriteString(s.Text)
		}
	}
	return out, strings.TrimSpace(sb.String())
}

// safeMarker 擋掉畫不出來的項目符號(Wingdings 落在私人使用區)。
func safeMarker(s string) string {
	r := []rune(s)
	if len(r) != 1 || (r[0] >= 0xE000 && r[0] <= 0xF8FF) || r[0] < 0x20 {
		return ""
	}
	return s
}

func (d *Deck) run(dec *xml.Decoder, part string) []markdown.Span {
	style := markdown.Style(0)
	href := ""
	var text string
	_ = ooxml.Each(dec, func(se xml.StartElement) (bool, error) {
		switch se.Name.Local {
		case "rPr":
			if ooxml.Attr(se, "b") == "1" {
				style |= markdown.Bold
			}
			if ooxml.Attr(se, "i") == "1" {
				style |= markdown.Italic
			}
			if strings.HasPrefix(ooxml.Attr(se, "strike"), "sng") ||
				strings.HasPrefix(ooxml.Attr(se, "strike"), "dbl") {
				style |= markdown.Strike
			}
			return true, ooxml.Each(dec, func(p xml.StartElement) (bool, error) {
				if p.Name.Local == "hlinkClick" {
					if id := ooxml.RelID(p); id != "" {
						if r, ok := d.pkg.Rels(part)[id]; ok && r.External {
							href = r.Target
						}
					}
				}
				return false, nil
			})
		case "t":
			text += ooxml.Text(dec)
			return true, nil
		}
		return false, nil
	})
	if text == "" {
		return nil
	}
	sp := markdown.Span{Text: text, Style: style}
	if href != "" {
		sp.Style |= markdown.Link
		sp.Href = href
	}
	return []markdown.Span{sp}
}

// picOf 走一張圖。
func (d *Deck) picOf(dec *xml.Decoder, part string) shape {
	s := shape{kind: phNone}
	alt := ""
	var walk func(*xml.Decoder) error
	walk = func(dd *xml.Decoder) error {
		return ooxml.Each(dd, func(se xml.StartElement) (bool, error) {
			switch se.Name.Local {
			case "cNvPr":
				if v := ooxml.Attr(se, "descr"); v != "" {
					alt = v
				} else if v := ooxml.Attr(se, "name"); v != "" && alt == "" {
					alt = v
				}
				return false, nil
			case "off":
				s.x, s.y, s.hasPos = atoi64(ooxml.Attr(se, "x")), atoi64(ooxml.Attr(se, "y")), true
				return false, nil
			case "blip":
				if id := ooxml.RelID(se); id != "" {
					if b, ok := d.imageBlock(part, id, alt); ok {
						s.blocks = append(s.blocks, b)
					}
				}
				return false, nil
			}
			return true, walk(dd)
		})
	}
	_ = walk(dec)
	return s
}

func (d *Deck) imageBlock(owner, relID, alt string) (markdown.Block, bool) {
	target := d.pkg.RelTarget(owner, relID)
	if target == "" || !d.pkg.Has(target) {
		return markdown.Block{}, false
	}
	d.imgN++
	ref := fmt.Sprintf("%d-%s", d.imgN, path.Base(target))
	d.imgs[ref] = target
	if alt == "" {
		alt = path.Base(target)
	}
	return markdown.Block{Kind: markdown.Image, Src: ref, Alt: alt}, true
}

// frameOf 走一個圖形框:表格與圖表都包在這裡面。
func (d *Deck) frameOf(dec *xml.Decoder, part string) shape {
	s := shape{kind: phNone}
	var walk func(*xml.Decoder) error
	walk = func(dd *xml.Decoder) error {
		return ooxml.Each(dd, func(se xml.StartElement) (bool, error) {
			switch se.Name.Local {
			case "off":
				s.x, s.y, s.hasPos = atoi64(ooxml.Attr(se, "x")), atoi64(ooxml.Attr(se, "y")), true
				return false, nil
			case "tbl":
				if b, ok := d.table(dd, part); ok {
					s.blocks = append(s.blocks, b)
				}
				return true, nil
			case "chart":
				// 圖表的資料在另一個組件裡,畫成表格會是一份跟畫面
				// 對不起來的數字。只標出這裡有一張圖表。
				s.blocks = append(s.blocks, markdown.Block{Kind: markdown.Para,
					Spans: []markdown.Span{{Text: i18n.T("(圖表)"), Style: markdown.Italic}}})
				return false, nil
			}
			return true, walk(dd)
		})
	}
	_ = walk(dec)
	return s
}

func (d *Deck) table(dec *xml.Decoder, part string) (markdown.Block, bool) {
	var rows [][]string
	_ = ooxml.Each(dec, func(se xml.StartElement) (bool, error) {
		if se.Name.Local != "tr" {
			return false, nil
		}
		var cells []string
		err := ooxml.Each(dec, func(tc xml.StartElement) (bool, error) {
			if tc.Name.Local != "tc" {
				return false, nil
			}
			span := clamp(atoiDef(ooxml.Attr(tc, "gridSpan"), 1), 1, 64)
			var text string
			err := ooxml.Each(dec, func(in xml.StartElement) (bool, error) {
				if in.Name.Local != "txBody" {
					return false, nil
				}
				_, text = d.textBody(dec, part, phTitle)
				return true, nil
			})
			cells = append(cells, strings.TrimSpace(text))
			for i := 1; i < span; i++ {
				cells = append(cells, "")
			}
			return true, err
		})
		rows = append(rows, cells)
		return true, err
	})
	if len(rows) == 0 {
		return markdown.Block{}, false
	}
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
	return markdown.Block{Kind: markdown.Table, Rows: rows}, true
}

// notes 取備忘稿。
func (d *Deck) notes(part string) []markdown.Block {
	var np string
	for _, t := range d.pkg.RelsByType(part, "/notesSlide") {
		if d.pkg.Has(t) {
			np = t
			break
		}
	}
	if np == "" {
		return nil
	}
	shapes := d.spTreeOf(np, nil)
	var body []markdown.Block
	for _, s := range shapes {
		for _, b := range s.blocks {
			// 備忘稿頁上那個「投影片編號」佔位只有一個數字,不是備忘稿。
			if b.Kind == markdown.Image {
				continue
			}
			body = append(body, b)
		}
	}
	if len(body) == 0 {
		return nil
	}
	out := []markdown.Block{
		{Kind: markdown.Rule},
		{Kind: markdown.Heading, Level: 3, Spans: []markdown.Span{{Text: i18n.T("備忘稿")}}},
	}
	for _, b := range body {
		// 備忘稿在畫面上用引言區塊,跟投影片本身的內容分開。
		out = append(out, markdown.Block{Kind: markdown.Quote, Spans: b.Spans})
	}
	return out
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
