package doc97

import "encoding/binary"

// 清單的編號方式存在另外兩張表裡,不在段落屬性上。段落只帶兩個數字:
// `ilfo`(屬於哪一串)與 `ilvl`(第幾層),要走到「這一層是編號還是項目
// 符號」得繞一圈:
//
//	ilfo → PlfLfo 的第 ilfo-1 筆 → lsid
//	lsid → PlfLst 裡同 lsid 的那一串 → 第 ilvl 層的 LVL → nfc
//
// 不繞這一圈的話,所有清單都只能當成項目符號 —— 而畫出來仍然是一份
// 排版正常的清單,只是 1. 2. 3. 全變成了圓點。

// FibRgFcLcb97 裡這兩張表的欄位編號。
const (
	fibPlfLst = 73
	fibPlfLfo = 74
)

// nfcBullet 與 nfcNone 是「不編號」的兩種 nfc。其餘的(阿拉伯數字、
// 大小寫羅馬數字、大小寫字母…)都是編號清單。
const (
	nfcBullet = 23
	nfcNone   = 255
)

// lvlInfo 是一層清單的格式。
type lvlInfo struct {
	nfc   int
	start int
}

func (l lvlInfo) ordered() bool { return l.nfc != nfcBullet && l.nfc != nfcNone }

// lstInfo 是一串清單(LSTF + 它的各層 LVL)。
type lstInfo struct {
	lvls []lvlInfo
}

// level 取第 ilvl 層。簡單清單只有一層,所有層都用它。
func (l *lstInfo) level(ilvl int) (lvlInfo, bool) {
	if len(l.lvls) == 0 {
		return lvlInfo{}, false
	}
	if ilvl < 0 || ilvl >= len(l.lvls) {
		return l.lvls[0], true
	}
	return l.lvls[ilvl], true
}

// readLists 讀清單格式表。讀不到就整份當成沒有編號資訊,不是錯誤 ——
// 沒有清單的文件本來就沒有這兩張表。
func (d *Doc) readLists(wd, tbl []byte) {
	d.lists = map[uint32]*lstInfo{}
	d.readPlfLst(wd, tbl)
	d.readPlfLfo(wd, tbl)
}

// readPlfLst 讀清單定義表。
//
// [雷] lcbPlfLst 只涵蓋「cLst + LSTF 陣列」,後面接著的 LVL 陣列**不算在
// 它裡面**。照 lcb 切下去的話,剛好切在第一組 LVL 之前 —— 讀到的 LSTF 完全
// 正確、數量也對,只是每一串清單都是零層,於是所有編號清單都變成項目符號。
// 沒有任何一個地方會報錯,因為切出來的那一段本身是完好的。
func (d *Doc) readPlfLst(wd, tbl []byte) {
	fc, lcb := fibFC(wd, fibPlfLst)
	if fc < 0 || lcb < 2 || fc+lcb > len(tbl) {
		return
	}
	// 只有 LSTF 陣列的長度由 lcb 決定;LVL 要一直往後讀到表尾。
	b := tbl[fc:]
	cLst := int(int16(binary.LittleEndian.Uint16(b)))
	if cLst <= 0 || 2+cLst*lstfSize > lcb {
		return
	}
	// 先把 LSTF 讀完,才知道後面跟著幾組 LVL:簡單清單一組,其餘九組。
	type head struct {
		lsid   uint32
		simple bool
	}
	heads := make([]head, 0, cLst)
	for i := 0; i < cLst; i++ {
		r := b[2+i*lstfSize:]
		heads = append(heads, head{
			lsid:   binary.LittleEndian.Uint32(r),
			simple: r[26]&1 != 0,
		})
	}
	p := 2 + cLst*lstfSize
	for _, h := range heads {
		n := 9
		if h.simple {
			n = 1
		}
		info := &lstInfo{}
		for i := 0; i < n; i++ {
			lv, next, ok := readLVL(b, p)
			if !ok {
				// 長度對不上就停手。後面每一組的位置都由前一組算出來,
				// 錯一個位元組之後讀到的全是別的資料 —— 而那些數字看起來
				// 仍然像合法的 nfc。
				if len(info.lvls) > 0 {
					d.lists[h.lsid] = info
				}
				return
			}
			info.lvls = append(info.lvls, lv)
			p = next
		}
		d.lists[h.lsid] = info
	}
}

// lstfSize 是一筆 LSTF 的長度,lvlfSize 是一筆 LVLF 的長度。
const (
	lstfSize = 28
	lvlfSize = 28
)

// readLVL 讀一組 LVL:定長的 LVLF,後面接兩段長度自述的屬性,
// 再接一個計數字串。回傳下一組的位置。
func readLVL(b []byte, p int) (lvlInfo, int, bool) {
	if p < 0 || p+lvlfSize > len(b) {
		return lvlInfo{}, 0, false
	}
	f := b[p : p+lvlfSize]
	lv := lvlInfo{
		start: int(int32(binary.LittleEndian.Uint32(f))),
		nfc:   int(f[4]),
	}
	cbChpx, cbPapx := int(f[24]), int(f[25])
	q := p + lvlfSize + cbPapx + cbChpx
	if q+2 > len(b) {
		return lvlInfo{}, 0, false
	}
	// 結尾是編號的樣板字串(「%1.」這種),長度是字元數不是位元組數。
	cch := int(binary.LittleEndian.Uint16(b[q:]))
	q += 2 + 2*cch
	if q > len(b) {
		return lvlInfo{}, 0, false
	}
	return lv, q, true
}

// readPlfLfo 讀「哪一個 ilfo 對到哪一串清單」。
//
// 只要 LFO 前面固定的 16 個位元組裡的 lsid;後面的 rgLfoData 是逐層的
// 覆寫,長度不定,這裡不解 —— 覆寫改的多半是起始號碼,而編號或項目符號
// 這件事在原本那一串上就決定了。
func (d *Doc) readPlfLfo(wd, tbl []byte) {
	fc, lcb := fibFC(wd, fibPlfLfo)
	if fc < 0 || lcb < 4 || fc+lcb > len(tbl) {
		return
	}
	b := tbl[fc : fc+lcb]
	n := int(binary.LittleEndian.Uint32(b))
	const lfoSize = 16
	if n <= 0 || 4+n*lfoSize > len(b) {
		return
	}
	d.lfo = make([]uint32, n)
	for i := 0; i < n; i++ {
		d.lfo[i] = binary.LittleEndian.Uint32(b[4+i*lfoSize:])
	}
}

// listLevel 查一個段落所屬的那一層清單格式。
func (d *Doc) listLevel(ilfo, ilvl int) (lvlInfo, bool) {
	// ilfo 是 1 起算的索引。
	if ilfo < 1 || ilfo > len(d.lfo) {
		return lvlInfo{}, false
	}
	l, ok := d.lists[d.lfo[ilfo-1]]
	if !ok {
		return lvlInfo{}, false
	}
	return l.level(ilvl)
}
