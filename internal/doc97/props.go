package doc97

import (
	"encoding/binary"
	"sort"
)

// paraProp 是一個段落的屬性,連同它在串流裡的位元組範圍。
type paraProp struct {
	fcStart, fcEnd int32
	istd           int
	inTable        bool
	ttp            bool // 這一段是「列結尾」的標記,不是儲存格
	ilfo           int  // 不為 0 表示屬於某一串清單
	ilvl           int
	outline        int // \outlinelevel + 1,0 表示沒有指定
}

// charRun 是一段字元格式相同的範圍。
type charRun struct {
	fcStart, fcEnd       int32
	bold, italic, strike bool
}

// 用得到的段落與字元 sprm。
const (
	sprmPIlvl    = 0x260A
	sprmPIlfo    = 0x460B
	sprmPFInTbl  = 0x2416
	sprmPFTtp    = 0x2417
	sprmPOutLvl  = 0x2640
	sprmCFBold   = 0x0835
	sprmCFItalic = 0x0836
	sprmCFStrike = 0x0837
)

// readStyles 讀樣式表,取出每個樣式的識別碼。
//
// 識別碼(sti)才是「這是不是標題」的真值:它與介面語言無關,
// 中文版的「標題 1」與英文版的 "heading 1" 識別碼都是 1。
// 用名字比對會在每一種語言版本上各壞一次。
func (d *Doc) readStyles(wd, tbl []byte) {
	fc, lcb := fibFC(wd, fibStshf)
	if fc < 0 || lcb < 4 || fc+lcb > len(tbl) {
		return
	}
	st := tbl[fc : fc+lcb]
	cbStshi := int(binary.LittleEndian.Uint16(st))
	if cbStshi < 4 || 2+cbStshi > len(st) {
		return
	}
	cstd := int(binary.LittleEndian.Uint16(st[2:]))
	if cstd < 0 || cstd > 4096 {
		return
	}
	d.stis = make([]int, cstd)
	for i := range d.stis {
		d.stis[i] = -1
	}
	p := 2 + cbStshi
	for i := 0; i < cstd && p+2 <= len(st); i++ {
		cbStd := int(binary.LittleEndian.Uint16(st[p:]))
		p += 2
		if cbStd == 0 || p+cbStd > len(st) {
			p += cbStd
			continue
		}
		// STD 的第一個 16 位元裡,低 12 位是樣式識別碼。
		d.stis[i] = int(binary.LittleEndian.Uint16(st[p:]) & 0x0FFF)
		p += cbStd
	}
}

// headingOf 回答某個樣式編號是第幾階標題,0 表示不是標題。
func (d *Doc) headingOf(istd int) int {
	if istd < 0 || istd >= len(d.stis) {
		return 0
	}
	sti := d.stis[istd]
	if sti >= 1 && sti <= 9 {
		if sti > 6 {
			return 6
		}
		return sti
	}
	return 0
}

// plcData 把一個 PLC 拆開:前面是 n+1 個位置,後面是 n 筆固定大小的資料。
func plcData(b []byte, size int) ([]int32, [][]byte) {
	if len(b) < 4+size {
		return nil, nil
	}
	n := (len(b) - 4) / (4 + size)
	if n <= 0 {
		return nil, nil
	}
	pos := make([]int32, n+1)
	for i := 0; i <= n; i++ {
		pos[i] = int32(binary.LittleEndian.Uint32(b[i*4:]))
	}
	base := 4 * (n + 1)
	data := make([][]byte, n)
	for i := 0; i < n; i++ {
		data[i] = b[base+i*size : base+(i+1)*size]
	}
	return pos, data
}

// fkpPages 找出屬性頁的頁號。
//
// 屬性不是集中放在一張表裡,而是散在 WordDocument 串流的 512 位元組
// 「頁」上,由這個 PLC 指出哪幾頁。這個設計是為了讓編輯時只要改一頁。
func fkpPages(wd, tbl []byte, idx int) [][]byte {
	fc, lcb := fibFC(wd, idx)
	if fc < 0 || lcb <= 0 || fc+lcb > len(tbl) {
		return nil
	}
	_, data := plcData(tbl[fc:fc+lcb], 4)
	var out [][]byte
	for _, d := range data {
		pn := int(binary.LittleEndian.Uint32(d) & 0x003FFFFF)
		off := pn * 512
		if off < 0 || off+512 > len(wd) {
			continue
		}
		out = append(out, wd[off:off+512])
	}
	return out
}

// readParaProps 走段落屬性頁。
func (d *Doc) readParaProps(wd, tbl []byte) {
	for _, page := range fkpPages(wd, tbl, fibPlcfBtePap) {
		crun := int(page[511])
		if crun == 0 || 4*(crun+1)+13*crun > 511 {
			continue
		}
		for j := 0; j < crun; j++ {
			p := paraProp{
				fcStart: int32(binary.LittleEndian.Uint32(page[j*4:])),
				fcEnd:   int32(binary.LittleEndian.Uint32(page[(j+1)*4:])),
				istd:    -1,
			}
			// 每一筆的第一個位元組是 PAPX 在這一頁裡的「字組」偏移量;
			// 0 表示這一段沒有自己的屬性,整段沿用樣式的預設值。
			wordOff := int(page[4*(crun+1)+j*13])
			if wordOff != 0 {
				if g := papxAt(page, wordOff*2); len(g) >= 2 {
					p.istd = int(binary.LittleEndian.Uint16(g))
					applyParaSprms(&p, g[2:])
				}
			}
			d.paras = append(d.paras, p)
		}
	}
	sort.Slice(d.paras, func(i, j int) bool { return d.paras[i].fcStart < d.paras[j].fcStart })
}

// papxAt 取出一頁裡某個位置的段落屬性串。
//
// 長度有兩種寫法:第一個位元組不為 0 時它就是長度(以字組計,再扣掉
// 自己那半個);為 0 時真正的長度在下一個位元組。第二種是後來為了
// 放得下比較長的屬性才加的。
func papxAt(page []byte, off int) []byte {
	if off < 0 || off >= len(page) {
		return nil
	}
	cb := int(page[off])
	if cb == 0 {
		if off+1 >= len(page) {
			return nil
		}
		n := int(page[off+1]) * 2
		if off+2+n > len(page) {
			return nil
		}
		return page[off+2 : off+2+n]
	}
	n := cb*2 - 1
	if off+1+n > len(page) {
		return nil
	}
	return page[off+1 : off+1+n]
}

func applyParaSprms(p *paraProp, g []byte) {
	walkSprms(g, func(op uint16, operand []byte) {
		switch op {
		case sprmPFInTbl:
			p.inTable = len(operand) > 0 && operand[0] != 0
		case sprmPFTtp:
			p.ttp = len(operand) > 0 && operand[0] != 0
		case sprmPIlvl:
			if len(operand) > 0 {
				p.ilvl = int(operand[0])
			}
		case sprmPIlfo:
			if len(operand) >= 2 {
				p.ilfo = int(binary.LittleEndian.Uint16(operand))
			}
		case sprmPOutLvl:
			if len(operand) > 0 && operand[0] <= 8 {
				p.outline = int(operand[0]) + 1
			}
		}
	})
}

// readCharRuns 走字元屬性頁。
func (d *Doc) readCharRuns(wd, tbl []byte) {
	for _, page := range fkpPages(wd, tbl, fibPlcfBteChp) {
		crun := int(page[511])
		if crun == 0 || 4*(crun+1)+crun > 511 {
			continue
		}
		for j := 0; j < crun; j++ {
			r := charRun{
				fcStart: int32(binary.LittleEndian.Uint32(page[j*4:])),
				fcEnd:   int32(binary.LittleEndian.Uint32(page[(j+1)*4:])),
			}
			// 字元屬性頁的索引只有一個位元組,而且長度的寫法也比較簡單 ——
			// 它跟段落屬性頁長得像但不一樣,混用會讀到偏移一格的資料。
			off := int(page[4*(crun+1)+j]) * 2
			if off > 0 && off < len(page) {
				cb := int(page[off])
				if off+1+cb <= len(page) {
					applyCharSprms(&r, page[off+1:off+1+cb])
				}
			}
			d.runs = append(d.runs, r)
		}
	}
	sort.Slice(d.runs, func(i, j int) bool { return d.runs[i].fcStart < d.runs[j].fcStart })
}

func applyCharSprms(r *charRun, g []byte) {
	walkSprms(g, func(op uint16, operand []byte) {
		if len(operand) == 0 {
			return
		}
		// 這幾個是開關型的:0 關、1 開,128 表示沿用樣式、129 表示反轉。
		// 後兩種要真正算出來得先解樣式的預設值,這裡只認明確的開與關。
		on := operand[0] == 1
		switch op {
		case sprmCFBold:
			r.bold = on
		case sprmCFItalic:
			r.italic = on
		case sprmCFStrike:
			r.strike = on
		}
	})
}

// walkSprms 走一串屬性。
//
// 每一筆的長度藏在操作碼本身:第 13–15 位是「操作元大小」的編碼。
// 算錯一筆的長度,後面整串就全部錯位,而錯位之後解出來的仍然是
// 合法的操作碼 —— 所以會得到一份自洽但完全錯誤的屬性。
func walkSprms(g []byte, fn func(op uint16, operand []byte)) {
	i := 0
	for i+2 <= len(g) {
		op := binary.LittleEndian.Uint16(g[i:])
		i += 2
		var n int
		switch (op >> 13) & 7 {
		case 0, 1:
			n = 1
		case 2, 4, 5:
			n = 2
		case 3:
			n = 4
		case 7:
			n = 3
		case 6:
			if i >= len(g) {
				return
			}
			n = int(g[i])
			if n == 255 {
				// 只有表格定義用這種寫法:長度改成兩個位元組。
				if i+3 > len(g) {
					return
				}
				n = int(binary.LittleEndian.Uint16(g[i+1:])) + 2
			}
			i++
		}
		if i+n > len(g) {
			return
		}
		fn(op, g[i:i+n])
		i += n
	}
}

// paraAt 找出涵蓋某個位元組位置的段落屬性。
func (d *Doc) paraAt(fc int32) paraProp {
	i := sort.Search(len(d.paras), func(i int) bool { return d.paras[i].fcEnd > fc })
	if i < len(d.paras) && d.paras[i].fcStart <= fc {
		return d.paras[i]
	}
	return paraProp{istd: -1}
}

// runAt 找出涵蓋某個位元組位置的字元屬性。
func (d *Doc) runAt(fc int32) charRun {
	i := sort.Search(len(d.runs), func(i int) bool { return d.runs[i].fcEnd > fc })
	if i < len(d.runs) && d.runs[i].fcStart <= fc {
		return d.runs[i]
	}
	return charRun{}
}
