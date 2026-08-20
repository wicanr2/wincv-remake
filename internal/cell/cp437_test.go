package cell

import "testing"

// 這張表最容易錯的地方是「以為它是 Latin-1」。
// 兩者只有 0x20-0x7E 相同,而錯掉的部分不會報錯,只會畫成別的字。
func TestCP437IsNotLatin1(t *testing.T) {
	for _, c := range []struct {
		b byte
		r rune
	}{
		{0xE9, 'Θ'}, // Latin-1 的 é 在 CP437 是希臘大寫 Theta
		{0x82, 'é'}, // é 真正的字碼
		{0xB0, '░'},
		{0xDB, '█'},
		{0xC4, '─'},
		{0x1E, '▲'},
	} {
		if got := CP437[c.b]; got != c.r {
			t.Errorf("CP437[%#02x] = %q, 想要 %q", c.b, got, c.r)
		}
		if got, ok := FromCP437(c.r); !ok || got != c.b {
			t.Errorf("FromCP437(%q) = %#02x,%v, 想要 %#02x", c.r, got, ok, c.b)
		}
	}
}

// ASCII 區段必須是恆等對應,而且不能被反查表裡的重複值蓋掉。
func TestCP437ASCIIRoundTrip(t *testing.T) {
	for b := 0x20; b <= 0x7E; b++ {
		if CP437[b] != 0 && CP437[b] != rune(b) {
			t.Fatalf("CP437[%#02x] = %q, ASCII 區應該是恆等", b, CP437[b])
		}
		if got, ok := FromCP437(rune(b)); !ok || got != byte(b) {
			t.Fatalf("FromCP437(%q) = %#02x,%v", rune(b), got, ok)
		}
	}
}

// 字型沒有的字要回 false,不能回 0 —— 回 0 會畫成 NUL 的字模(空白),
// 缺字就安靜地變成空格,上層的缺字框也不會出現。
func TestFromCP437Unknown(t *testing.T) {
	for _, r := range []rune{'中', '한', 0x1F600} {
		if b, ok := FromCP437(r); ok {
			t.Errorf("FromCP437(%q) 回了 %#02x,應該是查不到", r, b)
		}
	}
}
