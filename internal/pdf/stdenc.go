package pdf

import "sort"

// StandardEncoding 的字形名稱。seac(用兩個標準字形拼出重音字)要用它:
// charstring 裡帶的是「基底字與重音字在 StandardEncoding 上的字碼」,
// 不是字形編號,所以得先把字碼換回名字。
//
// 下半部(32–126)的名字與 ASCII 的排法一致,只有兩個引號不同:
// 0x27 是 quoteright、0x60 是 quoteleft。上半部借用 standardUpper。
var standardLower = [95]string{
	"space", "exclam", "quotedbl", "numbersign", "dollar", "percent",
	"ampersand", "quoteright", "parenleft", "parenright", "asterisk", "plus",
	"comma", "hyphen", "period", "slash",
	"zero", "one", "two", "three", "four", "five", "six", "seven", "eight", "nine",
	"colon", "semicolon", "less", "equal", "greater", "question", "at",
	"A", "B", "C", "D", "E", "F", "G", "H", "I", "J", "K", "L", "M",
	"N", "O", "P", "Q", "R", "S", "T", "U", "V", "W", "X", "Y", "Z",
	"bracketleft", "backslash", "bracketright", "asciicircum", "underscore",
	"quoteleft",
	"a", "b", "c", "d", "e", "f", "g", "h", "i", "j", "k", "l", "m",
	"n", "o", "p", "q", "r", "s", "t", "u", "v", "w", "x", "y", "z",
	"braceleft", "bar", "braceright", "asciitilde",
}

// standardName 把 StandardEncoding 的字碼換成字形名。
func standardName(code int) (string, bool) {
	if code >= 32 && code <= 126 {
		return standardLower[code-32], true
	}
	n, ok := standardUpper[code]
	return n, ok
}

// standardSID 把 StandardEncoding 的字碼換成 CFF 的標準字串編號(SID)。
//
// CFF 的前 149 個標準字串就是照 StandardEncoding 的順序排的:字碼 32–126
// 對到 SID 1–95,上半部有定義的那 54 個字碼依序對到 SID 96–149。
// 這不是巧合,是規格當初就這樣訂的 —— 所以不必抄一份 391 個字串的表。
// `TestStandardSIDAgainstRealFont` 拿真實的 CFF 字型驗過這個對應。
func standardSID(code int) (uint16, bool) {
	if code >= 32 && code <= 126 {
		return uint16(code - 31), true
	}
	if _, ok := standardUpper[code]; !ok {
		return 0, false
	}
	codes := standardUpperCodes()
	i := sort.SearchInts(codes, code)
	return uint16(96 + i), true
}

// standardUpperCodes 是 standardUpper 的字碼,由小到大。
var standardUpperCodesCache []int

func standardUpperCodes() []int {
	if standardUpperCodesCache == nil {
		c := make([]int, 0, len(standardUpper))
		for k := range standardUpper {
			c = append(c, k)
		}
		sort.Ints(c)
		standardUpperCodesCache = c
	}
	return standardUpperCodesCache
}
