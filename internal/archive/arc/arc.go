// Package arc 讀 ARC / PAK 壓縮檔。
//
// 這是 1980 年代的格式,沒有整體標頭:一個檔案就是一串「每筆各自帶標頭」
// 的記錄,方法為 0 的那一筆代表結束。
//
// 支援的壓縮方法:
//
//	1, 2  不壓縮(1 是舊版,標頭少一個欄位)
//	3     packed:0x90 當跳脫的 RLE
//	5, 6  crunched:固定 12 位的 LZW(6 再加一層 RLE)
//	8     Crunched:9-13 位的動態 LZW + RLE(ARC 5.x 的預設)
//	9     Squashed:9-13 位的動態 LZW,不加 RLE
//
// 方法 4(squeezed,CP/M SQ 的 Huffman)與 7 還沒做。造不出測試資料的
// 東西不寫進來 —— 沒驗過的解碼器比明講不支援更糟。
package arc

import (
	"encoding/binary"
	"fmt"
	"strings"
	"time"
)

// File 是壓縮檔裡的一筆。
type File struct {
	Name    string
	Size    int64
	ModTime time.Time
	Method  byte
	Data    []byte
}

// Read 讀出整份壓縮檔。
func Read(data []byte) ([]File, error) {
	at := 0
	// 自解壓縮檔在第一個 0x1A 之前最多有 3 個位元組。
	for at < 4 && at < len(data) && data[at] != 0x1A {
		at++
	}
	if at >= len(data) || data[at] != 0x1A {
		return nil, fmt.Errorf("不是 ARC 檔(找不到起始標記)")
	}

	var out []File
	for {
		if at+2 > len(data) || data[at] != 0x1A {
			if len(out) == 0 {
				return nil, fmt.Errorf("在 %d 讀不到記錄標頭", at)
			}
			return out, nil
		}
		method := data[at+1] & 0x7F
		sparkExtra := data[at+1]>>7 != 0
		if method == 0 {
			return out, nil // 檔尾
		}
		at += 2

		// 方法 1 的標頭少了「原始大小」那個欄位。
		fixed := 13 + 4 + 2 + 2 + 2 + 4
		if method == 1 {
			fixed -= 4
		}
		if at+fixed > len(data) {
			return out, fmt.Errorf("標頭被截斷")
		}
		raw := data[at : at+fixed]
		at += fixed

		le := binary.LittleEndian
		name := string(raw[:13])
		if i := strings.IndexByte(name, 0); i >= 0 {
			name = name[:i]
		}
		// 檔名的最高位是旗標位,不是字元的一部分。
		nb := []byte(name)
		for i := range nb {
			nb[i] &= 0x7F
		}
		name = strings.ToLower(string(nb))

		b := raw[13:]
		compSize := int64(le.Uint32(b[0:4]))
		date := le.Uint16(b[4:6])
		tm := le.Uint16(b[6:8])
		origSize := compSize
		if method != 1 {
			origSize = int64(le.Uint32(b[10:14]))
		}
		if sparkExtra {
			at += 12 // Spark 變體多帶 12 個位元組
		}

		if at+int(compSize) > len(data) {
			return out, fmt.Errorf("%s: 資料被截斷", name)
		}
		body := data[at : at+int(compSize)]
		at += int(compSize)

		content, err := decode(body, method, origSize)
		if err != nil {
			return out, fmt.Errorf("%s(方法 %d): %w", name, method, err)
		}
		out = append(out, File{
			Name:    name,
			Size:    origSize,
			ModTime: dosTime(date, tm),
			Method:  method,
			Data:    content,
		})
	}
}

func decode(body []byte, method byte, origSize int64) ([]byte, error) {
	switch method {
	case 1, 2:
		out := make([]byte, origSize)
		copy(out, body)
		return out, nil
	case 3:
		return unRLE(body, origSize), nil
	case 5:
		return lzwDynamic(body, 0, false, origSize)
	case 6:
		return lzwDynamic(body, 0, true, origSize)
	case 8:
		return lzwDynamic(body, 12, true, origSize)
	case 9:
		return lzwDynamic(body, 13, false, origSize)
	case 127:
		return lzwDynamic(body, 16, false, origSize)
	}
	return nil, fmt.Errorf("還不支援方法 %d", method)
}

func dosTime(date, tm uint16) time.Time {
	return time.Date(
		1980+int(date>>9), time.Month((date>>5)&0xF), int(date&0x1F),
		int(tm>>11), int((tm>>5)&0x3F), int(tm&0x1F)*2, 0, time.Local)
}
