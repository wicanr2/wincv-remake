package lzh

import (
	"fmt"
	"io"

)



// dicBits 是各壓縮法的滑動視窗大小(以位元數表示)。
var dicBits = map[string]uint{
	"-lh4-": 12,
	"-lh5-": 13,
	"-lh6-": 15,
	"-lh7-": 16,
}

// Supported 回傳這個壓縮法解不解得開。
func Supported(method string) bool {
	switch method {
	case "-lh0-", "-lhd-", "-lz4-":
		return true
	}
	_, ok := dicBits[method]
	return ok
}

// Decode 把 src 解成 origSize 個位元組。
//
// LZSS 的視窗就是輸出本身,所以整份留在記憶體裡最直接。壓縮檔裡
// 單一成員大到裝不下的情況,原版那個年代不存在。
func Decode(src io.Reader, method string, origSize int64) ([]byte, error) {
	switch method {
	case "-lh0-", "-lz4-": // 不壓縮
		out := make([]byte, origSize)
		if _, err := io.ReadFull(src, out); err != nil && err != io.ErrUnexpectedEOF {
			return nil, err
		}
		return out, nil
	case "-lhd-": // 目錄,沒有內容
		return nil, nil
	}

	bits, ok := dicBits[method]
	if !ok {
		return nil, fmt.Errorf("還不支援 %s", method)
	}
	return DecodeBits(src, bits, origSize)
}

// DecodeBits 是不看壓縮法名稱、直接指定視窗位元數的版本。
//
// ARJ 的方法 1-3 用的是同一套編碼(unarj 本來就是從 LHA 衍生的),
// 視窗 13 位,參數與 -lh5- 相同,所以共用這個解碼器而不是抄一份。
func DecodeBits(src io.Reader, bits uint, origSize int64) ([]byte, error) {
	if origSize < 0 || origSize > 1<<32 {
		return nil, fmt.Errorf("原始大小不合理: %d", origSize)
	}

	h := &huffman{np: int(bits) + 1, pbit: 4}
	if bits >= 15 { // lh6(15 位)與 lh7(16 位)都是 5,不是只有 lh7
		h.pbit = 5
	}
	b := newBitReader(src)

	out := make([]byte, 0, origSize)
	for int64(len(out)) < origSize {
		c, err := h.decodeC(b)
		if err != nil {
			return out, fmt.Errorf("第 %d 區塊,已輸出 %d/%d: %w", h.Blocks(), len(out), origSize, err)
		}
		if c <= 0xFF {
			out = append(out, byte(c))
			continue
		}
		length := c - (256 - threshold)
		dist, err := h.decodeP(b)
		if err != nil {
			return out, err
		}
		start := len(out) - dist - 1
		if start < 0 {
			return out, fmt.Errorf("距離超出視窗(%d,目前只有 %d 個位元組)", dist+1, len(out))
		}
		for i := 0; i < length && int64(len(out)) < origSize; i++ {
			out = append(out, out[start+i])
		}
	}
	return out, nil
}
