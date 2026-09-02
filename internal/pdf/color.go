package pdf

import (
	"image/color"
	"math"

	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"
)

// rgb 是 0–1 的顏色。留在浮點是因為 PDF 的顏色運算(CMYK 轉換、
// 索引查表)都在這個範圍做,最後才量化成位元組。
type rgb struct{ r, g, b float64 }

// rgba 換成 Go 的顏色。
//
// [雷] `color.RGBA` 依定義是**預乘**的:各通道已經乘過 alpha。不乘的話,
// 半透明的紅 `{255,0,0,128}` 交給光柵器會畫成灰色而不是淡紅 —— 顏色完全
// 不對,但畫出來仍然是一塊實心的方塊,不會有任何錯誤。
// (`image/draw` 與 `x/image/vector` 全都照這個定義走。)
func (c rgb) rgba(alpha float64) color.RGBA {
	a := math.Min(math.Max(alpha, 0), 1)
	f := func(v float64) uint8 {
		return uint8(math.Round(math.Min(math.Max(v, 0), 1) * a * 255))
	}
	return color.RGBA{f(c.r), f(c.g), f(c.b), uint8(math.Round(a * 255))}
}

func gray(v float64) rgb { return rgb{v, v, v} }

// cmyk 換成 RGB。用的是最簡單的那個公式,不走色彩管理 ——
// 走了也不會更準:PDF 裡的 CMYK 多半沒有附輸出裝置的描述檔。
func cmyk(c, m, y, k float64) rgb {
	return rgb{(1 - c) * (1 - k), (1 - m) * (1 - k), (1 - y) * (1 - k)}
}

type csKind int

const (
	csGray csKind = iota
	csRGB
	csCMYK
	csIndexed
	csSeparation
	csPattern
)

// colorSpace 是一個色彩空間。
type colorSpace struct {
	kind csKind
	n    int // 幾個分量

	// 索引色
	base   *colorSpace
	hival  int
	lookup []byte
}

var (
	csDeviceGray = &colorSpace{kind: csGray, n: 1}
	csDeviceRGB  = &colorSpace{kind: csRGB, n: 3}
	csDeviceCMYK = &colorSpace{kind: csCMYK, n: 4}
	csPatternCS  = &colorSpace{kind: csPattern, n: 1}
)

// initial 是選定一個色彩空間之後的預設顏色(規格說是黑)。
func (cs *colorSpace) initial() rgb {
	if cs == nil {
		return rgb{}
	}
	if cs.kind == csCMYK {
		return cmyk(0, 0, 0, 1)
	}
	return rgb{}
}

// color 把一組分量換成 RGB。
func (cs *colorSpace) color(v []float64) rgb {
	if cs == nil {
		cs = csDeviceGray
	}
	// 分量比預期多的時候取最後幾個:`scn` 可能在數字後面再帶一個
	// 圖樣名稱,而那個名稱不是數字,已經在上一層濾掉了。
	if len(v) > cs.n {
		v = v[len(v)-cs.n:]
	}
	switch cs.kind {
	case csRGB:
		if len(v) >= 3 {
			return rgb{v[0], v[1], v[2]}
		}
	case csCMYK:
		if len(v) >= 4 {
			return cmyk(v[0], v[1], v[2], v[3])
		}
	case csIndexed:
		if len(v) >= 1 {
			return cs.indexed(int(v[0]))
		}
	case csSeparation:
		// 分離色的正確算法要跑一個 PDF 函式把濃度換算到替代色彩空間。
		// 這裡取「濃度就是黑的濃度」—— 對最常見的專色黑與 All 是對的,
		// 對彩色專色會變成灰階。比整片不畫接近原樣。
		if len(v) >= 1 {
			t := v[0]
			for _, x := range v {
				t = math.Max(t, x)
			}
			return gray(1 - t)
		}
	case csPattern:
		return rgb{}
	default:
		if len(v) >= 1 {
			return gray(v[0])
		}
	}
	return rgb{}
}

func (cs *colorSpace) indexed(i int) rgb {
	base := cs.base
	if base == nil {
		base = csDeviceRGB
	}
	if i < 0 {
		i = 0
	}
	if i > cs.hival {
		i = cs.hival
	}
	off := i * base.n
	if off+base.n > len(cs.lookup) {
		return rgb{}
	}
	comps := make([]float64, base.n)
	for j := 0; j < base.n; j++ {
		comps[j] = float64(cs.lookup[off+j]) / 255
	}
	return base.color(comps)
}

// colorSpace 由名稱查出色彩空間。
func (in *interp) colorSpace(res types.Dict, name string) *colorSpace {
	switch name {
	case "DeviceGray", "G", "CalGray":
		return csDeviceGray
	case "DeviceRGB", "RGB", "CalRGB":
		return csDeviceRGB
	case "DeviceCMYK", "CMYK":
		return csDeviceCMYK
	case "Pattern":
		return csPatternCS
	}
	all, _ := deref(in.x, res["ColorSpace"]).(types.Dict)
	if all == nil {
		return csDeviceGray
	}
	return in.parseColorSpace(deref(in.x, all[name]), 0)
}

// parseColorSpace 解一個色彩空間物件。depth 擋住互相參照的檔案。
func (in *interp) parseColorSpace(o types.Object, depth int) *colorSpace {
	if depth > 4 {
		return csDeviceGray
	}
	switch v := o.(type) {
	case types.Name:
		switch v.Value() {
		case "DeviceRGB", "CalRGB", "RGB":
			return csDeviceRGB
		case "DeviceCMYK", "CMYK":
			return csDeviceCMYK
		case "Pattern":
			return csPatternCS
		}
		return csDeviceGray
	case types.Array:
		if len(v) == 0 {
			return csDeviceGray
		}
		switch nameOf(deref(in.x, v[0])) {
		case "ICCBased":
			// ICC 描述檔本身不解,照它宣告的分量數當成對應的裝置空間。
			// 這是規格給的替代做法,而且與檔案裡的資料一致。
			n := 3
			if sd, _, err := in.x.DereferenceStreamDict(v[1]); err == nil && sd != nil {
				if k, ok := numOf(deref(in.x, sd.Dict["N"])); ok {
					n = int(k)
				}
			}
			switch n {
			case 1:
				return csDeviceGray
			case 4:
				return csDeviceCMYK
			}
			return csDeviceRGB
		case "Indexed", "I":
			if len(v) < 4 {
				return csDeviceGray
			}
			cs := &colorSpace{kind: csIndexed, n: 1}
			cs.base = in.parseColorSpace(deref(in.x, v[1]), depth+1)
			if h, ok := numOf(deref(in.x, v[2])); ok {
				cs.hival = int(h)
			}
			cs.lookup = in.lookupTable(v[3])
			return cs
		case "Separation":
			return &colorSpace{kind: csSeparation, n: 1}
		case "DeviceN":
			n := 1
			if arr, ok := deref(in.x, v[1]).(types.Array); ok {
				n = len(arr)
			}
			return &colorSpace{kind: csSeparation, n: n}
		case "CalGray":
			return csDeviceGray
		case "CalRGB", "Lab":
			return csDeviceRGB
		case "Pattern":
			return csPatternCS
		case "DeviceGray":
			return csDeviceGray
		case "DeviceRGB":
			return csDeviceRGB
		case "DeviceCMYK":
			return csDeviceCMYK
		}
	}
	return csDeviceGray
}

// lookupTable 取索引色的調色盤。它可以寫成字串,也可以是一條串流。
func (in *interp) lookupTable(o types.Object) []byte {
	switch v := deref(in.x, o).(type) {
	case types.StringLiteral:
		s, _ := types.StringLiteralToString(v)
		return []byte(s)
	case types.HexLiteral:
		s, _ := types.HexLiteralToString(v)
		return []byte(s)
	}
	return streamBytes(in.x, o)
}
