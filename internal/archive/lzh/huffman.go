// Package lzh 解 LHA / LZH 壓縮檔。
//
// 沒有找到可用的純 Go 實作,所以照格式自己寫。演算法是 LHA 原始碼
// (huf.c / decode.c)那一套:LZSS 滑動視窗 + 靜態 Huffman,每個
// 區塊自帶碼長表。
//
// 支援 -lh0-(不壓縮)、-lhd-(目錄)、-lh4-/-lh5-/-lh6-/-lh7-。
// -lh1-/-lz5- 用的是另一套(動態 Huffman / LArc),還沒做。
package lzh

import (
	"fmt"
	"io"
)

const (
	maxMatch  = 256
	threshold = 3
	// nc 是字碼表的大小:256 個位元組值 + (maxMatch-threshold+1) 種比對長度。
	// 定義是 UCHAR_MAX + maxMatch + 2 - threshold = 510。
	//
	// [雷] 少算一個(509)只有在某個區塊剛好用滿全部字碼時才會現形 ——
	// 讀碼長表的迴圈以 nc 為上限,少一格就漏掉最後一個字碼,
	// Huffman 樹的權重和變成 65024 而不是 65536。小檔案全部正常,
	// 只有大到需要完整字碼表的成員會壞。
	nc        = 255 + maxMatch + 2 - threshold // 510
	nt        = 16 + 3                         // 19
	cbit      = 9
	tbit      = 5
	npt       = 0x80
)

// bitReader 是 LHA 的位元讀取器。
//
// 它一次看 16 個位元(bitbuf),因為碼長最長 16。讀到檔尾之後補 0 ——
// 最後一個碼可能跨過實體檔尾,原始實作也是這樣處理。
type bitReader struct {
	r         io.Reader
	buf       [4096]byte
	n, i      int
	eof       bool
	bitbuf    uint32
	subbitbuf uint32
	bitcount  uint
}

func newBitReader(r io.Reader) *bitReader {
	b := &bitReader{r: r}
	b.fill(2 * 8)
	return b
}

func (b *bitReader) byteAt() uint32 {
	if b.i >= b.n {
		if b.eof {
			return 0
		}
		n, err := b.r.Read(b.buf[:])
		if n <= 0 {
			b.eof = true
			if err == nil || err == io.EOF {
				return 0
			}
			return 0
		}
		b.n, b.i = n, 0
	}
	c := uint32(b.buf[b.i])
	b.i++
	return c
}

func (b *bitReader) fill(n uint) {
	for n > b.bitcount {
		n -= b.bitcount
		b.bitbuf = (b.bitbuf << b.bitcount) & 0xFFFF
		if b.bitcount > 0 {
			b.bitbuf |= b.subbitbuf >> (8 - b.bitcount)
		}
		b.subbitbuf = b.byteAt()
		b.bitcount = 8
	}
	b.bitcount -= n
	b.bitbuf = (b.bitbuf << n) & 0xFFFF
	if n > 0 {
		b.bitbuf |= (b.subbitbuf >> (8 - n)) & ((1 << n) - 1)
	}
	b.subbitbuf = (b.subbitbuf << n) & 0xFF
}

func (b *bitReader) peek(n uint) uint32 { return b.bitbuf >> (16 - n) }

func (b *bitReader) get(n uint) uint32 {
	if n == 0 {
		return 0
	}
	v := b.peek(n)
	b.fill(n)
	return v
}

// huffman 是一個區塊的解碼表。
//
// table 是「前 tablebits 位直接查」的快表;碼長超過 tablebits 的
// 走 left/right 這棵二元樹接下去。這是 LHA 原始的做法,照抄是因為
// 表的建法與碼的指派順序綁在一起,換一種建法就對不上原檔。
type huffman struct {
	left, right [2 * nc]uint16
	cLen        [nc]byte
	ptLen       [npt]byte
	cTable      [4096]uint16
	ptTable     [256]uint16
	blocksize   uint32
	blocks      int
	np          int
	pbit        uint
}

func makeTable(h *huffman, nchar int, bitlen []byte, tablebits uint, table []uint16, avail *int) error {
	var count [17]uint32
	var weight [17]uint32
	var start [18]uint32

	for i := 0; i < nchar; i++ {
		count[bitlen[i]]++
	}
	count[0] = 0
	for i := uint(1); i <= 16; i++ {
		start[i+1] = start[i] + (count[i] << (16 - i))
	}
	if start[17] != 1<<16 {
		return fmt.Errorf("碼長表不合法(不是完整的 Huffman 樹,權重和 %d)", start[17])
	}

	jutbits := 16 - tablebits
	var i uint
	for i = 1; i <= tablebits; i++ {
		start[i] >>= jutbits
		weight[i] = 1 << (tablebits - i)
	}
	for ; i <= 16; i++ {
		weight[i] = 1 << (16 - i)
	}

	if k := start[tablebits+1] >> jutbits; k != 1<<tablebits {
		for j := k; j < 1<<tablebits; j++ {
			table[j] = 0
		}
	}

	mask := uint32(1) << (15 - tablebits)
	for ch := 0; ch < nchar; ch++ {
		l := uint(bitlen[ch])
		if l == 0 {
			continue
		}
		next := start[l] + weight[l]
		if l <= tablebits {
			if next > 1<<tablebits {
				return fmt.Errorf("碼長表越界")
			}
			for j := start[l]; j < next; j++ {
				table[j] = uint16(ch)
			}
		} else {
			k := start[l]
			idx := k >> jutbits
			if int(idx) >= len(table) {
				return fmt.Errorf("碼長表越界")
			}
			p := &table[idx]
			for j := l - tablebits; j != 0; j-- {
				if *p == 0 {
					h.left[*avail], h.right[*avail] = 0, 0
					*p = uint16(*avail)
					*avail++
					if *avail >= 2*nc {
						return fmt.Errorf("碼長表節點用盡")
					}
				}
				if k&mask != 0 {
					p = &h.right[*p]
				} else {
					p = &h.left[*p]
				}
				k <<= 1
			}
			*p = uint16(ch)
		}
		start[l] = next
	}
	return nil
}

// readPtLen 讀「碼長表的碼長表」。iSpecial >= 0 時,讀到第 iSpecial 筆
// 之後有一個 2 位的欄位表示要再補幾個 0 —— 這是格式裡的特例,不是筆誤。
func (h *huffman) readPtLen(b *bitReader, nn int, nbit uint, iSpecial int) error {
	n := int(b.get(nbit))
	if n == 0 {
		c := b.get(nbit)
		for i := 0; i < nn; i++ {
			h.ptLen[i] = 0
		}
		for i := range h.ptTable {
			h.ptTable[i] = uint16(c)
		}
		return nil
	}
	i := 0
	for i < n && i < npt {
		c := b.peek(3)
		if c != 7 {
			b.fill(3)
		} else {
			mask := uint32(1) << (16 - 4)
			for mask&b.bitbuf != 0 {
				mask >>= 1
				c++
				if c > 16 {
					return fmt.Errorf("碼長超過 16")
				}
			}
			b.fill(uint(c - 3))
		}
		h.ptLen[i] = byte(c)
		i++
		if i == iSpecial {
			c := int(b.get(2))
			for c > 0 && i < nn {
				h.ptLen[i] = 0
				i++
				c--
			}
		}
	}
	for i < nn {
		h.ptLen[i] = 0
		i++
	}
	avail := nn
	if err := makeTable(h, nn, h.ptLen[:], 8, h.ptTable[:], &avail); err != nil {
		return fmt.Errorf("pt(nn=%d,n=%d): %w", nn, n, err)
	}
	return nil
}

func (h *huffman) readCLen(b *bitReader) error {
	n := int(b.get(cbit))
	if n == 0 {
		c := b.get(cbit)
		for i := 0; i < nc; i++ {
			h.cLen[i] = 0
		}
		for i := range h.cTable {
			h.cTable[i] = uint16(c)
		}
		return nil
	}
	i := 0
	for i < n && i < nc {
		c := int(h.ptTable[b.peek(8)])
		if c >= nt {
			mask := uint32(1) << (16 - 9)
			for c >= nt {
				if b.bitbuf&mask != 0 {
					c = int(h.right[c])
				} else {
					c = int(h.left[c])
				}
				mask >>= 1
				if mask == 0 {
					return fmt.Errorf("碼長表解碼卡住")
				}
			}
		}
		b.fill(uint(h.ptLen[c]))
		if c <= 2 {
			switch c {
			case 0:
				c = 1
			case 1:
				c = int(b.get(4)) + 3
			default:
				c = int(b.get(cbit)) + 20
			}
			for c > 0 && i < nc {
				h.cLen[i] = 0
				i++
				c--
			}
		} else {
			h.cLen[i] = byte(c - 2)
			i++
		}
	}
	for i < nc {
		h.cLen[i] = 0
		i++
	}
	avail := nc
	if err := makeTable(h, nc, h.cLen[:], 12, h.cTable[:], &avail); err != nil {
		return fmt.Errorf("c(n=%d): %w", n, err)
	}
	return nil
}

// Blocks 回傳已經讀過幾個區塊。
func (h *huffman) Blocks() int { return h.blocks }

func (h *huffman) decodeC(b *bitReader) (int, error) {
	if h.blocksize == 0 {
		h.blocksize = b.get(16)
		h.blocks++
		if err := h.readPtLen(b, nt, tbit, 3); err != nil {
			return 0, err
		}
		if err := h.readCLen(b); err != nil {
			return 0, err
		}
		if err := h.readPtLen(b, h.np, h.pbit, -1); err != nil {
			return 0, err
		}
	}
	h.blocksize--
	j := int(h.cTable[b.peek(12)])
	if j >= nc {
		mask := uint32(1) << (16 - 13)
		for j >= nc {
			if b.bitbuf&mask != 0 {
				j = int(h.right[j])
			} else {
				j = int(h.left[j])
			}
			mask >>= 1
			if mask == 0 {
				return 0, fmt.Errorf("字碼解碼卡住")
			}
		}
	}
	b.fill(uint(h.cLen[j]))
	return j, nil
}

func (h *huffman) decodeP(b *bitReader) (int, error) {
	j := int(h.ptTable[b.peek(8)])
	if j >= h.np {
		mask := uint32(1) << (16 - 9)
		for j >= h.np {
			if b.bitbuf&mask != 0 {
				j = int(h.right[j])
			} else {
				j = int(h.left[j])
			}
			mask >>= 1
			if mask == 0 {
				return 0, fmt.Errorf("距離解碼卡住")
			}
		}
	}
	b.fill(uint(h.ptLen[j]))
	if j != 0 {
		j = (1 << (j - 1)) + int(b.get(uint(j-1)))
	}
	return j, nil
}
