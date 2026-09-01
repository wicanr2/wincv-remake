package pdf

import (
	"math"
	"testing"
)

func near(a, b float64) bool { return math.Abs(a-b) < 1e-9 }

func evalAt(t *testing.T, f pdfFunc, in float64, want ...float64) {
	t.Helper()
	got := f.eval(in)
	if len(got) != len(want) {
		t.Fatalf("t=%g 回了 %d 個分量,期待 %d 個(%v)", in, len(got), len(want), got)
	}
	for i := range want {
		if !near(got[i], want[i]) {
			t.Errorf("t=%g 第 %d 個分量 = %g,期待 %g", in, i, got[i], want[i])
		}
	}
}

// 指數函式:N=1 是線性,N≠1 走冪次,定義域不是 0–1 時要先正規化。
func TestExpFunc(t *testing.T) {
	lin := &expFunc{domain: [2]float64{0, 1}, c0: []float64{1, 0, 0}, c1: []float64{0, 0, 1}, n: 1}
	evalAt(t, lin, 0, 1, 0, 0)
	evalAt(t, lin, 0.25, 0.75, 0, 0.25)
	evalAt(t, lin, 1, 0, 0, 1)
	// 超出定義域要夾住,不是外推。
	evalAt(t, lin, 2, 0, 0, 1)
	evalAt(t, lin, -1, 1, 0, 0)

	sq := &expFunc{domain: [2]float64{0, 1}, c0: []float64{0}, c1: []float64{1}, n: 2}
	evalAt(t, sq, 0.5, 0.25)

	// [雷] 定義域 [0,2] 時,t=1 是中點,結果該是 0.5 而不是 1。
	// 沒有正規化的話冪次會套在錯的尺度上,而 N=1 時看不出來。
	wide := &expFunc{domain: [2]float64{0, 2}, c0: []float64{0}, c1: []float64{1}, n: 1}
	evalAt(t, wide, 1, 0.5)
}

// 接合函式:各段有自己的輸入區間(Encode),可以是反的。
func TestStitchFunc(t *testing.T) {
	up := &expFunc{domain: [2]float64{0, 1}, c0: []float64{0}, c1: []float64{1}, n: 1}
	f := &stitchFunc{
		domain: [2]float64{0, 1},
		subs:   []pdfFunc{up, up},
		bounds: []float64{0.5},
		encode: []float64{0, 1, 1, 0}, // 第二段的輸入是反的
	}
	evalAt(t, f, 0, 0)
	evalAt(t, f, 0.25, 0.5) // 第一段走到一半
	evalAt(t, f, 0.5, 1)    // 第二段起點,Encode 是 1
	evalAt(t, f, 0.75, 0.5) // 反著走回來
	evalAt(t, f, 1, 0)

	// 沒有 Encode 時,每一段都是 0–1。
	g := &stitchFunc{domain: [2]float64{0, 1}, subs: []pdfFunc{up, up}, bounds: []float64{0.5}}
	evalAt(t, g, 0.25, 0.5)
	evalAt(t, g, 0.75, 0.5)
}

// 取樣函式:表在取樣點上要剛好等於樣本值,中間是線性內插。
func TestSampledFunc(t *testing.T) {
	f := &sampledFunc{
		domain: [2]float64{0, 1},
		size:   4, bps: 8, nOut: 3,
		encode:  [2]float64{0, 3},
		rng:     []float64{0, 1, 0, 1, 0, 1},
		samples: []byte{255, 0, 0, 0, 0, 255, 0, 255, 0, 255, 255, 0},
	}
	evalAt(t, f, 0, 1, 0, 0)
	evalAt(t, f, 1.0/3, 0, 0, 1)
	evalAt(t, f, 2.0/3, 0, 1, 0)
	evalAt(t, f, 1, 1, 1, 0)
	// 前兩個樣本正中間。
	evalAt(t, f, 1.0/6, 0.5, 0, 0.5)

	// 每個樣本只有 4 個位元的情形。0xF0 = 兩個樣本 15 與 0。
	half := &sampledFunc{
		domain: [2]float64{0, 1},
		size:   2, bps: 4, nOut: 1,
		encode:  [2]float64{0, 1},
		rng:     []float64{0, 1},
		samples: []byte{0xF0},
	}
	evalAt(t, half, 0, 1)
	evalAt(t, half, 1, 0)
	evalAt(t, half, 0.5, 0.5)

	// Decode 會改寫輸出範圍。
	dec := &sampledFunc{
		domain: [2]float64{0, 1},
		size:   2, bps: 8, nOut: 1,
		encode:  [2]float64{0, 1},
		rng:     []float64{0, 1},
		decode:  []float64{-1, 1},
		samples: []byte{0, 255},
	}
	evalAt(t, dec, 0, -1)
	evalAt(t, dec, 1, 1)
}

// 一組各出一個分量的函式。
func TestFuncArray(t *testing.T) {
	a := &expFunc{domain: [2]float64{0, 1}, c0: []float64{0}, c1: []float64{1}, n: 1}
	b := &expFunc{domain: [2]float64{0, 1}, c0: []float64{1}, c1: []float64{0}, n: 1}
	evalAt(t, funcArray{a, b, a}, 0.25, 0.25, 0.75, 0.25)
}

// 軸向漸層:把一點投影到軸上。
func TestAxialParam(t *testing.T) {
	sh := &shading{typ: 2, coords: []float64{100, 0, 300, 0}, extend: [2]bool{true, true}}
	for _, c := range []struct {
		x, want float64
	}{{100, 0}, {200, 0.5}, {300, 1}, {0, 0}, {400, 1}} {
		got, ok := sh.axialParam(c.x, 50)
		if !ok || !near(got, c.want) {
			t.Errorf("x=%g → %g(%v),期待 %g", c.x, got, ok, c.want)
		}
	}
	// 沒有延伸時,軸外的點不該畫。
	noExt := &shading{typ: 2, coords: []float64{100, 0, 300, 0}}
	if _, ok := noExt.axialParam(50, 0); ok {
		t.Error("軸的前面沒有 Extend,不該畫")
	}
	if _, ok := noExt.axialParam(350, 0); ok {
		t.Error("軸的後面沒有 Extend,不該畫")
	}
	// 長度為零的軸算不出方向。
	zero := &shading{typ: 2, coords: []float64{10, 10, 10, 10}}
	if _, ok := zero.axialParam(0, 0); ok {
		t.Error("零長度的軸不該畫")
	}
}

// 放射漸層:解「這一點落在哪一個圓上」。
func TestRadialParam(t *testing.T) {
	// 同心圓,內圓半徑 0、外圓 100:參數就是距離除以 100。
	con := &shading{typ: 3, coords: []float64{0, 0, 0, 0, 0, 100}, extend: [2]bool{false, true}}
	for _, c := range []struct{ d, want float64 }{{0, 0}, {50, 0.5}, {100, 1}} {
		got, ok := con.radialParam(c.d, 0)
		if !ok || !near(got, c.want) {
			t.Errorf("距離 %g → %g(%v),期待 %g", c.d, got, ok, c.want)
		}
	}
	// 圓外有 Extend,夾到 1。
	if got, ok := con.radialParam(500, 0); !ok || !near(got, 1) {
		t.Errorf("圓外應該延伸成 1,拿到 %g(%v)", got, ok)
	}
	noExt := &shading{typ: 3, coords: []float64{0, 0, 0, 0, 0, 100}}
	if _, ok := noExt.radialParam(500, 0); ok {
		t.Error("沒有 Extend 時圓外不該畫")
	}

	// 兩圓不同心且 dr 等於圓心距:二次項為零,走一次式那條路。
	// 點 (50,0) 落在 s=0.25 的圓上 —— 圓心 (25,0)、半徑 25。
	lin := &shading{typ: 3, coords: []float64{0, 0, 0, 100, 0, 100}, extend: [2]bool{true, true}}
	if got, ok := lin.radialParam(50, 0); !ok || !near(got, 0.25) {
		t.Errorf("一次式的解 = %g(%v),期待 0.25", got, ok)
	}
}

// 看不懂的漸層與函式一律不畫。留白比畫出一片猜來的顏色好,
// 而且更重要的是別把整頁弄掛。
func TestUnsupportedShadingDrawsNothing(t *testing.T) {
	r := renderPage(t, "testdata/shading-unsupported.pdf", 1, RenderOptions{DPI: 96})
	if got := inkRatio(r.Img); got != 0 {
		t.Errorf("這一頁該是全白的,墨水比例 %.4f", got)
	}
}
