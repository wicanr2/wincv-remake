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

// readInlineImage 讀一張內嵌影像:BI 後面的參數字典,加上 ID 後面的資料。
//
// [雷] 影像資料是**原始位元組**,裡面什麼都可能有,包括看起來像運算子的
// 東西、也包括 "EI" 這兩個字母。所以能算出長度時就照算的走,不去掃描 ——
// 掃描碰上剛好含 EI 的資料會提早結束,而後面的解讀會從影像資料中間開始,
// 解出一串合法但毫無意義的運算子。
//
// 呼叫的時候 BI 已經被讀掉了。回傳的 data 只在 ok 為真時有意義;
// 不論成功與否,解碼器都會停在這張影像之後。
func (l *lexer) readInlineImage() (dict map[string]value, data []byte, ok bool) {
	dict = map[string]value{}
	key := ""
	for {
		v, more := l.next()
		if !more {
			return dict, nil, false
		}
		if v.kind == vOp {
			if v.str == "ID" {
				break
			}
			// true / false 在這個掃描器眼裡是「裸關鍵字」,也就是運算子。
			// 它們是合法的字典值,要收下來;其他運算子表示這一張壞掉了。
			if key != "" && (v.str == "true" || v.str == "false") {
				dict[key] = v
				key = ""
				continue
			}
			return dict, nil, false
		}
		if key == "" && v.kind == vName {
			key = v.str
			continue
		}
		if key != "" {
			dict[key] = v
			key = ""
		}
	}
	// ID 後面固定接一個空白,再來才是資料。
	if l.i < len(l.b) && isWhite(l.b[l.i]) {
		l.i++
	}
	start := l.i
	if n := inlineDataLen(dict); n > 0 && start+n <= len(l.b) {
		l.i = start + n
		if l.skipToEI() {
			return dict, l.b[start : start+n], true
		}
		// 算出來的長度對不上(通常表示參數看錯了),退回去用掃描的。
		l.i = start
	}
	end := l.i
	for end+1 < len(l.b) {
		if l.b[end] == 'E' && l.b[end+1] == 'I' && end > start && isWhite(l.b[end-1]) &&
			(end+2 >= len(l.b) || isWhite(l.b[end+2]) || isDelim(l.b[end+2])) {
			l.i = end + 2
			return dict, l.b[start : end-1], true
		}
		end++
	}
	l.i = len(l.b)
	return dict, nil, false
}

// skipToEI 確認接下來就是 EI,並跳過它。
func (l *lexer) skipToEI() bool {
	j := l.i
	for j < len(l.b) && isWhite(l.b[j]) {
		j++
	}
	if j+1 < len(l.b) && l.b[j] == 'E' && l.b[j+1] == 'I' {
		l.i = j + 2
		return true
	}
	return false
}

// inlineDataLen 算沒有壓縮的影像資料有多長。有濾鏡時回 0(長度要解了才知道)。
//
// [雷] 每一列都補齊到整個位元組。1 位元的黑白影像最容易踩到:
// 寬度 12 的一列是 2 個位元組不是 1.5 個,少算的話整張影像會斜掉。
func inlineDataLen(dict map[string]value) int {
	if _, ok := dictOf(dict, "F", "Filter"); ok {
		return 0
	}
	w := int(numOfValue(dict, "W", "Width"))
	h := int(numOfValue(dict, "H", "Height"))
	if w <= 0 || h <= 0 {
		return 0
	}
	bpc := int(numOfValue(dict, "BPC", "BitsPerComponent"))
	n := inlineComponents(dict)
	if isInlineMask(dict) {
		bpc, n = 1, 1
	}
	if bpc <= 0 || n <= 0 {
		return 0
	}
	return (w*bpc*n + 7) / 8 * h
}

// inlineComponents 算一個像素有幾個分量。
func inlineComponents(dict map[string]value) int {
	v, ok := dictOf(dict, "CS", "ColorSpace")
	if !ok {
		return 1
	}
	switch v.str {
	case "RGB", "DeviceRGB", "CalRGB":
		return 3
	case "CMYK", "DeviceCMYK":
		return 4
	case "G", "DeviceGray", "CalGray", "I", "Indexed":
		return 1
	}
	// 具名的色彩空間要查資源才知道,交給掃描那條路。
	return 0
}

func isInlineMask(dict map[string]value) bool {
	v, ok := dictOf(dict, "IM", "ImageMask")
	return ok && (v.kind == vOp && v.str == "true" || v.kind == vName && v.str == "true" || v.num != 0)
}

// dictOf 依縮寫與全名兩種鍵查值。內嵌影像的參數兩種寫法都合法。
func dictOf(dict map[string]value, abbr, full string) (value, bool) {
	if v, ok := dict[abbr]; ok {
		return v, true
	}
	v, ok := dict[full]
	return v, ok
}

func numOfValue(dict map[string]value, abbr, full string) float64 {
	if v, ok := dictOf(dict, abbr, full); ok && v.kind == vNum {
		return v.num
	}
	return 0
}
