package pdf

import (
	"unicode/utf16"
)

// cmap 是一張字碼對照表。
//
// PDF 用同一種語法表達兩件不同的事:字碼 → CID(字型內部的字形編號),
// 以及字碼 → Unicode(ToUnicode)。兩者的結構相同,所以用同一份解析器。
type cmap struct {
	// spaces 說明一個字碼佔幾個位元組。沒有它就不知道要從字串裡
	// 一次取一個還是兩個位元組 —— 取錯的話整段都會錯開。
	spaces []codespace

	text   map[uint32]string // bfchar
	ranges []bfRange         // bfrange
	cids   map[uint32]uint32 // cidchar
	cidRgs []cidRange        // cidrange
}

type codespace struct {
	lo, hi uint32
	n      int // 位元組數
}

type bfRange struct {
	lo, hi uint32
	dst    []uint16 // 起點的 UTF-16 碼元;遞增時只加最後一個
	arr    []string // 逐一列出的形式
}

type cidRange struct {
	lo, hi uint32
	cid    uint32
}

// code 是從字串裡切出來的一個字碼。
type code struct {
	val uint32
	n   int
}

// parseCMap 解一份 CMap。
//
// CMap 是 PostScript 的一個子集,但要的東西只有四種區塊,所以用
// 「掃過所有 token,看到區塊開頭就收」的方式讀,不必實作直譯器。
func parseCMap(b []byte) *cmap {
	c := &cmap{text: map[uint32]string{}, cids: map[uint32]uint32{}}
	l := &lexer{b: b}
	var pending []value
	for {
		v, ok := l.next()
		if !ok {
			break
		}
		if v.kind != vOp {
			pending = append(pending, v)
			if len(pending) > 8 {
				pending = pending[1:]
			}
			continue
		}
		switch v.str {
		case "begincodespacerange":
			c.readCodespaces(l)
		case "beginbfchar":
			c.readBFChar(l)
		case "beginbfrange":
			c.readBFRange(l)
		case "begincidchar":
			c.readCIDChar(l)
		case "begincidrange":
			c.readCIDRange(l)
		case "usecmap":
			// 引用另一份 CMap。內嵌的 CMap 很少用到,而預先定義的那些
			// 本來就不在檔案裡 —— 兩種情形這裡都沒有東西可以接。
		}
		pending = pending[:0]
	}
	return c
}

func (c *cmap) readCodespaces(l *lexer) {
	for {
		lo, ok := l.next()
		if !ok || lo.kind == vOp {
			return
		}
		hi, ok := l.next()
		if !ok || hi.kind == vOp {
			return
		}
		if lo.kind != vStr || hi.kind != vStr || len(lo.str) == 0 {
			continue
		}
		c.spaces = append(c.spaces, codespace{
			lo: beUint(lo.str), hi: beUint(hi.str), n: len(lo.str)})
	}
}

func (c *cmap) readBFChar(l *lexer) {
	for {
		src, ok := l.next()
		if !ok || src.kind == vOp {
			return
		}
		dst, ok := l.next()
		if !ok || dst.kind == vOp {
			return
		}
		if src.kind != vStr {
			continue
		}
		switch dst.kind {
		case vStr:
			c.text[beUint(src.str)] = utf16BEString(dst.str)
		case vName:
			c.text[beUint(src.str)] = glyphRune(dst.str)
		}
	}
}

func (c *cmap) readBFRange(l *lexer) {
	for {
		lo, ok := l.next()
		if !ok || lo.kind == vOp {
			return
		}
		hi, ok := l.next()
		if !ok || hi.kind == vOp {
			return
		}
		dst, ok := l.next()
		if !ok || dst.kind == vOp {
			return
		}
		if lo.kind != vStr || hi.kind != vStr {
			continue
		}
		r := bfRange{lo: beUint(lo.str), hi: beUint(hi.str)}
		switch dst.kind {
		case vStr:
			r.dst = utf16Units(dst.str)
		case vArray:
			for _, e := range dst.arr {
				if e.kind == vStr {
					r.arr = append(r.arr, utf16BEString(e.str))
				}
			}
		default:
			continue
		}
		c.ranges = append(c.ranges, r)
	}
}

func (c *cmap) readCIDChar(l *lexer) {
	for {
		src, ok := l.next()
		if !ok || src.kind == vOp {
			return
		}
		dst, ok := l.next()
		if !ok || dst.kind == vOp {
			return
		}
		if src.kind == vStr && dst.kind == vNum {
			c.cids[beUint(src.str)] = uint32(dst.num)
		}
	}
}

func (c *cmap) readCIDRange(l *lexer) {
	for {
		lo, ok := l.next()
		if !ok || lo.kind == vOp {
			return
		}
		hi, ok := l.next()
		if !ok || hi.kind == vOp {
			return
		}
		cid, ok := l.next()
		if !ok || cid.kind == vOp {
			return
		}
		if lo.kind == vStr && hi.kind == vStr && cid.kind == vNum {
			c.cidRgs = append(c.cidRgs, cidRange{beUint(lo.str), beUint(hi.str), uint32(cid.num)})
		}
	}
}

// split 把一串位元組切成字碼。
//
// 切法由 codespace 決定:同一份 CMap 裡可以同時有一個位元組的碼
// (通常是 ASCII)與兩個位元組的碼(中文),而它們的長度只能靠
// 「這個值落在哪一段」判斷。
func (c *cmap) split(s string, defLen int) []code {
	var out []code
	minLen := defLen
	if len(c.spaces) > 0 {
		minLen = 4
		for _, sp := range c.spaces {
			if sp.n < minLen {
				minLen = sp.n
			}
		}
	}
	for i := 0; i < len(s); {
		matched := false
		for n := 1; n <= 4 && i+n <= len(s); n++ {
			v := beUint(s[i : i+n])
			for _, sp := range c.spaces {
				if sp.n == n && v >= sp.lo && v <= sp.hi {
					out = append(out, code{v, n})
					i += n
					matched = true
					break
				}
			}
			if matched {
				break
			}
		}
		if matched {
			continue
		}
		n := minLen
		if i+n > len(s) {
			n = len(s) - i
		}
		if n <= 0 {
			break
		}
		out = append(out, code{beUint(s[i : i+n]), n})
		i += n
	}
	return out
}

// lookup 查一個字碼對應的文字。
func (c *cmap) lookup(v uint32) (string, bool) {
	if s, ok := c.text[v]; ok {
		return s, true
	}
	for _, r := range c.ranges {
		if v < r.lo || v > r.hi {
			continue
		}
		if r.arr != nil {
			if i := int(v - r.lo); i < len(r.arr) {
				return r.arr[i], true
			}
			return "", false
		}
		if len(r.dst) == 0 {
			return "", false
		}
		// [雷] 遞增只加在**最後一個**碼元上。整個值一起加會讓
		// 代理對的高位跟著跑,解出來是完全不同的一批字。
		u := make([]uint16, len(r.dst))
		copy(u, r.dst)
		u[len(u)-1] += uint16(v - r.lo)
		return string(utf16.Decode(u)), true
	}
	return "", false
}

// cid 查一個字碼對應的 CID。
func (c *cmap) cid(v uint32) (uint32, bool) {
	if id, ok := c.cids[v]; ok {
		return id, true
	}
	for _, r := range c.cidRgs {
		if v >= r.lo && v <= r.hi {
			return r.cid + (v - r.lo), true
		}
	}
	return 0, false
}

func (c *cmap) empty() bool {
	return len(c.text) == 0 && len(c.ranges) == 0 && len(c.cids) == 0 && len(c.cidRgs) == 0
}

func beUint(s string) uint32 {
	var v uint32
	for i := 0; i < len(s) && i < 4; i++ {
		v = v<<8 | uint32(s[i])
	}
	return v
}

func utf16Units(s string) []uint16 {
	u := make([]uint16, 0, len(s)/2)
	for i := 0; i+1 < len(s); i += 2 {
		u = append(u, uint16(s[i])<<8|uint16(s[i+1]))
	}
	if len(s)%2 == 1 {
		u = append(u, uint16(s[len(s)-1]))
	}
	return u
}

// utf16BEString 把 ToUnicode 的目標值換成文字。
//
// 那個值一律是 UTF-16BE,不是 UTF-8 也不是單一字碼 —— 一個字碼可以
// 對應到好幾個字(連字就是這樣拆開的)。
func utf16BEString(s string) string {
	return string(utf16.Decode(utf16Units(s)))
}
