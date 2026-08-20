package imgfmt

import (
	"encoding/binary"
	"fmt"
	"image"
	"image/color"
)

// DecodePNM 解 Netpbm 家族:
//
//	P1 PBM ASCII    P2 PGM ASCII    P3 PPM ASCII
//	P4 PBM binary   P5 PGM binary   P6 PPM binary
//
// PBM 的 1 是**黑**、0 是白 —— 跟直覺相反,弄錯會整張反相。
func DecodePNM(d []byte) (image.Image, error) {
	if len(d) < 2 || d[0] != 'P' {
		return nil, fmt.Errorf("不是 PNM")
	}
	kind := d[1]
	if kind < '1' || kind > '6' {
		return nil, fmt.Errorf("不認得的 PNM 類型 P%c", kind)
	}
	p := 2

	next := func() (int, error) {
		for p < len(d) {
			c := d[p]
			if c == '#' { // 註解到行尾
				for p < len(d) && d[p] != '\n' {
					p++
				}
				continue
			}
			if c == ' ' || c == '\t' || c == '\n' || c == '\r' {
				p++
				continue
			}
			break
		}
		if p >= len(d) {
			return 0, fmt.Errorf("PNM 檔頭不完整")
		}
		v := 0
		got := false
		for p < len(d) && d[p] >= '0' && d[p] <= '9' {
			v = v*10 + int(d[p]-'0')
			p++
			got = true
		}
		if !got {
			return 0, fmt.Errorf("PNM 檔頭讀不到數字")
		}
		return v, nil
	}

	w, err := next()
	if err != nil {
		return nil, err
	}
	h, err := next()
	if err != nil {
		return nil, err
	}
	maxv := 1
	if kind != '1' && kind != '4' {
		if maxv, err = next(); err != nil {
			return nil, err
		}
	}
	if w <= 0 || h <= 0 || maxv <= 0 {
		return nil, fmt.Errorf("PNM 尺寸不合理 %dx%d maxv=%d", w, h, maxv)
	}
	// binary 格式在 maxval 之後**剛好一個** whitespace,然後就是資料。
	if kind >= '4' && p < len(d) {
		p++
	}

	img := image.NewRGBA(image.Rect(0, 0, w, h))
	scale := func(v int) uint8 {
		if maxv == 255 {
			return uint8(v)
		}
		return uint8(v * 255 / maxv)
	}

	switch kind {
	case '1', '2', '3': // ASCII
		for y := 0; y < h; y++ {
			for x := 0; x < w; x++ {
				switch kind {
				case '1':
					v, err := next()
					if err != nil {
						return img, nil // 資料不足就畫到哪算哪
					}
					c := uint8(255)
					if v == 1 {
						c = 0 // PBM:1 是黑
					}
					img.Set(x, y, color.RGBA{c, c, c, 0xFF})
				case '2':
					v, err := next()
					if err != nil {
						return img, nil
					}
					c := scale(v)
					img.Set(x, y, color.RGBA{c, c, c, 0xFF})
				case '3':
					r, err := next()
					if err != nil {
						return img, nil
					}
					g, _ := next()
					b, _ := next()
					img.Set(x, y, color.RGBA{scale(r), scale(g), scale(b), 0xFF})
				}
			}
		}
	case '4': // PBM binary:每列補齊到 byte 邊界
		rowBytes := (w + 7) / 8
		for y := 0; y < h; y++ {
			for x := 0; x < w; x++ {
				o := p + y*rowBytes + x/8
				bit := byte(0)
				if o < len(d) {
					bit = d[o] & (0x80 >> uint(x%8))
				}
				c := uint8(255)
				if bit != 0 {
					c = 0
				}
				img.Set(x, y, color.RGBA{c, c, c, 0xFF})
			}
		}
	case '5', '6': // PGM / PPM binary
		nch := 1
		if kind == '6' {
			nch = 3
		}
		wide := maxv > 255
		bpc := 1
		if wide {
			bpc = 2
		}
		read := func(o int) int {
			if wide {
				if o+1 >= len(d) {
					return 0
				}
				return int(binary.BigEndian.Uint16(d[o:]))
			}
			if o >= len(d) {
				return 0
			}
			return int(d[o])
		}
		stride := w * nch * bpc
		for y := 0; y < h; y++ {
			for x := 0; x < w; x++ {
				o := p + y*stride + x*nch*bpc
				if nch == 1 {
					c := scale(read(o))
					img.Set(x, y, color.RGBA{c, c, c, 0xFF})
				} else {
					img.Set(x, y, color.RGBA{
						scale(read(o)), scale(read(o + bpc)), scale(read(o + 2*bpc)), 0xFF})
				}
			}
		}
	}
	return img, nil
}
