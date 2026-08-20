// Package ace 解 ACE 壓縮檔(WinACE)。
//
// 原版 WinCV 自己不解 ACE,是載入 WinACE 原廠的 unace.dll / unacev2.dll,
// image 裡只有綁定層。這裡照公開規格自己寫:
//
//	規格   Marcel Lemke,〈Technical information of the archiver ACE v1.2〉(1998)
//	參照   droe/acefile(BSD,純 Python),用來對答案
//	測資   droe/acefile-testdata
//
// 支援 stored、ACE 1.0 的 LZ77、以及 ACE 2.0 的 blocked 模式底下的
// LZ77 / LZ77_DELTA / LZ77_EXE。SOUND 與 PIC 這兩種模式還沒做,
// 遇到會明確回報而不是解出垃圾。加密與跨片壓縮檔同樣先擋下。
package ace

import (
	"encoding/binary"
	"fmt"
)

// bitStream 是 ACE 的位元讀取器。
//
// [雷] 位元序很特別:資料先以**小端序**每 4 個位元組讀成一個 uint32,
// 再從那個 uint32 裡**由高位往低位**取位元。當成單純的 MSB-first
// 位元流去讀,每 4 個位元組就會錯一次序。
type bitStream struct {
	words []uint32
	pos   int // 以位元計
	nbits int
}

func newBitStream(data []byte) *bitStream {
	// 長度補到 4 的倍數:規格要求壓縮資料以 32 位元為單位,
	// 最後不足的部分補 0。
	n := (len(data) + 3) / 4
	w := make([]uint32, n, n+1)
	for i := 0; i < n; i++ {
		var b [4]byte
		copy(b[:], data[i*4:min(i*4+4, len(data))])
		w[i] = binary.LittleEndian.Uint32(b[:])
	}
	return &bitStream{words: w, nbits: n * 32}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// getBits 從一個 32 位元字裡取 length 個位元,start 是自最高位起算的位移。
func getBits(v uint32, start, length uint) uint32 {
	if length == 0 {
		return 0
	}
	mask := (uint32(0xFFFFFFFF) << (32 - length)) >> start
	return (v & mask) >> (32 - length - start)
}

// peek 看接下來的 n 個位元但不前進。
//
// 允許看過檔尾最多 31 個位元,補 0 —— 最後一個碼可能跨過實體結尾,
// 參考實作也是這樣處理。
func (b *bitStream) peek(n uint) (uint32, error) {
	if n == 0 {
		return 0, nil
	}
	if b.pos+int(n) > b.nbits {
		if b.pos+int(n) > b.nbits+31 {
			return 0, fmt.Errorf("位元流讀過頭")
		}
		if len(b.words)*32 == b.nbits {
			b.words = append(b.words, 0)
		}
	}
	idx := b.pos / 32
	off := uint(b.pos % 32)
	got := n
	if 32-off < got {
		got = 32 - off
	}
	res := getBits(b.word(idx), off, got)
	for n-got >= 32 {
		res <<= 32
		res += b.word((b.pos + int(got)) / 32)
		got += 32
	}
	if n-got > 0 {
		res <<= n - got
		res += getBits(b.word((b.pos+int(got))/32), 0, n-got)
	}
	return res, nil
}

func (b *bitStream) word(i int) uint32 {
	if i < 0 || i >= len(b.words) {
		return 0
	}
	return b.words[i]
}

func (b *bitStream) skip(n uint) { b.pos += int(n) }

func (b *bitStream) read(n uint) (uint32, error) {
	v, err := b.peek(n)
	if err != nil {
		return 0, err
	}
	b.skip(n)
	return v, nil
}

// readKnownWidthUint 讀一個已知位元寬的無號整數。
// 最高位不寫進位元流,因為它一定是 1。
func (b *bitStream) readKnownWidthUint(bits uint) (uint32, error) {
	if bits < 2 {
		return uint32(bits), nil
	}
	bits--
	v, err := b.read(bits)
	if err != nil {
		return 0, err
	}
	return v + 1<<bits, nil
}
