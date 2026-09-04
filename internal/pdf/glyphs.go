package pdf

import (
	"github.com/wicanr2/wincv-remake/internal/i18n"
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
	sf  *sfnt.Font
	cff *cffFont
	t1  *type1Font
	// composite 為真表示字碼要先變成 CID 再變成字形編號。
	composite bool
	// hasCmap 為真表示這份字型的字碼對照表就是這份文件用的定址方式。
	//
	// 這件事決定了「查不到字形」時該怎麼辦:對照表是定址方式就相信它,
	// 查不到就是真的沒有;不是的話才輪得到「字碼就是字形編號」這條退路。
	// 分不清楚的話,子集字型裡查不到的空白會被當成第 32 號字形畫出來
	// —— 而那是一個看起來很正常的字。
	hasCmap bool
}

// source 取一個字型的外框來源。解不開的回 nil,由後備字型接手。
func (c *glyphCache) source(f *Font) *outlines {
	if o, ok := c.sources[f]; ok {
		return o
	}
	var o *outlines
	switch {
	case f.kind == progSFNT && len(f.embedded) > 0:
		if sf := parseSFNT(f.embedded); sf != nil {
			o = &outlines{sf: sf, composite: f.composite, hasCmap: c.probeCmap(sf, f)}
		}
	case f.kind == progCFF && len(f.embedded) > 0:
		if cf, err := parseCFF(f.embedded); err == nil {
			o = &outlines{cff: cf, composite: f.composite}
		}
	case f.kind == progType1 && len(f.embedded) > 0:
		if t1, err := parseType1(f.embedded); err == nil {
			o = &outlines{t1: t1, composite: f.composite}
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
func (c *glyphCache) segments(f *Font, g Glyph) ([]gseg, glyphOrigin) {
	if o := c.source(f); o != nil {
		if o.t1 != nil {
			if segs, ok := o.t1.glyph(t1Name(o, f, g)); ok {
				return segs, fromEmbedded
			}
		} else if o.cff != nil {
			if gid, ok := c.cffGID(o, f, g); ok {
				if segs, ok := o.cff.glyph(int(gid)); ok {
					return segs, fromEmbedded
				}
			}
		} else if segs, ok := c.load(o.sf, c.gidOf(o, f, g)); ok {
			return segs, fromEmbedded
		}
	}
	if segs, ok := c.fallbackSegments(g); ok {
		return segs, fromFallback
	}
	return nil, fromNone
}

// t1Name 算出一個字在 Type1 字型裡叫什麼名字。
//
// Type1 的字形是按名字存的。名字有兩個來源:PDF 的 Encoding/Differences,
// 以及字型自己帶的編碼。前者優先 —— 它是這份文件實際用的那一套。
func t1Name(o *outlines, f *Font, g Glyph) string {
	if !o.composite && g.Code < 256 {
		if n := f.names[g.Code]; n != "" {
			return n
		}
		if n, ok := o.t1.enc[byte(g.Code)]; ok {
			return n
		}
	}
	return ""
}

// cffGID 算出一個字在 CFF 裡的字形編號。
func (c *glyphCache) cffGID(o *outlines, f *Font, g Glyph) (uint16, bool) {
	if o.cff.isCID {
		cid := g.Code
		if f.enc != nil {
			if v, ok := f.enc.cid(g.Code); ok {
				cid = v
			}
		}
		return o.cff.gidForCID(cid)
	}
	if o.cff.encoding != nil {
		gid, ok := o.cff.encoding[byte(g.Code)]
		return gid, ok
	}
	return 0, false
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

// probeCmap 問「這份字型的字碼對照表,是不是這份文件真正在用的定址方式」。
//
// 用探測而不是直接讀 cmap 表:x/image/font/sfnt 查不到字時回的是
// 「第 0 號字形」,與「這份字型根本沒有對照表」長得一模一樣。
//
// [雷] 不能只問「這張表認不認得幾個常見的字」。子集化的字型常常留著一張
// 只涵蓋少數幾個字的對照表(實測:56 個字形的子集,表裡只有兩個數字),
// 其餘的字是**用字碼直接定址**的。有一個字查得到就相信整張表的話,那份
// 字型會整個被判成「沒有這些字」而改用系統字型畫 —— 位置、字級、內容
// 全對,只有字形換了一套,看起來完全正常。
//
// 所以拿這份字型自己用到的字碼去問,大部分查得到才算數。
func (c *glyphCache) probeCmap(sf *sfnt.Font, f *Font) bool {
	hit, total := 0, 0
	for code := 0; code < 256; code++ {
		s := f.simple[code]
		if s == "" || isBlank(s) {
			continue
		}
		r := []rune(s)
		if len(r) != 1 {
			continue
		}
		total++
		if idx, err := sf.GlyphIndex(&c.buf, r[0]); err == nil && idx != 0 {
			hit++
		}
	}
	if total > 0 {
		return hit*2 >= total
	}
	// 這份字型沒有「碼 → 文字」可問(複合字型,或沒有 ToUnicode
	// 也沒有 Encoding),退回問幾個常見的字。
	for _, r := range []rune{'A', 'a', 'e', '0', '中', 'の'} {
		if idx, err := sf.GlyphIndex(&c.buf, r); err == nil && idx != 0 {
			return true
		}
	}
	return false
}

// load 取一個 TrueType / OpenType 字形的外框,並換成共用的表示法。
//
// [雷] sfnt 交出來的 Y 軸**向下**(那是 Go 繪圖的慣例),而字形空間的
// Y 軸向上。不翻的話每個字都是上下顛倒的 —— 而顛倒的字看起來仍然像字。
func (c *glyphCache) load(sf *sfnt.Font, gid sfnt.GlyphIndex) ([]gseg, bool) {
	if sf == nil || int(gid) >= sf.NumGlyphs() {
		return nil, false
	}
	segs, err := sf.LoadGlyph(&c.buf, gid, fixed.I(glyphEm), nil)
	if err != nil || len(segs) == 0 {
		return nil, false
	}
	out := make([]gseg, 0, len(segs))
	var cx, cy float64
	for _, s := range segs {
		switch s.Op {
		case sfnt.SegmentOpMoveTo:
			cx, cy = f26(s.Args[0].X), -f26(s.Args[0].Y)
			out = append(out, gseg{op: 'm', x: [3]float64{cx}, y: [3]float64{cy}})
		case sfnt.SegmentOpLineTo:
			cx, cy = f26(s.Args[0].X), -f26(s.Args[0].Y)
			out = append(out, gseg{op: 'l', x: [3]float64{cx}, y: [3]float64{cy}})
		case sfnt.SegmentOpQuadTo:
			// 二次貝茲升成三次:兩個控制點各取三分之二。
			qx, qy := f26(s.Args[0].X), -f26(s.Args[0].Y)
			ex, ey := f26(s.Args[1].X), -f26(s.Args[1].Y)
			out = append(out, gseg{op: 'c',
				x: [3]float64{cx + 2.0/3*(qx-cx), ex + 2.0/3*(qx-ex), ex},
				y: [3]float64{cy + 2.0/3*(qy-cy), ey + 2.0/3*(qy-ey), ey}})
			cx, cy = ex, ey
		case sfnt.SegmentOpCubeTo:
			out = append(out, gseg{op: 'c',
				x: [3]float64{f26(s.Args[0].X), f26(s.Args[1].X), f26(s.Args[2].X)},
				y: [3]float64{-f26(s.Args[0].Y), -f26(s.Args[1].Y), -f26(s.Args[2].Y)}})
			cx, cy = f26(s.Args[2].X), -f26(s.Args[2].Y)
		}
	}
	return out, true
}

// fallbackSegments 用系統字型畫一個字。
//
// 字形跟原檔不同,但位置、字級與內容都是對的。這是「畫不出來」與
// 「整段空白」之間的選擇 —— 少一段文字使用者看得出來,換一套字形不會。
func (c *glyphCache) fallbackSegments(g Glyph) ([]gseg, bool) {
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
	d.use(gs)
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
	name := i18n.T("(未具名字型)")
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
	// 外框是千分之一字身,乘上字級的變換矩陣就落到畫面上。
	m := mul(matrix{1.0 / glyphEm, 0, 0, 1.0 / glyphEm, 0, 0}, trm)

	var p path
	open := false
	for _, s := range segs {
		switch s.op {
		case 'm':
			// 字形的每一圈都是封閉的。開新的一圈之前要把上一圈收掉,
			// 不然內圈與外圈會被當成同一條路徑,洞就填實了。
			if open {
				p.close()
			}
			x, y := m.apply(s.x[0], s.y[0])
			p.moveTo(x, y)
			open = true
		case 'l':
			x, y := m.apply(s.x[0], s.y[0])
			p.lineTo(x, y)
		case 'c':
			x1, y1 := m.apply(s.x[0], s.y[0])
			x2, y2 := m.apply(s.x[1], s.y[1])
			x3, y3 := m.apply(s.x[2], s.y[2])
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
