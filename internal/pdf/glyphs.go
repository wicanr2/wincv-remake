package pdf

import (
	"os"
	"sync"

	"golang.org/x/image/font/sfnt"
	"golang.org/x/image/math/fixed"

	"github.com/wicanr2/wincv-remake/internal/ttf"
)

// glyphEm 是取字形外框時用的字身高度。
//
// 取 1000 是因為 PDF 的字形空間就是千分之一字身 —— 外框拿到手之後
// 只要再乘上字級的變換矩陣就位置正確,中間不必再換算一次單位。
const glyphEm = 1000

// glyphCache 依字型留住解析好的外框來源。
//
// 一頁裡同一個字型會被查上千次,每次重新解析一份幾百 KB 的字型程式
// 是畫一頁要好幾秒的主因。
type glyphCache struct {
	sources map[*Font]*outlines
	buf     sfnt.Buffer

	fbOnce sync.Once
	fb     []*sfnt.Font
}

func newGlyphCache() *glyphCache {
	return &glyphCache{sources: map[*Font]*outlines{}}
}

// outlines 是一個字型的外框來源。
type outlines struct {
	sf *sfnt.Font
	// composite 為真表示字碼要先變成 CID 再變成字形編號。
	composite bool
	// hasCmap 為真表示這份字型帶著可用的字碼對照表。
	//
	// 這件事決定了「查不到字形」時該怎麼辦:有對照表就相信它,查不到
	// 就是真的沒有;沒有對照表(子集化時被拿掉)才輪得到「字碼就是
	// 字形編號」這條退路。分不清楚的話,子集字型裡查不到的空白會被
	// 當成第 32 號字形畫出來 —— 而那是一個看起來很正常的字。
	hasCmap bool
}

// source 取一個字型的外框來源。解不開的回 nil,由後備字型接手。
func (c *glyphCache) source(f *Font) *outlines {
	if o, ok := c.sources[f]; ok {
		return o
	}
	var o *outlines
	if f.kind == progSFNT && len(f.embedded) > 0 {
		if sf := parseSFNT(f.embedded); sf != nil {
			o = &outlines{sf: sf, composite: f.composite, hasCmap: c.probeCmap(sf)}
		}
	}
	c.sources[f] = o
	return o
}

// parseSFNT 解析一份 TrueType / OpenType 字型。
func parseSFNT(b []byte) *sfnt.Font {
	if sf, err := sfnt.Parse(b); err == nil {
		return sf
	}
	// 字型集合(.ttc)要用另一個入口。裡面通常是同一套字的不同字重,
	// 取第一個就夠。
	if col, err := sfnt.ParseCollection(b); err == nil && col.NumFonts() > 0 {
		if sf, err := col.Font(0); err == nil {
			return sf
		}
	}
	return nil
}

// glyphOrigin 說明一個字的外框是從哪裡來的。
type glyphOrigin int

const (
	fromNone     glyphOrigin = iota
	fromEmbedded             // 檔案裡嵌的字型
	fromFallback             // 系統字型補的
)

// segments 取一個字的外框,座標是千分之一字身、Y 軸向上。
//
// 嵌入的字型解不開(格式還不會解)或裡面沒有這個字時,改用系統字型 ——
// 字形不同但位置、字級與內容都對。回傳 fromNone 表示兩邊都畫不出來。
func (c *glyphCache) segments(f *Font, g Glyph) ([]sfnt.Segment, glyphOrigin) {
	if o := c.source(f); o != nil {
		if segs, ok := c.load(o.sf, c.gidOf(o, f, g)); ok {
			return segs, fromEmbedded
		}
	}
	if segs, ok := c.fallbackSegments(g); ok {
		return segs, fromFallback
	}
	return nil, fromNone
}

// gidOf 算出一個字在字型裡的字形編號。
func (c *glyphCache) gidOf(o *outlines, f *Font, g Glyph) sfnt.GlyphIndex {
	if o.composite {
		cid := g.Code
		if f.enc != nil {
			if v, ok := f.enc.cid(g.Code); ok {
				cid = v
			}
		}
		// [雷] CIDToGIDMap 是**大端序的兩位元組**一組。照小端序讀會
		// 取到完全不同的字形,而畫出來仍然是「某些字」,不是空白 ——
		// 看起來像字型壞掉,不像對照表讀錯。
		if m := f.cidToGID; m != nil {
			if i := int(cid) * 2; i+1 < len(m) {
				return sfnt.GlyphIndex(uint16(m[i])<<8 | uint16(m[i+1]))
			}
			return 0
		}
		return sfnt.GlyphIndex(cid)
	}
	// 簡單字型:先用解出來的文字去查字型自己的對照表。
	if o.hasCmap {
		if g.Text != "" {
			r := []rune(g.Text)[0]
			if idx, err := o.sf.GlyphIndex(&c.buf, r); err == nil && idx != 0 {
				return idx
			}
		}
		// 符號字型的對照表放在私人使用區(0xF000 起),Unicode 查不到。
		if idx, err := o.sf.GlyphIndex(&c.buf, rune(0xF000+(g.Code&0xFF))); err == nil && idx != 0 {
			return idx
		}
		// 有對照表卻查不到,就是這份字型真的沒有這個字。交給後備字型,
		// 不要拿字碼去猜編號 —— 猜出來的是別的字,而且長得很正常。
		return 0
	}
	// 沒有對照表(子集化時被拿掉)時,字碼就是字形編號。
	return sfnt.GlyphIndex(g.Code)
}

// probeCmap 問這份字型認不認得幾個常見的字。
//
// 用探測而不是直接讀 cmap 表:x/image/font/sfnt 查不到字時回的是
// 「第 0 號字形」,與「這份字型根本沒有對照表」長得一模一樣。
func (c *glyphCache) probeCmap(sf *sfnt.Font) bool {
	for _, r := range []rune{'A', 'a', 'e', '0', '中', 'の'} {
		if idx, err := sf.GlyphIndex(&c.buf, r); err == nil && idx != 0 {
			return true
		}
	}
	return false
}

func (c *glyphCache) load(sf *sfnt.Font, gid sfnt.GlyphIndex) ([]sfnt.Segment, bool) {
	if sf == nil || int(gid) >= sf.NumGlyphs() {
		return nil, false
	}
	segs, err := sf.LoadGlyph(&c.buf, gid, fixed.I(glyphEm), nil)
	if err != nil || len(segs) == 0 {
		return nil, false
	}
	return segs, true
}

// fallbackSegments 用系統字型畫一個字。
//
// 字形跟原檔不同,但位置、字級與內容都是對的。這是「畫不出來」與
// 「整段空白」之間的選擇 —— 少一段文字使用者看得出來,換一套字形不會。
func (c *glyphCache) fallbackSegments(g Glyph) ([]sfnt.Segment, bool) {
	if g.Text == "" {
		return nil, false
	}
	r := []rune(g.Text)[0]
	for _, sf := range c.fallbackFonts() {
		idx, err := sf.GlyphIndex(&c.buf, r)
		if err != nil || idx == 0 {
			continue
		}
		if segs, ok := c.load(sf, idx); ok {
			return segs, true
		}
	}
	return nil, false
}

// MaxFallbackFonts 是後備字型最多載幾份。
//
// 有上限是因為一台機器上可以有上百份字型,而全部載進來只是為了
// 找幾個罕用字 —— 前幾份(通常含一份 CJK)就涵蓋絕大多數內容。
const MaxFallbackFonts = 6

func (c *glyphCache) fallbackFonts() []*sfnt.Font {
	c.fbOnce.Do(func() {
		seen := map[string]bool{}
		for _, path := range ttf.Candidates() {
			if len(c.fb) >= MaxFallbackFonts || seen[path] {
				continue
			}
			seen[path] = true
			b, err := os.ReadFile(path)
			if err != nil {
				continue
			}
			if sf := parseSFNT(b); sf != nil {
				c.fb = append(c.fb, sf)
			}
		}
	})
	return c.fb
}

// glyph 把一個字畫上去。
func (d *rasterDevice) glyph(g Glyph, f *Font, trm matrix, gs *gstate) {
	// 繪製模式 3 與 7 是不可見的文字。掃描出來的 PDF 會把辨識結果
	// 疊在影像底下 —— 那些字要拿得到(取文字時照收),但不能畫出來,
	// 畫了會蓋在影像上變成兩層字。
	if gs.render == 3 || gs.render == 7 {
		return
	}
	// 空白沒有形狀。不先擋掉的話,它會一路走到「這個字型畫不出來」,
	// 然後被記成缺字,或更糟 —— 被某條退路畫成別的字形。
	if isBlank(g.Text) {
		return
	}
	segs, origin := d.glyphs.segments(f, g)
	name := "(未具名字型)"
	if f != nil && f.baseFont != "" {
		name = f.baseFont
	}
	switch origin {
	case fromNone:
		d.missing[name] = true
		return
	case fromFallback:
		d.substituted[name] = true
	}
	// 外框是千分之一字身、Y 軸向上;sfnt 交出來的 Y 軸向下,所以縱向要翻。
	m := mul(matrix{1.0 / glyphEm, 0, 0, -1.0 / glyphEm, 0, 0}, trm)

	var p path
	for _, s := range segs {
		switch s.Op {
		case sfnt.SegmentOpMoveTo:
			x, y := m.apply(f26(s.Args[0].X), f26(s.Args[0].Y))
			p.moveTo(x, y)
		case sfnt.SegmentOpLineTo:
			x, y := m.apply(f26(s.Args[0].X), f26(s.Args[0].Y))
			p.lineTo(x, y)
		case sfnt.SegmentOpQuadTo:
			// 二次貝茲升成三次:控制點各取三分之二。
			x0, y0 := lastPoint(&p)
			cx, cy := m.apply(f26(s.Args[0].X), f26(s.Args[0].Y))
			x3, y3 := m.apply(f26(s.Args[1].X), f26(s.Args[1].Y))
			p.curveTo(x0+2.0/3*(cx-x0), y0+2.0/3*(cy-y0),
				x3+2.0/3*(cx-x3), y3+2.0/3*(cy-y3), x3, y3)
		case sfnt.SegmentOpCubeTo:
			x1, y1 := m.apply(f26(s.Args[0].X), f26(s.Args[0].Y))
			x2, y2 := m.apply(f26(s.Args[1].X), f26(s.Args[1].Y))
			x3, y3 := m.apply(f26(s.Args[2].X), f26(s.Args[2].Y))
			p.curveTo(x1, y1, x2, y2, x3, y3)
		}
	}
	if p.empty() {
		return
	}
	p.close()

	// 文字用填色畫。描邊模式(1、5)在真實檔案裡幾乎只用在標題的
	// 外框字,用填色畫出來的差別看不太出來,而分開處理要多一條路徑。
	col := gs.fill
	alpha := gs.fillAlpha
	if gs.render == 1 || gs.render == 5 {
		col, alpha = gs.stroke, gs.strokeAlpha
	}
	d.drawPath(&p, col.rgba(alpha), d.clipRectOf(gs), 1, func() {
		addPath(d.filler, &p, d.offX, d.offY)
		d.filler.Draw()
		d.filler.Clear()
	})
}

// isBlank 判斷一個字是不是只有空白。
func isBlank(s string) bool {
	if s == "" {
		return true
	}
	for _, r := range s {
		switch r {
		case ' ', '\t', '\n', '\r', 0x00A0, 0x2007, 0x202F, 0x3000:
		default:
			return false
		}
	}
	return true
}

func f26(v fixed.Int26_6) float64 { return float64(v) / 64 }

// lastPoint 是路徑目前的終點。二次貝茲升三次要用到它。
func lastPoint(p *path) (float64, float64) {
	if len(p.ops) == 0 {
		return 0, 0
	}
	o := p.ops[len(p.ops)-1]
	switch o.op {
	case 'c':
		return o.p[2].x, o.p[2].y
	case 'm', 'l':
		return o.p[0].x, o.p[0].y
	}
	return 0, 0
}
