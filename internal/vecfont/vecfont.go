// Package vecfont 回答一個問題:「這個字的外框長什麼樣」。
//
// 點陣那一路(internal/fnt、internal/eten、internal/ttf)產出的是固定格點的
// 字模,格點畫面用那個就夠。但有些地方要在任意字級、任意位置畫字 ——
// SVG 裡的標題與座標軸刻度就是這樣 —— 那時需要的是外框,不是字模。
//
// 字型來源與點陣那一路共用:先問內嵌的,再問系統的(internal/ttf 的候選清單)。
// 每個字逐一問下去,第一個畫得出來的就用,所以中英混排不必先決定字型。
package vecfont

import (
	"os"
	"sync"

	"golang.org/x/image/font"
	"golang.org/x/image/font/sfnt"
	"golang.org/x/image/math/fixed"

	"github.com/wicanr2/wincv-remake/internal/bundled"
	"github.com/wicanr2/wincv-remake/internal/ttf"
)

// Em 是外框座標的字身大小。外框的數值以此為單位,乘上「字級 / Em」
// 就落到畫面上。
const Em = 1000

// MaxFonts 是最多載幾份字型。
//
// 一台機器上可以有上百份字型,全部載進來只為了找幾個罕用字並不划算。
// 前幾份(通常含一份 CJK)已經涵蓋絕大多數內容。
const MaxFonts = 8

// Seg 是外框的一段。Op 是 'm' 移動、'l' 直線、'q' 二次貝茲、'c' 三次貝茲;
// Args 依序放各控制點的 x、y。
type Seg struct {
	Op   byte
	Args [6]float64
}

// Set 是一組字型,對外像單一字型:問它一個字,它自己決定由誰畫。
type Set struct {
	once  sync.Once
	fonts []*sfnt.Font

	mu    sync.Mutex
	buf   sfnt.Buffer
	cache map[rune]glyph
}

type glyph struct {
	segs []Seg
	adv  float64
	ok   bool
}

var std = &Set{}

// Default 是共用的那一組。字型只在第一次要用時才載入。
func Default() *Set { return std }

// Glyph 回一個字的外框與前進寬度(單位同 Em)。
//
// ok 為假表示所有字型都沒有這個字。空白有前進寬度但沒有外框,
// 那時 segs 是空的而 ok 為真 —— 兩者要分開,不然空白會被當成缺字。
func (s *Set) Glyph(r rune) (segs []Seg, adv float64, ok bool) {
	s.load()
	s.mu.Lock()
	defer s.mu.Unlock()
	if g, hit := s.cache[r]; hit {
		return g.segs, g.adv, g.ok
	}
	g := s.lookup(r)
	if s.cache == nil {
		s.cache = map[rune]glyph{}
	}
	s.cache[r] = g
	return g.segs, g.adv, g.ok
}

// Ready 說明有沒有任何字型可用。沒有的話畫字這件事整個做不到,
// 呼叫端要能分辨「這段沒有字」與「這台機器沒有字型」。
func (s *Set) Ready() bool {
	s.load()
	return len(s.fonts) > 0
}

func (s *Set) lookup(r rune) glyph {
	for _, sf := range s.fonts {
		idx, err := sf.GlyphIndex(&s.buf, r)
		if err != nil || idx == 0 {
			continue
		}
		a, err := sf.GlyphAdvance(&s.buf, idx, fixed.I(Em), font.HintingNone)
		if err != nil {
			continue
		}
		segs, err := sf.LoadGlyph(&s.buf, idx, fixed.I(Em), nil)
		if err != nil {
			// 彩色 emoji 之類的沒有單色外框。前進寬度還是對的,
			// 讓後面的字排在正確位置上。
			return glyph{adv: fromFixed(a), ok: true}
		}
		return glyph{segs: convert(segs), adv: fromFixed(a), ok: true}
	}
	return glyph{}
}

// convert 把 sfnt 的線段換成本套件的形式。sfnt 的座標 Y 軸向下,
// 與畫面同向,所以不必翻轉。
func convert(in sfnt.Segments) []Seg {
	out := make([]Seg, 0, len(in))
	for _, s := range in {
		var g Seg
		switch s.Op {
		case sfnt.SegmentOpMoveTo:
			g = Seg{Op: 'm'}
			g.Args[0], g.Args[1] = fromFixed(s.Args[0].X), fromFixed(s.Args[0].Y)
		case sfnt.SegmentOpLineTo:
			g = Seg{Op: 'l'}
			g.Args[0], g.Args[1] = fromFixed(s.Args[0].X), fromFixed(s.Args[0].Y)
		case sfnt.SegmentOpQuadTo:
			g = Seg{Op: 'q'}
			g.Args[0], g.Args[1] = fromFixed(s.Args[0].X), fromFixed(s.Args[0].Y)
			g.Args[2], g.Args[3] = fromFixed(s.Args[1].X), fromFixed(s.Args[1].Y)
		case sfnt.SegmentOpCubeTo:
			g = Seg{Op: 'c'}
			g.Args[0], g.Args[1] = fromFixed(s.Args[0].X), fromFixed(s.Args[0].Y)
			g.Args[2], g.Args[3] = fromFixed(s.Args[1].X), fromFixed(s.Args[1].Y)
			g.Args[4], g.Args[5] = fromFixed(s.Args[2].X), fromFixed(s.Args[2].Y)
		default:
			continue
		}
		out = append(out, g)
	}
	return out
}

func fromFixed(v fixed.Int26_6) float64 { return float64(v) / 64 }

func (s *Set) load() {
	s.once.Do(func() {
		for _, b := range bundled.Fallbacks() {
			if len(s.fonts) >= MaxFonts {
				return
			}
			if sf := Parse(b); sf != nil {
				s.fonts = append(s.fonts, sf)
			}
		}
		seen := map[string]bool{}
		for _, path := range ttf.Candidates() {
			if len(s.fonts) >= MaxFonts || seen[path] {
				continue
			}
			seen[path] = true
			b, err := os.ReadFile(path)
			if err != nil {
				continue
			}
			if sf := Parse(b); sf != nil {
				s.fonts = append(s.fonts, sf)
			}
		}
	})
}

// Parse 解析一份 TrueType / OpenType 字型。
func Parse(b []byte) *sfnt.Font {
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
