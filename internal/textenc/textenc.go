// Package textenc 處理 WinCV 會遇到的文字編碼:Big5、GBK、Shift_JIS、
// EUC-KR、UTF-8、UTF-16,以及它們之間的互轉。
//
// 原版的招牌功能之一是「以繁體字型看簡體碼、日文碼」與「批次轉碼」,
// 所以判讀必須夠準,而且判錯時要能讓使用者手動指定 —— 自動判讀永遠有
// 判錯的時候,原版自己的說明也寫「辨識率並非 100%」。
package textenc

import (
	"bytes"
	"unicode/utf8"

	"golang.org/x/text/encoding"
	"golang.org/x/text/encoding/japanese"
	"golang.org/x/text/encoding/korean"
	"golang.org/x/text/encoding/simplifiedchinese"
	"golang.org/x/text/encoding/traditionalchinese"
	"golang.org/x/text/encoding/unicode"
)

// Enc 是一種編碼。
type Enc int

const (
	Unknown Enc = iota
	ASCII
	UTF8
	UTF16LE
	UTF16BE
	Big5
	GBK
	ShiftJIS
	EUCKR
	Binary // 不是文字
)

var names = map[Enc]string{
	Unknown: "未知", ASCII: "ASCII", UTF8: "UTF-8",
	UTF16LE: "UTF-16LE", UTF16BE: "UTF-16BE",
	Big5: "Big5", GBK: "GBK", ShiftJIS: "Shift_JIS", EUCKR: "EUC-KR",
	Binary: "二進位",
}

func (e Enc) String() string {
	if n, ok := names[e]; ok {
		return n
	}
	return "?"
}

// Decoder 回傳把該編碼轉成 UTF-8 的解碼器。ASCII / UTF8 回 nil。
func (e Enc) Decoder() *encoding.Decoder {
	switch e {
	case Big5:
		return traditionalchinese.Big5.NewDecoder()
	case GBK:
		return simplifiedchinese.GBK.NewDecoder()
	case ShiftJIS:
		return japanese.ShiftJIS.NewDecoder()
	case EUCKR:
		return korean.EUCKR.NewDecoder()
	case UTF16LE:
		return unicode.UTF16(unicode.LittleEndian, unicode.IgnoreBOM).NewDecoder()
	case UTF16BE:
		return unicode.UTF16(unicode.BigEndian, unicode.IgnoreBOM).NewDecoder()
	}
	return nil
}

// Decode 把位元組轉成 UTF-8。無法解碼的位元組以 U+FFFD 取代,
// 不會整份失敗 —— 看檔案時「大部分看得到、壞的地方顯示替代字」
// 遠比「整個檔打不開」有用。
func Decode(b []byte, e Enc) string {
	switch e {
	case UTF16LE, UTF16BE:
		b = trimBOM(b, e)
	case UTF8:
		b = bytes.TrimPrefix(b, []byte{0xEF, 0xBB, 0xBF})
	}
	d := e.Decoder()
	if d == nil {
		return string(b)
	}
	out, err := d.Bytes(b)
	if err != nil {
		// x/text 的 decoder 遇到壞位元組會停在那裡。逐段解、跳過壞的。
		return decodeLossy(b, e)
	}
	return string(out)
}

func trimBOM(b []byte, e Enc) []byte {
	if e == UTF16LE && len(b) >= 2 && b[0] == 0xFF && b[1] == 0xFE {
		return b[2:]
	}
	if e == UTF16BE && len(b) >= 2 && b[0] == 0xFE && b[1] == 0xFF {
		return b[2:]
	}
	return b
}

// decodeLossy 逐字元解,壞的位元組換成 U+FFFD 之後繼續。
//
// x/text 的 decoder 碰到不合法位元組就停在那裡,整份後面都拿不到;
// 看檔案時「大部分看得到、壞的地方顯示替代字」遠比「整個檔打不開」有用。
func decodeLossy(b []byte, e Enc) string {
	var sb bytes.Buffer
	i := 0
	for i < len(b) {
		c := b[i]
		if c < 0x80 {
			sb.WriteByte(c)
			i++
			continue
		}
		if i+1 < len(b) && validDouble(c, b[i+1], e) {
			if r, err := e.Decoder().Bytes(b[i : i+2]); err == nil {
				sb.Write(r)
				i += 2
				continue
			}
		}
		sb.WriteRune(0xFFFD)
		i++
	}
	return sb.String()
}

// Detect 判斷一段位元組是哪種編碼。
//
// 順序有意義:BOM 最確定,其次是 UTF-8 的位元樣式(誤判機率極低),
// 最後才是雙位元組編碼的統計比較 —— 那一段本質上是猜的。
func Detect(b []byte) Enc {
	if len(b) == 0 {
		return ASCII
	}
	switch {
	case bytes.HasPrefix(b, []byte{0xEF, 0xBB, 0xBF}):
		return UTF8
	case bytes.HasPrefix(b, []byte{0xFF, 0xFE}):
		return UTF16LE
	case bytes.HasPrefix(b, []byte{0xFE, 0xFF}):
		return UTF16BE
	}

	sample := b
	if len(sample) > 64*1024 {
		sample = sample[:64*1024]
	}

	// UTF-16 要比二進位判斷先做:沒有 BOM 的 UTF-16 純 ASCII 文字
	// 有一半是 0x00,照位元組統計看起來就是二進位。
	if e := detectUTF16NoBOM(sample); e != Unknown {
		return e
	}
	if isBinary(sample) {
		return Binary
	}
	if isASCII(sample) {
		return ASCII
	}
	if utf8.Valid(sample) && hasMultibyte(sample) {
		return UTF8
	}

	best, bestScore := Unknown, -1.0
	for _, e := range []Enc{Big5, GBK, ShiftJIS, EUCKR} {
		if s := score(sample, e); s > bestScore {
			best, bestScore = e, s
		}
	}
	if bestScore <= 0 {
		return Binary
	}
	return best
}

func isASCII(b []byte) bool {
	for _, c := range b {
		if c >= 0x80 {
			return false
		}
	}
	return true
}

func hasMultibyte(b []byte) bool {
	for _, c := range b {
		if c >= 0x80 {
			return true
		}
	}
	return false
}

// isBinary 用控制字元比例判斷。允許 \t \n \r \f \v 與 ESC
// (ESC 要留,BBS 簽名檔的 ANSI 色碼就是 ESC 開頭)。
func isBinary(b []byte) bool {
	ctrl, nul := 0, 0
	for _, c := range b {
		if c == 0 {
			nul++
		}
		if c < 0x20 && c != '\t' && c != '\n' && c != '\r' && c != 0x0C && c != 0x0B && c != 0x1B {
			ctrl++
		}
	}
	if nul*100 > len(b)*2 { // 超過 2% 是 NUL
		return true
	}
	return ctrl*100 > len(b)*5 // 超過 5% 是控制字元
}

func detectUTF16NoBOM(b []byte) Enc {
	if len(b) < 16 {
		return Unknown
	}
	var evenZero, oddZero int
	n := len(b) &^ 1
	for i := 0; i < n; i += 2 {
		if b[i] == 0 {
			evenZero++
		}
		if b[i+1] == 0 {
			oddZero++
		}
	}
	half := n / 2
	if oddZero*100 > half*60 && evenZero*100 < half*10 {
		return UTF16LE
	}
	if evenZero*100 > half*60 && oddZero*100 < half*10 {
		return UTF16BE
	}
	return Unknown
}

// score 給一個編碼打分:能合法解碼的雙位元組比例扣掉解不出來的懲罰。
//
// 這只是啟發式。Big5 與 GBK 的位元組範圍高度重疊,同一份短文字
// 兩邊都能「解得出來」,只是解出來的字不同 —— 所以另外用常用字加權。
func score(b []byte, e Enc) float64 {
	var ok, bad, dbl int
	i := 0
	for i < len(b) {
		c := b[i]
		if c < 0x80 {
			i++
			continue
		}
		if i+1 >= len(b) {
			bad++
			break
		}
		if validDouble(c, b[i+1], e) {
			ok++
			dbl++
			i += 2
		} else {
			bad++
			i++
		}
	}
	if dbl == 0 {
		return 0
	}
	base := float64(ok) / float64(ok+bad)
	return base + commonBonus(b, e)
}

func validDouble(hi, lo byte, e Enc) bool {
	switch e {
	case Big5:
		return hi >= 0xA1 && hi <= 0xF9 &&
			((lo >= 0x40 && lo <= 0x7E) || (lo >= 0xA1 && lo <= 0xFE))
	case GBK:
		return hi >= 0x81 && hi <= 0xFE &&
			((lo >= 0x40 && lo <= 0x7E) || (lo >= 0x80 && lo <= 0xFE))
	case ShiftJIS:
		return ((hi >= 0x81 && hi <= 0x9F) || (hi >= 0xE0 && hi <= 0xEF)) &&
			((lo >= 0x40 && lo <= 0x7E) || (lo >= 0x80 && lo <= 0xFC))
	case EUCKR:
		return hi >= 0xA1 && hi <= 0xFE && lo >= 0xA1 && lo <= 0xFE
	}
	return false
}

// commonBonus 用「這個編碼下常見的字有沒有出現」加權。
// 挑的是各語言裡出現頻率極高、而且在別的編碼下會落到罕用區的字。
func commonBonus(b []byte, e Enc) float64 {
	var pats [][]byte
	switch e {
	case Big5:
		pats = [][]byte{
			{0xAA, 0xBA}, // 的
			{0xA4, 0x40}, // 一
			{0xA4, 0xA4}, // 中
			{0xA4, 0x48}, // 人
			{0xAC, 0xB0}, // 為
			{0xA1, 0x41}, // ，
			{0xA1, 0x43}, // 。
		}
	case GBK:
		pats = [][]byte{
			{0xB5, 0xC4}, // 的
			{0xD2, 0xBB}, // 一
			{0xD6, 0xD0}, // 中
			{0xC8, 0xCB}, // 人
			{0xA3, 0xAC}, // ，
			{0xA1, 0xA3}, // 。
		}
	case ShiftJIS:
		pats = [][]byte{
			{0x82, 0xCC}, // の
			{0x82, 0xC8}, // な
			{0x83, 0x8A}, // リ
			{0x81, 0x41}, // 、
			{0x81, 0x42}, // 。
		}
	case EUCKR:
		pats = [][]byte{
			{0xC0, 0xBB}, // 을
			{0xC0, 0xCC}, // 이
			{0xB4, 0xC2}, // 는
		}
	}
	hits := 0
	for _, p := range pats {
		hits += bytes.Count(b, p)
	}
	if hits == 0 {
		return 0
	}
	// 上限 0.5,避免單一常見字把分數灌爆。
	v := float64(hits) / float64(len(b)/64+1) * 0.1
	if v > 0.5 {
		v = 0.5
	}
	return v
}
