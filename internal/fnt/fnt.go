// Package fnt 讀 Windows 2.x/3.x 的 NE `.FON` 點陣字型。
//
// WinCV 隨附 cvga (8x15) / cvga1018 (10x18) / cvga1224 (12x24) 三個半形字型,
// 三者都是定寬、涵蓋 0x00-0xFF。原版以 AddFontResource 註冊後用 face 名指定。
//
// 格式細節與各欄位的 offset 見 tools/fnt.py 的檔頭註解;兩邊是同一份規格,
// Python 版拿來探索與驗證,這裡是執行期用的。
package fnt

import (
	"encoding/binary"
	"fmt"
	"github.com/wicanr2/wincv-remake/internal/i18n"
)

// Font 是一個 .FON 裡的單一字型。
type Font struct {
	Face      string
	PixWidth  int // 0 表示變寬
	PixHeight int
	Ascent    int
	First     byte
	Last      byte
	widths    []int
	offsets   []int
	data      []byte
	base      int
}

// Size 回傳格子的字身大小。實作 render.HalfSource。
func (f *Font) Size() (w, h int) { return f.PixWidth, f.PixHeight }

// Glyph 是一個字的點陣圖,以 row-major 的 bool 表示。
type Glyph struct {
	W, H int
	Bits []bool // len == W*H,true 表示前景
	// Alpha 是每個像素的覆蓋率(0–255),有的話畫的時候用它混色,
	// 沒有(nil)就是純 1-bit 的點陣字。向量字型在大字級時用這個
	// 做反鋸齒;點陣字永遠是 nil —— 它的邊緣本來就該是硬的。
	Alpha []uint8
}

// Cov 回傳 (x, y) 的覆蓋率。1-bit 字模回 0 或 255。
func (g *Glyph) Cov(x, y int) uint8 {
	if x < 0 || y < 0 || x >= g.W || y >= g.H {
		return 0
	}
	if g.Alpha != nil {
		return g.Alpha[y*g.W+x]
	}
	if g.Bits[y*g.W+x] {
		return 255
	}
	return 0
}

func (g *Glyph) At(x, y int) bool {
	if x < 0 || y < 0 || x >= g.W || y >= g.H {
		return false
	}
	return g.Bits[y*g.W+x]
}

func u16(d []byte, o int) int { return int(binary.LittleEndian.Uint16(d[o:])) }
func u32(d []byte, o int) int { return int(binary.LittleEndian.Uint32(d[o:])) }

// neFontResources 找出 NE 檔裡所有 RT_FONT (type 8) 的位置。
func neFontResources(d []byte) ([]int, error) {
	if len(d) < 0x40 || d[0] != 'M' || d[1] != 'Z' {
		return nil, fmt.Errorf(i18n.T("不是 MZ 檔"))
	}
	ne := u16(d, 0x3C)
	if ne+0x26 > len(d) || d[ne] != 'N' || d[ne+1] != 'E' {
		return nil, fmt.Errorf(i18n.T("找不到 NE header"))
	}
	rt := ne + u16(d, ne+0x24)
	if rt+2 > len(d) {
		return nil, fmt.Errorf(i18n.T("resource table 超出檔案"))
	}
	shift := u16(d, rt)
	p := rt + 2
	var out []int
	for p+8 <= len(d) {
		tid := u16(d, p)
		if tid == 0 {
			break
		}
		cnt := u16(d, p+2)
		p += 8
		for i := 0; i < cnt && p+12 <= len(d); i++ {
			off := u16(d, p) << shift
			if tid&0x7FFF == 8 {
				out = append(out, off)
			}
			p += 12
		}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf(i18n.T("沒有 RT_FONT resource"))
	}
	return out, nil
}

// Parse 讀一個 .FON 的位元組,回傳裡面第一個字型。
func Parse(d []byte) (*Font, error) {
	offs, err := neFontResources(d)
	if err != nil {
		return nil, err
	}
	return parseAt(d, offs[0])
}

func parseAt(d []byte, base int) (*Font, error) {
	if base+0x76 > len(d) {
		return nil, fmt.Errorf(i18n.T("FNT header 超出檔案"))
	}
	f := &Font{
		data:      d,
		base:      base,
		Ascent:    u16(d, base+0x4A),
		PixWidth:  u16(d, base+0x56),
		PixHeight: u16(d, base+0x58),
		First:     d[base+0x5F],
		Last:      d[base+0x60],
	}
	faceOff := base + u32(d, base+0x69)
	if faceOff >= len(d) {
		return nil, fmt.Errorf(i18n.T("dfFace 超出檔案"))
	}
	end := faceOff
	for end < len(d) && d[end] != 0 {
		end++
	}
	f.Face = string(d[faceOff:end])

	n := int(f.Last) - int(f.First) + 2
	if base+0x76+n*4 > len(d) {
		return nil, fmt.Errorf(i18n.T("dfCharTable 超出檔案"))
	}
	f.widths = make([]int, n)
	f.offsets = make([]int, n)
	for i := 0; i < n; i++ {
		f.widths[i] = u16(d, base+0x76+i*4)
		f.offsets[i] = u16(d, base+0x76+i*4+2)
	}
	return f, nil
}

// Glyph 取一個字的點陣圖。字模是 column-major:先存第 0 欄的全部列
// (每 8 列一個 byte),再第 1 欄;寬度超過 8 的字因此分成多個 8-pixel 欄組。
// 不在範圍內回 nil。
func (f *Font) Glyph(code byte) *Glyph {
	if code < f.First || code > f.Last {
		return nil
	}
	i := int(code) - int(f.First)
	w, off := f.widths[i], f.offsets[i]
	h := f.PixHeight
	if w == 0 || h == 0 {
		return nil
	}
	g := &Glyph{W: w, H: h, Bits: make([]bool, w*h)}
	cols := (w + 7) / 8
	for c := 0; c < cols; c++ {
		colBase := f.base + off + c*h
		for y := 0; y < h; y++ {
			if colBase+y >= len(f.data) {
				return g
			}
			b := f.data[colBase+y]
			for bit := 0; bit < 8; bit++ {
				x := c*8 + bit
				if x >= w {
					break
				}
				if b&(0x80>>uint(bit)) != 0 {
					g.Bits[y*w+x] = true
				}
			}
		}
	}
	return g
}
