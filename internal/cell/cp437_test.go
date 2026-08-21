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

// 反查(字碼 → rune)對 ASCII 也要成立。
//
// [雷] 表裡 0x20-0x7E 沒有一個一個列出來,而漏掉的話 CP437[0x6D] 是 0
// 不是 'm' —— 用字碼查字模的那一側(系統 TrueType 產的半形字模走這條)
// 會認為每一個英數字都沒有字,然後整批退到後備的比例字型。
func TestCP437CoversASCII(t *testing.T) {
	for b := 0x20; b <= 0x7E; b++ {
		if got := CP437[b]; got != rune(b) {
			t.Fatalf("CP437[0x%02X] = %q,想要 %q", b, got, rune(b))
		}
	}
	// 兩個方向要對得起來。
	for b := 0; b < 256; b++ {
		r := CP437[byte(b)]
		if r == 0 {
			continue
		}
		if back, ok := FromCP437(r); !ok || back != byte(b) {
			// 重複對應的字碼(例如 0x1A '→' 與別處)以先填的為準,
			// 只有 ASCII 區要求嚴格往返。
			if b >= 0x20 && b <= 0x7E {
				t.Errorf("0x%02X → %q → 0x%02X(ok=%v)", b, r, back, ok)
			}
		}
	}
}
