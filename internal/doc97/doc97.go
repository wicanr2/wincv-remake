// Package doc97 讀 Word 97–2003 的 .doc。
//
// 外殼是 OLE2 複合文件(internal/cfb),裡面主要是三條串流:
//
//	WordDocument   FIB(檔案資訊區塊)+ 正文的位元組 + 屬性頁
//	0Table/1Table  各種對照表;用哪一條由 FIB 裡的一個位元決定
//	Data           圖片等大塊資料
//
// 正文不是一段連續的文字。它被切成「片段」(piece),每一段各自說明
// 自己在哪裡、是單位元組還是 UTF-16 —— 這是 Word 為了讓編輯時不必
// 搬動整份文件而設計的。所以取文字的第一步是走 piece table,
// 把片段接回一條連續的字元序列。
//
// 兩件事讓「看起來對」與「真的對」差很多:
//
//   - **表格的邊界不在文字裡。** 儲存格結尾與列結尾用的是同一個字元
//     (0x07),差別記在段落屬性的一個位元上。只看文字的話,分不出
//     一列有幾格,拼出來的表格會是自洽但錯的。
//   - **標題不是靠字體大小認出來的。** 段落屬性帶著樣式編號,樣式表裡
//     記著那個樣式的識別碼;識別碼 1 到 9 就是標題一到標題九,而且
//     與介面語言無關 —— 中文版 Word 的「標題 1」識別碼一樣是 1。
//
// 沒有做的:圖片(內容在 Data 串流裡,是另一套 PICF 結構)、
// 加密的檔案、Word 6/95(FIB 的版面不同,遇到會明說)。
package doc97

import (
	"encoding/binary"
	"fmt"
	"github.com/wicanr2/wincv-remake/internal/i18n"
	"unicode/utf16"

	"golang.org/x/text/encoding/charmap"

	"github.com/wicanr2/wincv-remake/internal/cfb"
)

// Doc 是一份打開著的 .doc。
type Doc struct {
	// chars 是整份文件的字元,索引就是 CP(character position)。
	// 各種對照表都用 CP 或 FC 指位置,所以先攤平成一條序列,
	// 後面的工作全部變成查表。
	chars []rune
	// fcs[i] 是 chars[i] 在 WordDocument 串流裡的位元組位置。
	// 屬性表用的是 FC 不是 CP,兩者之間隔著 piece table。
	fcs []int32

	ccpText int // 正文有幾個字元;之後是註腳、頁首頁尾等
	ccpFtn  int

	paras []paraProp
	runs  []charRun
	stis  []int // istd → sti(樣式識別碼)

	// lists 是「清單編號方式」表,lfo 把段落上的 ilfo 對到那張表。
	// 見 lists.go。
	lists map[uint32]*lstInfo
	lfo   []uint32
}

// Open 打開一份 .doc。
func Open(name string) (*Doc, error) {
	f, err := cfb.Open(name)
	if err != nil {
		return nil, err
	}
	return New(f)
}

// New 從一個已經打開的複合文件建 Doc。
func New(f *cfb.File) (*Doc, error) {
	wd, err := f.Stream("WordDocument")
	if err != nil {
		return nil, fmt.Errorf(i18n.T("這不是 Word 文件(沒有 WordDocument 串流)"))
	}
	if len(wd) < 0x200 {
		return nil, fmt.Errorf(i18n.T("WordDocument 串流太短"))
	}
	if magic := binary.LittleEndian.Uint16(wd); magic != 0xA5EC {
		return nil, fmt.Errorf(i18n.T("這不是 Word 文件(識別碼 0x%04X)"), magic)
	}
	nFib := int(binary.LittleEndian.Uint16(wd[0x02:]))
	flags := binary.LittleEndian.Uint16(wd[0x0A:])
	if flags&0x0100 != 0 {
		return nil, fmt.Errorf(i18n.T("這份文件有密碼保護"))
	}
	if nFib < 193 {
		// Word 6/95 的 FIB 版面不同,對照表的位置也不一樣。
		// 照 Word 97 的位置去讀會拿到別的欄位,而那些數字都是
		// 合法的偏移量 —— 解出來是垃圾而不是錯誤。
		return nil, fmt.Errorf(i18n.T("這是 Word 6/95 的格式(nFib=%d),尚未支援"), nFib)
	}

	tableName := "0Table"
	if flags&0x0200 != 0 {
		tableName = "1Table"
	}
	tbl, err := f.Stream(tableName)
	if err != nil {
		return nil, fmt.Errorf(i18n.T("找不到 %s 串流"), tableName)
	}

	d := &Doc{
		ccpText: int(int32(binary.LittleEndian.Uint32(wd[0x4C:]))),
		ccpFtn:  int(int32(binary.LittleEndian.Uint32(wd[0x50:]))),
	}
	if err := d.readText(wd, tbl); err != nil {
		return nil, err
	}
	d.readStyles(wd, tbl)
	d.readParaProps(wd, tbl)
	d.readCharRuns(wd, tbl)
	d.readLists(wd, tbl)
	return d, nil
}

func (d *Doc) Close() error { return nil }

// fibFC 取 FibRgFcLcb 裡第 i 組的 fc 與 lcb。
//
// 這個陣列從 0x9A 開始,每組 8 個位元組(4 位元組位置 + 4 位元組長度)。
// 位置固定在這裡是因為它前面的三段(FibBase、FibRgW97、FibRgLw97)
// 都是定長的。
func fibFC(wd []byte, i int) (int, int) {
	off := 0x9A + i*8
	if off+8 > len(wd) {
		return 0, 0
	}
	return int(int32(binary.LittleEndian.Uint32(wd[off:]))),
		int(int32(binary.LittleEndian.Uint32(wd[off+4:])))
}

// FibRgFcLcb97 裡用得到的欄位編號。
const (
	fibStshf      = 1
	fibPlcfBteChp = 12
	fibPlcfBtePap = 13
	fibClx        = 33
)

// piece 是 piece table 裡的一段。
type piece struct {
	cpStart int
	cpEnd   int
	fc      int // 在 WordDocument 串流裡的位元組位置
	wide    bool
}

// readText 走 piece table,把正文攤平成一條字元序列。
func (d *Doc) readText(wd, tbl []byte) error {
	fcClx, lcbClx := fibFC(wd, fibClx)
	if fcClx < 0 || lcbClx <= 0 || fcClx+lcbClx > len(tbl) {
		return fmt.Errorf(i18n.T("piece table 的位置不合理(fc=%d lcb=%d)"), fcClx, lcbClx)
	}
	pieces, err := parseCLX(tbl[fcClx : fcClx+lcbClx])
	if err != nil {
		return err
	}
	for _, p := range pieces {
		n := p.cpEnd - p.cpStart
		if n <= 0 {
			continue
		}
		if p.wide {
			if p.fc+2*n > len(wd) {
				n = (len(wd) - p.fc) / 2
			}
			if n <= 0 {
				continue
			}
			u := make([]uint16, n)
			for i := 0; i < n; i++ {
				u[i] = binary.LittleEndian.Uint16(wd[p.fc+2*i:])
			}
			for i, r := range utf16.Decode(u) {
				d.chars = append(d.chars, r)
				d.fcs = append(d.fcs, int32(p.fc+2*i))
			}
			continue
		}
		if p.fc+n > len(wd) {
			n = len(wd) - p.fc
		}
		if n <= 0 {
			continue
		}
		// [雷] 單位元組的片段一律是 cp1252,不是文件本身的語系字碼頁。
		// Word 只在整段文字都塞得進 cp1252 時才用這種寫法,所以
		// 中文的內容必定是 UTF-16 的片段 —— 拿 Big5 去解這一段
		// 會把好好的英文變成亂碼。
		dec := charmap.Windows1252.NewDecoder()
		for i := 0; i < n; i++ {
			s, err := dec.Bytes([]byte{wd[p.fc+i]})
			r := rune(wd[p.fc+i])
			if err == nil && len(s) > 0 {
				rs := []rune(string(s))
				r = rs[0]
			}
			d.chars = append(d.chars, r)
			d.fcs = append(d.fcs, int32(p.fc+i))
		}
	}
	if len(d.chars) == 0 {
		return fmt.Errorf(i18n.T("這份文件沒有文字"))
	}
	if d.ccpText < 0 || d.ccpText > len(d.chars) {
		d.ccpText = len(d.chars)
	}
	return nil
}

// parseCLX 解 piece table。
//
// CLX 是「幾筆 Prc(直接寫在這裡的屬性)之後接一筆 Pcdt(片段表)」。
// Prc 用不到,但它們的長度必須算對才走得到 Pcdt。
func parseCLX(clx []byte) ([]piece, error) {
	i := 0
	for i < len(clx) {
		switch clx[i] {
		case 1: // Prc
			if i+3 > len(clx) {
				return nil, fmt.Errorf(i18n.T("piece table 被截斷"))
			}
			cb := int(int16(binary.LittleEndian.Uint16(clx[i+1:])))
			i += 3 + cb
		case 2: // Pcdt
			if i+5 > len(clx) {
				return nil, fmt.Errorf(i18n.T("piece table 被截斷"))
			}
			lcb := int(binary.LittleEndian.Uint32(clx[i+1:]))
			if lcb < 0 || i+5+lcb > len(clx) {
				lcb = len(clx) - i - 5
			}
			return parsePlcPcd(clx[i+5 : i+5+lcb])
		default:
			return nil, fmt.Errorf(i18n.T("piece table 裡有看不懂的項目(0x%02X)"), clx[i])
		}
	}
	return nil, fmt.Errorf(i18n.T("piece table 裡沒有片段表"))
}

// parsePlcPcd 解片段表:n+1 個 CP 之後接 n 個 8 位元組的描述。
func parsePlcPcd(b []byte) ([]piece, error) {
	if len(b) < 16 {
		return nil, fmt.Errorf(i18n.T("片段表太短"))
	}
	n := (len(b) - 4) / 12
	if n <= 0 {
		return nil, fmt.Errorf(i18n.T("片段表是空的"))
	}
	cps := make([]int, n+1)
	for i := 0; i <= n; i++ {
		cps[i] = int(int32(binary.LittleEndian.Uint32(b[i*4:])))
	}
	base := 4 * (n + 1)
	out := make([]piece, 0, n)
	for i := 0; i < n; i++ {
		off := base + i*8
		fcRaw := binary.LittleEndian.Uint32(b[off+2:])
		p := piece{cpStart: cps[i], cpEnd: cps[i+1]}
		// [雷] 第 30 個位元是「這一段是單位元組」的旗標,而位置本身
		// 要除以二才是真正的位元組偏移量。不處理的話會從檔案裡
		// 完全不相干的地方開始讀,而讀出來的東西看起來像亂碼,
		// 不像位置算錯。
		if fcRaw&0x40000000 != 0 {
			p.fc = int(fcRaw&0x3FFFFFFF) / 2
			p.wide = false
		} else {
			p.fc = int(fcRaw & 0x3FFFFFFF)
			p.wide = true
		}
		out = append(out, p)
	}
	return out, nil
}
