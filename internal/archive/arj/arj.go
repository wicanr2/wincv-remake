// Package arj 讀 ARJ 壓縮檔。
//
// 方法 0 是不壓縮,方法 1-3 用的是與 LHA -lh5- 同一套編碼
// (unarj 本來就是從 LHA 衍生的),所以直接借 internal/archive/lzh 的解碼器,
// 不再抄一份。方法 4 是另一套(LZ77 + 固定編碼),另外寫在 m4.go。
package arj

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"github.com/wicanr2/wincv-remake/internal/i18n"
	"strings"
	"time"

	"github.com/wicanr2/wincv-remake/internal/archive/lzh"
)

// dicBits 決定距離表的大小:np = dicBits+1、pbit = 5。
//
// [雷] ARJ 的方法 1-3 **不是** -lh5- 的參數。Huffman 的部分
// (NC / NT / CBIT / TBIT)一模一樣,但 NP = MAXDICBIT+1 = 17、PBIT = 5,
// 而 -lh5- 是 14 / 4。照 lh5 去解,碼長表一開始就對不上。
// 在 lzh.DecodeBits 的算法裡,16 剛好給出 np=17、pbit=5。
// (參考:arj 3.10 defines.h 的 NP / PBIT;字典是 DICSIZ=26624,
// 不是 2 的冪,所以「視窗位元數」在這裡只是距離表的參數。)
const dicBits = 16

// threshold 是最短的比對長度。LHA 與 ARJ 都是 3。
const threshold = 3

// File 是壓縮檔裡的一筆。
type File struct {
	Name    string
	Size    int64
	ModTime time.Time
	IsDir   bool
	Method  byte
	Data    []byte
}

const (
	magic0 = 0x60
	magic1 = 0xEA

	flagGarbled = 0x01 // 有密碼
	flagVolume  = 0x04
	flagExtFile = 0x08 // 跨片的續檔
	flagPathSym = 0x10 // 路徑用 / 而不是 \

	typeDir = 3
)

// Read 讀出整份壓縮檔。
func Read(data []byte) ([]File, error) {
	at, _, err := readBlock(data, 0) // 主標頭
	if err != nil {
		return nil, err
	}

	var out []File
	for at < len(data) {
		next, h, err := readBlock(data, at)
		if err != nil {
			return out, err
		}
		if h == nil { // 長度 0 的標頭 = 檔尾
			break
		}
		f := File{
			Name:    h.name,
			Size:    h.origSize,
			ModTime: h.modTime,
			Method:  h.method,
			IsDir:   h.fileType == typeDir,
		}
		if h.flags&flagGarbled != 0 {
			return out, fmt.Errorf(i18n.T("%s: 有密碼,還不支援"), h.name)
		}
		if h.flags&flagExtFile != 0 {
			return out, fmt.Errorf(i18n.T("%s: 是跨片壓縮檔的續檔,還不支援"), h.name)
		}
		if !f.IsDir {
			end := next + int(h.compSize)
			if end > len(data) {
				return out, fmt.Errorf(i18n.T("%s: 資料被截斷"), h.name)
			}
			body, derr := decode(data[next:end], h.method, h.origSize)
			if derr != nil {
				return out, fmt.Errorf("%s: %w", h.name, derr)
			}
			f.Data = body
		}
		out = append(out, f)
		at = next + int(h.compSize)
	}
	return out, nil
}

type header struct {
	flags    byte
	method   byte
	fileType byte
	modTime  time.Time
	compSize int64
	origSize int64
	name     string
}

// readBlock 讀一個標頭區塊,回傳資料的起點。
//
// 版面:magic(2) + 基本標頭長度(2) + 基本標頭 + CRC(4) +
// 一連串擴充標頭(各自是 長度(2) + 內容 + CRC(4),長度 0 表示結束)。
func readBlock(data []byte, at int) (int, *header, error) {
	if at+4 > len(data) {
		return 0, nil, fmt.Errorf(i18n.T("在 %d 讀不到標頭"), at)
	}
	if data[at] != magic0 || data[at+1] != magic1 {
		return 0, nil, fmt.Errorf(i18n.T("在 %d 找不到 ARJ 的標記"), at)
	}
	le := binary.LittleEndian
	basic := int(le.Uint16(data[at+2 : at+4]))
	if basic == 0 {
		return at + 4, nil, nil // 檔尾
	}
	if basic > 2600 || at+4+basic+4 > len(data) {
		return 0, nil, fmt.Errorf(i18n.T("標頭長度 %d 不合理"), basic)
	}
	b := data[at+4 : at+4+basic]
	if len(b) < 30 {
		return 0, nil, fmt.Errorf(i18n.T("標頭太短(%d)"), len(b))
	}
	firstHdr := int(b[0])
	h := &header{
		flags:    b[4],
		method:   b[5],
		fileType: b[6],
		modTime:  dosTime(le.Uint32(b[8:12])),
		compSize: int64(le.Uint32(b[12:16])),
		origSize: int64(le.Uint32(b[16:20])),
	}
	// 檔名與註解接在固定欄位後面,各自以 0 結尾。
	// firstHdr 隨版本不同(通常 30 或 34),所以要照欄位值走不能寫死。
	p := firstHdr
	if p < 30 || p > len(b) {
		p = 30
	}
	end := p
	for end < len(b) && b[end] != 0 {
		end++
	}
	h.name = string(b[p:end])
	if h.flags&flagPathSym != 0 {
		h.name = strings.ReplaceAll(h.name, "/", "\x00")
	}
	h.name = strings.ReplaceAll(h.name, "\\", "/")
	h.name = strings.ReplaceAll(h.name, "\x00", "/")
	h.name = strings.TrimPrefix(h.name, "/")

	next := at + 4 + basic + 4 // 基本標頭 + 它的 CRC
	for {
		if next+2 > len(data) {
			return 0, nil, fmt.Errorf(i18n.T("擴充標頭被截斷"))
		}
		n := int(le.Uint16(data[next : next+2]))
		next += 2
		if n == 0 {
			break
		}
		next += n + 4 // 內容 + CRC
		if next > len(data) {
			return 0, nil, fmt.Errorf(i18n.T("擴充標頭被截斷"))
		}
	}
	return next, h, nil
}

func decode(body []byte, method byte, origSize int64) ([]byte, error) {
	switch method {
	case 0:
		out := make([]byte, origSize)
		n := copy(out, body)
		if int64(n) < origSize {
			return out[:n], fmt.Errorf(i18n.T("不壓縮的資料只有 %d 個位元組,期望 %d"), n, origSize)
		}
		return out, nil
	case 1, 2, 3:
		return lzh.DecodeBits(bytes.NewReader(body), dicBits, origSize)
	case 4:
		return decodeM4(body, origSize)
	}
	return nil, fmt.Errorf(i18n.T("還不支援壓縮方法 %d"), method)
}

func dosTime(v uint32) time.Time {
	t := uint16(v)
	d := uint16(v >> 16)
	return time.Date(
		1980+int(d>>9), time.Month((d>>5)&0xF), int(d&0x1F),
		int(t>>11), int((t>>5)&0x3F), int(t&0x1F)*2, 0, time.Local)
}
