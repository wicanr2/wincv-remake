package imgfmt

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"image"
	"image/color"
	_ "image/png"
)

// DecodeICO 解 Windows .ico,挑面積最大的那一張。
//
// 目錄項的 width/height 是 1 byte,0 表示 256 —— 這是最容易踩的一點,
// 照字面讀會把 256x256 的圖當成 0x0 而挑到小的那張。
//
// 圖片本體可能是 PNG(Vista 之後)或 BMP。BMP 的高度是**兩倍**,
// 因為底下接著 AND 遮罩;而且沒有 BITMAPFILEHEADER,要自己補。
func DecodeICO(d []byte) (image.Image, error) {
	if len(d) < 6 {
		return nil, fmt.Errorf("不是 ICO")
	}
	if binary.LittleEndian.Uint16(d[0:]) != 0 || binary.LittleEndian.Uint16(d[2:]) != 1 {
		return nil, fmt.Errorf("不是 ICO")
	}
	n := int(binary.LittleEndian.Uint16(d[4:]))
	if n == 0 {
		return nil, fmt.Errorf("ICO 沒有任何圖")
	}

	best, bestArea := -1, -1
	for i := 0; i < n; i++ {
		o := 6 + i*16
		if o+16 > len(d) {
			break
		}
		w, h := int(d[o]), int(d[o+1])
		if w == 0 {
			w = 256
		}
		if h == 0 {
			h = 256
		}
		if w*h > bestArea {
			best, bestArea = i, w*h
		}
	}
	if best < 0 {
		return nil, fmt.Errorf("ICO 目錄壞了")
	}

	o := 6 + best*16
	size := int(binary.LittleEndian.Uint32(d[o+8:]))
	off := int(binary.LittleEndian.Uint32(d[o+12:]))
	if off < 0 || off+size > len(d) {
		return nil, fmt.Errorf("ICO 圖片資料超出檔案")
	}
	body := d[off : off+size]

	if len(body) > 8 && bytes.Equal(body[:8], []byte{0x89, 'P', 'N', 'G', 0x0D, 0x0A, 0x1A, 0x0A}) {
		img, _, err := image.Decode(bytes.NewReader(body))
		return img, err
	}
	return decodeICOBMP(body)
}

func decodeICOBMP(b []byte) (image.Image, error) {
	if len(b) < 40 {
		return nil, fmt.Errorf("ICO 內的 BMP 檔頭不完整")
	}
	w := int(int32(binary.LittleEndian.Uint32(b[4:])))
	h2 := int(int32(binary.LittleEndian.Uint32(b[8:])))
	bpp := int(binary.LittleEndian.Uint16(b[14:]))
	h := h2 / 2 // 上半是圖、下半是 AND 遮罩
	if w <= 0 || h <= 0 {
		return nil, fmt.Errorf("ICO 內的 BMP 尺寸不合理 %dx%d", w, h)
	}

	hdr := int(binary.LittleEndian.Uint32(b[0:]))
	if hdr < 40 || hdr > len(b) {
		hdr = 40
	}
	nColors := int(binary.LittleEndian.Uint32(b[32:]))
	if nColors == 0 && bpp <= 8 {
		nColors = 1 << uint(bpp)
	}
	palOff := hdr
	var pal color.Palette
	for i := 0; i < nColors && palOff+i*4+3 < len(b); i++ {
		p := b[palOff+i*4:]
		pal = append(pal, color.RGBA{p[2], p[1], p[0], 0xFF})
	}
	dataOff := palOff + nColors*4

	rowBits := w * bpp
	rowBytes := (rowBits + 31) / 32 * 4 // 每列補齊到 4 bytes
	maskRowBytes := (w + 31) / 32 * 4
	maskOff := dataOff + rowBytes*h

	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		sy := h - 1 - y // BMP 由下往上
		row := dataOff + sy*rowBytes
		for x := 0; x < w; x++ {
			var c color.RGBA
			switch bpp {
			case 32:
				o := row + x*4
				if o+3 < len(b) {
					c = color.RGBA{b[o+2], b[o+1], b[o], b[o+3]}
				}
			case 24:
				o := row + x*3
				if o+2 < len(b) {
					c = color.RGBA{b[o+2], b[o+1], b[o], 0xFF}
				}
			case 8:
				o := row + x
				if o < len(b) && int(b[o]) < len(pal) {
					c = pal[b[o]].(color.RGBA)
				}
			case 4:
				o := row + x/2
				if o < len(b) {
					v := b[o] >> 4
					if x%2 == 1 {
						v = b[o] & 0x0F
					}
					if int(v) < len(pal) {
						c = pal[v].(color.RGBA)
					}
				}
			case 1:
				o := row + x/8
				if o < len(b) {
					v := byte(0)
					if b[o]&(0x80>>uint(x%8)) != 0 {
						v = 1
					}
					if int(v) < len(pal) {
						c = pal[v].(color.RGBA)
					}
				}
			default:
				return nil, fmt.Errorf("ICO: 還沒支援 %d bpp", bpp)
			}
			// 32bpp 自帶 alpha,其餘看 AND 遮罩:1 表示透明。
			if bpp != 32 {
				c.A = 0xFF
				mo := maskOff + sy*maskRowBytes + x/8
				if mo < len(b) && b[mo]&(0x80>>uint(x%8)) != 0 {
					c.A = 0
				}
			}
			img.SetRGBA(x, y, c)
		}
	}
	return img, nil
}
