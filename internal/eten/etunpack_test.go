package eten

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"os"
	"testing"
)

func TestUnpackETUNPACKRoundTrip(t *testing.T) {
	want := make([]byte, etunpackGlyphs*etunpackStride)
	for i := range want {
		want[i] = byte(i*37 + i/17)
	}
	got, err := UnpackETUNPACK(testETUNPACK(want, 0, 22))
	if err != nil {
		t.Fatalf("UnpackETUNPACK: %v", err)
	}
	if !bytes.Equal(got, want) {
		t.Fatal("解壓與 XOR 還原後的字模不相同")
	}
}

func TestUnpackETUNPACKRejectsInvalidInput(t *testing.T) {
	if _, err := UnpackETUNPACK([]byte(etunpackMagic)); err == nil {
		t.Fatal("截斷檔案不應被接受")
	}
	bad := testETUNPACK(make([]byte, etunpackGlyphs*etunpackStride), 0, 22)
	bad[0] = 'X'
	if _, err := UnpackETUNPACK(bad); err == nil {
		t.Fatal("錯誤魔術字不應被接受")
	}
}

// 此收據鎖定使用者提供的 ETEN 3.53 FILES/STD.24M。外部字型不入版控，
// 所以只有在測試環境明確提供路徑時執行。
func TestUnpackETUNPACKET353SReceipt(t *testing.T) {
	path := os.Getenv("WINCV_ETEN24_STD")
	if path == "" {
		t.Skip("未提供 WINCV_ETEN24_STD，跳過外部 ETEN 3.53 收據")
	}
	in, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := sha256.Sum256(in); got != [32]byte{0x34, 0x7a, 0xe2, 0x65, 0x58, 0x07, 0xfc, 0x25, 0x0a, 0x18, 0x67, 0x3e, 0x66, 0x34, 0xa3, 0x63, 0xdf, 0xba, 0x7f, 0xee, 0xe6, 0xc3, 0x35, 0x5b, 0x88, 0x6a, 0x08, 0x10, 0xd2, 0xd9, 0xc0, 0x30} {
		t.Fatalf("不是已驗證的 ETEN 3.53 STD.24M：%x", got)
	}
	out, err := UnpackETUNPACK(in)
	if err != nil {
		t.Fatal(err)
	}
	if got := sha256.Sum256(out); got != [32]byte{0x65, 0xaa, 0xfa, 0x2a, 0x59, 0xba, 0xa7, 0x3e, 0x83, 0x5d, 0xbf, 0xeb, 0xee, 0xaf, 0x6c, 0x99, 0xae, 0x85, 0xdb, 0x52, 0xb3, 0xbc, 0x8e, 0x18, 0x4c, 0xbc, 0x1d, 0x3f, 0x7b, 0xd3, 0x64, 0xe6} {
		t.Fatalf("解開的 STD.24M 雜湊不符：%x", got)
	}
}

// testETUNPACK 只用於測試，產生固定 8-bit Huffman 碼的合法 ETUNPACK
// 流。它不包含任何第三方字形資料，卻能覆蓋三樹選擇、位元序與 XOR 還原。
func testETUNPACK(want []byte, start, end int) []byte {
	packed := append([]byte(nil), want...)
	for glyph := etunpackGlyphs - 1; glyph >= 0; glyph-- {
		g := packed[glyph*etunpackStride : (glyph+1)*etunpackStride]
		for i := end*3 - 1; i >= start*3; i-- {
			g[i+3] ^= g[i]
		}
	}

	var bits testBitWriter
	for range 3 {
		for symbol := range 256 {
			bits.write(8, 5)
			bits.write(uint32(symbol), 8)
		}
	}
	for _, b := range packed {
		bits.write(uint32(b), 8)
	}
	out := make([]byte, 0x40, 0x40+len(bits.data))
	copy(out, etunpackMagic)
	binary.LittleEndian.PutUint32(out[0x10:0x14], 0x20)
	binary.LittleEndian.PutUint32(out[0x14:0x18], 0x40)
	copy(out[0x24:0x30], "stdfont.24")
	out[0x32], out[0x33] = byte(start), byte(end)
	return append(out, bits.data...)
}

type testBitWriter struct {
	data []byte
	n    int
}

func (w *testBitWriter) write(v uint32, n int) {
	for shift := n - 1; shift >= 0; shift-- {
		if w.n%8 == 0 {
			w.data = append(w.data, 0)
		}
		if v&(1<<uint(shift)) != 0 {
			w.data[len(w.data)-1] |= 0x80 >> uint(w.n%8)
		}
		w.n++
	}
}
