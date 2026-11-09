package textenc

import (
	"sync"
	"unicode/utf8"

	"github.com/wicanr2/wincv-remake/internal/i18n"
	"golang.org/x/text/encoding/traditionalchinese"
)

// Big5 的編碼方向(字 → 位元組)不用 x/text 的編碼器。
//
// [雷] x/text 的 Big5 編碼器在 33 個字上給的是香港增補字符集(HKSCS)的
// FA–FE 碼,不是標準 Big5 碼:「包」給 FABD(標準是 A55D)、「港」給 FDBB、
// 「晴」「煮」「偽」「婷」「撐」也一樣。倚天字庫只到 F9,所以查字形時這些字
// 變成缺字框;轉碼存檔時寫出來的 FA–FE 碼,一般的 Big5 程式也讀不懂。
//
// 這裡照 WHATWG Encoding Standard 的 Big5 編碼規則建表:依碼的順序走過
// A140–FEFE,用解碼器得到對應的字;同一個字有好幾個碼時取第一個,但
// U+2550、U+255E、U+2561、U+256A、U+5341(十)、U+5345(卅)取最後一個。
// 「取第一個」讓同時在標準區與 HKSCS 區的字(包、港…)拿到標準碼;只在
// HKSCS 區的字(香港用字)仍編成 FA–FE 碼,轉碼時不會失敗。倚天字庫沒有
// FA 以後的區段,那些字查字形時本來就是缺字。Rust 改寫用的 encoding_rs
// 是同一套規則,兩邊在全部 14,653 個字上逐一相同。
var (
	big5Once  sync.Once
	big5Table map[rune][2]byte
)

func buildBig5Table() {
	big5Table = make(map[rune][2]byte, 14000)
	dec := traditionalchinese.Big5.NewDecoder()
	last := map[rune]bool{0x2550: true, 0x255E: true, 0x2561: true, 0x256A: true, 0x5341: true, 0x5345: true}
	for hi := 0xA1; hi <= 0xFE; hi++ {
		for lo := 0x40; lo <= 0xFE; lo++ {
			if lo > 0x7E && lo < 0xA1 {
				continue
			}
			out, err := dec.Bytes([]byte{byte(hi), byte(lo)})
			if err != nil {
				continue
			}
			r, n := utf8.DecodeRune(out)
			if r == utf8.RuneError || n != len(out) {
				continue // 沒有定義,或解出不只一個字
			}
			if _, seen := big5Table[r]; seen && !last[r] {
				continue
			}
			big5Table[r] = [2]byte{byte(hi), byte(lo)}
		}
	}
}

// Big5Code 回傳一個字的 Big5 雙位元組碼。不在 Big5 裡(含 ASCII)回 false。
func Big5Code(r rune) ([2]byte, bool) {
	big5Once.Do(buildBig5Table)
	b, ok := big5Table[r]
	return b, ok
}

// EncodeBig5 把 UTF-8 字串編成 Big5。ASCII 原樣保留;有放不進 Big5 的字
// 就整個失敗,與 x/text 編碼器的行為相同。
func EncodeBig5(s string) ([]byte, error) {
	out := make([]byte, 0, len(s))
	for _, r := range s {
		if r < 0x80 {
			out = append(out, byte(r))
			continue
		}
		b, ok := Big5Code(r)
		if !ok {
			return nil, i18n.Errorf("%q 無法以 Big5 表示", string(r))
		}
		out = append(out, b[0], b[1])
	}
	return out, nil
}
