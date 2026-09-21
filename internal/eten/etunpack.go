package eten

import (
	"encoding/binary"
	"fmt"

	"github.com/wicanr2/wincv-remake/internal/i18n"
)

const (
	etunpackMagic  = "ETUNPACK V1.00"
	Native24W      = 24
	Native24H      = 24
	etunpackGlyphs = 13094
	etunpackStride = Native24W / 8 * Native24H
)

// UnpackETUNPACK 解開倚天 ETUNPACK V1.00 的 24×24 漢字區。
//
// 格式證據：ETEN 3.53 的 FILES/ETUNPACK.EXE 與 STD.24M，輸入輸出分別
// 是 657096 與 942768 bytes。資料以三棵靜態 Huffman 樹壓縮，解碼後再做
// 列間 XOR 還原。此函式只接受完整且自洽的單一字庫資料，避免把截斷資料
// 靜默當成缺字。
func UnpackETUNPACK(in []byte) ([]byte, error) {
	if len(in) < 0x40 || string(in[:len(etunpackMagic)]) != etunpackMagic {
		return nil, fmt.Errorf(i18n.T("不是 ETUNPACK V1.00 字型"))
	}
	dir := int(binary.LittleEndian.Uint32(in[0x10:0x14]))
	stream := int(binary.LittleEndian.Uint32(in[0x14:0x18]))
	if dir < 0 || dir+0x14 > len(in) || stream < 0 || stream >= len(in) || dir >= stream {
		return nil, fmt.Errorf(i18n.T("ETUNPACK 目錄或碼流位移無效"))
	}
	start, end := int(in[dir+0x12]), int(in[dir+0x13])
	// i+3 必須仍在同一個 24×24 字模內；end=24 會越過最後一列。
	if start > end || end >= Native24H {
		return nil, fmt.Errorf(i18n.T("ETUNPACK XOR 列範圍無效: %d..%d"), start, end)
	}

	r := bitReader{data: in, bit: stream * 8}
	trees := make([]huffmanTree, 3)
	for i := range trees {
		var err error
		if trees[i], err = readHuffmanTree(&r); err != nil {
			return nil, fmt.Errorf(i18n.T("ETUNPACK Huffman 樹 %d 無效: %w"), i, err)
		}
	}

	out := make([]byte, etunpackGlyphs*etunpackStride)
	for i := range out {
		v, err := trees[i%len(trees)].decode(&r)
		if err != nil {
			return nil, fmt.Errorf(i18n.T("ETUNPACK 在輸出 byte %d 截斷或碼字無效: %w"), i, err)
		}
		out[i] = v
	}
	for glyph := 0; glyph < etunpackGlyphs; glyph++ {
		g := out[glyph*etunpackStride : (glyph+1)*etunpackStride]
		for i := start * 3; i < end*3; i++ {
			g[i+3] ^= g[i]
		}
	}
	return out, nil
}

type bitReader struct {
	data []byte
	bit  int
}

func (r *bitReader) read(n int) (uint32, error) {
	if n < 0 || r.bit+n > len(r.data)*8 {
		return 0, fmt.Errorf(i18n.T("碼流不足"))
	}
	var v uint32
	for range n {
		v = v<<1 | uint32((r.data[r.bit/8]>>(7-uint(r.bit%8)))&1)
		r.bit++
	}
	return v, nil
}

type huffmanCode struct {
	bits uint32
	len  uint8
}

type huffmanTree struct {
	codes map[huffmanCode]byte
	max   uint8
}

func readHuffmanTree(r *bitReader) (huffmanTree, error) {
	t := huffmanTree{codes: make(map[huffmanCode]byte, 256)}
	for symbol := range 256 {
		n, err := r.read(5)
		if err != nil {
			return huffmanTree{}, err
		}
		if n == 0 {
			return huffmanTree{}, fmt.Errorf(i18n.T("碼長不可為零"))
		}
		bits, err := r.read(int(n))
		if err != nil {
			return huffmanTree{}, err
		}
		code := huffmanCode{bits: bits, len: uint8(n)}
		if _, exists := t.codes[code]; exists {
			return huffmanTree{}, fmt.Errorf(i18n.T("重複碼字"))
		}
		t.codes[code] = byte(symbol)
		if code.len > t.max {
			t.max = code.len
		}
	}
	for a := range t.codes {
		for b := range t.codes {
			if a == b || a.len > b.len {
				continue
			}
			if b.bits>>uint(b.len-a.len) == a.bits {
				return huffmanTree{}, fmt.Errorf(i18n.T("碼字不是前綴自由"))
			}
		}
	}
	return t, nil
}

func (t huffmanTree) decode(r *bitReader) (byte, error) {
	var bits uint32
	for n := uint8(1); n <= t.max; n++ {
		bit, err := r.read(1)
		if err != nil {
			return 0, err
		}
		bits = bits<<1 | bit
		if v, ok := t.codes[huffmanCode{bits: bits, len: n}]; ok {
			return v, nil
		}
	}
	return 0, fmt.Errorf(i18n.T("找不到對應碼字"))
}
