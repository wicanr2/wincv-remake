package imgfmt

import (
	"encoding/binary"
	"fmt"
	"github.com/wicanr2/wincv-remake/internal/i18n"
	"image"
	"image/color"
)

// DecodeTGA 解 Truevision TGA。
//
// 檔頭 18 bytes:
//
//	0  idLength
//	1  colorMapType
//	2  imageType  1=索引 2=RGB 3=灰階 9/10/11=同前但 RLE
//	3  colorMapFirst(2) 5 colorMapLen(2) 7 colorMapDepth(1)
//	8  xorigin(2) 10 yorigin(2) 12 width(2) 14 height(2)
//	16 pixelDepth 17 descriptor
//
// descriptor 的 bit5 是「原點在上」。TGA 預設原點在**左下**,
// 忘了處理就會整張上下顛倒 —— 而顛倒的圖看起來「有解出來」,很容易漏掉。
func DecodeTGA(d []byte) (image.Image, error) {
	if len(d) < 18 {
		return nil, fmt.Errorf(i18n.T("不是 TGA:檔案太短"))
	}
	idLen := int(d[0])
	cmType := d[1]
	imgType := d[2]
	cmLen := int(binary.LittleEndian.Uint16(d[5:]))
	cmDepth := int(d[7])
	w := int(binary.LittleEndian.Uint16(d[12:]))
	h := int(binary.LittleEndian.Uint16(d[14:]))
	depth := int(d[16])
	topDown := d[17]&0x20 != 0

	if w <= 0 || h <= 0 {
		return nil, fmt.Errorf(i18n.T("TGA 尺寸不合理 %dx%d"), w, h)
	}
	p := 18 + idLen

	var pal color.Palette
	if cmType == 1 {
		bytesPer := (cmDepth + 7) / 8
		need := cmLen * bytesPer
		if p+need > len(d) {
			return nil, fmt.Errorf(i18n.T("TGA 調色盤超出檔案"))
		}
		for i := 0; i < cmLen; i++ {
			pal = append(pal, readColor(d[p+i*bytesPer:], cmDepth))
		}
		p += need
	}

	bytesPer := (depth + 7) / 8
	if bytesPer == 0 {
		return nil, fmt.Errorf("TGA pixelDepth = 0")
	}
	npix := w * h
	pix := make([]byte, 0, npix*bytesPer)

	rle := imgType >= 9 && imgType <= 11
	if rle {
		for p < len(d) && len(pix) < npix*bytesPer {
			c := d[p]
			p++
			n := int(c&0x7F) + 1
			if c&0x80 != 0 { // 重複
				if p+bytesPer > len(d) {
					break
				}
				for k := 0; k < n; k++ {
					pix = append(pix, d[p:p+bytesPer]...)
				}
				p += bytesPer
			} else { // 原樣
				need := n * bytesPer
				if p+need > len(d) {
					need = len(d) - p
				}
				pix = append(pix, d[p:p+need]...)
				p += need
			}
		}
	} else {
		need := npix * bytesPer
		if p+need > len(d) {
			need = len(d) - p
		}
		pix = append(pix, d[p:p+need]...)
	}
	for len(pix) < npix*bytesPer {
		pix = append(pix, 0)
	}

	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		dy := y
		if !topDown {
			dy = h - 1 - y
		}
		for x := 0; x < w; x++ {
			o := (y*w + x) * bytesPer
			var c color.Color
			switch imgType {
			case 1, 9: // 索引
				idx := int(pix[o])
				if idx < len(pal) {
					c = pal[idx]
				} else {
					c = color.RGBA{}
				}
			case 3, 11: // 灰階
				v := pix[o]
				c = color.RGBA{v, v, v, 0xFF}
			default: // RGB
				c = readColor(pix[o:], depth)
			}
			img.Set(x, dy, c)
		}
	}
	return img, nil
}

// readColor 讀一個 TGA 像素。TGA 的位元組順序是 BGR(A)。
func readColor(b []byte, depth int) color.RGBA {
	switch depth {
	case 15, 16:
		if len(b) < 2 {
			return color.RGBA{}
		}
		v := binary.LittleEndian.Uint16(b)
		r := uint8((v>>10)&0x1F) << 3
		g := uint8((v>>5)&0x1F) << 3
		bl := uint8(v&0x1F) << 3
		return color.RGBA{r, g, bl, 0xFF}
	case 24:
		if len(b) < 3 {
			return color.RGBA{}
		}
		return color.RGBA{b[2], b[1], b[0], 0xFF}
	case 32:
		if len(b) < 4 {
			return color.RGBA{}
		}
		return color.RGBA{b[2], b[1], b[0], b[3]}
	}
	return color.RGBA{}
}
