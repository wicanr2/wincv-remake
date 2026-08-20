// Package cab 讀 Microsoft Cabinet(.cab)。
//
// 支援不壓縮與 MSZIP。LZX 與 Quantum 還沒做,遇到會明確回報,
// 不會拿錯的解法硬解出垃圾。
//
// CAB 的結構有兩層:CFFOLDER 是壓縮單位,CFFILE 只是「在第幾個 folder
// 的第幾個位元組起、多長」。所以要先把整個 folder 解開,再照 offset 切檔案。
package cab

import (
	"bytes"
	"compress/flate"
	"encoding/binary"
	"fmt"
	"io"
	"strings"
	"time"
)

// Compression 是 folder 的壓縮方式。
type Compression uint16

const (
	CompNone    Compression = 0
	CompMSZIP   Compression = 1
	CompQuantum Compression = 2
	CompLZX     Compression = 3
)

func (c Compression) String() string {
	switch c & 0x0F {
	case CompNone:
		return "不壓縮"
	case CompMSZIP:
		return "MSZIP"
	case CompQuantum:
		return "Quantum"
	case CompLZX:
		return "LZX"
	}
	return fmt.Sprintf("未知(%d)", uint16(c))
}

// File 是壓縮檔裡的一筆。
type File struct {
	Name    string
	Size    int64
	ModTime time.Time
	IsDir   bool
	Data    []byte
}

type folder struct {
	dataOffset uint32
	blocks     uint16
	comp       Compression
	out        []byte
}

// Read 讀出整份 cabinet。
//
// folder 一定要整份解開才能切出檔案,所以內容全部留在記憶體裡。
func Read(data []byte) ([]File, error) {
	if len(data) < 36 || string(data[0:4]) != "MSCF" {
		return nil, fmt.Errorf("不是 CAB 檔(magic 不對)")
	}
	le := binary.LittleEndian
	nFolders := int(le.Uint16(data[26:28]))
	nFiles := int(le.Uint16(data[28:30]))
	flags := le.Uint16(data[30:32])
	if flags&0x0004 != 0 {
		// 有 RESERVE 欄位時,每個 folder / block 的表頭多幾個位元組,
		// 位置全部要偏移。還沒遇到過真檔,先誠實回報而不是算錯。
		return nil, fmt.Errorf("這個 CAB 帶 RESERVE 欄位,還沒支援")
	}
	if flags&0x0001 != 0 || flags&0x0002 != 0 {
		return nil, fmt.Errorf("這是多片 cabinet 的一部分,還沒支援")
	}
	fileOffset := le.Uint32(data[16:20])

	folders := make([]folder, nFolders)
	at := 36
	for i := 0; i < nFolders; i++ {
		if at+8 > len(data) {
			return nil, fmt.Errorf("folder 表被截斷")
		}
		folders[i] = folder{
			dataOffset: le.Uint32(data[at : at+4]),
			blocks:     le.Uint16(data[at+4 : at+6]),
			comp:       Compression(le.Uint16(data[at+6 : at+8])),
		}
		at += 8
	}
	for i := range folders {
		out, err := decodeFolder(data, &folders[i])
		if err != nil {
			return nil, fmt.Errorf("folder %d(%s): %w", i, folders[i].comp, err)
		}
		folders[i].out = out
	}

	var files []File
	at = int(fileOffset)
	for i := 0; i < nFiles; i++ {
		if at+16 > len(data) {
			return nil, fmt.Errorf("檔案表被截斷")
		}
		size := le.Uint32(data[at : at+4])
		off := le.Uint32(data[at+4 : at+8])
		fidx := le.Uint16(data[at+8 : at+10])
		date := le.Uint16(data[at+10 : at+12])
		tm := le.Uint16(data[at+12 : at+14])
		attrs := le.Uint16(data[at+14 : at+16])
		at += 16
		end := at
		for end < len(data) && data[end] != 0 {
			end++
		}
		name := string(data[at:end])
		at = end + 1

		// 0x80 表示檔名是 UTF-8,否則是該系統的 ANSI 編碼。
		// CAB 的目錄分隔字元是 \,統一成 /。
		name = strings.ReplaceAll(name, "\\", "/")

		f := File{
			Name:    name,
			Size:    int64(size),
			ModTime: dosTime(date, tm),
			IsDir:   attrs&0x10 != 0,
		}
		if int(fidx) < len(folders) && !f.IsDir {
			out := folders[fidx].out
			if int(off)+int(size) > len(out) {
				return files, fmt.Errorf("%s: folder 解出來只有 %d 個位元組,取不到 [%d,%d)",
					name, len(out), off, off+size)
			}
			f.Data = out[off : off+size]
		}
		files = append(files, f)
	}
	return files, nil
}

func decodeFolder(data []byte, f *folder) ([]byte, error) {
	comp := f.comp & 0x0F
	if comp != CompNone && comp != CompMSZIP {
		return nil, fmt.Errorf("還不支援 %s", f.comp)
	}
	le := binary.LittleEndian
	at := int(f.dataOffset)
	var out []byte
	for b := 0; b < int(f.blocks); b++ {
		if at+8 > len(data) {
			return out, fmt.Errorf("第 %d 個區塊的表頭被截斷", b)
		}
		nComp := int(le.Uint16(data[at+4 : at+6]))
		nRaw := int(le.Uint16(data[at+6 : at+8]))
		at += 8
		if at+nComp > len(data) {
			return out, fmt.Errorf("第 %d 個區塊的資料被截斷", b)
		}
		blk := data[at : at+nComp]
		at += nComp

		if comp == CompNone {
			out = append(out, blk...)
			continue
		}
		if len(blk) < 2 || blk[0] != 'C' || blk[1] != 'K' {
			return out, fmt.Errorf("第 %d 個區塊少了 MSZIP 的 CK 標記", b)
		}
		// [雷] 每個區塊是獨立的 deflate 串流,但 LZ77 的視窗**接續前一個
		// 區塊的輸出**。不把前 32 KB 當字典餵進去,前 32 KB 會正常,
		// 之後才開始壞 —— 小檔案測不出來。
		dict := out
		if len(dict) > 32768 {
			dict = dict[len(dict)-32768:]
		}
		zr := flate.NewReaderDict(bytes.NewReader(blk[2:]), dict)
		raw, err := io.ReadAll(zr)
		zr.Close()
		if err != nil && len(raw) != nRaw {
			return out, fmt.Errorf("第 %d 個區塊解不開: %w", b, err)
		}
		out = append(out, raw...)
	}
	return out, nil
}

func dosTime(date, tm uint16) time.Time {
	return time.Date(
		1980+int(date>>9), time.Month((date>>5)&0xF), int(date&0x1F),
		int(tm>>11), int((tm>>5)&0x3F), int(tm&0x1F)*2, 0, time.Local)
}
