package xlsx

import (
	"archive/zip"
	"os"
	"path/filepath"
	"testing"

	"github.com/wicanr2/wincv-remake/internal/markdown"
)

const ns = `xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main"
 xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships"`

func build(t *testing.T, parts map[string]string) string {
	t.Helper()
	name := filepath.Join(t.TempDir(), "t.xlsx")
	f, err := os.Create(name)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	zw := zip.NewWriter(f)
	all := map[string]string{
		"[Content_Types].xml": `<?xml version="1.0"?><Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types">
		  <Default Extension="xml" ContentType="application/xml"/>
		  <Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/></Types>`,
		"_rels/.rels": `<?xml version="1.0"?><Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">
		  <Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="xl/workbook.xml"/></Relationships>`,
	}
	for k, v := range parts {
		all[k] = v
	}
	for k, v := range all {
		w, err := zw.Create(k)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte(v)); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return name
}

// book 組一份只有一張工作表的活頁簿。
func book(t *testing.T, sheetData, styles, shared, wbPr string) *Book {
	t.Helper()
	parts := map[string]string{
		"xl/workbook.xml": `<?xml version="1.0"?><workbook ` + ns + `>` + wbPr +
			`<sheets><sheet name="表一" sheetId="1" r:id="rS"/></sheets></workbook>`,
		"xl/_rels/workbook.xml.rels": `<?xml version="1.0"?><Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">
		  <Relationship Id="rS" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/worksheet" Target="worksheets/sheet1.xml"/>
		  <Relationship Id="rSS" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/sharedStrings" Target="sharedStrings.xml"/>
		  <Relationship Id="rSt" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/styles" Target="styles.xml"/></Relationships>`,
		"xl/worksheets/sheet1.xml": `<?xml version="1.0"?><worksheet ` + ns + `><sheetData>` + sheetData + `</sheetData></worksheet>`,
		"xl/sharedStrings.xml":     `<?xml version="1.0"?><sst ` + ns + `>` + shared + `</sst>`,
		"xl/styles.xml":            `<?xml version="1.0"?><styleSheet ` + ns + `>` + styles + `</styleSheet>`,
	}
	b, err := Open(build(t, parts))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { b.Close() })
	return b
}

func table(t *testing.T, b *Book) [][]string {
	t.Helper()
	for _, blk := range b.Blocks(0) {
		if blk.Kind == markdown.Table {
			return blk.Rows
		}
	}
	t.Fatalf("這張工作表沒有表格")
	return nil
}

func TestSharedStringsAndInlineAndTypes(t *testing.T) {
	b := book(t,
		`<row r="1"><c r="A1" t="s"><v>1</v></c><c r="B1" t="inlineStr"><is><t>行內</t></is></c>
		  <c r="C1" t="b"><v>1</v></c><c r="D1"><v>3.5</v></c></row>`,
		`<cellXfs><xf numFmtId="0"/></cellXfs>`,
		`<si><t>第零個</t></si><si><r><t>富</t></r><r><t>文字</t></r></si>`, "")
	got := table(t, b)
	want := []string{"富文字", "行內", "TRUE", "3.5"}
	for i, w := range want {
		if got[0][i] != w {
			t.Errorf("第 %d 欄是 %q,要 %q", i, got[0][i], w)
		}
	}
}

// 儲存格的參照決定它在哪一欄。跳過的欄要留空,不能整列往左擠。
func TestSparseCellsKeepColumns(t *testing.T) {
	b := book(t, `<row r="2"><c r="C2" t="inlineStr"><is><t>丙</t></is></c></row>`,
		`<cellXfs><xf numFmtId="0"/></cellXfs>`, "", "")
	got := table(t, b)
	if len(got) != 2 || len(got[0]) != 3 {
		t.Fatalf("表格大小是 %d 列 × %d 欄", len(got), len(got[0]))
	}
	if got[1][2] != "丙" || got[0][0] != "" {
		t.Errorf("位置不對:%v", got)
	}
}

func TestDateFormats(t *testing.T) {
	styles := `<numFmts><numFmt numFmtId="200" formatCode="yyyy&quot;年&quot;m&quot;月&quot;"/>
	  <numFmt numFmtId="201" formatCode="&quot;May&quot; 0.00"/></numFmts>
	  <cellXfs><xf numFmtId="0"/><xf numFmtId="14"/><xf numFmtId="22"/><xf numFmtId="200"/><xf numFmtId="201"/></cellXfs>`
	b := book(t, `<row r="1">
	   <c r="A1" s="1"><v>45000</v></c>
	   <c r="B1" s="2"><v>45000.5</v></c>
	   <c r="C1" s="3"><v>45000</v></c>
	   <c r="D1" s="4"><v>1.5</v></c>
	   <c r="E1" s="0"><v>45000</v></c></row>`, styles, "", "")
	got := table(t, b)[0]
	if got[0] != "2023-03-15" {
		t.Errorf("內建日期格式:%q", got[0])
	}
	if got[1] != "2023-03-15 12:00:00" {
		t.Errorf("日期加時間:%q", got[1])
	}
	if got[2] != "2023-03-15" {
		t.Errorf("自訂日期格式:%q", got[2])
	}
	// "May" 在引號裡,那個 y 不是年份欄位。
	if got[3] != "1.5" {
		t.Errorf("引號裡的字母不該讓它變成日期:%q", got[3])
	}
	if got[4] != "45000" {
		t.Errorf("沒有日期格式的數字要照原樣:%q", got[4])
	}
}

// Mac 上建的活頁簿用 1904 制。同一個序號在兩制之間差 1462 天:
// 1900 制的 45000 是 2023-03-15,1904 制的 45000 是 2027-03-16。
func TestDate1904(t *testing.T) {
	b := book(t, `<row r="1"><c r="A1" s="1"><v>45000</v></c></row>`,
		`<cellXfs><xf numFmtId="0"/><xf numFmtId="14"/></cellXfs>`, "", `<workbookPr date1904="1"/>`)
	if got := table(t, b)[0][0]; got != "2027-03-16" {
		t.Errorf("1904 制算出來是 %q", got)
	}
}

func TestPercent(t *testing.T) {
	b := book(t, `<row r="1"><c r="A1" s="1"><v>0.125</v></c></row>`,
		`<numFmts><numFmt numFmtId="180" formatCode="0.0%"/></numFmts><cellXfs><xf numFmtId="0"/><xf numFmtId="180"/></cellXfs>`,
		"", "")
	if got := table(t, b)[0][0]; got != "12.5%" {
		t.Errorf("百分比算出來是 %q", got)
	}
}

func TestColOf(t *testing.T) {
	for ref, want := range map[string]int{"A1": 1, "Z9": 26, "AA1": 27, "AB12": 28, "BA1": 53} {
		if got := colOf(ref); got != want {
			t.Errorf("colOf(%q)=%d,要 %d", ref, got, want)
		}
	}
}

func TestNotAnXlsx(t *testing.T) {
	if _, err := Open(build(t, map[string]string{"x.xml": "<a/>"})); err == nil {
		t.Fatal("沒有 workbook.xml 應該要失敗")
	}
}
