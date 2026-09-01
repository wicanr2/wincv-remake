package pdf

import (
	"encoding/binary"
	"fmt"
	"math"
)

// CFF(Compact Font Format)是 PDF 裡第二常見的嵌入字型格式:
// LaTeX 產的 Type1C、以及所有 OpenType/CFF 的中日韓字型(Noto CJK 就是)
// 都走這裡。`x/image/font/sfnt` 只認得完整的 sfnt 容器,而 PDF 嵌的是
// **裸的 CFF**,沒有那層容器,所以這一份自己解。
//
// 字形的形狀寫成 Type2 charstring:一種堆疊機的位元組碼,畫線與貝茲曲線
// 之外還有子程式呼叫與提示(hint)指令。提示對畫面沒有影響(那是給
// 低解析度的字形微調用的),但它們的運算元一定要正確消化掉 ——
// 少吃一個運算元,後面整串座標就會錯位,而畫出來仍然是「某種形狀」。

// cffFont 是一份解析好的 CFF。
type cffFont struct {
	charStrings [][]byte
	gsubrs      [][]byte
	subrs       [][]byte

	// CID 字型把字形分成好幾組,各組有自己的區域子程式。
	isCID    bool
	fdSelect []uint8
	fdSubrs  [][][]byte

	// matrix 是字形座標到字身的變換。預設是千分之一,但不是所有字型都用它。
	matrix [6]float64

	// encoding 是非 CID 字型自帶的「字碼 → 字形編號」對照。
	// 沒有這一張(用預設編碼)時是 nil,那時候交給後備字型 ——
	// 拿字碼去猜編號會畫出別的字,而那看起來很正常。
	encoding map[byte]uint16

	// cidToGID 是 CID 到字形編號的對照,來自 CFF 自己的 charset。
	//
	// [雷] CID 字型的 CID **不等於**字形編號。PDF 的 CIDToGIDMap 對
	// CFF 的 CID 字型不適用(那是給 TrueType 用的),對照表在 CFF 裡面。
	// 當成相等的話畫出來是一整篇別的字,而每個字看起來都很正常。
	cidToGID map[uint32]uint16
}

// parseCFF 解析一份裸的 CFF。
func parseCFF(b []byte) (f *cffFont, err error) {
	defer func() {
		if r := recover(); r != nil {
			f, err = nil, fmt.Errorf("CFF 解析失敗(%v)", r)
		}
	}()
	if len(b) < 4 {
		return nil, fmt.Errorf("CFF 太短")
	}
	if b[0] != 1 {
		// 第 2 版(CFF2)是可變字型用的,結構不同。
		return nil, fmt.Errorf("不支援的 CFF 版本 %d", b[0])
	}
	hdrSize := int(b[2])
	if hdrSize < 4 || hdrSize > len(b) {
		return nil, fmt.Errorf("CFF 標頭長度不合理")
	}

	pos := hdrSize
	if _, pos, err = readIndex(b, pos); err != nil { // Name INDEX
		return nil, err
	}
	topDicts, pos, err := readIndex(b, pos)
	if err != nil || len(topDicts) == 0 {
		return nil, fmt.Errorf("CFF 沒有 Top DICT")
	}
	if _, pos, err = readIndex(b, pos); err != nil { // String INDEX
		return nil, err
	}
	gsubrs, _, err := readIndex(b, pos)
	if err != nil {
		return nil, err
	}

	f = &cffFont{gsubrs: gsubrs, matrix: [6]float64{0.001, 0, 0, 0.001, 0, 0}}
	top := parseDict(topDicts[0])
	if m, ok := top[cffFontMatrix]; ok && len(m) == 6 {
		copy(f.matrix[:], m)
	}
	_, f.isCID = top[cffROS]

	csOff := dictInt(top, cffCharStrings)
	if csOff <= 0 || csOff >= len(b) {
		return nil, fmt.Errorf("CFF 沒有字形資料")
	}
	if f.charStrings, _, err = readIndex(b, csOff); err != nil {
		return nil, err
	}

	// 非 CID 字型的區域子程式在 Private DICT 裡。
	if p, ok := top[cffPrivate]; ok && len(p) == 2 {
		f.subrs = readPrivateSubrs(b, int(p[1]), int(p[0]))
	}

	if f.isCID {
		f.readFDArray(b, top)
		f.readFDSelect(b, dictInt(top, cffFDSelect))
		f.readCharset(b, dictInt(top, cffCharset))
	} else {
		f.readEncoding(b, dictInt(top, cffEncoding))
	}
	return f, nil
}

// readEncoding 讀非 CID 字型自帶的字碼對照。
//
// 位移 0 與 1 是預先定義的編碼(Standard / Expert),那兩種要靠字形
// 名稱轉一手才對得起來,這裡不做 —— 對不起來時交給後備字型,
// 比拿字碼去猜編號安全。
func (f *cffFont) readEncoding(b []byte, off int) {
	if off <= 1 || off >= len(b) {
		return
	}
	m := map[byte]uint16{}
	format := b[off] & 0x7F
	switch format {
	case 0:
		if off+1 >= len(b) {
			return
		}
		n := int(b[off+1])
		for i := 0; i < n && off+2+i < len(b); i++ {
			m[b[off+2+i]] = uint16(i + 1)
		}
	case 1:
		if off+1 >= len(b) {
			return
		}
		nRanges := int(b[off+1])
		gid := uint16(1)
		p := off + 2
		for i := 0; i < nRanges && p+1 < len(b); i++ {
			first := b[p]
			left := int(b[p+1])
			for k := 0; k <= left; k++ {
				m[first+byte(k)] = gid
				gid++
			}
			p += 2
		}
	default:
		return
	}
	if len(m) > 0 {
		f.encoding = m
	}
}

// Top / Private DICT 用得到的運算子。兩位元組的用 1200+x 表示。
const (
	cffCharset     = 15
	cffEncoding    = 16
	cffCharStrings = 17
	cffPrivate     = 18
	cffSubrs       = 19
	cffFontMatrix  = 1200 + 7
	cffROS         = 1200 + 30
	cffFDArray     = 1200 + 36
	cffFDSelect    = 1200 + 37
)

func dictInt(d map[int][]float64, key int) int {
	if v, ok := d[key]; ok && len(v) > 0 {
		return int(v[len(v)-1])
	}
	return 0
}

// readPrivateSubrs 讀一份 Private DICT 裡的區域子程式。
//
// [雷] Subrs 的位置是**相對於 Private DICT 的開頭**,不是檔案開頭。
// 照檔案開頭算會讀到別的地方,而那裡通常也是合法的位元組序列 ——
// 解出來是一堆能跑但畫錯的子程式。
func readPrivateSubrs(b []byte, off, size int) [][]byte {
	if off <= 0 || size <= 0 || off+size > len(b) {
		return nil
	}
	priv := parseDict(b[off : off+size])
	so := dictInt(priv, cffSubrs)
	if so <= 0 || off+so >= len(b) {
		return nil
	}
	subrs, _, err := readIndex(b, off+so)
	if err != nil {
		return nil
	}
	return subrs
}

func (f *cffFont) readFDArray(b []byte, top map[int][]float64) {
	off := dictInt(top, cffFDArray)
	if off <= 0 || off >= len(b) {
		return
	}
	fds, _, err := readIndex(b, off)
	if err != nil {
		return
	}
	for _, fd := range fds {
		d := parseDict(fd)
		var subrs [][]byte
		if p, ok := d[cffPrivate]; ok && len(p) == 2 {
			subrs = readPrivateSubrs(b, int(p[1]), int(p[0]))
		}
		f.fdSubrs = append(f.fdSubrs, subrs)
	}
}

// readFDSelect 讀「哪個字形屬於哪一組」。
func (f *cffFont) readFDSelect(b []byte, off int) {
	if off <= 0 || off >= len(b) {
		return
	}
	n := len(f.charStrings)
	f.fdSelect = make([]uint8, n)
	switch b[off] {
	case 0:
		for i := 0; i < n && off+1+i < len(b); i++ {
			f.fdSelect[i] = b[off+1+i]
		}
	case 3:
		if off+5 > len(b) {
			return
		}
		nRanges := int(binary.BigEndian.Uint16(b[off+1:]))
		p := off + 3
		if p+nRanges*3+2 > len(b) {
			return
		}
		for i := 0; i < nRanges; i++ {
			first := int(binary.BigEndian.Uint16(b[p:]))
			fd := b[p+2]
			next := int(binary.BigEndian.Uint16(b[p+3:]))
			for g := first; g < next && g < n; g++ {
				if g >= 0 {
					f.fdSelect[g] = fd
				}
			}
			p += 3
		}
	}
}

// readCharset 讀字形編號到 CID 的對照,並反過來建表。
func (f *cffFont) readCharset(b []byte, off int) {
	n := len(f.charStrings)
	f.cidToGID = make(map[uint32]uint16, n)
	// 第 0 號字形一定是 .notdef,charset 不列它。
	f.cidToGID[0] = 0
	if off <= 2 || off >= len(b) {
		// 0/1/2 是預先定義的 charset。CID 字型不會用,但真的遇到時
		// 當成「CID 就是字形編號」比整份不畫好。
		for g := 0; g < n; g++ {
			f.cidToGID[uint32(g)] = uint16(g)
		}
		return
	}
	switch b[off] {
	case 0:
		p := off + 1
		for g := 1; g < n && p+1 < len(b); g++ {
			f.cidToGID[uint32(binary.BigEndian.Uint16(b[p:]))] = uint16(g)
			p += 2
		}
	case 1, 2:
		step := 3
		if b[off] == 2 {
			step = 4
		}
		p := off + 1
		g := 1
		for g < n && p+step <= len(b) {
			first := uint32(binary.BigEndian.Uint16(b[p:]))
			left := 0
			if step == 3 {
				left = int(b[p+2])
			} else {
				left = int(binary.BigEndian.Uint16(b[p+2:]))
			}
			for i := 0; i <= left && g < n; i++ {
				f.cidToGID[first+uint32(i)] = uint16(g)
				g++
			}
			p += step
		}
	}
}

// gidForCID 查一個 CID 對應的字形編號。
func (f *cffFont) gidForCID(cid uint32) (uint16, bool) {
	if !f.isCID {
		return uint16(cid), true
	}
	g, ok := f.cidToGID[cid]
	return g, ok
}

// readIndex 讀一個 INDEX 結構。
func readIndex(b []byte, pos int) ([][]byte, int, error) {
	if pos < 0 || pos+2 > len(b) {
		return nil, pos, fmt.Errorf("INDEX 超出範圍")
	}
	count := int(binary.BigEndian.Uint16(b[pos:]))
	if count == 0 {
		return nil, pos + 2, nil
	}
	if pos+3 > len(b) {
		return nil, pos, fmt.Errorf("INDEX 被截斷")
	}
	offSize := int(b[pos+2])
	if offSize < 1 || offSize > 4 {
		return nil, pos, fmt.Errorf("INDEX 的位移大小不合理(%d)", offSize)
	}
	offBase := pos + 3
	end := offBase + (count+1)*offSize
	if end > len(b) {
		return nil, pos, fmt.Errorf("INDEX 被截斷")
	}
	off := func(i int) int {
		v := 0
		for j := 0; j < offSize; j++ {
			v = v<<8 | int(b[offBase+i*offSize+j])
		}
		return v
	}
	// 位移是 1 起算的,而且相對於資料區的前一個位元組。
	dataStart := end - 1
	items := make([][]byte, 0, count)
	for i := 0; i < count; i++ {
		lo, hi := dataStart+off(i), dataStart+off(i+1)
		if lo < dataStart || hi > len(b) || hi < lo {
			return nil, pos, fmt.Errorf("INDEX 的第 %d 筆位移不合理", i)
		}
		items = append(items, b[lo:hi])
	}
	return items, dataStart + off(count), nil
}

// parseDict 解一份 DICT。運算元先進堆疊,遇到運算子才收成一筆。
func parseDict(b []byte) map[int][]float64 {
	out := map[int][]float64{}
	var stack []float64
	for i := 0; i < len(b); {
		c := int(b[i])
		switch {
		case c <= 21: // 運算子
			op := c
			i++
			if c == 12 {
				if i >= len(b) {
					return out
				}
				op = 1200 + int(b[i])
				i++
			}
			vals := make([]float64, len(stack))
			copy(vals, stack)
			out[op] = vals
			stack = stack[:0]
		case c == 28:
			if i+3 > len(b) {
				return out
			}
			stack = append(stack, float64(int16(binary.BigEndian.Uint16(b[i+1:]))))
			i += 3
		case c == 29:
			if i+5 > len(b) {
				return out
			}
			stack = append(stack, float64(int32(binary.BigEndian.Uint32(b[i+1:]))))
			i += 5
		case c == 30: // 實數,用半位元組編碼
			v, n := parseRealNibbles(b[i+1:])
			stack = append(stack, v)
			i += 1 + n
		case c >= 32 && c <= 246:
			stack = append(stack, float64(c-139))
			i++
		case c >= 247 && c <= 250:
			if i+2 > len(b) {
				return out
			}
			stack = append(stack, float64((c-247)*256+int(b[i+1])+108))
			i += 2
		case c >= 251 && c <= 254:
			if i+2 > len(b) {
				return out
			}
			stack = append(stack, float64(-(c-251)*256-int(b[i+1])-108))
			i += 2
		default:
			i++
		}
		if len(stack) > 48 {
			stack = stack[:48]
		}
	}
	return out
}

// parseRealNibbles 解 DICT 裡的實數。每個半位元組是一個字元。
func parseRealNibbles(b []byte) (float64, int) {
	var s []byte
	for i := 0; i < len(b); i++ {
		for _, nib := range [2]byte{b[i] >> 4, b[i] & 0x0F} {
			switch {
			case nib <= 9:
				s = append(s, '0'+nib)
			case nib == 0x0a:
				s = append(s, '.')
			case nib == 0x0b:
				s = append(s, 'E')
			case nib == 0x0c:
				s = append(s, 'E', '-')
			case nib == 0x0e:
				s = append(s, '-')
			case nib == 0x0f:
				return parseFloatBytes(s), i + 1
			}
		}
	}
	return parseFloatBytes(s), len(b)
}

func parseFloatBytes(s []byte) float64 {
	var v float64
	if _, err := fmt.Sscanf(string(s), "%g", &v); err != nil {
		return 0
	}
	if math.IsNaN(v) || math.IsInf(v, 0) {
		return 0
	}
	return v
}
