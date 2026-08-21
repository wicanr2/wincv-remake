package ttf

import "testing"

// 半形字模的字碼是 CP437,不是 Latin-1。走錯表的話 0x80 以上
// 會畫成別的字,而畫面上看起來只是「有些字怪怪的」。
func TestHalfFontUsesCP437(t *testing.T) {
	f := loadOne(t)
	h := NewHalf(f, 8, 16)
	if w, hh := h.Size(); w != 8 || hh != 16 {
		t.Fatalf("Size = %d×%d", w, hh)
	}
	for _, c := range []struct {
		code byte
		want rune
	}{{'A', 'A'}, {0x82, 'é'}, {0xE9, 'Θ'}} {
		g := h.Glyph(c.code)
		if g == nil {
			t.Logf("字型沒有 %#02x(%q),跳過", c.code, c.want)
			continue
		}
		want := f.Glyph(c.want)
		if want == nil {
			continue
		}
		if g.W != want.W || g.H != want.H {
			t.Errorf("%#02x 的字模尺寸 %d×%d,想要 %d×%d", c.code, g.W, g.H, want.W, want.H)
		}
	}
	// 空白與 NUL 沒有筆畫,要回 nil 讓上層畫缺字框而不是空格。
	if g := h.Glyph(0x00); g != nil {
		t.Error("NUL 不該有字模")
	}
}
