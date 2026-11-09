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

// 表裡每一個碼都必須解回同一個字,而且不超出 F9。
func TestBig5CodeRoundTrip(t *testing.T) {
	big5Once.Do(buildBig5Table)
	dec := traditionalchinese.Big5.NewDecoder()
	for r, b := range big5Table {
		if b[0] > 0xF9 {
			t.Errorf("%q → % X 落在 HKSCS 區", r, b)
		}
		out, err := dec.Bytes(b[:])
		if got, _ := utf8.DecodeRune(out); err != nil || got != r {
			t.Errorf("%q → % X → %q", r, b, got)
		}
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
	if _, err := EncodeBig5("😀"); err == nil {
		t.Error("放不進 Big5 的字應該回錯誤")
	}
}
