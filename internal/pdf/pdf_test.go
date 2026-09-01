package pdf

import (
	"strings"
	"testing"
)

func lexAll(s string) []value {
	l := &lexer{b: []byte(s)}
	var out []value
	for {
		v, ok := l.next()
		if !ok {
			return out
		}
		out = append(out, v)
	}
}

func TestLexStrings(t *testing.T) {
	// 最後一個是奇數個十六進位數字:規格說補一個 0 在後面,
	// 所以 <414> 是 0x41 0x40,不是 0x41 0x04。
	got := lexAll(`(plain) (a\(b\)c) (oct\101) (line\
continued) <48656C6C6F> <414>`)
	want := []string{"plain", "a(b)c", "octA", "linecontinued", "Hello", "A@"}
	if len(got) != len(want) {
		t.Fatalf("拿到 %d 個 token:%+v", len(got), got)
	}
	for i := range want {
		if got[i].kind != vStr || got[i].str != want[i] {
			t.Errorf("第 %d 個是 %q,要 %q", i, got[i].str, want[i])
		}
	}
}

func TestLexArraysAndOps(t *testing.T) {
	got := lexAll("[(a) -12.5 (b)] TJ /F1 12 Tf")
	if len(got) != 5 {
		t.Fatalf("拿到 %+v", got)
	}
	if got[0].kind != vArray || len(got[0].arr) != 3 {
		t.Fatalf("陣列不對:%+v", got[0])
	}
	if got[0].arr[1].num != -12.5 {
		t.Errorf("負數解成 %v", got[0].arr[1].num)
	}
	if got[1].kind != vOp || got[1].str != "TJ" {
		t.Errorf("運算子是 %+v", got[1])
	}
	if got[2].kind != vName || got[2].str != "F1" {
		t.Errorf("名稱是 %+v", got[2])
	}
}

// 內嵌影像的資料是原始位元組,裡面什麼都可能有。不整段跳過的話,
// 後面會從影像資料中間開始解讀,而那會解出一串合法但無意義的運算子。
func TestSkipInlineImage(t *testing.T) {
	l := &lexer{b: []byte("BI /W 2 /H 2 ID \x00(fake) Tj\xff\xfe EI (after) Tj")}
	v, _ := l.next()
	if v.str != "BI" {
		t.Fatalf("第一個 token 是 %+v", v)
	}
	l.skipInlineImage()
	got, _ := l.next()
	if got.kind != vStr || got.str != "after" {
		t.Fatalf("跳過影像之後拿到 %+v", got)
	}
}

func TestCMapBFRange(t *testing.T) {
	c := parseCMap([]byte(`
	1 begincodespacerange <0000> <FFFF> endcodespacerange
	2 beginbfchar <0003> <0020> <0004> <4E2D> endbfchar
	2 beginbfrange
	<0010> <0012> <0041>
	<0020> <0021> [<0058> <0059>]
	endbfrange`))
	for _, tc := range []struct {
		code uint32
		want string
	}{{3, " "}, {4, "中"}, {0x10, "A"}, {0x11, "B"}, {0x12, "C"}, {0x20, "X"}, {0x21, "Y"}} {
		got, ok := c.lookup(tc.code)
		if !ok || got != tc.want {
			t.Errorf("碼 %04X 解成 %q(%v),要 %q", tc.code, got, ok, tc.want)
		}
	}
	if len(c.spaces) != 1 || c.spaces[0].n != 2 {
		t.Errorf("碼長沒讀出來:%+v", c.spaces)
	}
}

// 一份 CMap 裡可以同時有一個位元組與兩個位元組的碼,長度只能靠
// 「這個值落在哪一段」判斷。切錯的話整串都會錯開。
func TestCMapMixedCodeLengths(t *testing.T) {
	c := parseCMap([]byte(`
	2 begincodespacerange <00> <80> <8140> <FEFE> endcodespacerange
	endcodespacerange`))
	got := c.split("A\x81\x40B", 1)
	want := []code{{0x41, 1}, {0x8140, 2}, {0x42, 1}}
	if len(got) != len(want) {
		t.Fatalf("切成 %+v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("第 %d 個是 %+v,要 %+v", i, got[i], want[i])
		}
	}
}

func TestGlyphNames(t *testing.T) {
	for name, want := range map[string]string{
		"space": " ", "quoteright": "’", "fi": "ﬁ",
		"uni4E2D": "中", "u20AC": "€",
		"eacute": "é", "Ocircumflex": "Ô", "ccedilla": "ç",
		"A": "A", "a.sc": "a",
		"g123": "",
	} {
		if got := glyphRune(name); got != want {
			t.Errorf("glyphRune(%q)=%q,要 %q", name, got, want)
		}
	}
}

func TestCoreFontNames(t *testing.T) {
	for name, want := range map[string]string{
		"Helvetica":            "Helvetica",
		"ABCDEF+Helvetica":     "Helvetica",
		"Arial,Bold":           "Helvetica-Bold",
		"Arial-BoldItalicMT":   "Helvetica-BoldOblique",
		"TimesNewRomanPSMT":    "Times-Roman",
		"TimesNewRomanPS-Bold": "Times-Bold",
		"CourierNew":           "Courier",
	} {
		if got := coreFontName(name); got != want {
			t.Errorf("coreFontName(%q)=%q,要 %q", name, got, want)
		}
	}
}

// 真檔:LibreOffice 產生的 PDF 用子集化的 CID 字型,
// 字碼是字型內部的編號,要走 ToUnicode 才變得回文字。
func TestRealPageTexts(t *testing.T) {
	d, err := Open("testdata/rich.pdf")
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	p, err := d.Page(1)
	if err != nil {
		t.Fatal(err)
	}
	var sb strings.Builder
	for _, tx := range p.Texts() {
		sb.WriteString(tx.S)
	}
	got := sb.String()
	for _, want := range []string{"第一章", "粗體", "A plain ASCII"} {
		if !strings.Contains(got, want) {
			t.Errorf("少了 %q:%q", want, got)
		}
	}
	if p.Width() < 500 || p.Height() < 700 {
		t.Errorf("頁面尺寸是 %.0f×%.0f", p.Width(), p.Height())
	}
}

func TestRunawayFileIsRejected(t *testing.T) {
	// 40 個位元組的垃圾就能讓物件層無止境地掃下去。
	if _, err := Open("testdata/README.md"); err == nil {
		t.Fatal("不是 PDF 應該要失敗")
	}
}
