package render

import (
	"testing"

	"github.com/wicanr2/wincv-remake/internal/cell"
	"github.com/wicanr2/wincv-remake/internal/fnt"
)

// 帶覆蓋率的字模要混色,不是非黑即白。
func TestAlphaGlyphBlends(t *testing.T) {
	half := &stubHalf{w: 8, h: 15}
	cjk := srcFunc(func(r rune) *fnt.Glyph {
		g := &fnt.Glyph{W: 16, H: 16, Bits: make([]bool, 256), Alpha: make([]uint8, 256)}
		g.Alpha[0] = 255 // 全蓋
		g.Alpha[1] = 128 // 一半
		g.Bits[0], g.Bits[1] = true, true
		return g
	})
	r := New(half, cjk)
	s := cell.New(2, 1)
	s.Print(0, 0, "中", cell.White, cell.Black)
	img := r.Draw(s)
	full := img.RGBAAt(0, 0)
	halfPx := img.RGBAAt(1, 0)
	if full.R != 255 {
		t.Fatalf("全蓋的像素 R=%d,預期 255", full.R)
	}
	if halfPx.R < 100 || halfPx.R > 160 {
		t.Fatalf("一半覆蓋的像素 R=%d,預期在 128 附近", halfPx.R)
	}
}

// 16×15 放進 16×16 是補一列空白,不是把 15 列拉成 16 列。
func TestScaleCJKPadsLineGap(t *testing.T) {
	src := srcFunc(func(r rune) *fnt.Glyph {
		g := &fnt.Glyph{W: 16, H: 15, Bits: make([]bool, 240)}
		for x := 0; x < 16; x++ {
			g.Bits[14*16+x] = true // 只有最後一列有筆畫
		}
		return g
	})
	g := ScaleCJK(src, 16, 15, 16, 16).Glyph('中')
	if g.H != 16 || !g.At(0, 14) || g.At(0, 15) || g.At(0, 13) {
		t.Fatalf("補列距補錯了:row13=%v row14=%v row15=%v", g.At(0, 13), g.At(0, 14), g.At(0, 15))
	}
}

type srcFunc func(rune) *fnt.Glyph

func (f srcFunc) Glyph(r rune) *fnt.Glyph { return f(r) }
