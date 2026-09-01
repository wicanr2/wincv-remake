package pdf

import (
	"math"

	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"
)

// PDF 的「函式」是把一組數字換成另一組數字的規則,漸層的顏色就是這樣算的:
// 沿著漸層軸的位置 t 丟進去,吐出一組顏色分量。
//
// 四種型別裡實作了三種。第 4 種是一小段 PostScript 程式,要一個直譯器才跑得動;
// 遇到它會回 nil,而上層看到 nil 就不畫那個漸層 —— 留白比畫出一片猜來的顏色好。
type pdfFunc interface {
	// eval 把一個輸入換成一組輸出。
	eval(t float64) []float64
}

// loadFunc 讀一個函式物件。可以是一個函式,也可以是一組各出一個分量的函式。
func loadFunc(x *model.XRefTable, o types.Object) pdfFunc {
	switch v := deref(x, o).(type) {
	case types.Array:
		var fs []pdfFunc
		for _, e := range v {
			f := loadFunc(x, e)
			if f == nil {
				return nil
			}
			fs = append(fs, f)
		}
		if len(fs) == 0 {
			return nil
		}
		return funcArray(fs)
	case types.Dict:
		return loadFuncDict(x, v, nil)
	case types.StreamDict:
		return loadFuncDict(x, v.Dict, streamBytes(x, o))
	}
	// 串流型的函式(第 0 與第 4 型)要走串流的入口。
	if sd, _, err := x.DereferenceStreamDict(o); err == nil && sd != nil {
		return loadFuncDict(x, sd.Dict, streamBytes(x, o))
	}
	return nil
}

func loadFuncDict(x *model.XRefTable, d types.Dict, data []byte) pdfFunc {
	domain := floatsOf(x, d["Domain"])
	if len(domain) < 2 {
		domain = []float64{0, 1}
	}
	switch int(numOrZero(x, d["FunctionType"])) {
	case 2:
		f := &expFunc{n: 1, domain: [2]float64{domain[0], domain[1]}}
		f.c0 = floatsOf(x, d["C0"])
		f.c1 = floatsOf(x, d["C1"])
		if len(f.c0) == 0 {
			f.c0 = []float64{0}
		}
		if len(f.c1) == 0 {
			f.c1 = []float64{1}
		}
		if v, ok := numOf(deref(x, d["N"])); ok && v != 0 {
			f.n = v
		}
		return f
	case 3:
		f := &stitchFunc{domain: [2]float64{domain[0], domain[1]}}
		arr, _ := deref(x, d["Functions"]).(types.Array)
		for _, e := range arr {
			sub := loadFunc(x, e)
			if sub == nil {
				return nil
			}
			f.subs = append(f.subs, sub)
		}
		f.bounds = floatsOf(x, d["Bounds"])
		f.encode = floatsOf(x, d["Encode"])
		if len(f.subs) == 0 {
			return nil
		}
		return f
	case 0:
		return loadSampledFunc(x, d, data, domain)
	}
	return nil
}

// funcArray 是「每個分量各一個函式」的組合。
type funcArray []pdfFunc

func (fs funcArray) eval(t float64) []float64 {
	out := make([]float64, 0, len(fs))
	for _, f := range fs {
		v := f.eval(t)
		if len(v) == 0 {
			out = append(out, 0)
			continue
		}
		out = append(out, v[0])
	}
	return out
}

// expFunc 是指數內插:C0 與 C1 之間,依 t^N 走。
type expFunc struct {
	domain [2]float64
	c0, c1 []float64
	n      float64
}

func (f *expFunc) eval(t float64) []float64 {
	t = clampF(t, f.domain[0], f.domain[1])
	// 定義域不是 0–1 時要先正規化,不然指數會套在錯的尺度上。
	if d := f.domain[1] - f.domain[0]; d != 0 {
		t = (t - f.domain[0]) / d
	}
	k := t
	if f.n != 1 {
		k = math.Pow(t, f.n)
	}
	n := len(f.c0)
	if len(f.c1) < n {
		n = len(f.c1)
	}
	out := make([]float64, n)
	for i := 0; i < n; i++ {
		out[i] = f.c0[i] + k*(f.c1[i]-f.c0[i])
	}
	return out
}

// stitchFunc 把好幾個函式接在一起,各管一段定義域。
type stitchFunc struct {
	domain [2]float64
	subs   []pdfFunc
	bounds []float64
	encode []float64
}

func (f *stitchFunc) eval(t float64) []float64 {
	lo, hi := f.domain[0], f.domain[1]
	t = clampF(t, lo, hi)
	i := 0
	for i < len(f.bounds) && t >= f.bounds[i] {
		i++
	}
	if i >= len(f.subs) {
		i = len(f.subs) - 1
	}
	// 這一段的範圍。
	segLo, segHi := lo, hi
	if i > 0 {
		segLo = f.bounds[i-1]
	}
	if i < len(f.bounds) {
		segHi = f.bounds[i]
	}
	// 每一段各自有自己的輸入區間(Encode),不一定是 0–1。
	e0, e1 := 0.0, 1.0
	if 2*i+1 < len(f.encode) {
		e0, e1 = f.encode[2*i], f.encode[2*i+1]
	}
	u := e0
	if segHi != segLo {
		u = e0 + (t-segLo)/(segHi-segLo)*(e1-e0)
	}
	return f.subs[i].eval(u)
}

// sampledFunc 是查表加線性內插。表存在串流裡,每個取樣值可能只有幾個位元。
type sampledFunc struct {
	domain  [2]float64
	size    int
	bps     int
	nOut    int
	encode  [2]float64
	decode  []float64
	rng     []float64
	samples []byte
}

func loadSampledFunc(x *model.XRefTable, d types.Dict, data []byte, domain []float64) pdfFunc {
	size := floatsOf(x, d["Size"])
	rng := floatsOf(x, d["Range"])
	if len(size) < 1 || len(rng) < 2 || len(data) == 0 {
		return nil
	}
	f := &sampledFunc{
		domain: [2]float64{domain[0], domain[1]},
		size:   int(size[0]),
		bps:    int(numOrZero(x, d["BitsPerSample"])),
		nOut:   len(rng) / 2,
		rng:    rng,
		encode: [2]float64{0, float64(int(size[0]) - 1)},
		decode: floatsOf(x, d["Decode"]),
	}
	if enc := floatsOf(x, d["Encode"]); len(enc) >= 2 {
		f.encode = [2]float64{enc[0], enc[1]}
	}
	if f.size < 1 || f.bps < 1 || f.nOut < 1 {
		return nil
	}
	f.samples = data
	return f
}

func (f *sampledFunc) eval(t float64) []float64 {
	t = clampF(t, f.domain[0], f.domain[1])
	u := f.encode[0]
	if d := f.domain[1] - f.domain[0]; d != 0 {
		u = f.encode[0] + (t-f.domain[0])/d*(f.encode[1]-f.encode[0])
	}
	u = clampF(u, 0, float64(f.size-1))
	i0 := int(math.Floor(u))
	i1 := i0 + 1
	if i1 > f.size-1 {
		i1 = f.size - 1
	}
	frac := u - float64(i0)
	maxVal := math.Pow(2, float64(f.bps)) - 1
	out := make([]float64, f.nOut)
	for j := 0; j < f.nOut; j++ {
		a := float64(readBits(f.samples, (i0*f.nOut+j)*f.bps, f.bps))
		b := float64(readBits(f.samples, (i1*f.nOut+j)*f.bps, f.bps))
		v := (a + (b-a)*frac) / maxVal
		lo, hi := f.rng[2*j], f.rng[2*j+1]
		if len(f.decode) >= 2*j+2 {
			lo, hi = f.decode[2*j], f.decode[2*j+1]
		}
		out[j] = lo + v*(hi-lo)
	}
	return out
}

func clampF(v, lo, hi float64) float64 {
	if lo > hi {
		lo, hi = hi, lo
	}
	return math.Min(math.Max(v, lo), hi)
}

func floatsOf(x *model.XRefTable, o types.Object) []float64 {
	arr, ok := deref(x, o).(types.Array)
	if !ok {
		return nil
	}
	out := make([]float64, 0, len(arr))
	for _, e := range arr {
		v, _ := numOf(deref(x, e))
		out = append(out, v)
	}
	return out
}

func numOrZero(x *model.XRefTable, o types.Object) float64 {
	v, _ := numOf(deref(x, o))
	return v
}
