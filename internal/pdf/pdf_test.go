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

// 內嵌影像的資料是原始位元組,裡面什麼都可能有 —— 包括看起來像運算子的
// 東西,也包括 "EI" 這兩個字母。算得出長度時就照算的走,不去掃描。
func TestReadInlineImage(t *testing.T) {
	// 2×2 的灰階影像,每個像素一個位元組。資料裡故意放一個 " EI " ——
	// 掃描法會在那裡提早結束,照長度走的不會。
	data := " EI "
	l := &lexer{b: []byte("BI /W 2 /H 2 /CS /G /BPC 8 ID " + data + "\nEI (after) Tj")}
	v, _ := l.next()
	if v.str != "BI" {
		t.Fatalf("第一個 token 是 %+v", v)
	}
	dict, got, ok := l.readInlineImage()
	if !ok {
		t.Fatal("內嵌影像讀不出來")
	}
	if string(got) != data {
		t.Errorf("影像資料是 %q,要 %q", got, data)
	}
	if dict["W"].num != 2 || dict["H"].num != 2 || dict["CS"].str != "G" {
		t.Errorf("參數字典不對:%+v", dict)
	}
	next, _ := l.next()
	if next.kind != vStr || next.str != "after" {
		t.Fatalf("影像之後拿到 %+v", next)
	}
}

// 有濾鏡時算不出長度,只能掃到 EI 為止。
func TestReadInlineImageScanned(t *testing.T) {
	l := &lexer{b: []byte("BI /W 2 /H 2 /F /AHx ID 41424344> EI (after) Tj")}
	l.next()
	_, got, ok := l.readInlineImage()
	if !ok || string(got) != "41424344>" {
		t.Fatalf("拿到 %q ok=%v", got, ok)
	}
	next, _ := l.next()
	if next.str != "after" {
		t.Fatalf("影像之後拿到 %+v", next)
	}
}

// 每一列補齊到整個位元組。1 位元的影像最容易踩到:寬 12 的一列是 2 個
// 位元組不是 1.5 個,少算的話整張圖會逐列斜掉。
func TestInlineDataLenRowPadding(t *testing.T) {
	for _, tc := range []struct {
		w, h, bpc int
		cs        string
		want      int
	}{
		{12, 3, 1, "G", 6},   // 每列 12 位元 → 2 個位元組
		{2, 2, 8, "RGB", 12}, // 每列 2 像素 × 3 分量
		{4, 4, 8, "RGB", 48},
		{8, 1, 4, "G", 4},
	} {
		d := map[string]value{
			"W":   {kind: vNum, num: float64(tc.w)},
			"H":   {kind: vNum, num: float64(tc.h)},
			"BPC": {kind: vNum, num: float64(tc.bpc)},
			"CS":  {kind: vName, str: tc.cs},
		}
		if got := inlineDataLen(d); got != tc.want {
			t.Errorf("%d×%d %d 位元 %s → %d,要 %d", tc.w, tc.h, tc.bpc, tc.cs, got, tc.want)
		}
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
