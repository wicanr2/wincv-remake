package fnt

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// 原版素材不進版控(見 .gitignore),沒有就跳過。
func load(t *testing.T, name string) *Font {
	t.Helper()
	p := filepath.Join("..", "..", "original", "app", name)
	d, err := os.ReadFile(p)
	if err != nil {
		t.Skipf("找不到原版字型 %s,跳過(跑 tools/setup-wine-oracle.sh 可取得)", p)
	}
	f, err := Parse(d)
	if err != nil {
		t.Fatalf("Parse(%s): %v", name, err)
	}
	return f
}

func render(g *Glyph) string {
	var sb strings.Builder
	for y := 0; y < g.H; y++ {
		for x := 0; x < g.W; x++ {
			if g.At(x, y) {
				sb.WriteByte('#')
			} else {
				sb.WriteByte('.')
			}
		}
		sb.WriteByte('\n')
	}
	return sb.String()
}

func TestMetrics(t *testing.T) {
	for _, tc := range []struct {
		file             string
		face             string
		w, h, asc        int
		first, last      byte
	}{
		{"cvga.fon", "cvga", 8, 15, 11, 0x00, 0xFF},
		{"CVGA1018.FON", "cvga1018", 10, 18, 16, 0x00, 0xFF},
		{"cvga1224.FON", "cvga1224", 12, 24, 20, 0x00, 0xFF},
	} {
		f := load(t, tc.file)
		if f.Face != tc.face {
			t.Errorf("%s: face = %q, 應為 %q", tc.file, f.Face, tc.face)
		}
		if f.PixWidth != tc.w || f.PixHeight != tc.h {
			t.Errorf("%s: %dx%d, 應為 %dx%d", tc.file, f.PixWidth, f.PixHeight, tc.w, tc.h)
		}
		if f.Ascent != tc.asc {
			t.Errorf("%s: ascent = %d, 應為 %d", tc.file, f.Ascent, tc.asc)
		}
		if f.First != tc.first || f.Last != tc.last {
			t.Errorf("%s: 範圍 %#x-%#x, 應為 %#x-%#x",
				tc.file, f.First, f.Last, tc.first, tc.last)
		}
	}
}

// 這個字模是用 tools/fnt.py 從原版檔案獨立解出來的,
// 兩支解析器互為對照 —— 一邊改壞了這裡就會紅。
const wantA8x15 = `........
........
........
....#...
...###..
..##.##.
.##...##
.##...##
.#######
.##...##
.##...##
.##...##
........
........
........
`

func TestGlyphA(t *testing.T) {
	f := load(t, "cvga.fon")
	g := f.Glyph('A')
	if g == nil {
		t.Fatal("cvga 沒有 'A'")
	}
	if got := render(g); got != wantA8x15 {
		t.Errorf("'A' 字模不符\n得到:\n%s\n預期:\n%s", got, wantA8x15)
	}
}

func TestAllGlyphsPresent(t *testing.T) {
	for _, name := range []string{"cvga.fon", "CVGA1018.FON", "cvga1224.FON"} {
		f := load(t, name)
		for c := 0; c <= 0xFF; c++ {
			g := f.Glyph(byte(c))
			if g == nil {
				t.Errorf("%s: %#02x 取不到字模", name, c)
				continue
			}
			if g.W != f.PixWidth || g.H != f.PixHeight {
				t.Errorf("%s: %#02x 尺寸 %dx%d, 應為 %dx%d",
					name, c, g.W, g.H, f.PixWidth, f.PixHeight)
			}
		}
	}
}
