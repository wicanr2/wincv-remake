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
	"github.com/wicanr2/wincv-remake/internal/i18n"
	"os"
	"path/filepath"
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
//	符號補充 raw 5872..6279    408 個碼位  SPCFSUPP.15 只有 365 個  → 中間有洞
//
// 補充區的字模是「把有定義的碼位擠在一起」存的:408 個碼位裡只有 365 個
// 在 Big5 有定義,其餘 43 個是空的。用線性索引去取會整批錯位,而錯位取到的
// 是**另一個看起來正常的字**,比缺字難發現得多。
//
// 洞在哪不必寫死一張表:直接問 Big5 解碼器。走一次 C6A1-C8FE,解得開的
// 才佔一個字模序號,累加就是索引(suppIndex)。這樣做的附帶好處是
// **數量本身就是驗證** —— 算出來剛好 365,與 SPCFSUPP.15 的字模數相符,
// 對不上就表示假設錯了,而不是安靜地取到別的字。
//
// 實際的 43 個洞:C8A5-C8CC(40 個)與 C8F2-C8F4(3 個)。

const nCommon = 5401

// NativeW / NativeH 是 `.15` 這一組字庫的點陣尺寸。
//
// Load / LoadBytes 的 w、h 指的是**字庫本身**的尺寸(要靠 stride 把
// 位元組切成一個一個字模),不是「想畫多大」—— 要別的大小是
// render.ScaleCJK 的事。傳成目標格子大小的症狀是那個字級整批沒有中文,
// 而且不會有訊息:呼叫端通常只在第一個字級印警告,而第一個字級的
// 格子剛好就是 16×15,所以只有它會過。
const (
	NativeW = 16
	NativeH = 15
)

// 補充符號區的界線。
var (
	baseC6A1 = rawIndex(0xC6, 0xA1)
	lastC8FE = rawIndex(0xC8, 0xFE)
)

// suppIndex[raw-baseC6A1] 是該碼位在 SPCFSUPP 裡的字模序號,-1 表示沒有字模。
var suppIndex, suppCount = buildSuppIndex()

func buildSuppIndex() ([]int, int) {
	n := lastC8FE - baseC6A1 + 1
	out := make([]int, n)
	dec := traditionalchinese.Big5.NewDecoder()
	next := 0
	for i := range out {
		out[i] = -1
	}
	for hi := 0xC6; hi <= 0xC8; hi++ {
		for lo := 0x40; lo <= 0xFE; lo++ {
			if lo > 0x7E && lo < 0xA1 {
				continue
			}
			code := hi<<8 | lo
			if code < 0xC6A1 || code > 0xC8FE {
				continue
			}
			s, err := dec.Bytes([]byte{byte(hi), byte(lo)})
			r := []rune(string(s))
			if err != nil || len(r) != 1 || r[0] == 0xFFFD {
				continue // 這個碼位沒有定義,不佔字模
			}
			out[rawIndex(byte(hi), byte(lo))-baseC6A1] = next
			next++
		}
	}
	return out, next
}

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
	std, spc, supp []byte
	W, H           int
	stride         int

	mu sync.Mutex
	b5 map[rune][2]byte // rune -> Big5 的快取
}

// Load 讀入漢字區與符號區。spcPath 可以留空,但那樣全形標點會缺字。
//
// 符號補充區(SPCFSUPP)會自動在 spcPath 的同目錄找,找不到就當那一區缺字。
func Load(stdPath, spcPath string, w, h int) (*Font, error) {
	std, err := os.ReadFile(stdPath)
	if err != nil {
		return nil, fmt.Errorf(i18n.T("讀 STDFONT: %w"), err)
	}
	var spc, supp []byte
	if spcPath != "" {
		if spc, err = os.ReadFile(spcPath); err != nil {
			return nil, fmt.Errorf(i18n.T("讀 SPCFONT: %w"), err)
		}
		// 找不到補充區不是錯 —— 那一區當缺字就好。
		supp, _ = os.ReadFile(suppPathFor(spcPath))
	}
	return LoadBytes(std, spc, supp, w, h)
}

// LoadBytes 與 Load 相同,但字庫直接給位元組。
//
// 給的是打包進執行檔的字型用的(見 internal/bundled)—— 那時候沒有路徑可讀。
// spc / supp 可以是 nil:沒有 spc 全形標點缺字,沒有 supp 符號補充區缺字。
func LoadBytes(std, spc, supp []byte, w, h int) (*Font, error) {
	f := &Font{std: std, W: w, H: h, stride: (w + 7) / 8 * h, b5: map[rune][2]byte{}}
	if len(std)%f.stride != 0 {
		return nil, fmt.Errorf(i18n.T("STDFONT 大小 %d 不是 stride %d 的整數倍"), len(std), f.stride)
	}
	f.spc = spc
	if len(supp) > 0 {
		if len(supp)%f.stride == 0 && len(supp)/f.stride == suppCount {
			f.supp = supp
		} else {
			return nil, fmt.Errorf(i18n.T("SPCFSUPP 有 %d 個字模,但 Big5 在 C6A1-C8FE 只定義 %d 個碼位"),
				len(supp)/f.stride, suppCount)
		}
	}
	return f, nil
}

// suppPathFor 由 SPCFONT 的路徑推出 SPCFSUPP 的路徑(同目錄、同副檔名)。
func suppPathFor(spcPath string) string {
	dir, base := filepath.Split(spcPath)
	ext := filepath.Ext(base)
	return filepath.Join(dir, "SPCFSUPP"+ext)
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
		if f.supp == nil {
			return nil
		}
		i := raw - baseC6A1
		if i < 0 || i >= len(suppIndex) || suppIndex[i] < 0 {
			return nil // 這個碼位本來就沒有字
		}
		data, idx = f.supp, suppIndex[i]
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
