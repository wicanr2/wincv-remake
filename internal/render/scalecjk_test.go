package render

import (
	"testing"

	"github.com/wicanr2/wincv-remake/internal/fnt"
)

type oneGlyph struct{ g *fnt.Glyph }

func (o oneGlyph) Glyph(rune) *fnt.Glyph { return o.g }

// 尺寸相同就要原樣回傳,不包一層 —— 15 點那一級是最常用的,
// 每個全形字都多繞一次縮放迴圈是白花的。
func TestScaleCJKPassesThroughSameSize(t *testing.T) {
	src := oneGlyph{&fnt.Glyph{W: 16, H: 15, Bits: make([]bool, 16*15)}}
	if got := ScaleCJK(src, 16, 15, 16, 15); got != CJKSource(src) {
		t.Fatal("同尺寸應該原樣回傳")
	}
	if ScaleCJK(nil, 16, 15, 24, 24) != nil {
		t.Fatal("nil 來源應該回 nil")
	}
}

// 放大要保持邊緣硬(最近鄰),而且四個角要對得上 —— 
// 角落錯位在真的字上看起來只是「有點糊」,很難用眼睛抓。
func TestScaleCJKNearestNeighbour(t *testing.T) {
	g := &fnt.Glyph{W: 2, H: 2, Bits: []bool{true, false, false, true}}
	s := ScaleCJK(oneGlyph{g}, 2, 2, 4, 4).Glyph('一')
	if s.W != 4 || s.H != 4 {
		t.Fatalf("尺寸 = %d×%d, 想要 4×4", s.W, s.H)
	}
	want := [][]bool{
		{true, true, false, false},
		{true, true, false, false},
		{false, false, true, true},
		{false, false, true, true},
	}
	for y := range want {
		for x := range want[y] {
			if s.At(x, y) != want[y][x] {
				t.Errorf("(%d,%d) = %v, 想要 %v", x, y, s.At(x, y), want[y][x])
			}
		}
	}
}

// 來源沒有這個字時要一路回 nil,不能生出一個空白字模 —— 
// 空白字模會讓上層的缺字標記(MissingMark)失效。
func TestScaleCJKMissingStaysMissing(t *testing.T) {
	if got := ScaleCJK(oneGlyph{nil}, 16, 15, 24, 24).Glyph('X'); got != nil {
		t.Fatalf("缺字應該回 nil, 得到 %v", got)
	}
}
