// Package eten 讀倚天中文系統 (ETEN 3.53) 的原生點陣字庫,當作全形字的來源。
//
// 尺寸剛好對上 WinCV 隨附的半形字型:
//
//	cvga     8x15  ←→  STDFONT.15 / SPCFONT.15  16x15
//	cvga1224 12x24 ←→  STD.24M                  24x24  (ETUNPACK 壓縮,尚未支援)
//
// 這不是巧合 —— 兩者是同一個年代的中文環境規格。
//
// 檔案格式(裸格式,無檔頭):每列 (W+7)/8 bytes、MSB-first、由上而下,
// 每字 stride = rowBytes * H。
//
//	STDFONT.15  漢字區    16x15  stride 30  13094 字
//	SPCFONT.15  全形符號  16x15  stride 30    408 字
//
// [雷] STDFONT 從 A440(「一」)起,不含 A140-A3BF 的全形標點。
// 只載 STDFONT 的話,「，。！？「」（）《》」全部會變成缺字。
package eten

import (
	"fmt"
	"os"
	"sync"

	"github.com/wicanr2/wincv-remake/internal/fnt"
	"golang.org/x/text/encoding/traditionalchinese"
)

// Big5 分區索引的界線。公式與常數取自
// ~/.claude/knowledge-base/retro-cht/eten-bitmap-font/SKILL.md(已實測驗證)。
var (
	lastSpc    = rawIndex(0xA3, 0xBF) // 符號區尾 = 407
	baseA440   = rawIndex(0xA4, 0x40) // 漢字區起點
	lastCommon = rawIndex(0xC6, 0x7E) // 常用字尾
	baseC940   = rawIndex(0xC9, 0x40) // 次常用起點
)

// 各區的字模數與碼位數是否對得起來,決定線性索引能不能用:
//
//	符號區   raw 0..407        408 個碼位  SPCFONT.15  408 個字模  → 對齊
//	常用字   raw 471..5871    5401 個碼位  STDFONT 前段 5401 個     → 對齊
//	次常用   raw 6280..13972  7693 個碼位  STDFONT 後段 7693 個     → 對齊
//	符號補充 raw 5872..6279    408 個碼位  SPCFSUPP.15 只有 365 個  → **對不起來**
//
// 最後一列表示補充區的字模是「把有定義的碼位擠在一起」存的,中間有洞。
// 用線性索引去取會整批錯位,而錯位取到的是**另一個看起來正常的字**
// —— 那比缺字難發現得多(實測:C6E7 應為「ゃ」,線性索引取到的是別的字)。
// 所以在補齊那張洞表之前,這一區一律當缺字。

const nCommon = 5401

// rawIndex 把 Big5 雙位元組換成線性序號。
// 低位元組有兩段(0x40-0x7E 與 0xA1-0xFE),所以不是單純相減。
func rawIndex(hi, lo byte) int {
	off := int(lo) - 0x40
	if lo >= 0x7F {
		off = int(lo) - 0x62
	}
	return (int(hi)-0xA1)*157 + off
}

// Font 是一組同尺寸的倚天字庫。
type Font struct {
	std, spc []byte
	W, H     int
	stride   int

	mu    sync.Mutex
	b5    map[rune][2]byte // rune -> Big5 的快取
}

// Load 讀入漢字區與符號區。spcPath 可以留空,但那樣全形標點會缺字。
func Load(stdPath, spcPath string, w, h int) (*Font, error) {
	std, err := os.ReadFile(stdPath)
	if err != nil {
		return nil, fmt.Errorf("讀 STDFONT: %w", err)
	}
	f := &Font{std: std, W: w, H: h, stride: (w + 7) / 8 * h, b5: map[rune][2]byte{}}
	if len(std)%f.stride != 0 {
		return nil, fmt.Errorf("STDFONT 大小 %d 不是 stride %d 的整數倍", len(std), f.stride)
	}
	if spcPath != "" {
		spc, err := os.ReadFile(spcPath)
		if err != nil {
			return nil, fmt.Errorf("讀 SPCFONT: %w", err)
		}
		f.spc = spc
	}
	return f, nil
}

// manualBig5 補 Python/Go 的 Big5 codec 與倚天字庫對不上的少數符號。
var manualBig5 = map[rune][2]byte{
	'～': {0xA1, 0xE3}, // U+FF5E vs U+301C 的歧義
}

func (f *Font) big5Bytes(r rune) ([2]byte, bool) {
	if b, ok := manualBig5[r]; ok {
		return b, true
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if b, ok := f.b5[r]; ok {
		return b, b != [2]byte{}
	}
	out, err := traditionalchinese.Big5.NewEncoder().Bytes([]byte(string(r)))
	var b [2]byte
	if err == nil && len(out) == 2 {
		b = [2]byte{out[0], out[1]}
	}
	f.b5[r] = b
	return b, b != [2]byte{}
}

// Glyph 取一個全形字的點陣圖。缺字回 nil,由呼叫端決定怎麼表示。
func (f *Font) Glyph(r rune) *fnt.Glyph {
	b, ok := f.big5Bytes(r)
	if !ok {
		return nil
	}
	hi, lo := b[0], b[1]
	if hi < 0xA1 || hi > 0xF9 {
		return nil
	}
	raw := rawIndex(hi, lo)

	var data []byte
	var idx int
	switch {
	case raw <= lastSpc:
		data, idx = f.spc, raw
	case raw < baseA440:
		return nil // 符號區與漢字區之間的空隙
	case raw <= lastCommon:
		data, idx = f.std, raw-baseA440
	case raw < baseC940:
		return nil // 符號補充區,見上面的說明
	default:
		data, idx = f.std, nCommon+(raw-baseC940)
	}
	if data == nil || idx < 0 {
		return nil
	}
	off := idx * f.stride
	if off+f.stride > len(data) {
		return nil
	}
	return decode(data[off:off+f.stride], f.W, f.H)
}

// decode 把裸點陣位元組攤成 row-major 的 bool。
func decode(b []byte, w, h int) *fnt.Glyph {
	rowBytes := (w + 7) / 8
	g := &fnt.Glyph{W: w, H: h, Bits: make([]bool, w*h)}
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			by := b[y*rowBytes+x/8]
			if by&(0x80>>uint(x%8)) != 0 {
				g.Bits[y*w+x] = true
			}
		}
	}
	return g
}
