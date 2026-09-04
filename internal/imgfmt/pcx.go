package imgfmt

import (
	"encoding/binary"
	"fmt"
	"github.com/wicanr2/wincv-remake/internal/i18n"
	"image"
	"image/color"
)

// DecodePCX 解 ZSoft PCX。
//
// 檔頭 128 bytes:
//
//	0x00 manufacturer 固定 0x0A
//	0x01 version
//	0x02 encoding    1 = RLE
//	0x03 bitsPerPixel(每個 plane)
//	0x04 xmin ymin xmax ymax(各 2 bytes,是**含端點**的座標)
//	0x41 nPlanes
//	0x42 bytesPerLine(每個 plane 每列幾個 byte)
//
// 256 色調色盤在檔尾:最後 769 bytes,第一個 byte 是 0x0C。
func DecodePCX(d []byte) (image.Image, error) {
	if len(d) < 128 || d[0] != 0x0A {
		return nil, fmt.Errorf(i18n.T("不是 PCX"))
	}
	enc := d[2]
	bpp := int(d[3])
	xmin := int(binary.LittleEndian.Uint16(d[4:]))
	ymin := int(binary.LittleEndian.Uint16(d[6:]))
	xmax := int(binary.LittleEndian.Uint16(d[8:]))
	ymax := int(binary.LittleEndian.Uint16(d[10:]))
	planes := int(d[0x41])
	bpl := int(binary.LittleEndian.Uint16(d[0x42:]))

	w, h := xmax-xmin+1, ymax-ymin+1
	if w <= 0 || h <= 0 || w > 1<<16 || h > 1<<16 || planes == 0 || bpl == 0 {
		return nil, fmt.Errorf(i18n.T("PCX 尺寸不合理: %dx%d planes=%d bpl=%d"), w, h, planes, bpl)
	}

	total := bpl * planes * h
	raw := make([]byte, 0, total)
	body := d[128:]
	if enc == 1 {
		for i := 0; i < len(body) && len(raw) < total; {
			b := body[i]
			i++
			if b&0xC0 == 0xC0 {
				n := int(b & 0x3F)
				if i >= len(body) {
					break
				}
				v := body[i]
				i++
				for k := 0; k < n && len(raw) < total; k++ {
					raw = append(raw, v)
				}
			} else {
				raw = append(raw, b)
			}
		}
	} else {
		n := total
		if n > len(body) {
			n = len(body)
		}
		raw = append(raw, body[:n]...)
	}
	for len(raw) < total {
		raw = append(raw, 0)
	}

	// 調色盤:VGA 256 色在檔尾;16 色的在檔頭 0x10 起。
	var pal color.Palette
	if bpp == 8 && planes == 1 {
		if i := len(d) - 769; i > 0 && d[i] == 0x0C {
			p := d[i+1:]
			for c := 0; c < 256; c++ {
				pal = append(pal, color.RGBA{p[c*3], p[c*3+1], p[c*3+2], 0xFF})
			}
		} else {
			for c := 0; c < 256; c++ {
				pal = append(pal, color.RGBA{uint8(c), uint8(c), uint8(c), 0xFF})
			}
		}
	} else {
		for c := 0; c < 16; c++ {
			pal = append(pal, color.RGBA{d[16+c*3], d[16+c*3+1], d[16+c*3+2], 0xFF})
		}
	}

	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		line := raw[y*bpl*planes : (y+1)*bpl*planes]
		for x := 0; x < w; x++ {
			var c color.Color
			switch {
			case planes == 3 && bpp == 8:
				c = color.RGBA{line[x], line[bpl+x], line[2*bpl+x], 0xFF}
			case planes == 1 && bpp == 8:
				c = pal[line[x]]
			case bpp == 1:
				// 每個 plane 一個位元,合起來是調色盤索引。
				idx := 0
				for p := 0; p < planes; p++ {
					b := line[p*bpl+x/8]
					if b&(0x80>>uint(x%8)) != 0 {
						idx |= 1 << uint(p)
					}
				}
				if idx < len(pal) {
					c = pal[idx]
				} else {
					c = color.RGBA{}
				}
			default:
				return nil, fmt.Errorf(i18n.T("PCX: 還沒支援 %d planes x %d bit"), planes, bpp)
			}
			img.Set(x, y, c)
		}
	}
	return img, nil
}
