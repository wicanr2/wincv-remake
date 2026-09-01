package pdf

import (
	"bytes"
	"fmt"
	"strconv"
)

// Type1 是最早的一種 PDF 嵌入字型,LaTeX 產的舊檔案與很多轉檔工具都用它
// (LibreOffice 把中日韓的子集也嵌成這一種)。
//
// 它的外殼是一份 PostScript 程式,而字形資料被加密兩層:整段二進位區用
// eexec 加密,裡面每一個字形的 charstring 再各自加密一次。兩層用的是同一
// 個演算法、不同的起始亂數種子。
//
// [雷] 解密之後要丟掉前面幾個位元組(eexec 是 4 個,charstring 由 lenIV
// 決定,預設也是 4)。不丟的話後面每一個位元組都對得上、只是整串位移了
// 幾格 —— 解出來是一堆能跑但畫錯的指令,不是亂碼。
type type1Font struct {
	chars  map[string][]byte // 字形名 → 已解密的 charstring
	subrs  [][]byte
	enc    map[byte]string // 字型自帶的編碼:字碼 → 字形名
	matrix [6]float64
}

// parseType1 解析一份 Type1 字型程式。
func parseType1(b []byte) (f *type1Font, err error) {
	defer func() {
		if r := recover(); r != nil {
			f, err = nil, fmt.Errorf("Type1 解析失敗(%v)", r)
		}
	}()
	if !bytes.HasPrefix(b, []byte("%!")) {
		// PFB 的每一段前面有 6 個位元組的標頭(0x80 0x01 加上長度)。
		if len(b) > 6 && b[0] == 0x80 {
			b = stripPFB(b)
		}
	}
	i := bytes.Index(b, []byte("eexec"))
	if i < 0 {
		return nil, fmt.Errorf("找不到 eexec")
	}
	f = &type1Font{chars: map[string][]byte{}, enc: map[byte]string{},
		matrix: [6]float64{0.001, 0, 0, 0.001, 0, 0}}
	f.readEncoding(b[:i])
	f.readMatrix(b[:i])

	enc := b[i+5:]
	// eexec 後面的空白不算資料。
	for len(enc) > 0 && (enc[0] == '\r' || enc[0] == '\n' || enc[0] == ' ' || enc[0] == '\t') {
		enc = enc[1:]
	}
	if isHexBlock(enc) {
		enc = unhex(enc)
	}
	priv := eexecDecrypt(enc, 55665, 4)
	if len(priv) == 0 {
		return nil, fmt.Errorf("eexec 解不開")
	}
	lenIV := 4
	if v, ok := intAfter(priv, []byte("/lenIV")); ok && v >= 0 && v <= 16 {
		lenIV = v
	}
	f.readSubrs(priv, lenIV)
	f.readCharStrings(priv, lenIV)
	if len(f.chars) == 0 {
		return nil, fmt.Errorf("Type1 裡沒有字形")
	}
	return f, nil
}

// stripPFB 把 PFB 的分段標頭拿掉,只留內容。
func stripPFB(b []byte) []byte {
	var out []byte
	for i := 0; i+6 <= len(b) && b[i] == 0x80; {
		t := b[i+1]
		if t == 3 { // 結束標記
			break
		}
		n := int(b[i+2]) | int(b[i+3])<<8 | int(b[i+4])<<16 | int(b[i+5])<<24
		i += 6
		if n < 0 || i+n > len(b) {
			break
		}
		out = append(out, b[i:i+n]...)
		i += n
	}
	if len(out) == 0 {
		return b
	}
	return out
}

func isHexBlock(b []byte) bool {
	n := 0
	for i := 0; i < len(b) && n < 4; i++ {
		if hexVal(b[i]) < 0 {
			return false
		}
		n++
	}
	return n == 4
}

func unhex(b []byte) []byte {
	out := make([]byte, 0, len(b)/2)
	hi := -1
	for _, c := range b {
		v := hexVal(c)
		if v < 0 {
			continue
		}
		if hi < 0 {
			hi = v
			continue
		}
		out = append(out, byte(hi*16+v))
		hi = -1
	}
	return out
}

// eexecDecrypt 是 Type1 的解密。兩層加密用同一個演算法,只有種子與
// 要丟掉的前綴長度不同。
func eexecDecrypt(b []byte, key uint16, skip int) []byte {
	r := key
	const c1, c2 = 52845, 22719
	out := make([]byte, 0, len(b))
	for _, c := range b {
		out = append(out, c^byte(r>>8))
		r = (uint16(c)+r)*c1 + c2
	}
	if len(out) <= skip {
		return nil
	}
	return out[skip:]
}

// readEncoding 讀字型自帶的編碼:`dup 32 /space put` 一行一個。
//
// 需要它是因為 Type1 的字形是**按名字**存的,而 PDF 給的是字碼。
// PDF 自己的 Differences 優先,沒寫到的碼位就靠這一張。
func (f *type1Font) readEncoding(b []byte) {
	rest := b
	for {
		i := bytes.Index(rest, []byte("dup "))
		if i < 0 {
			return
		}
		rest = rest[i+4:]
		j := 0
		for j < len(rest) && rest[j] >= '0' && rest[j] <= '9' {
			j++
		}
		if j == 0 {
			continue
		}
		code, err := strconv.Atoi(string(rest[:j]))
		if err != nil || code < 0 || code > 255 {
			continue
		}
		k := j
		for k < len(rest) && rest[k] == ' ' {
			k++
		}
		if k >= len(rest) || rest[k] != '/' {
			continue
		}
		k++
		s := k
		for k < len(rest) && !isPSDelim(rest[k]) {
			k++
		}
		if k > s {
			f.enc[byte(code)] = string(rest[s:k])
		}
		rest = rest[k:]
	}
}

func (f *type1Font) readMatrix(b []byte) {
	i := bytes.Index(b, []byte("/FontMatrix"))
	if i < 0 {
		return
	}
	j := bytes.IndexByte(b[i:], '[')
	k := bytes.IndexByte(b[i:], ']')
	if j < 0 || k < 0 || k < j {
		return
	}
	var m [6]float64
	n := 0
	for _, fld := range bytes.Fields(b[i+j+1 : i+k]) {
		v, err := strconv.ParseFloat(string(fld), 64)
		if err != nil || n >= 6 {
			return
		}
		m[n] = v
		n++
	}
	if n == 6 {
		f.matrix = m
	}
}

func isPSDelim(c byte) bool {
	switch c {
	case ' ', '\t', '\r', '\n', '/', '(', ')', '[', ']', '{', '}', '<', '>':
		return true
	}
	return false
}

// intAfter 找出某個關鍵字後面的第一個整數。
func intAfter(b, key []byte) (int, bool) {
	i := bytes.Index(b, key)
	if i < 0 {
		return 0, false
	}
	rest := b[i+len(key):]
	j := 0
	for j < len(rest) && (rest[j] == ' ' || rest[j] == '\t') {
		j++
	}
	s := j
	for j < len(rest) && rest[j] >= '0' && rest[j] <= '9' {
		j++
	}
	if j == s {
		return 0, false
	}
	v, err := strconv.Atoi(string(rest[s:j]))
	return v, err == nil
}

// readSubrs 讀子程式陣列。每一筆的形狀是 `dup <編號> <長度> RD <資料> NP`。
func (f *type1Font) readSubrs(b []byte, lenIV int) {
	i := bytes.Index(b, []byte("/Subrs"))
	if i < 0 {
		return
	}
	n, ok := intAfter(b[i:], []byte("/Subrs"))
	if !ok || n <= 0 || n > 65536 {
		return
	}
	f.subrs = make([][]byte, n)
	rest := b[i:]
	for k := 0; k < n; k++ {
		j := bytes.Index(rest, []byte("dup "))
		if j < 0 {
			return
		}
		rest = rest[j+4:]
		idx, p, ok := readInt(rest, 0)
		if !ok {
			return
		}
		data, next, ok := readRDData(rest[p:])
		if !ok {
			return
		}
		if idx >= 0 && idx < n {
			f.subrs[idx] = eexecDecrypt(data, 4330, lenIV)
		}
		rest = next
	}
}

// readInt 讀一個十進位整數,略過前面的空白。
func readInt(b []byte, i int) (int, int, bool) {
	for i < len(b) && (b[i] == ' ' || b[i] == '\t' || b[i] == '\r' || b[i] == '\n') {
		i++
	}
	s := i
	for i < len(b) && b[i] >= '0' && b[i] <= '9' {
		i++
	}
	if i == s {
		return 0, i, false
	}
	v, err := strconv.Atoi(string(b[s:i]))
	return v, i, err == nil
}

// readRDData 讀 `<長度> RD <資料>` 這個形狀。
//
// [雷] 字形項目只有一個數字(長度),子程式項目有兩個(編號與長度)。
// 拿同一支函式讀兩種的話,字形那邊會把 `RD` 當成第二個數字去解析,
// 然後一筆都讀不到 —— 而症狀是「這份字型裡沒有字形」,看起來像解密失敗。
func readRDData(b []byte) (data, next []byte, ok bool) {
	count, i, ok := readInt(b, 0)
	if !ok || count < 0 {
		return nil, b, false
	}
	// RD 與 -| 是同一件事的兩種寫法,後面固定接一個空白再接資料。
	for i < len(b) && b[i] == ' ' {
		i++
	}
	s := i
	for i < len(b) && b[i] != ' ' {
		i++
	}
	if i >= len(b) || i == s {
		return nil, b, false
	}
	i++ // 分隔用的那一個空白
	if i+count > len(b) {
		return nil, b, false
	}
	return b[i : i+count], b[i+count:], true
}

// readCharStrings 讀字形。每一筆的形狀是 `/<名字> <長度> RD <資料> ND`。
func (f *type1Font) readCharStrings(b []byte, lenIV int) {
	i := bytes.Index(b, []byte("/CharStrings"))
	if i < 0 {
		return
	}
	rest := b[i+len("/CharStrings"):]
	for {
		j := bytes.IndexByte(rest, '/')
		if j < 0 {
			return
		}
		rest = rest[j+1:]
		k := 0
		for k < len(rest) && !isPSDelim(rest[k]) {
			k++
		}
		if k == 0 {
			continue
		}
		name := string(rest[:k])
		data, next, ok := readRDData(rest[k:])
		if !ok {
			// 不是字形項目(例如 /CharStrings 底下的 end),跳過這個名字。
			rest = rest[k:]
			continue
		}
		f.chars[name] = eexecDecrypt(data, 4330, lenIV)
		rest = next
		if len(f.chars) > 20000 {
			return
		}
	}
}

// glyph 依字形名解出外框。
func (f *type1Font) glyph(name string) ([]gseg, bool) {
	cs, ok := f.chars[name]
	if !ok || len(cs) == 0 {
		return nil, false
	}
	t := &t1{f: f}
	t.run(cs)
	if len(t.out) == 0 {
		return nil, false
	}
	m := f.matrix
	if m[0] == 0.001 && m[1] == 0 && m[2] == 0 && m[3] == 0.001 && m[4] == 0 && m[5] == 0 {
		return t.out, true
	}
	for i := range t.out {
		for j := 0; j < 3; j++ {
			x, y := t.out[i].x[j], t.out[i].y[j]
			t.out[i].x[j] = (m[0]*x + m[2]*y + m[4]) * 1000
			t.out[i].y[j] = (m[1]*x + m[3]*y + m[5]) * 1000
		}
	}
	return t.out, true
}
