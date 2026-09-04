package arj

import (
	"bytes"
	"fmt"
	"github.com/wicanr2/wincv-remake/internal/i18n"

	"github.com/wicanr2/wincv-remake/internal/archive/lzh"
)

// fdicSiz 是方法 4 的視窗大小。參考實作(arj 3.10 decode.c)寫死 32768,
// 與方法 1-3 的 26624 不同,別混。
const fdicSiz = 32768

// decodeM4 解方法 4。
//
// 沒有 Huffman:長度與距離都用「前綴的 1 的個數決定欄位寬度」這種
// 固定編碼。長度欄位 0-7 位,距離欄位 9-13 位。
func decodeM4(body []byte, origSize int64) ([]byte, error) {
	b := lzh.NewBits(bytes.NewReader(body))

	// decodeLen:數前面有幾個 1(最多 7 個),再讀同樣寬度的欄位。
	// 第一個位元就是 0 的話結果是 0,代表「接下來是一個原樣的位元組」。
	decodeLen := func() int {
		plus, pwr := 0, 1
		width := 0
		c := 0
		for width = 0; width < 7; width++ {
			if b.Get(1) == 0 {
				break
			}
			plus += pwr
			pwr <<= 1
		}
		if width != 0 {
			c = int(b.Get(uint(width)))
		}
		return c + plus
	}
	// decodePtr:同樣的做法,但寬度從 9 起跳、最多 13。
	decodePtr := func() int {
		plus, pwr := 0, 1<<9
		width := 9
		for ; width < 13; width++ {
			if b.Get(1) == 0 {
				break
			}
			plus += pwr
			pwr <<= 1
		}
		return int(b.Get(uint(width))) + plus
	}

	out := make([]byte, 0, origSize)
	for int64(len(out)) < origSize {
		c := decodeLen()
		if c == 0 {
			out = append(out, byte(b.Get(8)))
			continue
		}
		length := c - 1 + threshold
		dist := decodePtr()
		start := len(out) - dist - 1
		if start < 0 {
			return out, fmt.Errorf(i18n.T("距離 %d 超出目前的 %d 個位元組"), dist+1, len(out))
		}
		if dist >= fdicSiz {
			return out, fmt.Errorf(i18n.T("距離 %d 超過視窗 %d"), dist+1, fdicSiz)
		}
		for i := 0; i < length && int64(len(out)) < origSize; i++ {
			out = append(out, out[start+i])
		}
	}
	return out, nil
}
