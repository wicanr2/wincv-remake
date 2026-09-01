// Package pdf 讀 PDF 的內容資料流:把頁面上的文字連同位置與字型取出來。
//
// 物件層(交叉參照表、解密、串流的濾鏡)交給 pdfcpu,這一包做的是它
// 沒有做的那一半:解讀內容資料流,以及把字碼變回文字。
//
// 「把字碼變回文字」是中文 PDF 能不能讀的關鍵。PDF 裡的字串不是文字,
// 是**字型內部的編號**;要變回文字得看字型的編碼設定與 ToUnicode 對照表。
// 少了這一步會抽出一串看起來像亂碼的東西 —— 而那是合法的字串,
// 不會有任何錯誤訊息,只是每個字都不對。
package pdf

import (
	"strconv"
	"strings"
)

// value 是內容資料流裡的一個運算元。
type value struct {
	kind valKind
	num  float64
	str  string // 字串的位元組、名稱、或運算子
	arr  []value
}

type valKind int

const (
	vNum valKind = iota
	vStr
	vName
	vArray
	vDict
	vOp
)

// lexer 走一段內容資料流。
//
// 內容資料流的語法就是 PDF 的物件語法再加上「裸關鍵字是運算子」。
// 自己寫而不是借用物件層的解析器:那一層的入口是為了讀檔案結構設計的,
// 而這裡要的是「一個一個 token 交出來」,兩者對錯誤的容忍度也不同 ——
// 內容資料流壞掉時應該畫出前面解得出來的部分,而不是整頁拒絕。
type lexer struct {
	b []byte
	i int
}

func isWhite(c byte) bool {
	return c == 0 || c == '\t' || c == '\n' || c == '\f' || c == '\r' || c == ' '
}

func isDelim(c byte) bool {
	switch c {
	case '(', ')', '<', '>', '[', ']', '{', '}', '/', '%':
		return true
	}
	return false
}

func (l *lexer) skipSpace() {
	for l.i < len(l.b) {
		c := l.b[l.i]
		if isWhite(c) {
			l.i++
			continue
		}
		if c == '%' {
			for l.i < len(l.b) && l.b[l.i] != '\n' && l.b[l.i] != '\r' {
				l.i++
			}
			continue
		}
		return
	}
}

// next 取下一個運算元或運算子。回傳 false 表示走完了。
func (l *lexer) next() (value, bool) {
	l.skipSpace()
	if l.i >= len(l.b) {
		return value{}, false
	}
	c := l.b[l.i]
	switch {
	case c == '/':
		l.i++
		return value{kind: vName, str: l.readName()}, true
	case c == '(':
		l.i++
		return value{kind: vStr, str: l.readLiteralString()}, true
	case c == '<':
		if l.i+1 < len(l.b) && l.b[l.i+1] == '<' {
			l.i += 2
			return l.readDict(), true
		}
		l.i++
		return value{kind: vStr, str: l.readHexString()}, true
	case c == '[':
		l.i++
		return l.readArray(), true
	case c == ']' || c == '>' || c == ')' || c == '{' || c == '}':
		// 落單的收尾符號:跳過,不要卡住。
		l.i++
		return l.next()
	case c == '+' || c == '-' || c == '.' || (c >= '0' && c <= '9'):
		return value{kind: vNum, num: l.readNumber()}, true
	default:
		start := l.i
		for l.i < len(l.b) && !isWhite(l.b[l.i]) && !isDelim(l.b[l.i]) {
			l.i++
		}
		if l.i == start {
			l.i++
			return l.next()
		}
		return value{kind: vOp, str: string(l.b[start:l.i])}, true
	}
}

func (l *lexer) readNumber() float64 {
	start := l.i
	if l.i < len(l.b) && (l.b[l.i] == '+' || l.b[l.i] == '-') {
		l.i++
	}
	for l.i < len(l.b) {
		c := l.b[l.i]
		if c >= '0' && c <= '9' || c == '.' || c == '-' || c == '+' || c == 'e' || c == 'E' {
			l.i++
			continue
		}
		break
	}
	f, err := strconv.ParseFloat(strings.TrimRight(string(l.b[start:l.i]), ".+-eE"), 64)
	if err != nil {
		return 0
	}
	return f
}

func (l *lexer) readName() string {
	start := l.i
	for l.i < len(l.b) && !isWhite(l.b[l.i]) && !isDelim(l.b[l.i]) {
		l.i++
	}
	s := string(l.b[start:l.i])
	if !strings.Contains(s, "#") {
		return s
	}
	// 名稱裡的 #hh 是跳脫寫法。
	var sb strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == '#' && i+2 < len(s) {
			if n, err := strconv.ParseUint(s[i+1:i+3], 16, 8); err == nil {
				sb.WriteByte(byte(n))
				i += 2
				continue
			}
		}
		sb.WriteByte(s[i])
	}
	return sb.String()
}

// readLiteralString 讀 ( ) 括起來的字串。
//
// 回傳的是**位元組**不是文字:這時候還不知道它是什麼編碼,
// 那要等查到字型才知道。提早解碼是這一整條路上最容易出錯的地方。
func (l *lexer) readLiteralString() string {
	var sb strings.Builder
	depth := 1
	for l.i < len(l.b) {
		c := l.b[l.i]
		l.i++
		switch c {
		case '\\':
			if l.i >= len(l.b) {
				return sb.String()
			}
			e := l.b[l.i]
			l.i++
			switch e {
			case 'n':
				sb.WriteByte('\n')
			case 'r':
				sb.WriteByte('\r')
			case 't':
				sb.WriteByte('\t')
			case 'b':
				sb.WriteByte('\b')
			case 'f':
				sb.WriteByte('\f')
			case '(', ')', '\\':
				sb.WriteByte(e)
			case '\r':
				// 行尾的反斜線是續行,連換行一起吃掉。
				if l.i < len(l.b) && l.b[l.i] == '\n' {
					l.i++
				}
			case '\n':
			default:
				if e >= '0' && e <= '7' {
					n := int(e - '0')
					for k := 0; k < 2 && l.i < len(l.b) && l.b[l.i] >= '0' && l.b[l.i] <= '7'; k++ {
						n = n*8 + int(l.b[l.i]-'0')
						l.i++
					}
					sb.WriteByte(byte(n))
					continue
				}
				sb.WriteByte(e)
			}
		case '(':
			depth++
			sb.WriteByte(c)
		case ')':
			depth--
			if depth == 0 {
				return sb.String()
			}
			sb.WriteByte(c)
		default:
			sb.WriteByte(c)
		}
	}
	return sb.String()
}

func (l *lexer) readHexString() string {
	var sb strings.Builder
	var hi int = -1
	for l.i < len(l.b) {
		c := l.b[l.i]
		l.i++
		if c == '>' {
			break
		}
		v := hexVal(c)
		if v < 0 {
			continue
		}
		if hi < 0 {
			hi = v
			continue
		}
		sb.WriteByte(byte(hi*16 + v))
		hi = -1
	}
	// 奇數個十六進位數字時,最後一個當成高半位元組(規格如此)。
	if hi >= 0 {
		sb.WriteByte(byte(hi * 16))
	}
	return sb.String()
}

func hexVal(c byte) int {
	switch {
	case c >= '0' && c <= '9':
		return int(c - '0')
	case c >= 'a' && c <= 'f':
		return int(c-'a') + 10
	case c >= 'A' && c <= 'F':
		return int(c-'A') + 10
	}
	return -1
}

func (l *lexer) readArray() value {
	out := value{kind: vArray}
	for l.i < len(l.b) {
		l.skipSpace()
		if l.i < len(l.b) && l.b[l.i] == ']' {
			l.i++
			break
		}
		v, ok := l.next()
		if !ok {
			break
		}
		out.arr = append(out.arr, v)
	}
	return out
}

// readDict 讀內容資料流裡的字典。只有內嵌影像的參數會用到,
// 內容不留 —— 這一包不畫圖。
func (l *lexer) readDict() value {
	for l.i < len(l.b) {
		l.skipSpace()
		if l.i+1 < len(l.b) && l.b[l.i] == '>' && l.b[l.i+1] == '>' {
			l.i += 2
			break
		}
		if _, ok := l.next(); !ok {
			break
		}
	}
	return value{kind: vDict}
}

// skipInlineImage 跳過一張內嵌影像。
//
// [雷] 內嵌影像的資料是**原始位元組**,裡面什麼都可能有,包括看起來
// 像運算子的東西。不整段跳過的話,後面的解讀會從影像資料中間開始,
// 而那會解出一串合法但毫無意義的運算子。
func (l *lexer) skipInlineImage() {
	// 先走到 ID。
	for l.i < len(l.b) {
		v, ok := l.next()
		if !ok {
			return
		}
		if v.kind == vOp && v.str == "ID" {
			break
		}
	}
	if l.i < len(l.b) && isWhite(l.b[l.i]) {
		l.i++
	}
	for l.i+1 < len(l.b) {
		if l.b[l.i] == 'E' && l.b[l.i+1] == 'I' &&
			(l.i == 0 || isWhite(l.b[l.i-1])) &&
			(l.i+2 >= len(l.b) || isWhite(l.b[l.i+2]) || isDelim(l.b[l.i+2])) {
			l.i += 2
			return
		}
		l.i++
	}
	l.i = len(l.b)
}
