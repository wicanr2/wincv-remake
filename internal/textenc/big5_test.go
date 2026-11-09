package textenc

import (
	"testing"
	"unicode/utf8"

	"golang.org/x/text/encoding/traditionalchinese"
)

// x/text 的編碼器把這些字編成 HKSCS 的 FA–FE 碼;這裡必須給標準 Big5 碼。
func TestBig5CodeAvoidsHKSCS(t *testing.T) {
	for _, c := range []struct {
		r    rune
		want [2]byte
	}{
		{'包', [2]byte{0xA5, 0x5D}}, {'港', [2]byte{0xB4, 0xE4}}, {'晴', [2]byte{0xB4, 0xB8}},
		{'煮', [2]byte{0xB5, 0x4E}}, {'偽', [2]byte{0xB0, 0xB0}}, {'婷', [2]byte{0xB4, 0x40}},
		{'十', [2]byte{0xA4, 0x51}}, {'卅', [2]byte{0xA4, 0xCA}}, {'中', [2]byte{0xA4, 0xA4}},
	} {
		got, ok := Big5Code(c.r)
		if !ok || got != c.want {
			t.Errorf("Big5Code(%q) = % X %v,想要 % X", c.r, got, ok, c.want)
		}
	}
	if _, ok := Big5Code('A'); ok {
		t.Error("ASCII 不在雙位元組表裡")
	}
	if _, ok := Big5Code('😀'); ok {
		t.Error("不在 Big5 的字應該回 false")
	}
}

// 表裡每一個碼都必須解回同一個字;標準區(A1–F9)有的字不可以拿到
// HKSCS 區的碼。只在 HKSCS 區的字仍要編得出來(香港用字轉碼時不失敗)。
func TestBig5CodeRoundTrip(t *testing.T) {
	big5Once.Do(buildBig5Table)
	dec := traditionalchinese.Big5.NewDecoder()
	std := map[rune]bool{}
	for hi := 0xA1; hi <= 0xF9; hi++ {
		for lo := 0x40; lo <= 0xFE; lo++ {
			if out, err := dec.Bytes([]byte{byte(hi), byte(lo)}); err == nil {
				if r, n := utf8.DecodeRune(out); r != utf8.RuneError && n == len(out) {
					std[r] = true
				}
			}
		}
	}
	hk := 0
	for r, b := range big5Table {
		if b[0] > 0xF9 {
			hk++
			if std[r] {
				t.Errorf("%q 在標準區有碼,卻拿到 HKSCS 的 % X", r, b)
			}
		}
		out, err := dec.Bytes(b[:])
		if got, _ := utf8.DecodeRune(out); err != nil || got != r {
			t.Errorf("%q → % X → %q", r, b, got)
		}
	}
	if hk == 0 {
		t.Error("只在 HKSCS 區的字一個都編不出來")
	}
	if len(big5Table) < 13000 {
		t.Errorf("表只有 %d 個字", len(big5Table))
	}
}

func TestEncodeBig5(t *testing.T) {
	b, err := EncodeBig5("A包港\n")
	if err != nil || string(b) != "A\xa5\x5d\xb4\xe4\n" {
		t.Errorf("EncodeBig5 = % X %v", b, err)
	}
	// 只在 HKSCS 區的字(U+92DB)編成 FA–FE 碼,與 WHATWG 相同。
	if b, err := EncodeBig5("\u92db"); err != nil || string(b) != "\xfa\x41" {
		t.Errorf("HKSCS 字 = % X %v,想要 FA 41", b, err)
	}
	if _, err := EncodeBig5("😀"); err == nil {
		t.Error("放不進 Big5 的字應該回錯誤")
	}
}
