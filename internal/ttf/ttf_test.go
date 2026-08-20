package ttf

import (
	"testing"

	"github.com/wicanr2/wincv-remake/internal/cell"
	"github.com/wicanr2/wincv-remake/internal/fnt"
)

// 這一包要有真的字型檔才驗得動。CI 或別台機器上沒裝就跳過 ——
// 但只跳過「需要字型」的那幾條,不是整包。
func loadOne(t *testing.T) *Font {
	t.Helper()
	chain, used, errs := LoadChain(8, 16, 16)
	for _, err := range errs {
		t.Logf("候選字型載不起來: %v", err)
	}
	if len(chain) == 0 {
		t.Skipf("這台機器沒有可用的 CJK TrueType(試過 %d 個候選)", len(Candidates()))
	}
	t.Logf("用 %s", used[0])
	return chain[0]
}

// 字模不能超出格子。這是後備字型最容易出的錯:
// 用 face 的 ascent 當基線會把中文推出格子下緣,而畫面上只看到
// 「中文比英數低半格」,不像 bug 像設計。
func TestGlyphFitsCell(t *testing.T) {
	f := loadOne(t)
	for _, r := range []rune{'简', '体', '한', 'Ω', 'Д'} {
		g := f.Glyph(r)
		if g == nil {
			t.Logf("%c 這個字型沒有,跳過", r)
			continue
		}
		if g.H != 16 {
			t.Errorf("%c 高 %d, 想要 16", r, g.H)
		}
		if w := wantW(r); g.W != w {
			t.Errorf("%c 寬 %d, 想要 %d", r, g.W, w)
		}
		if top, bot := inkRows(g); top < 0 {
			t.Errorf("%c 整個是空的", r)
		} else if bot > 15 {
			t.Errorf("%c 的筆畫畫到第 %d 列,超出格子", r, bot)
		}
	}
}

// 字型真的沒有的字要回 nil,不能回一個空白字模 ——
// 上層靠 nil 決定要不要畫缺字框,回空白字模會讓缺字安靜地變成空格。
func TestBlankGlyphReturnsNil(t *testing.T) {
	f := loadOne(t)
	// 空白字元在字型裡是存在的,但一個筆畫都沒有,正好用來驗
	// 「有 advance 但沒墨」這條路徑。
	for _, r := range []rune{' ', '\u3000'} {
		if g := f.Glyph(r); g != nil {
			t.Errorf("%U 沒有筆畫卻回了字模", r)
		}
	}
}

func wantW(r rune) int {
	if cell.IsWide(r) {
		return 16
	}
	return 8
}

func inkRows(g *fnt.Glyph) (top, bot int) {
	top, bot = -1, -1
	for y := 0; y < g.H; y++ {
		for x := 0; x < g.W; x++ {
			if g.At(x, y) {
				if top < 0 {
					top = y
				}
				bot = y
			}
		}
	}
	return
}
