package render

import "github.com/wicanr2/wincv-remake/internal/fnt"

// Sources 是一串來源,取第一個有字的。
//
// 用途:大字級的全形字先問向量字型(反鋸齒),沒有的字再退到倚天縮放。
type Sources []CJKSource

func (ss Sources) Glyph(r rune) *fnt.Glyph {
	for _, s := range ss {
		if s == nil {
			continue
		}
		if g := s.Glyph(r); g != nil {
			return g
		}
	}
	return nil
}

// scaledCJK 把一個全形字模來源縮放到指定尺寸。
//
// 為什麼需要:原版隨附三種尺寸的半形字型(8×15 / 10×18 / 12×24),
// 換字級時半形字有真的點陣可用;但倚天字庫的漢字只有 15 點那一份
// (24 點的 STD.24x 是 ETUNPACK 壓縮的,還沒支援)。所以其他字級的
// 全形字只能從 16×15 縮放。
//
// 用最近鄰:點陣字放大要保持邊緣硬,做插值會糊成一團,那比鋸齒難看得多。
type scaledCJK struct {
	src    CJKSource
	sw, sh int
	w, h   int
}

// ScaleCJK 把 src(原本是 sw×sh)包成輸出 w×h 的來源。
// 尺寸相同就原樣回傳,不多繞一層。
func ScaleCJK(src CJKSource, sw, sh, w, h int) CJKSource {
	if src == nil || (sw == w && sh == h) {
		return src
	}
	return &scaledCJK{src: src, sw: sw, sh: sh, w: w, h: h}
}

func (s *scaledCJK) Glyph(r rune) *fnt.Glyph {
	g := s.src.Glyph(r)
	if g == nil {
		return nil
	}
	out := &fnt.Glyph{W: s.w, H: s.h, Bits: make([]bool, s.w*s.h)}
	// 寬一樣、只差列距那一列時是**補白**不是縮放:16×15 的字模放進
	// 16×16 的格子,多的那一列就是列距,該留白。拿 15 列去拉成 16 列
	// 會把某一列畫兩次,字看起來像被壓扁又接了一截。
	if g.W == s.w && s.h > g.H && s.h-g.H <= LineGap {
		copy(out.Bits, g.Bits)
		return out
	}
	for y := 0; y < s.h; y++ {
		sy := y * g.H / s.h
		for x := 0; x < s.w; x++ {
			sx := x * g.W / s.w
			out.Bits[y*s.w+x] = g.At(sx, sy)
		}
	}
	return out
}
