package pdf

import (
	"strings"

	pdffont "github.com/pdfcpu/pdfcpu/pkg/font"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"
)

// loadFont 把一份字型字典變成可以解碼的 Font。
func loadFont(x *model.XRefTable, d types.Dict) *Font {
	f := &Font{widths: map[uint32]float64{}}
	f.baseFont = nameOf(deref(x, d["BaseFont"]))
	sub := nameOf(deref(x, d["Subtype"]))
	if tu := streamBytes(x, d["ToUnicode"]); len(tu) > 0 {
		if c := parseCMap(tu); !c.empty() {
			f.toUni = c
		}
	}
	if sub == "Type0" {
		f.composite = true
		f.loadType0(x, d)
		return f
	}
	f.loadSimple(x, d)
	if sub == "Type3" {
		f.loadType3Scale(x, d)
	}
	return f
}

// loadType3Scale 算 Type3 字型的寬度換算倍率。
//
// [雷] Type3 字型的寬度不是千分之一字寬,而是它自己的座標系 ——
// FontMatrix 決定那個座標系有多大。照一般字型的方式解,前進量會
// 差好幾倍,整頁的字會疊在一起或散開,但每個字本身都是對的。
func (f *Font) loadType3Scale(x *model.XRefTable, d types.Dict) {
	arr, ok := deref(x, d["FontMatrix"]).(types.Array)
	if !ok || len(arr) < 1 {
		return
	}
	a, ok := numOf(deref(x, arr[0]))
	if !ok || a == 0 {
		return
	}
	f.scale = a * 1000
}

func (f *Font) loadType0(x *model.XRefTable, d types.Dict) {
	f.codeLen = 2
	switch enc := deref(x, d["Encoding"]).(type) {
	case types.Name:
		name := enc.Value()
		if e, ok := predefinedCMap(name); ok {
			// [雷] 這一類編碼的 CMap 檔案不在 PDF 裡,而它的字碼就是
			// 該語系傳統編碼的位元組。當成 Identity 解的話,每個中文字
			// 都會變成一個不存在的字碼 —— 而那不會出錯,只會全部解不出來。
			f.cjk = e
		}
	default:
		if b := streamBytes(x, d["Encoding"]); len(b) > 0 {
			if c := parseCMap(b); !c.empty() || len(c.spaces) > 0 {
				f.enc = c
			}
		}
	}
	desc := firstDescendant(x, d)
	if desc == nil {
		return
	}
	f.loadProgram(x, desc)
	// CIDToGIDMap 是串流時要查表;是 /Identity 或沒寫時 CID 就是字形編號。
	if b := streamBytes(x, desc["CIDToGIDMap"]); len(b) > 0 {
		f.cidToGID = b
	}
	f.dw = 1000
	if v, ok := numOf(deref(x, desc["DW"])); ok {
		f.dw = v
	}
	f.loadW(x, desc["W"])
}

// loadW 讀複合字型的寬度陣列。
//
// 它有兩種寫法混在同一個陣列裡:「起始碼 [寬度…]」逐一列出,
// 或「起始碼 結束碼 寬度」一整段同寬。分不清的話會把第二種的
// 結束碼當成寬度,得到一堆幾千單位寬的字。
func (f *Font) loadW(x *model.XRefTable, o types.Object) {
	arr, ok := deref(x, o).(types.Array)
	if !ok {
		return
	}
	for i := 0; i < len(arr); {
		first, ok := numOf(deref(x, arr[i]))
		if !ok {
			return
		}
		i++
		if i >= len(arr) {
			return
		}
		if list, ok := deref(x, arr[i]).(types.Array); ok {
			for j, w := range list {
				if v, ok := numOf(deref(x, w)); ok {
					f.widths[uint32(first)+uint32(j)] = v
				}
			}
			i++
			continue
		}
		last, ok := numOf(deref(x, arr[i]))
		if !ok {
			return
		}
		i++
		if i >= len(arr) {
			return
		}
		w, _ := numOf(deref(x, arr[i]))
		i++
		// 上限擋住損壞的檔案:一段幾百萬個字碼的範圍會把記憶體吃光。
		if last < first || last-first > 65536 {
			continue
		}
		for c := first; c <= last; c++ {
			f.widths[uint32(c)] = w
		}
	}
}

func (f *Font) loadSimple(x *model.XRefTable, d types.Dict) {
	base := coreFontName(nameOf(deref(x, d["BaseFont"])))
	if pdffont.IsCoreFont(base) {
		f.coreFont = base
	}

	encName := "StandardEncoding"
	if f.coreFont == "Symbol" || f.coreFont == "ZapfDingbats" {
		// 這兩個字型有自己的字形集,套 Latin 的編碼表只會全部解錯。
		encName = ""
		f.symbolFont = true
	}
	var diffs types.Array
	switch enc := deref(x, d["Encoding"]).(type) {
	case types.Name:
		encName = enc.Value()
	case types.Dict:
		if n := nameOf(enc["BaseEncoding"]); n != "" {
			encName = n
		}
		diffs, _ = deref(x, enc["Differences"]).(types.Array)
	}
	if encName != "" {
		f.simple = baseEncoding(encName)
	}
	// Differences 是「從第幾號開始,接下來這幾個字形依序是」。
	// 中間出現數字就是換一個起點。
	cur := 0
	for _, o := range diffs {
		switch v := deref(x, o).(type) {
		case types.Integer:
			cur = v.Value()
		case types.Float:
			cur = int(v.Value())
		case types.Name:
			if cur >= 0 && cur < 256 {
				f.names[cur] = v.Value()
				if s := glyphRune(v.Value()); s != "" {
					f.simple[cur] = s
				}
			}
			cur++
		}
	}

	first := 0
	if v, ok := numOf(deref(x, d["FirstChar"])); ok {
		first = int(v)
	}
	if arr, ok := deref(x, d["Widths"]).(types.Array); ok {
		for i, o := range arr {
			if w, ok := numOf(deref(x, o)); ok {
				f.widths[uint32(first+i)] = w
			}
		}
	}
	if fd, ok := deref(x, d["FontDescriptor"]).(types.Dict); ok {
		if v, ok := numOf(deref(x, fd["MissingWidth"])); ok {
			f.dw = v
		}
	}
	f.loadProgram(x, d)
}

// loadProgram 取出嵌在檔案裡的字型程式。
//
// 三個欄位各自對應一種格式,而且**只會有一個**:FontFile 是 Type1、
// FontFile2 是 TrueType、FontFile3 看它的 Subtype(OpenType 是完整的
// sfnt 容器,Type1C 與 CIDFontType0C 是裸的 CFF)。
func (f *Font) loadProgram(x *model.XRefTable, d types.Dict) {
	fd, ok := deref(x, d["FontDescriptor"]).(types.Dict)
	if !ok {
		return
	}
	if b := streamBytes(x, fd["FontFile2"]); len(b) > 0 {
		f.embedded, f.kind = b, progSFNT
		return
	}
	if sd, _, err := x.DereferenceStreamDict(fd["FontFile3"]); err == nil && sd != nil {
		b := streamBytes(x, fd["FontFile3"])
		if len(b) > 0 {
			f.embedded = b
			if nameOf(deref(x, sd.Dict["Subtype"])) == "OpenType" {
				f.kind = progSFNT
			} else {
				f.kind = progCFF
			}
			return
		}
	}
	if b := streamBytes(x, fd["FontFile"]); len(b) > 0 {
		f.embedded, f.kind = b, progType1
	}
}

func firstDescendant(x *model.XRefTable, d types.Dict) types.Dict {
	arr, ok := deref(x, d["DescendantFonts"]).(types.Array)
	if !ok || len(arr) == 0 {
		return nil
	}
	sub, _ := deref(x, arr[0]).(types.Dict)
	return sub
}

// coreFontName 把 BaseFont 的名字正規化成核心字型的名字。
//
// 三件事要處理:子集前綴(`ABCDEF+`)、逗號式的樣式後綴(`Arial,Bold`),
// 以及各家對同一個字型的不同叫法(Arial 就是 Helvetica 的度量)。
func coreFontName(name string) string {
	if i := strings.IndexByte(name, '+'); i == 6 {
		name = name[i+1:]
	}
	name = strings.ReplaceAll(name, ",", "-")
	if pdffont.IsCoreFont(name) {
		return name
	}
	lower := strings.ToLower(name)
	bold := strings.Contains(lower, "bold")
	italic := strings.Contains(lower, "italic") || strings.Contains(lower, "oblique")
	family := ""
	switch {
	case strings.Contains(lower, "arial"), strings.Contains(lower, "helvetica"):
		family = "Helvetica"
	case strings.Contains(lower, "times"):
		family = "Times"
	case strings.Contains(lower, "courier"):
		family = "Courier"
	case strings.Contains(lower, "symbol"):
		return "Symbol"
	case strings.Contains(lower, "zapf"), strings.Contains(lower, "dingbat"):
		return "ZapfDingbats"
	default:
		return name
	}
	if family == "Times" {
		switch {
		case bold && italic:
			return "Times-BoldItalic"
		case bold:
			return "Times-Bold"
		case italic:
			return "Times-Italic"
		}
		return "Times-Roman"
	}
	switch {
	case bold && italic:
		return family + "-BoldOblique"
	case bold:
		return family + "-Bold"
	case italic:
		return family + "-Oblique"
	}
	return family
}

// coreWidth 查核心字型的字寬。
//
// [雷] pdfcpu 的 CharWidth 對「不是核心字型也沒載入的字型」會直接
// os.Exit —— 所以一定要先問 IsCoreFont。
func coreWidth(name string, r rune) float64 {
	if name == "" || !pdffont.IsCoreFont(name) {
		return 0
	}
	return float64(pdffont.CharWidth(name, r))
}

// --- pdfcpu 物件的小工具 ---

func deref(x *model.XRefTable, o types.Object) types.Object {
	v, err := x.Dereference(o)
	if err != nil {
		return nil
	}
	return v
}

func nameOf(o types.Object) string {
	switch v := o.(type) {
	case types.Name:
		return v.Value()
	}
	return ""
}

func numOf(o types.Object) (float64, bool) {
	switch v := o.(type) {
	case types.Integer:
		return float64(v.Value()), true
	case types.Float:
		return v.Value(), true
	}
	return 0, false
}

func streamBytes(x *model.XRefTable, o types.Object) []byte {
	sd, _, err := x.DereferenceStreamDict(o)
	if err != nil || sd == nil {
		return nil
	}
	if len(sd.Content) == 0 {
		if err := sd.Decode(); err != nil {
			return nil
		}
	}
	return sd.Content
}
