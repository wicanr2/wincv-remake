package lzh

import (
	"encoding/binary"
	"fmt"
	"github.com/wicanr2/wincv-remake/internal/i18n"
	"io"
	"strings"
	"time"
)

// Entry 是壓縮檔裡的一筆。
type Entry struct {
	Name     string // 已經把目錄分隔字元正規化成 /
	Method   string // 例如 "-lh5-"
	Packed   int64
	Original int64
	ModTime  time.Time
	IsDir    bool
	CRC      uint16
	Offset   int64 // 壓縮資料在檔案裡的位移
}

// List 讀出全部成員。r 要能 Seek,因為要跳過每一筆的壓縮資料。
func List(r io.ReadSeeker) ([]Entry, error) {
	var out []Entry
	pos, err := r.Seek(0, io.SeekStart)
	if err != nil {
		return nil, err
	}
	for {
		e, next, err := readHeader(r, pos)
		if err != nil {
			return out, err
		}
		if e == nil { // 檔尾:header size 為 0
			return out, nil
		}
		out = append(out, *e)
		pos = next
		if _, err := r.Seek(pos, io.SeekStart); err != nil {
			return out, err
		}
		if len(out) > 100000 {
			return out, fmt.Errorf(i18n.T("成員數異常,可能不是 LZH 檔"))
		}
	}
}

// readHeader 讀一筆標頭。回傳的 Entry 為 nil 表示到了檔尾。
//
// LHA 有四種標頭格式(level 0-3),差別大到不能共用一段程式碼。
// 常見的是 0/1/2;level 3 幾乎沒人用,遇到就報錯而不是猜。
func readHeader(r io.ReadSeeker, pos int64) (*Entry, int64, error) {
	if _, err := r.Seek(pos, io.SeekStart); err != nil {
		return nil, 0, err
	}
	var head [2]byte
	n, err := io.ReadFull(r, head[:])
	if err != nil || n < 2 {
		return nil, 0, nil // 讀不到就當檔尾,尾端補 0 的檔很常見
	}
	if head[0] == 0 {
		return nil, 0, nil
	}

	// level 在第 20 個位元組。先把足夠的前段讀進來再分流。
	if _, err := r.Seek(pos, io.SeekStart); err != nil {
		return nil, 0, err
	}
	buf := make([]byte, 26)
	if _, err := io.ReadFull(r, buf); err != nil {
		return nil, 0, nil
	}
	level := buf[20]

	switch level {
	case 0, 1:
		return readHeaderLv01(r, pos, int(head[0]), level)
	case 2:
		return readHeaderLv2(r, pos)
	default:
		return nil, 0, fmt.Errorf(i18n.T("還不支援 level %d 的標頭"), level)
	}
}

func readHeaderLv01(r io.ReadSeeker, pos int64, hsize int, level byte) (*Entry, int64, error) {
	// level 0/1 的 header size 不含最前面那兩個位元組
	full := make([]byte, hsize+2)
	if _, err := r.Seek(pos, io.SeekStart); err != nil {
		return nil, 0, err
	}
	if _, err := io.ReadFull(r, full); err != nil {
		return nil, 0, fmt.Errorf(i18n.T("標頭不完整: %w"), err)
	}
	e := &Entry{
		Method:   string(full[2:7]),
		Packed:   int64(binary.LittleEndian.Uint32(full[7:11])),
		Original: int64(binary.LittleEndian.Uint32(full[11:15])),
		ModTime:  dosTime(binary.LittleEndian.Uint32(full[15:19])),
	}
	nameLen := int(full[21])
	if 22+nameLen+2 > len(full) {
		return nil, 0, fmt.Errorf(i18n.T("標頭裡的檔名長度不合理"))
	}
	e.Name = normalizeName(string(full[22 : 22+nameLen]))
	e.CRC = binary.LittleEndian.Uint16(full[22+nameLen : 24+nameLen])

	dataAt := pos + int64(hsize) + 2
	if level == 1 {
		// level 1 的 packed size 含後面的擴充標頭,要一路走完才知道
		// 資料真正從哪裡開始。擴充標頭裡也可能帶著完整的路徑。
		//
		// [雷] **每個擴充標頭的長度欄位存在前一個標頭的尾巴**,
		// 不是自己的開頭。基本標頭最後兩個位元組就是第一個擴充標頭的
		// 長度,所以起點是 dataAt-2 而不是 dataAt。沒有擴充標頭的檔案
		// 那兩個位元組是 0,起點錯了也常常剛好讀到 0 而看起來正常 ——
		// 這種錯只有在真的有擴充標頭的檔案上才會現形。
		at := dataAt - 2
		dir := ""
		for {
			if _, err := r.Seek(at, io.SeekStart); err != nil {
				return nil, 0, err
			}
			var sz [2]byte
			if _, err := io.ReadFull(r, sz[:]); err != nil {
				break
			}
			n := int(binary.LittleEndian.Uint16(sz[:]))
			if n == 0 {
				at += 2
				break
			}
			if n < 3 {
				return nil, 0, fmt.Errorf(i18n.T("擴充標頭長度不合理: %d"), n)
			}
			ext := make([]byte, n-2)
			if _, err := io.ReadFull(r, ext); err != nil {
				break
			}
			applyExt(e, &dir, ext)
			e.Packed -= int64(n)
			at += int64(n)
		}
		if dir != "" {
			e.Name = dir + e.Name
		}
		dataAt = at
	}
	// 目錄項有兩種寫法:壓縮法寫 -lhd-,或是基本標頭的檔名留空、
	// 只在擴充標頭裡給目錄名(Amiga 的 LHA 是後者)。兩種都要認。
	if e.Method == "-lhd-" || strings.HasSuffix(e.Name, "/") {
		e.IsDir = true
	}
	e.Offset = dataAt
	return e, dataAt + e.Packed, nil
}

func readHeaderLv2(r io.ReadSeeker, pos int64) (*Entry, int64, error) {
	if _, err := r.Seek(pos, io.SeekStart); err != nil {
		return nil, 0, err
	}
	base := make([]byte, 26)
	if _, err := io.ReadFull(r, base); err != nil {
		return nil, 0, fmt.Errorf(i18n.T("標頭不完整: %w"), err)
	}
	total := int(binary.LittleEndian.Uint16(base[0:2]))
	e := &Entry{
		Method:   string(base[2:7]),
		Packed:   int64(binary.LittleEndian.Uint32(base[7:11])),
		Original: int64(binary.LittleEndian.Uint32(base[11:15])),
		// level 2 用的是 Unix 時間,不是 MS-DOS 那套位元欄位
		ModTime: time.Unix(int64(binary.LittleEndian.Uint32(base[15:19])), 0),
		CRC:     binary.LittleEndian.Uint16(base[21:23]),
	}

	at := pos + 24 // 擴充標頭從第 24 個位元組起(base[24:26] 是第一個的大小)
	dir, name := "", ""
	for {
		if _, err := r.Seek(at, io.SeekStart); err != nil {
			return nil, 0, err
		}
		var sz [2]byte
		if _, err := io.ReadFull(r, sz[:]); err != nil {
			break
		}
		n := int(binary.LittleEndian.Uint16(sz[:]))
		if n < 3 {
			at += 2
			break
		}
		ext := make([]byte, n-2)
		if _, err := io.ReadFull(r, ext); err != nil {
			break
		}
		if ext[0] == 0x01 {
			name = string(ext[1:])
		} else {
			applyExt(e, &dir, ext)
		}
		at += int64(n)
		if at > pos+int64(total) {
			break
		}
	}
	if name != "" {
		e.Name = normalizeName(name)
	}
	if dir != "" {
		e.Name = dir + e.Name
	}
	if e.Method == "-lhd-" || e.Name == "" || strings.HasSuffix(e.Name, "/") {
		e.IsDir = true
	}
	e.Offset = pos + int64(total)
	return e, e.Offset + e.Packed, nil
}

// applyExt 處理一個擴充標頭。
func applyExt(e *Entry, dir *string, ext []byte) {
	if len(ext) == 0 {
		return
	}
	switch ext[0] {
	case 0x01: // 檔名
		e.Name = normalizeName(string(ext[1:]))
	case 0x02: // 目錄名。分隔字元是 0xFF,不是 / 也不是 \
		d := strings.ReplaceAll(string(ext[1:]), "\xff", "/")
		if d != "" && !strings.HasSuffix(d, "/") {
			d += "/"
		}
		*dir = d
	case 0x54: // Unix 時間
		if len(ext) >= 5 {
			e.ModTime = time.Unix(int64(binary.LittleEndian.Uint32(ext[1:5])), 0)
		}
	}
}

// normalizeName 把路徑分隔字元統一成 /。
// LHA 在不同系統上用 \ 或 0xFF,兩種都會遇到。
func normalizeName(s string) string {
	s = strings.ReplaceAll(s, "\xff", "/")
	s = strings.ReplaceAll(s, "\\", "/")
	return strings.TrimPrefix(s, "/")
}

// dosTime 把 MS-DOS 的日期時間欄位轉成 time.Time(本地時區,原版就是這樣存的)。
func dosTime(v uint32) time.Time {
	t := uint16(v)
	d := uint16(v >> 16)
	return time.Date(
		1980+int(d>>9), time.Month((d>>5)&0xF), int(d&0x1F),
		int(t>>11), int((t>>5)&0x3F), int(t&0x1F)*2, 0, time.Local)
}
