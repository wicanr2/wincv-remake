package imgfmt

import (
	"image/color"
	"math"
	"testing"
)

func TestParseSVGTextBasic(t *testing.T) {
	src := `<svg viewBox="0 0 100 50">
	  <text x="10" y="20" font-size="12" fill="#ff0000" text-anchor="middle">你好 world</text>
	</svg>`
	got := parseSVGText([]byte(src))
	if len(got) != 1 {
		t.Fatalf("塊數 %d,預期 1", len(got))
	}
	c := got[0]
	if c.x != 10 || c.y != 20 {
		t.Errorf("位置 (%v,%v),預期 (10,20)", c.x, c.y)
	}
	if c.anchor != "middle" {
		t.Errorf("對齊 %q,預期 middle", c.anchor)
	}
	if len(c.pieces) != 1 || c.pieces[0].text != "你好 world" {
		t.Fatalf("內容 %+v", c.pieces)
	}
	p := c.pieces[0]
	if p.size != 12 {
		t.Errorf("字級 %v,預期 12", p.size)
	}
	if p.fill != (color.RGBA{255, 0, 0, 255}) {
		t.Errorf("顏色 %+v,預期紅色", p.fill)
	}
}

// 排版用的換行與縮排要折起來,而且不能在頭尾留下空白 ——
// 留著的話置中會整段偏掉,而畫面看起來只是「有點歪」。
func TestParseSVGTextCollapsesWhitespace(t *testing.T) {
	src := "<svg viewBox=\"0 0 10 10\"><text x=\"1\" y=\"2\">\n    甲   乙\n  </text></svg>"
	got := parseSVGText([]byte(src))
	if len(got) != 1 || got[0].pieces[0].text != "甲 乙" {
		t.Fatalf("得到 %q", got[0].pieces[0].text)
	}
}

// 有絕對座標的 tspan 會開一個新的文字塊。對齊是以塊為單位算的,
// 併成一塊的話兩段會疊在一起。
func TestParseSVGTextTspanChunks(t *testing.T) {
	src := `<svg viewBox="0 0 100 50"><text x="5" y="9" font-size="8">甲<tspan x="50">乙</tspan><tspan dx="3">丙</tspan></text></svg>`
	got := parseSVGText([]byte(src))
	if len(got) != 2 {
		t.Fatalf("塊數 %d,預期 2", len(got))
	}
	if got[0].x != 5 || got[0].pieces[0].text != "甲" {
		t.Errorf("第一塊 %+v", got[0])
	}
	// 第二塊從 x=50 起,dx=3 再往右挪;「丙」跟在「乙」後面同一塊。
	if got[1].x != 53 {
		t.Errorf("第二塊 x=%v,預期 53", got[1].x)
	}
	txt := ""
	for _, p := range got[1].pieces {
		txt += p.text
	}
	if txt != "乙丙" {
		t.Errorf("第二塊內容 %q", txt)
	}
}

// 樣式沿著元素樹往下繼承,style="" 蓋過同名的表現屬性。
func TestParseSVGTextInheritsAndStyleWins(t *testing.T) {
	src := `<svg viewBox="0 0 10 10">
	  <g font-size="20" fill="#00ff00" font-weight="700">
	    <text x="0" y="0">大</text>
	    <text x="0" y="0" fill="#0000ff" style="font-size:5px;fill:#111111">小</text>
	  </g>
	</svg>`
	got := parseSVGText([]byte(src))
	if len(got) != 2 {
		t.Fatalf("塊數 %d", len(got))
	}
	if got[0].pieces[0].size != 20 || got[0].pieces[0].fill != (color.RGBA{0, 255, 0, 255}) {
		t.Errorf("繼承失敗 %+v", got[0].pieces[0])
	}
	if !got[0].pieces[0].bold {
		t.Error("font-weight=700 應該是粗體")
	}
	if got[1].pieces[0].size != 5 || got[1].pieces[0].fill != (color.RGBA{0x11, 0x11, 0x11, 255}) {
		t.Errorf("style 沒有蓋過表現屬性 %+v", got[1].pieces[0])
	}
}

// <defs> 與 <title> 底下的文字是零件與說明,不是畫面內容。
// 畫出來的話會有一段字憑空出現在左上角。
func TestParseSVGTextSkipsNonContent(t *testing.T) {
	src := `<svg viewBox="0 0 10 10">
	  <title>檔案標題</title>
	  <defs><text x="1" y="1">零件</text></defs>
	  <g display="none"><text x="2" y="2">隱藏</text></g>
	  <text x="3" y="3">看得到</text>
	</svg>`
	got := parseSVGText([]byte(src))
	if len(got) != 1 || got[0].pieces[0].text != "看得到" {
		t.Fatalf("得到 %d 塊:%+v", len(got), got)
	}
}

func TestParseSVGTransform(t *testing.T) {
	cases := []struct {
		in     string
		x, y   float64
		wx, wy float64
	}{
		{"translate(10,5)", 1, 2, 11, 7},
		{"scale(2)", 3, 4, 6, 8},
		{"scale(2,3)", 3, 4, 6, 12},
		{"translate(10,0) scale(2)", 1, 0, 12, 0},
		{"matrix(1 0 0 1 5 5)", 0, 0, 5, 5},
		{"rotate(90)", 1, 0, 0, 1},
	}
	for _, c := range cases {
		m := parseSVGTransform(c.in)
		gx, gy := m.apply(c.x, c.y)
		if math.Abs(gx-c.wx) > 1e-9 || math.Abs(gy-c.wy) > 1e-9 {
			t.Errorf("%q:(%v,%v) → (%v,%v),預期 (%v,%v)", c.in, c.x, c.y, gx, gy, c.wx, c.wy)
		}
	}
}

// transform 沿著祖先累積。只看 <text> 自己那一個的話,
// 放在 <g transform> 裡的整組標籤會全部落在錯的地方。
func TestParseSVGTextInheritsTransform(t *testing.T) {
	src := `<svg viewBox="0 0 100 100"><g transform="translate(30,40)"><text x="1" y="2">字</text></g></svg>`
	got := parseSVGText([]byte(src))
	if len(got) != 1 {
		t.Fatalf("塊數 %d", len(got))
	}
	x, y := got[0].tm.apply(got[0].x, got[0].y)
	if x != 31 || y != 42 {
		t.Errorf("得到 (%v,%v),預期 (31,42)", x, y)
	}
}

func TestParseSVGColor(t *testing.T) {
	cases := []struct {
		in   string
		want color.RGBA
		ok   bool
	}{
		{"#abc", color.RGBA{0xaa, 0xbb, 0xcc, 255}, true},
		{"#1C1C1A", color.RGBA{0x1c, 0x1c, 0x1a, 255}, true},
		{"rgb(10, 20, 30)", color.RGBA{10, 20, 30, 255}, true},
		{"rgba(10,20,30,0.5)", color.RGBA{10, 20, 30, 128}, true},
		{"red", color.RGBA{255, 0, 0, 255}, true},
		{"url(#grad)", color.RGBA{}, false},
	}
	for _, c := range cases {
		got, ok := parseSVGColor(c.in)
		if ok != c.ok || (ok && got != c.want) {
			t.Errorf("%q → %+v,%v;預期 %+v,%v", c.in, got, ok, c.want, c.ok)
		}
	}
}

func TestParseLength(t *testing.T) {
	cases := []struct {
		in   string
		rel  float64
		want float64
	}{
		{"12", 16, 12},
		{"12px", 16, 12},
		{".08em", 10, 0.8},
		{"50%", 20, 10},
		{"12pt", 16, 16},
	}
	for _, c := range cases {
		if got := parseLength(c.in, c.rel, -1); math.Abs(got-c.want) > 1e-9 {
			t.Errorf("%q(rel=%v) → %v,預期 %v", c.in, c.rel, got, c.want)
		}
	}
}
