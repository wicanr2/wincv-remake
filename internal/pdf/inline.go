package pdf

import (
	"bytes"
	"compress/zlib"
	"fmt"
	"github.com/wicanr2/wincv-remake/internal/i18n"
	"image"
	"image/color"
	_ "image/jpeg"
	"io"

	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"
)

// 內嵌影像(BI / ID / EI)是把整張圖直接寫在內容資料流裡,不另外開一個物件。
// 它的參數用縮寫(W、H、BPC、CS、F),而且色彩空間可以指到頁面的資源。
//
// [雷] 影像的每一列都補齊到整個位元組。1 位元的黑白影像最容易踩到:
// 寬度 12 的一列佔 2 個位元組而不是 1.5 個。少算的話整張圖會逐列斜掉,
// 而斜掉的圖看起來仍然是一張圖。

// MaxInlinePixels 是內嵌影像的像素上限。
//
// 內嵌影像的用途本來就是小圖(圖示、網底、一個色塊)。上限擋住的是
// 參數被解錯時算出來的天文數字 —— 那會當場配掉幾 GB。
const MaxInlinePixels = 16 << 20

// decodeInline 把一張內嵌影像變成可以取樣的影像。
func (in *interp) decodeInline(dict map[string]value, data []byte, res types.Dict) (image.Image, bool, error) {
	w := int(numOfValue(dict, "W", "Width"))
	h := int(numOfValue(dict, "H", "Height"))
	if w <= 0 || h <= 0 || w*h > MaxInlinePixels {
		return nil, false, fmt.Errorf(i18n.T("內嵌影像的尺寸不合理(%d×%d)"), w, h)
	}
	isMask := isInlineMask(dict)
	bpc := int(numOfValue(dict, "BPC", "BitsPerComponent"))
	if isMask {
		bpc = 1
	}

	raw, jpegDecoded, err := in.inlineData(dict, data)
	if err != nil {
		return nil, isMask, err
	}
	if jpegDecoded != nil {
		return jpegDecoded, isMask, nil
	}

	cs := in.inlineColorSpace(dict, res)
	if isMask {
		return maskImage(raw, w, h, inlineDecodeFlipped(dict)), true, nil
	}
	if bpc <= 0 {
		bpc = 8
	}
	return samplesImage(raw, w, h, bpc, cs), false, nil
}

// inlineData 把濾鏡拆掉。回傳的第二個值不為 nil 表示那是 JPEG,
// 已經直接解成影像了(JPEG 沒辦法交回原始取樣值)。
func (in *interp) inlineData(dict map[string]value, data []byte) ([]byte, image.Image, error) {
	f, ok := dictOf(dict, "F", "Filter")
	if !ok {
		return data, nil, nil
	}
	names := []string{}
	switch f.kind {
	case vName:
		names = append(names, f.str)
	case vArray:
		for _, e := range f.arr {
			if e.kind == vName {
				names = append(names, e.str)
			}
		}
	}
	cur := data
	for _, name := range names {
		switch name {
		case "AHx", "ASCIIHexDecode":
			cur = unhexStream(cur)
		case "A85", "ASCII85Decode":
			cur = a85Decode(cur)
		case "Fl", "FlateDecode":
			zr, err := zlib.NewReader(bytes.NewReader(cur))
			if err != nil {
				return nil, nil, fmt.Errorf(i18n.T("內嵌影像解壓失敗:%w"), err)
			}
			out, err := io.ReadAll(io.LimitReader(zr, MaxInlinePixels*4))
			zr.Close()
			if err != nil {
				return nil, nil, err
			}
			cur = out
		case "RL", "RunLengthDecode":
			cur = runLengthDecode(cur)
		case "DCT", "DCTDecode":
			m, _, err := image.Decode(bytes.NewReader(cur))
			if err != nil {
				return nil, nil, fmt.Errorf(i18n.T("內嵌的 JPEG 解不開:%w"), err)
			}
			return nil, m, nil
		default:
			return nil, nil, fmt.Errorf(i18n.T("內嵌影像用了還不支援的濾鏡 %s"), name)
		}
	}
	return cur, nil, nil
}

// inlineColorSpace 查一張內嵌影像的色彩空間。
func (in *interp) inlineColorSpace(dict map[string]value, res types.Dict) *colorSpace {
	v, ok := dictOf(dict, "CS", "ColorSpace")
	if !ok {
		return csDeviceGray
	}
	switch v.kind {
	case vName:
		switch v.str {
		case "G", "DeviceGray", "CalGray":
			return csDeviceGray
		case "RGB", "DeviceRGB", "CalRGB":
			return csDeviceRGB
		case "CMYK", "DeviceCMYK":
			return csDeviceCMYK
		}
		return in.colorSpace(res, v.str)
	case vArray:
		return inlineIndexed(v.arr, in, res)
	}
	return csDeviceGray
}

// inlineIndexed 解 `[/I /RGB 255 <調色盤>]` 這種寫在影像參數裡的索引色。
func inlineIndexed(arr []value, in *interp, res types.Dict) *colorSpace {
	if len(arr) < 4 || arr[0].kind != vName {
		return csDeviceGray
	}
	switch arr[0].str {
	case "I", "Indexed":
	default:
		return csDeviceGray
	}
	cs := &colorSpace{kind: csIndexed, n: 1}
	switch arr[1].str {
	case "RGB", "DeviceRGB", "CalRGB":
		cs.base = csDeviceRGB
	case "CMYK", "DeviceCMYK":
		cs.base = csDeviceCMYK
	case "G", "DeviceGray", "CalGray":
		cs.base = csDeviceGray
	default:
		cs.base = in.colorSpace(res, arr[1].str)
	}
	if arr[2].kind == vNum {
		cs.hival = int(arr[2].num)
	}
	if arr[3].kind == vStr {
		cs.lookup = []byte(arr[3].str)
	}
	return cs
}

// inlineDecodeFlipped 回答遮罩影像的 Decode 陣列有沒有把黑白對調。
//
// 預設是 [0 1]:取樣值 0 的地方上色。寫成 [1 0] 就整個反過來 ——
// 少判這一項的話,圖案與底色會對調,而畫面上仍然是一張看得懂的圖。
func inlineDecodeFlipped(dict map[string]value) bool {
	v, ok := dictOf(dict, "D", "Decode")
	if !ok || v.kind != vArray || len(v.arr) < 1 {
		return false
	}
	return v.arr[0].kind == vNum && v.arr[0].num == 1
}

// maskImage 把 1 位元的遮罩變成覆蓋率圖:要上色的地方是不透明的。
func maskImage(data []byte, w, h int, flipped bool) image.Image {
	m := image.NewAlpha(image.Rect(0, 0, w, h))
	rowBytes := (w + 7) / 8
	for y := 0; y < h; y++ {
		row := y * rowBytes
		for x := 0; x < w; x++ {
			i := row + x/8
			if i >= len(data) {
				break
			}
			bit := data[i]>>(7-uint(x%8))&1 == 1
			if bit == flipped {
				m.SetAlpha(x, y, color.Alpha{A: 255})
			}
		}
	}
	return m
}

// samplesImage 把原始取樣值換成影像。
func samplesImage(data []byte, w, h, bpc int, cs *colorSpace) image.Image {
	if cs == nil {
		cs = csDeviceGray
	}
	n := cs.n
	if n <= 0 {
		n = 1
	}
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	rowBits := w * n * bpc
	rowBytes := (rowBits + 7) / 8
	maxVal := float64(int(1)<<uint(bpc)) - 1
	comps := make([]float64, n)
	for y := 0; y < h; y++ {
		base := y * rowBytes
		for x := 0; x < w; x++ {
			for c := 0; c < n; c++ {
				bit := (x*n + c) * bpc
				v := readBits(data, base*8+bit, bpc)
				if cs.kind == csIndexed {
					comps[c] = float64(v)
				} else {
					comps[c] = float64(v) / maxVal
				}
			}
			img.SetRGBA(x, y, cs.color(comps).rgba(1))
		}
	}
	return img
}

// readBits 從位元流裡取 n 個位元。影像的取樣值不對齊位元組,
// 1、2、4 位元的影像都是好幾個像素擠在同一個位元組裡。
func readBits(data []byte, bitPos, n int) uint32 {
	var v uint32
	for i := 0; i < n; i++ {
		p := bitPos + i
		idx := p / 8
		if idx >= len(data) {
			return v << uint(n-i)
		}
		v = v<<1 | uint32(data[idx]>>(7-uint(p%8))&1)
	}
	return v
}

func unhexStream(b []byte) []byte {
	out := make([]byte, 0, len(b)/2)
	hi := -1
	for _, c := range b {
		if c == '>' {
			break
		}
		v := hexVal(c)
		if v < 0 {
			continue
		}
		if hi < 0 {
			hi = v
			continue
		}
		out = append(out, byte(hi*16+v))
		hi = -1
	}
	if hi >= 0 {
		out = append(out, byte(hi*16))
	}
	return out
}

// a85Decode 解 ASCII85。
func a85Decode(b []byte) []byte {
	out := make([]byte, 0, len(b)*4/5)
	var group [5]byte
	n := 0
	for i := 0; i < len(b); i++ {
		c := b[i]
		switch {
		case c == '~':
			i = len(b)
		case c == 'z' && n == 0:
			out = append(out, 0, 0, 0, 0)
			continue
		case c < '!' || c > 'u':
			continue
		default:
			group[n] = c - '!'
			n++
			if n == 5 {
				out = appendA85(out, group, 5)
				n = 0
			}
		}
	}
	if n > 1 {
		for i := n; i < 5; i++ {
			group[i] = 84
		}
		out = appendA85(out, group, n)
	}
	return out
}

func appendA85(out []byte, g [5]byte, n int) []byte {
	v := uint32(0)
	for _, c := range g {
		v = v*85 + uint32(c)
	}
	buf := [4]byte{byte(v >> 24), byte(v >> 16), byte(v >> 8), byte(v)}
	return append(out, buf[:n-1]...)
}

// runLengthDecode 解 PDF 的行程長度編碼。
func runLengthDecode(b []byte) []byte {
	out := make([]byte, 0, len(b)*2)
	for i := 0; i < len(b); {
		n := int(b[i])
		i++
		switch {
		case n == 128:
			return out
		case n < 128:
			end := i + n + 1
			if end > len(b) {
				end = len(b)
			}
			out = append(out, b[i:end]...)
			i = end
		default:
			if i >= len(b) {
				return out
			}
			for k := 0; k < 257-n; k++ {
				out = append(out, b[i])
			}
			i++
		}
	}
	return out
}
