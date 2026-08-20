package dict

import "strings"

// kk.txt.dat 的音標是**一個位元組一個音素**的自訂編碼,不是可以直接顯示的文字。
//
// 對照表怎麼解出來的:作者在資料裡留了兩筆條目,把整套符號按順序列了一遍 ——
//
//	aaaaa         !"#$%&'()*+,-.>?ABCDEFGHIJKLMNOPQRSTUVWXa
//	aaaaaaaaaaaa  >?ABCDEFGHIJKLMNOPQRSTUVWX!"#$%&'()*+,-.a
//
// 也就是 14 個母音(0x21-0x2E)、2 個重音記號、24 個子音(0x41-0x58),
// 外加一個 'a' 當雙母音的前半。各符號的音值再拿已知發音的單字反推:
//
//	be   B!    → bi        ship M"A → ʃɪp      thin  I"Q  → θɪn
//	this J"K   → ðɪs       zoo  L(  → zu       sing  K"R  → sɪŋ
//	book B'E   → bʊk       go   F.  → ɡo       law   S&   → lɔ
//	little >S"CW → ˈlɪtl̩   button >B)CX → ˈbʌtn̩
//
// 最後兩筆定出 W 與 X 是成音節的 l 與 n。
var kkSymbol = map[byte]string{
	// 母音 0x21-0x2E
	'!': "i", '"': "ɪ", '#': "ɛ", '$': "æ", '%': "ɑ", '&': "ɔ", '\'': "ʊ",
	'(': "u", ')': "ʌ", '*': "ɝ", '+': "ə", ',': "ɚ", '-': "e", '.': "o",
	// 雙母音的前半:a 之後接 " 或 ' 就是 aɪ / aʊ
	'a': "a",
	// 重音
	'>': "ˈ", '?': "ˌ",
	// 子音 0x41-0x58
	'A': "p", 'B': "b", 'C': "t", 'D': "d", 'E': "k", 'F': "ɡ",
	'G': "f", 'H': "v", 'I': "θ", 'J': "ð", 'K': "s", 'L': "z",
	'M': "ʃ", 'N': "ʒ", 'O': "h", 'P': "m", 'Q': "n", 'R': "ŋ",
	'S': "l", 'T': "r", 'U': "j", 'V': "w",
	'W': "l̩", 'X': "n̩",
}

// KKToIPA 把原始編碼轉成看得懂的音標。
//
// 認不得的位元組原樣留著:少數條目夾了 Big5 的中文註記(例如 eh 的
// 「用口上語調」),那些不是音素,硬轉會把它們變成亂碼。
func KKToIPA(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		// tʃ 與 dʒ 沒有各自的符號,寫成兩個位元組
		if i+1 < len(s) {
			switch {
			case c == 'C' && s[i+1] == 'M':
				b.WriteString("tʃ")
				i++
				continue
			case c == 'D' && s[i+1] == 'N':
				b.WriteString("dʒ")
				i++
				continue
			}
		}
		if v, ok := kkSymbol[c]; ok {
			b.WriteString(v)
		} else {
			b.WriteByte(c)
		}
	}
	return b.String()
}
