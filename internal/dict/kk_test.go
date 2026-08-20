package dict

import "testing"

// 對照表是用已知發音的單字反推的,測試把那批單字鎖住。
func TestKKToIPA(t *testing.T) {
	cases := []struct{ raw, want, word string }{
		{"B!", "bi", "be"},
		{"K!", "si", "see"},
		{`M"A`, "ʃɪp", "ship"},
		{`I"Q`, "θɪn", "thin"},
		{`J"K`, "ðɪs", "this"},
		{`K"R`, "sɪŋ", "sing"},
		{"U#K", "jɛs", "yes"},
		{"O$C", "hæt", "hat"},
		{"L(", "zu", "zoo"},
		{"B'E", "bʊk", "book"},
		{"F.", "ɡo", "go"},
		{"A-", "pe", "pay"},
		{"S&", "lɔ", "law"},
		{`B&"`, "bɔɪ", "boy"},
		{"Oa'", "haʊ", "how"},
		{`Pa"`, "maɪ", "my"},
		{"B*D", "bɝd", "bird"},
		{">P#N,", "ˈmɛʒɚ", "measure"},
		{">G%J,", "ˈfɑðɚ", "father"},
		// tʃ / dʒ 沒有各自的符號,寫成兩個位元組
		{"CM*CM", "tʃɝtʃ", "church"},
		{"DN)DN", "dʒʌdʒ", "judge"},
		// W / X 是成音節的 l 與 n
		{`>S"CW`, "ˈlɪtl̩", "little"},
		{">B)CX", "ˈbʌtn̩", "button"},
		{`>S"KX`, "ˈlɪsn̩", "listen"},
		// 次重音
		{"+>Ba'C", "əˈbaʊt", "about"},
	}
	for _, c := range cases {
		if got := KKToIPA(c.raw); got != c.want {
			t.Errorf("%s: KKToIPA(%q) = %q,期望 %q", c.word, c.raw, got, c.want)
		}
	}
}

// 少數條目夾了 Big5 的中文註記,那些不是音素,要原樣留著不要當成音標轉。
func TestKKKeepsUnknownBytes(t *testing.T) {
	got := KKToIPA("B!\x01xyz")
	if got != "bi\x01xyz" {
		t.Errorf("= %q", got)
	}
}
