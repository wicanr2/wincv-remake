package pdf

import (
	"encoding/binary"
	"math"
	"os"
	"testing"

	"golang.org/x/image/font/sfnt"
	"golang.org/x/image/math/fixed"

	"github.com/wicanr2/wincv-remake/internal/ttf"
)

// bboxOf 是一組外框的外接矩形。比對兩個獨立的解譯器時用它 ——
// 逐點比對會被「曲線切成幾段」這種無關的差異卡住,而外接矩形
// 只要有一個座標算錯就會偏掉。
func bboxOf(segs []gseg) (x0, y0, x1, y1 float64) {
	x0, y0 = math.Inf(1), math.Inf(1)
	x1, y1 = math.Inf(-1), math.Inf(-1)
	for _, s := range segs {
		n := 1
		if s.op == 'c' {
			n = 3
		}
		for i := 0; i < n; i++ {
			x0, y0 = math.Min(x0, s.x[i]), math.Min(y0, s.y[i])
			x1, y1 = math.Max(x1, s.x[i]), math.Max(y1, s.y[i])
		}
	}
	return
}

// testdata 的 rich.pdf 裡,中文字型是 LibreOffice 嵌進去的 Type1 子集
// (`%!FontType1-1.0`)。那是真的檔案,不是自己組的 —— eexec 兩層加密、
// 子程式、flex 都在裡面。
func TestType1FromRealPDF(t *testing.T) {
	d, err := Open("testdata/rich.pdf")
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	p, err := d.Page(1)
	if err != nil {
		t.Fatal(err)
	}
	p.Texts() // 走一遍才會把字型載進來

	found := 0
	for _, f := range d.fonts {
		if f.kind != progType1 || len(f.embedded) == 0 {
			continue
		}
		found++
		t1, err := parseType1(f.embedded)
		if err != nil {
			t.Fatalf("%s 解不開:%v", f.baseFont, err)
		}
		if len(t1.chars) == 0 {
			t.Fatalf("%s 裡沒有字形", f.baseFont)
		}
		if len(t1.enc) == 0 {
			t.Errorf("%s 沒有自帶的編碼,字碼就對不到字形名", f.baseFont)
		}
		// 字型自帶的編碼裡的字碼要畫得出東西來。空白字是例外 ——
		// 它的 charstring 只有「設寬度、結束」兩個指令,本來就沒有外框。
		drawn, blank := 0, 0
		for code, name := range t1.enc {
			segs, ok := t1.glyph(name)
			if !ok {
				blank++
				continue
			}
			drawn++
			x0, y0, x1, y1 := bboxOf(segs)
			// 中日韓的字身大約是 0–1000。超出太多表示座標算錯了 ——
			// 而畫出來仍然是某種形狀,不是空白。
			if x0 < -400 || y0 < -400 || x1 > 1400 || y1 > 1400 {
				t.Errorf("%s 的字碼 %d(/%s)外框超出字身太多:%.0f %.0f %.0f %.0f",
					f.baseFont, code, name, x0, y0, x1, y1)
			}
		}
		if drawn == 0 || blank > drawn {
			t.Errorf("%s:畫得出來的有 %d 個,空的有 %d 個 —— 空的太多,像是解錯了",
				f.baseFont, drawn, blank)
		}
	}
	if found == 0 {
		t.Fatal("這份 PDF 裡沒有 Type1 字型,測試材料不對")
	}
}

// CFF 的字形指令與 OpenType/CFF 是同一套,而 x/image/font/sfnt 有自己的
// 一份解譯器。拿系統上的 OpenType 字型,把裡面的 CFF 表抽出來給自己的
// 解析器,再跟 sfnt 解同一個字形的結果比 —— 同一份資料、兩個獨立實作。
func TestCFFAgainstSFNT(t *testing.T) {
	path, data, cff := findCFFFont(t)
	if cff == nil {
		t.Skip("這台機器上沒有 OpenType/CFF 字型")
	}
	sf, err := sfnt.Parse(data)
	if err != nil {
		if col, err2 := sfnt.ParseCollection(data); err2 == nil && col.NumFonts() > 0 {
			sf, err = col.Font(0)
		}
		if err != nil {
			t.Skipf("%s 這份 sfnt 解不開:%v", path, err)
		}
	}
	mine, err := parseCFF(cff)
	if err != nil {
		t.Fatalf("%s 的 CFF 表解不開:%v", path, err)
	}
	if len(mine.charStrings) == 0 {
		t.Fatalf("%s 的 CFF 表裡沒有字形", path)
	}

	var buf sfnt.Buffer
	checked, mismatch := 0, 0
	// 抽樣就夠:要抓的是「整套指令解錯」,那種錯不會只錯一個字形。
	step := len(mine.charStrings)/200 + 1
	for gid := 1; gid < len(mine.charStrings) && checked < 200; gid += step {
		want, err := sf.LoadGlyph(&buf, sfnt.GlyphIndex(gid), fixed.I(glyphEm), nil)
		if err != nil || len(want) == 0 {
			continue
		}
		got, ok := mine.glyph(gid)
		if !ok {
			t.Errorf("第 %d 號字形自己解不出來", gid)
			mismatch++
			continue
		}
		checked++
		wx0, wy0, wx1, wy1 := sfntBBox(want)
		gx0, gy0, gx1, gy1 := bboxOf(got)
		// 一個字身單位的容差:兩邊的曲線切法不同,端點應該完全一致。
		const tol = 1.5
		if math.Abs(wx0-gx0) > tol || math.Abs(wy0-gy0) > tol ||
			math.Abs(wx1-gx1) > tol || math.Abs(wy1-gy1) > tol {
			mismatch++
			if mismatch <= 3 {
				t.Errorf("第 %d 號字形的外接矩形不同:自己 %.1f %.1f %.1f %.1f,sfnt %.1f %.1f %.1f %.1f",
					gid, gx0, gy0, gx1, gy1, wx0, wy0, wx1, wy1)
			}
		}
	}
	if checked < 10 {
		t.Skipf("%s 只比對到 %d 個字形,樣本太少", path, checked)
	}
	if mismatch > 0 {
		t.Errorf("%s:%d 個字形裡有 %d 個對不上", path, checked, mismatch)
	}
	t.Logf("%s:比對了 %d 個字形", path, checked)
}

// sfntBBox 把 sfnt 的外框換算成同一個座標系(Y 軸向上)再取外接矩形。
func sfntBBox(segs []sfnt.Segment) (x0, y0, x1, y1 float64) {
	x0, y0 = math.Inf(1), math.Inf(1)
	x1, y1 = math.Inf(-1), math.Inf(-1)
	for _, s := range segs {
		n := 1
		switch s.Op {
		case sfnt.SegmentOpQuadTo:
			n = 2
		case sfnt.SegmentOpCubeTo:
			n = 3
		}
		for i := 0; i < n; i++ {
			x, y := f26(s.Args[i].X), -f26(s.Args[i].Y)
			x0, y0 = math.Min(x0, x), math.Min(y0, y)
			x1, y1 = math.Max(x1, x), math.Max(y1, y)
		}
	}
	return
}

// findCFFFont 在系統字型裡找一份帶 CFF 表的,回傳整份檔案與 CFF 表。
func findCFFFont(t *testing.T) (string, []byte, []byte) {
	t.Helper()
	for _, path := range ttf.Candidates() {
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		if cff := sfntTable(data, "CFF "); cff != nil {
			return path, data, cff
		}
	}
	return "", nil, nil
}

// sfntTable 從 sfnt 容器裡取出一張表。字型集合(.ttc)取第一份字型的。
func sfntTable(b []byte, tag string) []byte {
	if len(b) < 12 {
		return nil
	}
	off := 0
	if string(b[:4]) == "ttcf" {
		if len(b) < 16 {
			return nil
		}
		off = int(binary.BigEndian.Uint32(b[12:]))
		if off+12 > len(b) {
			return nil
		}
	}
	n := int(binary.BigEndian.Uint16(b[off+4:]))
	for i := 0; i < n; i++ {
		rec := off + 12 + i*16
		if rec+16 > len(b) {
			return nil
		}
		if string(b[rec:rec+4]) != tag {
			continue
		}
		start := int(binary.BigEndian.Uint32(b[rec+8:]))
		length := int(binary.BigEndian.Uint32(b[rec+12:]))
		if start < 0 || length < 0 || start+length > len(b) {
			return nil
		}
		return b[start : start+length]
	}
	return nil
}
