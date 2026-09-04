// Package convert 是原版 Ctrl-O 那一組轉換功能。
//
// 對照 file_id.diz 列出的項目:
//   - 日文碼 ↔ 繁體碼 ↔ 簡體碼 文字檔轉換(第 6 條)
//   - UNIX ↔ PC 文字檔換行轉換(第 9 條)
//   - 批次更改檔名:大小寫、簡繁日文碼轉碼(第 10 條)
//   - 去除 HTML、ANSI 彩色控制碼(第 11 條)
package convert

import (
	"bytes"
	"regexp"
	"strings"

	"github.com/wicanr2/wincv-remake/internal/textenc"
	"golang.org/x/text/encoding/japanese"
	"golang.org/x/text/encoding/korean"
	"golang.org/x/text/encoding/simplifiedchinese"
	"golang.org/x/text/encoding/traditionalchinese"
)

// EOL 是換行樣式。
type EOL int

const (
	EOLUnix EOL = iota
	EOLPC
	EOLMac
)

// ToEOL 換掉換行樣式。先全部正規化成 \n 再換,
// 這樣混用兩種換行的檔案也會被統一 —— 原版就是這樣才修得好壞掉的檔。
func ToEOL(b []byte, e EOL) []byte {
	s := bytes.ReplaceAll(b, []byte("\r\n"), []byte("\n"))
	s = bytes.ReplaceAll(s, []byte("\r"), []byte("\n"))
	switch e {
	case EOLPC:
		return bytes.ReplaceAll(s, []byte("\n"), []byte("\r\n"))
	case EOLMac:
		return bytes.ReplaceAll(s, []byte("\n"), []byte("\r"))
	}
	return s
}

// DetectEOL 判斷目前的換行樣式。混用時回傳出現最多的那一種。
func DetectEOL(b []byte) EOL {
	crlf := bytes.Count(b, []byte("\r\n"))
	lf := bytes.Count(b, []byte("\n")) - crlf
	cr := bytes.Count(b, []byte("\r")) - crlf
	switch {
	case crlf >= lf && crlf >= cr:
		return EOLPC
	case cr > lf:
		return EOLMac
	}
	return EOLUnix
}

// Recode 把位元組從一種編碼轉成另一種。
//
// 中間一律經過 UTF-8:直接做 Big5→GBK 的位元組對映表要維護一張大表,
// 而且遇到某一邊沒有的字時沒有統一的處理方式。
func Recode(b []byte, from, to textenc.Enc) ([]byte, error) {
	if from == to {
		return b, nil
	}
	utf8s := textenc.Decode(b, from)
	return encodeTo([]byte(utf8s), to)
}

func encodeTo(utf8b []byte, to textenc.Enc) ([]byte, error) {
	switch to {
	case textenc.UTF8, textenc.ASCII:
		return utf8b, nil
	case textenc.Big5:
		return traditionalchinese.Big5.NewEncoder().Bytes(utf8b)
	case textenc.GBK:
		return simplifiedchinese.GBK.NewEncoder().Bytes(utf8b)
	case textenc.ShiftJIS:
		return japanese.ShiftJIS.NewEncoder().Bytes(utf8b)
	case textenc.EUCKR:
		return korean.EUCKR.NewEncoder().Bytes(utf8b)
	}
	return utf8b, nil
}

// RecodeLossy 跟 Recode 一樣,但目標編碼放不下的字用 repl 代替,
// 不會整份失敗。轉繁簡時一定會遇到對不過去的字,
// 停在第一個轉不了的字對使用者沒有幫助。
func RecodeLossy(b []byte, from, to textenc.Enc, repl string) []byte {
	if from == to {
		return b
	}
	s := textenc.Decode(b, from)
	var out bytes.Buffer
	for _, r := range s {
		enc, err := encodeTo([]byte(string(r)), to)
		if err != nil {
			out.WriteString(repl)
			continue
		}
		out.Write(enc)
	}
	return out.Bytes()
}

var (
	ansiRe    = regexp.MustCompile(`\x1b\[[0-9;?]*[a-zA-Z]`)
	htmlTagRe = regexp.MustCompile(`(?s)<[^>]*>`)
	// Go 的 RE2 沒有 backreference,script 與 style 只能各寫一條。
	scriptRe = regexp.MustCompile(`(?is)<script\b[^>]*>.*?</\s*script\s*>`)
	styleRe  = regexp.MustCompile(`(?is)<style\b[^>]*>.*?</\s*style\s*>`)
	brRe     = regexp.MustCompile(`(?i)<br\s*/?>`)
	pRe      = regexp.MustCompile(`(?i)</p\s*>`)
	spaceRe  = regexp.MustCompile(`[ \t]+`)
	blankRe  = regexp.MustCompile(`\n{3,}`)
)

// StripANSI 去掉 ANSI 控制碼。
func StripANSI(s string) string { return ansiRe.ReplaceAllString(s, "") }

// StripHTML 把 HTML 變成純文字。
//
// 順序有意義:先整段拿掉 script/style(它們的**內容**也不要),
// 再拆標籤,最後還原實體。反過來做會把 script 裡的程式碼留在正文。
func StripHTML(s string) string {
	s = scriptRe.ReplaceAllString(s, "")
	s = styleRe.ReplaceAllString(s, "")
	s = brRe.ReplaceAllString(s, "\n")
	s = pRe.ReplaceAllString(s, "\n\n")
	s = htmlTagRe.ReplaceAllString(s, "")
	s = unescapeEntities(s)
	s = spaceRe.ReplaceAllString(s, " ")
	s = blankRe.ReplaceAllString(s, "\n\n")
	return strings.TrimSpace(s)
}

var entities = map[string]string{
	"&amp;": "&", "&lt;": "<", "&gt;": ">", "&quot;": `"`,
	"&apos;": "'", "&nbsp;": " ", "&#39;": "'", "&#34;": `"`,
}

func unescapeEntities(s string) string {
	// &amp; 要最後換 —— 先換的話 "&amp;lt;" 會變成 "<",
	// 但它原本表示的是字面上的 "&lt;"。
	for k, v := range entities {
		if k == "&amp;" {
			continue
		}
		s = strings.ReplaceAll(s, k, v)
	}
	return strings.ReplaceAll(s, "&amp;", "&")
}

// NameCase 是批次改名的大小寫模式。
type NameCase int

const (
	CaseLower NameCase = iota
	CaseUpper
	CaseTitle // 首字母大寫,副檔名小寫
)

// RenameCase 依模式改一個檔名的大小寫。副檔名與主檔名分開處理。
func RenameCase(name string, c NameCase) string {
	base, ext := splitExt(name)
	switch c {
	case CaseUpper:
		return strings.ToUpper(base) + strings.ToUpper(ext)
	case CaseTitle:
		low := strings.ToLower(base)
		if low == "" {
			return low + strings.ToLower(ext)
		}
		return strings.ToUpper(low[:1]) + low[1:] + strings.ToLower(ext)
	}
	return strings.ToLower(base) + strings.ToLower(ext)
}

func splitExt(name string) (string, string) {
	if i := strings.LastIndexByte(name, '.'); i > 0 {
		return name[:i], name[i:]
	}
	return name, ""
}

// NumberedRename 產生連續編號的新檔名 —— 原版的「連續編號改名」。
// pattern 裡的 # 會被換成編號,# 的個數就是補零的位數。
func NumberedRename(pattern string, n int) string {
	i := strings.IndexByte(pattern, '#')
	if i < 0 {
		return pattern
	}
	j := i
	for j < len(pattern) && pattern[j] == '#' {
		j++
	}
	width := j - i
	num := padNum(n, width)
	return pattern[:i] + num + pattern[j:]
}

func padNum(n, width int) string {
	s := ""
	for v := n; ; v /= 10 {
		s = string(rune('0'+v%10)) + s
		if v < 10 {
			break
		}
	}
	for len(s) < width {
		s = "0" + s
	}
	return s
}
