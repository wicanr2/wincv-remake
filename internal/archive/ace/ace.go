package ace

import (
	"encoding/binary"
	"fmt"
	"hash/crc32"
	"strings"
	"time"
)

// 壓縮法。
const (
	compStored  = 0
	compLZ77    = 1 // ACE 1.0
	compBlocked = 2 // ACE 2.0
)

// blocked 模式底下的子模式。
const (
	modeLZ77      = 0
	modeLZ77Delta = 1 // 先重排位元組再取差值
	modeLZ77Exe   = 2 // 先把 x86 JMP/CALL 的目標改成相對值
	modeSound8    = 3
	modeSound16   = 4
	modeSound32A  = 5
	modeSound32B  = 6
	modePic       = 7
)

func modeName(m int) string {
	names := []string{"LZ77", "LZ77_DELTA", "LZ77_EXE",
		"SOUND_8", "SOUND_16", "SOUND_32A", "SOUND_32B", "PIC"}
	if m >= 0 && m < len(names) {
		return names[m]
	}
	return fmt.Sprintf("未知(%d)", m)
}

type aceMode struct {
	mode      int
	deltaDist int
	deltaLen  int
	exeMode   int
}

func readMode(b *bitStream) (*aceMode, error) {
	v, err := b.read(8)
	if err != nil {
		return nil, err
	}
	m := &aceMode{mode: int(v)}
	switch m.mode {
	case modeLZ77Delta:
		d, err := b.read(8)
		if err != nil {
			return nil, err
		}
		l, err := b.read(17)
		if err != nil {
			return nil, err
		}
		m.deltaDist, m.deltaLen = int(d), int(l)
	case modeLZ77Exe:
		e, err := b.read(8)
		if err != nil {
			return nil, err
		}
		m.exeMode = int(e)
	}
	return m, nil
}

// 標頭型別與旗標。
const (
	typeMain   = 0
	typeFile32 = 1
	typeFile64 = 3

	flagAddSize  = 1 << 0
	flagComment  = 1 << 1
	flag64Bit    = 1 << 2
	flagNTSec    = 1 << 10
	flagContPrev = 1 << 12
	flagContNext = 1 << 13
	flagPassword = 1 << 14
	flagSolid    = 1 << 15
)

var magic = []byte("**ACE**")

// File 是壓縮檔裡的一筆。
type File struct {
	Name     string
	Size     int64
	ModTime  time.Time
	IsDir    bool
	CRC32    uint32
	Data     []byte
	CompType int
}

// aceCRC32 是 ACE 用的 CRC-32:多項式與位元序都與標準相同,
// 但**最後不做反相**。
func aceCRC32(b []byte) uint32 { return crc32.ChecksumIEEE(b) ^ 0xFFFFFFFF }

func aceCRC16(b []byte) uint16 { return uint16(aceCRC32(b) & 0xFFFF) }

// Read 讀出整份壓縮檔。
func Read(data []byte) ([]File, error) {
	at, err := findMain(data)
	if err != nil {
		return nil, err
	}
	dec := &decoder{lz: newLZ77()}
	var out []File

	for at < len(data) {
		hdr, next, err := parseHeader(data, at)
		if err != nil {
			if len(out) > 0 {
				return out, nil // 尾端可能有填補,已讀到的先給
			}
			return nil, err
		}
		if hdr != nil {
			f, err := dec.member(data, hdr)
			if err != nil {
				return out, fmt.Errorf("%s: %w", hdr.name, err)
			}
			out = append(out, f)
		}
		if next <= at {
			return out, fmt.Errorf("標頭位置沒有前進,可能是壞檔")
		}
		at = next
	}
	return out, nil
}

func findMain(data []byte) (int, error) {
	if len(data) >= 14 && string(data[7:14]) == string(magic) {
		return 0, nil
	}
	// 自解壓縮檔:主標頭藏在 stub 後面
	limit := min(len(data), 512*1024)
	for i := 8; i+7 <= limit; i++ {
		if string(data[i:i+7]) == string(magic) {
			return i - 7, nil
		}
	}
	return 0, fmt.Errorf("不是 ACE 檔(找不到 **ACE** 標記)")
}

type header struct {
	kind     int
	flags    uint16
	packSize int64
	origSize int64
	modTime  time.Time
	attribs  uint32
	crc32    uint32
	compType int
	compQual int
	params   uint16
	name     string
	dataAt   int
}

// parseHeader 讀一個標頭區塊。回傳的 header 為 nil 表示這一塊不是檔案
// (主標頭、修復記錄等),但仍然要照回傳的位置往下走。
func parseHeader(data []byte, at int) (*header, int, error) {
	if at+4 > len(data) {
		return nil, 0, fmt.Errorf("標頭被截斷")
	}
	le := binary.LittleEndian
	hcrc := le.Uint16(data[at : at+2])
	hsize := int(le.Uint16(data[at+2 : at+4]))
	body := at + 4
	if body+hsize > len(data) {
		return nil, 0, fmt.Errorf("標頭被截斷")
	}
	buf := data[body : body+hsize]
	if aceCRC16(buf) != hcrc {
		return nil, 0, fmt.Errorf("標頭 CRC 不符")
	}
	if len(buf) < 3 {
		return nil, 0, fmt.Errorf("標頭太短")
	}
	kind := int(buf[0])
	flags := le.Uint16(buf[1:3])
	i := 3
	after := body + hsize

	switch kind {
	case typeMain:
		return nil, after, nil

	case typeFile32, typeFile64:
		h := &header{kind: kind, flags: flags}
		if flags&flag64Bit != 0 {
			if i+16 > len(buf) {
				return nil, 0, fmt.Errorf("標頭被截斷")
			}
			h.packSize = int64(le.Uint64(buf[i : i+8]))
			h.origSize = int64(le.Uint64(buf[i+8 : i+16]))
			i += 16
		} else {
			if i+8 > len(buf) {
				return nil, 0, fmt.Errorf("標頭被截斷")
			}
			h.packSize = int64(le.Uint32(buf[i : i+4]))
			h.origSize = int64(le.Uint32(buf[i+4 : i+8]))
			i += 8
		}
		if i+20 > len(buf) {
			return nil, 0, fmt.Errorf("標頭被截斷")
		}
		h.modTime = dosTime(le.Uint32(buf[i : i+4]))
		h.attribs = le.Uint32(buf[i+4 : i+8])
		h.crc32 = le.Uint32(buf[i+8 : i+12])
		h.compType = int(buf[i+12])
		h.compQual = int(buf[i+13])
		h.params = le.Uint16(buf[i+14 : i+16])
		fnsz := int(le.Uint16(buf[i+18 : i+20]))
		i += 20
		if i+fnsz > len(buf) {
			return nil, 0, fmt.Errorf("標頭被截斷")
		}
		h.name = normalizeName(string(buf[i : i+fnsz]))
		h.dataAt = after
		return h, after + int(h.packSize), nil

	default:
		// 其他標頭(修復記錄等):照 addsize 跳過
		addsz := int64(0)
		if flags&flagAddSize != 0 {
			if flags&flag64Bit != 0 && i+8 <= len(buf) {
				addsz = int64(le.Uint64(buf[i : i+8]))
			} else if i+4 <= len(buf) {
				addsz = int64(le.Uint32(buf[i : i+4]))
			}
		}
		return nil, after + int(addsz), nil
	}
}

// normalizeName 把路徑分隔字元統一成 /。
func normalizeName(s string) string {
	s = strings.ReplaceAll(s, "\\", "/")
	return strings.TrimPrefix(s, "/")
}

func dosTime(v uint32) time.Time {
	t := uint16(v)
	d := uint16(v >> 16)
	return time.Date(
		1980+int(d>>9), time.Month((d>>5)&0xF), int(d&0x1F),
		int(t>>11), int((t>>5)&0x3F), int(t&0x1F)*2, 0, time.Local)
}
