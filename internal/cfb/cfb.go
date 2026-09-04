// Package cfb 讀 Compound File Binary(也就是 OLE2 複合文件)。
//
// 這是 Office 97–2003 那一代的容器格式:一個檔案裡放好幾條「串流」,
// 用一張像 FAT 的配置表串起來。`.doc`、`.xls`、`.ppt` 的外殼都是它,
// 裡面的內容各自不同。
//
// 三個容易解錯的地方:
//
//   - **磁區編號從 0 起算,但檔案開頭有 512 位元組的標頭。** 所以第 N 個
//     磁區在 (N+1) × 磁區大小。忘記加一會讀到前一個磁區的內容 ——
//     那是合法的位元組,不會出錯,只會解出垃圾。
//   - **小串流走另一張表。** 小於門檻(通常 4096)的串流不佔完整磁區,
//     它們擠在一條「迷你串流」裡,用迷你 FAT 串接。照大串流的方式讀
//     會拿到整份檔案裡別的東西。
//   - **鏈結可能成環。** 損壞的檔案會讓「跟著下一個磁區走」變成無窮迴圈。
package cfb

import (
	"encoding/binary"
	"fmt"
	"github.com/wicanr2/wincv-remake/internal/i18n"
	"os"
	"strings"
	"unicode/utf16"
)

// MaxBytes 是檔案大小上限。
const MaxBytes = 256 << 20

var magic = [8]byte{0xD0, 0xCF, 0x11, 0xE0, 0xA1, 0xB1, 0x1A, 0xE1}

// 特殊的磁區編號。
const (
	maxRegSect = 0xFFFFFFFA
	difSect    = 0xFFFFFFFC
	fatSect    = 0xFFFFFFFD
	endOfChain = 0xFFFFFFFE
	freeSect   = 0xFFFFFFFF
)

// File 是一個打開著的複合文件。
type File struct {
	data []byte

	sectorSize int
	miniSize   int
	miniCutoff uint32

	fat     []uint32
	miniFat []uint32
	mini    []byte

	entries []Entry
	byName  map[string]int
}

// Entry 是目錄裡的一筆:一條串流或一個儲存區。
type Entry struct {
	Name  string
	Type  byte // 1 儲存區,2 串流,5 根目錄
	Start uint32
	Size  uint64
}

// Open 打開一個複合文件。
func Open(name string) (*File, error) {
	st, err := os.Stat(name)
	if err != nil {
		return nil, err
	}
	if st.Size() > MaxBytes {
		return nil, fmt.Errorf(i18n.T("檔案太大(%d 位元組)"), st.Size())
	}
	b, err := os.ReadFile(name)
	if err != nil {
		return nil, err
	}
	return Parse(b)
}

// Parse 解析一份記憶體裡的複合文件。
func Parse(b []byte) (*File, error) {
	if len(b) < 512 {
		return nil, fmt.Errorf(i18n.T("太短,不是複合文件"))
	}
	for i, c := range magic {
		if b[i] != c {
			return nil, fmt.Errorf(i18n.T("不是複合文件(簽章不符)"))
		}
	}
	f := &File{data: b, byName: map[string]int{}}
	shift := binary.LittleEndian.Uint16(b[0x1E:])
	miniShift := binary.LittleEndian.Uint16(b[0x20:])
	if shift < 7 || shift > 20 || miniShift < 2 || miniShift >= shift {
		return nil, fmt.Errorf(i18n.T("磁區大小不合理(2^%d / 2^%d)"), shift, miniShift)
	}
	f.sectorSize = 1 << shift
	f.miniSize = 1 << miniShift
	f.miniCutoff = binary.LittleEndian.Uint32(b[0x38:])
	if f.miniCutoff == 0 {
		f.miniCutoff = 4096
	}

	if err := f.readFAT(); err != nil {
		return nil, err
	}
	if err := f.readDir(); err != nil {
		return nil, err
	}
	f.readMini()
	return f, nil
}

// sector 取一個磁區的內容。
func (f *File) sector(n uint32) []byte {
	off := (int(n) + 1) * f.sectorSize
	if off < 0 || off+f.sectorSize > len(f.data) {
		return nil
	}
	return f.data[off : off+f.sectorSize]
}

// readFAT 把 FAT 讀成一張表。
//
// FAT 自己也存在磁區裡,而「哪些磁區是 FAT」記在 DIFAT。DIFAT 的前
// 109 筆在標頭裡,不夠的話再串到別的磁區去 —— 一份大檔案的 FAT 本身
// 就要好幾百個磁區。
func (f *File) readFAT() error {
	b := f.data
	nFat := int(binary.LittleEndian.Uint32(b[0x2C:]))
	difStart := binary.LittleEndian.Uint32(b[0x44:])
	nDif := int(binary.LittleEndian.Uint32(b[0x48:]))

	var fatSects []uint32
	for i := 0; i < 109 && len(fatSects) < nFat; i++ {
		v := binary.LittleEndian.Uint32(b[0x4C+i*4:])
		if v > maxRegSect {
			break
		}
		fatSects = append(fatSects, v)
	}
	next := difStart
	per := f.sectorSize/4 - 1
	for i := 0; i < nDif+1 && next <= maxRegSect && len(fatSects) < nFat; i++ {
		s := f.sector(next)
		if s == nil {
			break
		}
		for j := 0; j < per && len(fatSects) < nFat; j++ {
			v := binary.LittleEndian.Uint32(s[j*4:])
			if v > maxRegSect {
				continue
			}
			fatSects = append(fatSects, v)
		}
		next = binary.LittleEndian.Uint32(s[per*4:])
	}
	if len(fatSects) == 0 {
		return fmt.Errorf(i18n.T("找不到配置表"))
	}
	for _, sn := range fatSects {
		s := f.sector(sn)
		if s == nil {
			continue
		}
		for j := 0; j+4 <= len(s); j += 4 {
			f.fat = append(f.fat, binary.LittleEndian.Uint32(s[j:]))
		}
	}
	return nil
}

// chain 沿著配置表把一條鏈上的磁區編號列出來。
func chainOf(fat []uint32, start uint32, limit int) []uint32 {
	var out []uint32
	seen := map[uint32]bool{}
	for n := start; n <= maxRegSect; {
		if int(n) >= len(fat) || seen[n] || len(out) >= limit {
			break
		}
		seen[n] = true
		out = append(out, n)
		n = fat[n]
	}
	return out
}

func (f *File) readDir() error {
	start := binary.LittleEndian.Uint32(f.data[0x30:])
	sects := chainOf(f.fat, start, len(f.fat)+1)
	if len(sects) == 0 {
		return fmt.Errorf(i18n.T("找不到目錄"))
	}
	perSect := f.sectorSize / 128
	for _, sn := range sects {
		s := f.sector(sn)
		if s == nil {
			continue
		}
		for i := 0; i < perSect; i++ {
			e, ok := parseEntry(s[i*128 : (i+1)*128])
			if !ok {
				continue
			}
			idx := len(f.entries)
			f.entries = append(f.entries, e)
			// 同名的以第一筆為準:.doc 的串流名在根儲存區裡是唯一的,
			// 而巢狀儲存區裡的同名項目不是我們要的那一條。
			if _, dup := f.byName[e.Name]; !dup {
				f.byName[e.Name] = idx
			}
		}
	}
	if len(f.entries) == 0 {
		return fmt.Errorf(i18n.T("目錄是空的"))
	}
	return nil
}

func parseEntry(b []byte) (Entry, bool) {
	typ := b[0x42]
	if typ != 1 && typ != 2 && typ != 5 {
		return Entry{}, false
	}
	n := int(binary.LittleEndian.Uint16(b[0x40:]))
	if n < 2 || n > 64 {
		return Entry{}, false
	}
	// 名字是 UTF-16LE,長度含結尾的空字元。
	u := make([]uint16, 0, n/2)
	for i := 0; i+1 < n-2; i += 2 {
		u = append(u, binary.LittleEndian.Uint16(b[i:]))
	}
	return Entry{
		Name:  string(utf16.Decode(u)),
		Type:  typ,
		Start: binary.LittleEndian.Uint32(b[0x74:]),
		Size:  binary.LittleEndian.Uint64(b[0x78:]),
	}, true
}

// readMini 讀出迷你串流與迷你配置表。
func (f *File) readMini() {
	miniStart := binary.LittleEndian.Uint32(f.data[0x3C:])
	nMini := int(binary.LittleEndian.Uint32(f.data[0x40:]))
	for _, sn := range chainOf(f.fat, miniStart, nMini+1) {
		s := f.sector(sn)
		if s == nil {
			continue
		}
		for j := 0; j+4 <= len(s); j += 4 {
			f.miniFat = append(f.miniFat, binary.LittleEndian.Uint32(s[j:]))
		}
	}
	// 根目錄那一筆的起始磁區指向迷你串流本身。
	if len(f.entries) > 0 && f.entries[0].Type == 5 {
		f.mini = f.readChain(f.entries[0].Start, f.entries[0].Size)
	}
}

// readChain 沿著大串流的鏈讀出內容。
func (f *File) readChain(start uint32, size uint64) []byte {
	if size > MaxBytes {
		size = MaxBytes
	}
	need := int(size)
	sects := chainOf(f.fat, start, need/f.sectorSize+2)
	out := make([]byte, 0, need)
	for _, sn := range sects {
		s := f.sector(sn)
		if s == nil {
			break
		}
		out = append(out, s...)
		if len(out) >= need {
			break
		}
	}
	if len(out) > need {
		out = out[:need]
	}
	return out
}

// readMiniChain 沿著迷你串流的鏈讀出內容。
func (f *File) readMiniChain(start uint32, size uint64) []byte {
	need := int(size)
	out := make([]byte, 0, need)
	seen := map[uint32]bool{}
	for n := start; n <= maxRegSect && len(out) < need; {
		if int(n) >= len(f.miniFat) || seen[n] {
			break
		}
		seen[n] = true
		off := int(n) * f.miniSize
		if off+f.miniSize > len(f.mini) {
			break
		}
		out = append(out, f.mini[off:off+f.miniSize]...)
		n = f.miniFat[n]
	}
	if len(out) > need {
		out = out[:need]
	}
	return out
}

// Names 列出所有串流與儲存區的名字。
func (f *File) Names() []string {
	out := make([]string, 0, len(f.entries))
	for _, e := range f.entries {
		out = append(out, e.Name)
	}
	return out
}

// Has 回答有沒有這條串流。
func (f *File) Has(name string) bool {
	i, ok := f.byName[name]
	return ok && f.entries[i].Type == 2
}

// Stream 讀出一條串流。
func (f *File) Stream(name string) ([]byte, error) {
	i, ok := f.byName[name]
	if !ok {
		return nil, fmt.Errorf(i18n.T("沒有 %s 這條串流"), printable(name))
	}
	e := f.entries[i]
	if e.Type != 2 {
		return nil, fmt.Errorf(i18n.T("%s 不是串流"), printable(name))
	}
	if e.Size < uint64(f.miniCutoff) {
		return f.readMiniChain(e.Start, e.Size), nil
	}
	return f.readChain(e.Start, e.Size), nil
}

// printable 把名字裡的控制字元換掉。
//
// Office 的串流名開頭常常是 0x01 到 0x05 這種控制字元(例如
// `\x05SummaryInformation`),直接印出去會弄亂終端機。
func printable(s string) string {
	var sb strings.Builder
	for _, r := range s {
		if r < 0x20 {
			fmt.Fprintf(&sb, "\\x%02x", r)
			continue
		}
		sb.WriteRune(r)
	}
	return sb.String()
}
