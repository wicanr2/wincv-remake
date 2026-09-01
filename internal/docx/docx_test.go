package docx

import (
	"archive/zip"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/wicanr2/wincv-remake/internal/markdown"
)

// build 組一份 .docx。鍵是 ZIP 內的路徑,值是內容。
func build(t *testing.T, parts map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	name := filepath.Join(dir, "t.docx")
	f, err := os.Create(name)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	zw := zip.NewWriter(f)
	all := map[string]string{
		"[Content_Types].xml": `<?xml version="1.0"?><Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types">
		  <Default Extension="xml" ContentType="application/xml"/>
		  <Default Extension="png" ContentType="image/png"/>
		  <Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/>
		  <Override PartName="/word/document.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.document.main+xml"/>
		</Types>`,
		"_rels/.rels": `<?xml version="1.0"?><Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">
		  <Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="word/document.xml"/>
		</Relationships>`,
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

// doc 把 body 的內容包成一份完整的 document.xml。
func doc(body string) string {
	return `<?xml version="1.0"?><w:document
	  xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"
	  xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships"
	  xmlns:mc="http://schemas.openxmlformats.org/markup-compatibility/2006"
	  xmlns:wps="http://schemas.microsoft.com/office/word/2010/wordprocessingShape"
	  xmlns:a="http://schemas.openxmlformats.org/drawingml/2006/main"
	  xmlns:wp="http://schemas.openxmlformats.org/drawingml/2006/wordprocessingDrawing"
	  xmlns:v="urn:schemas-microsoft-com:vml"><w:body>` + body + `</w:body></w:document>`
}

func open(t *testing.T, parts map[string]string) *Doc {
	t.Helper()
	d, err := Open(build(t, parts))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	return d
}

func text(bs []markdown.Block) string {
	var sb strings.Builder
	for _, b := range bs {
		for _, s := range b.Spans {
			sb.WriteString(s.Text)
		}
		sb.WriteString("\n")
	}
	return sb.String()
}

func TestParagraphsAndRuns(t *testing.T) {
	d := open(t, map[string]string{
		"word/document.xml": doc(`
		  <w:p><w:r><w:t xml:space="preserve">hello </w:t></w:r>
		       <w:r><w:rPr><w:b/></w:rPr><w:t>world</w:t></w:r></w:p>
		  <w:p><w:r><w:rPr><w:i w:val="1"/></w:rPr><w:t>斜體</w:t></w:r></w:p>`),
	})
	bs := d.Blocks()
	if len(bs) != 2 {
		t.Fatalf("要 2 個區塊,拿到 %d:%q", len(bs), text(bs))
	}
	if got := text(bs[:1]); got != "hello world\n" {
		t.Errorf("第一段是 %q", got)
	}
	if bs[0].Spans[1].Style&markdown.Bold == 0 {
		t.Error("<w:b/> 沒有 val 屬性時應該是粗體")
	}
	if bs[1].Spans[0].Style&markdown.Italic == 0 {
		t.Error("斜體沒套上")
	}
}

// 空段落是排版用的間距,不該變成空區塊。
func TestEmptyParagraphDropped(t *testing.T) {
	d := open(t, map[string]string{
		"word/document.xml": doc(`<w:p/><w:p><w:r><w:t>x</w:t></w:r></w:p><w:p><w:r><w:t></w:t></w:r></w:p>`),
	})
	if bs := d.Blocks(); len(bs) != 1 {
		t.Fatalf("要 1 個區塊,拿到 %d:%q", len(bs), text(bs))
	}
}

func TestHeadingFromOutlineLevelAndName(t *testing.T) {
	styles := `<?xml version="1.0"?><w:styles xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main">
	  <w:style w:type="paragraph" w:styleId="H1"><w:name w:val="Whatever"/><w:pPr><w:outlineLvl w:val="0"/></w:pPr></w:style>
	  <w:style w:type="paragraph" w:styleId="X"><w:name w:val="標題 3"/></w:style>
	  <w:style w:type="paragraph" w:styleId="Y"><w:name w:val="我的樣式"/><w:basedOn w:val="X"/></w:style>
	</w:styles>`
	d := open(t, map[string]string{
		"word/document.xml": doc(`
		  <w:p><w:pPr><w:pStyle w:val="H1"/></w:pPr><w:r><w:t>甲</w:t></w:r></w:p>
		  <w:p><w:pPr><w:pStyle w:val="X"/></w:pPr><w:r><w:t>乙</w:t></w:r></w:p>
		  <w:p><w:pPr><w:pStyle w:val="Y"/></w:pPr><w:r><w:t>丙</w:t></w:r></w:p>`),
		"word/styles.xml": styles,
		"word/_rels/document.xml.rels": `<?xml version="1.0"?><Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">
		  <Relationship Id="rs" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/styles" Target="styles.xml"/>
		</Relationships>`,
	})
	bs := d.Blocks()
	want := []int{1, 3, 3} // 第三個靠 basedOn 繼承
	for i, w := range want {
		if bs[i].Kind != markdown.Heading || bs[i].Level != w {
			t.Errorf("第 %d 段是 kind=%v level=%d,要 Heading level=%d", i, bs[i].Kind, bs[i].Level, w)
		}
	}
}

func TestListNumberingAndReset(t *testing.T) {
	num := `<?xml version="1.0"?><w:numbering xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main">
	  <w:abstractNum w:abstractNumId="0">
	    <w:lvl w:ilvl="0"><w:start w:val="1"/><w:numFmt w:val="decimal"/><w:lvlText w:val="%1."/></w:lvl>
	    <w:lvl w:ilvl="1"><w:start w:val="1"/><w:numFmt w:val="decimal"/><w:lvlText w:val="%2."/></w:lvl>
	  </w:abstractNum>
	  <w:abstractNum w:abstractNumId="9">
	    <w:lvl w:ilvl="0"><w:numFmt w:val="bullet"/><w:lvlText w:val="&#xF0B7;"/></w:lvl>
	  </w:abstractNum>
	  <w:num w:numId="1"><w:abstractNumId w:val="0"/></w:num>
	  <w:num w:numId="2"><w:abstractNumId w:val="9"/></w:num>
	</w:numbering>`
	item := func(numID, ilvl, s string) string {
		return `<w:p><w:pPr><w:numPr><w:ilvl w:val="` + ilvl + `"/><w:numId w:val="` + numID + `"/></w:numPr></w:pPr><w:r><w:t>` + s + `</w:t></w:r></w:p>`
	}
	d := open(t, map[string]string{
		"word/document.xml": doc(item("1", "0", "a") + item("1", "1", "a1") + item("1", "1", "a2") +
			item("1", "0", "b") + item("1", "1", "b1") + item("2", "0", "點")),
		"word/numbering.xml": num,
		"word/_rels/document.xml.rels": `<?xml version="1.0"?><Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">
		  <Relationship Id="rn" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/numbering" Target="numbering.xml"/>
		</Relationships>`,
	})
	bs := d.Blocks()
	wantNum := []int{1, 1, 2, 2, 1, 0}
	wantLvl := []int{1, 2, 2, 1, 2, 1}
	for i := range wantNum {
		if bs[i].Kind != markdown.List {
			t.Fatalf("第 %d 個不是清單", i)
		}
		if bs[i].Num != wantNum[i] || bs[i].Level != wantLvl[i] {
			t.Errorf("第 %d 個 num=%d level=%d,要 num=%d level=%d",
				i, bs[i].Num, bs[i].Level, wantNum[i], wantLvl[i])
		}
	}
	if bs[5].Ordered {
		t.Error("bullet 不該是有序清單")
	}
	// Wingdings 的字碼在私人使用區,畫出來是缺字方框,要交回預設符號。
	if bs[5].Marker != "" {
		t.Errorf("私人使用區的項目符號應該丟掉,拿到 %q", bs[5].Marker)
	}
}

func TestTableWithGridSpan(t *testing.T) {
	d := open(t, map[string]string{
		"word/document.xml": doc(`<w:tbl>
		  <w:tr><w:tc><w:p><w:r><w:t>甲</w:t></w:r></w:p></w:tc>
		        <w:tc><w:p><w:r><w:t>乙</w:t></w:r></w:p></w:tc>
		        <w:tc><w:p><w:r><w:t>丙</w:t></w:r></w:p></w:tc></w:tr>
		  <w:tr><w:tc><w:tcPr><w:gridSpan w:val="2"/></w:tcPr><w:p><w:r><w:t>橫跨</w:t></w:r></w:p></w:tc>
		        <w:tc><w:p><w:r><w:t>丁</w:t></w:r></w:p></w:tc></w:tr>
		</w:tbl>`),
	})
	bs := d.Blocks()
	if len(bs) != 1 || bs[0].Kind != markdown.Table {
		t.Fatalf("要一個表格,拿到 %v", bs)
	}
	want := [][]string{{"甲", "乙", "丙"}, {"橫跨", "", "丁"}}
	for i, row := range want {
		for j, cell := range row {
			if bs[0].Rows[i][j] != cell {
				t.Errorf("第 %d 列第 %d 欄是 %q,要 %q", i, j, bs[0].Rows[i][j], cell)
			}
		}
	}
}

func TestHyperlinkBothForms(t *testing.T) {
	d := open(t, map[string]string{
		"word/document.xml": doc(`
		  <w:p><w:hyperlink r:id="rL"><w:r><w:t>關聯式</w:t></w:r></w:hyperlink></w:p>
		  <w:p><w:r><w:fldChar w:fldCharType="begin"/></w:r>
		       <w:r><w:instrText xml:space="preserve"> HYPERLINK "https://b.example/" </w:instrText></w:r>
		       <w:r><w:fldChar w:fldCharType="separate"/></w:r>
		       <w:r><w:t>欄位式</w:t></w:r>
		       <w:r><w:fldChar w:fldCharType="end"/></w:r>
		       <w:r><w:t>之後</w:t></w:r></w:p>`),
		"word/_rels/document.xml.rels": `<?xml version="1.0"?><Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">
		  <Relationship Id="rL" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/hyperlink" Target="https://a.example/" TargetMode="External"/>
		</Relationships>`,
	})
	bs := d.Blocks()
	if bs[0].Spans[0].Href != "https://a.example/" {
		t.Errorf("關聯式連結的目標是 %q", bs[0].Spans[0].Href)
	}
	var link, plain string
	for _, s := range bs[1].Spans {
		if s.Style&markdown.Link != 0 {
			link += s.Text
		} else {
			plain += s.Text
		}
	}
	if link != "欄位式" || plain != "之後" {
		t.Errorf("欄位式連結:連結部分 %q,一般部分 %q", link, plain)
	}
}

// mc:AlternateContent 的兩個分支是同一段內容的兩個版本,只能取一個。
func TestAlternateContentNotDuplicated(t *testing.T) {
	d := open(t, map[string]string{
		"word/document.xml": doc(`<w:p><w:r><mc:AlternateContent>
		  <mc:Choice Requires="wps"><w:drawing><wp:inline><a:graphic><a:graphicData>
		    <wps:wsp><wps:txbx><w:txbxContent><w:p><w:r><w:t>方塊裡的字</w:t></w:r></w:p></w:txbxContent></wps:txbx></wps:wsp>
		  </a:graphicData></a:graphic></wp:inline></w:drawing></mc:Choice>
		  <mc:Fallback><w:pict><v:shape><v:textbox><w:txbxContent>
		    <w:p><w:r><w:t>方塊裡的字</w:t></w:r></w:p></w:txbxContent></v:textbox></v:shape></w:pict></mc:Fallback>
		</mc:AlternateContent></w:r></w:p>`),
	})
	got := text(d.Blocks())
	if n := strings.Count(got, "方塊裡的字"); n != 1 {
		t.Fatalf("文字方塊的內容出現 %d 次,應該只有 1 次:%q", n, got)
	}
}

func TestImageAndBytes(t *testing.T) {
	png := "\x89PNG\r\n\x1a\n這是假的但夠用"
	d := open(t, map[string]string{
		"word/document.xml": doc(`<w:p><w:r><w:drawing><wp:inline>
		  <wp:docPr id="1" name="pic.png" descr="一張圖"/>
		  <a:graphic><a:graphicData><a:blip r:embed="rI"/></a:graphicData></a:graphic>
		</wp:inline></w:drawing></w:r></w:p>`),
		"word/media/image1.png": png,
		"word/_rels/document.xml.rels": `<?xml version="1.0"?><Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">
		  <Relationship Id="rI" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/image" Target="media/image1.png"/>
		</Relationships>`,
	})
	bs := d.Blocks()
	if len(bs) != 1 || bs[0].Kind != markdown.Image {
		t.Fatalf("要一個圖片區塊,拿到 %v", bs)
	}
	if bs[0].Alt != "一張圖" {
		t.Errorf("替代文字是 %q", bs[0].Alt)
	}
	got, err := d.Image(bs[0].Src)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != png {
		t.Error("取回的圖片內容不對")
	}
}

func TestFootnotesAppended(t *testing.T) {
	d := open(t, map[string]string{
		"word/document.xml": doc(`<w:p><w:r><w:t>正文</w:t></w:r>
		  <w:r><w:footnoteReference w:id="2"/></w:r></w:p>`),
		"word/footnotes.xml": `<?xml version="1.0"?><w:footnotes xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main">
		  <w:footnote w:type="separator" w:id="0"><w:p><w:r><w:t>不要這個</w:t></w:r></w:p></w:footnote>
		  <w:footnote w:id="2"><w:p><w:r><w:t>註腳內容</w:t></w:r></w:p></w:footnote>
		</w:footnotes>`,
		"word/_rels/document.xml.rels": `<?xml version="1.0"?><Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">
		  <Relationship Id="rf" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/footnotes" Target="footnotes.xml"/>
		</Relationships>`,
	})
	got := text(d.Blocks())
	if !strings.Contains(got, "[註 1]") {
		t.Errorf("正文少了註腳標記:%q", got)
	}
	if !strings.Contains(got, "註腳內容") {
		t.Errorf("文末少了註腳內容:%q", got)
	}
	if strings.Contains(got, "不要這個") {
		t.Errorf("分隔用的 footnote 不該出現:%q", got)
	}
}

// 修訂:插入的內容要留,刪除的不要。
func TestTrackedChanges(t *testing.T) {
	d := open(t, map[string]string{
		"word/document.xml": doc(`<w:p>
		  <w:ins><w:r><w:t>新增的</w:t></w:r></w:ins>
		  <w:del><w:r><w:delText>刪掉的</w:delText></w:r></w:del>
		  <w:r><w:t>原有的</w:t></w:r></w:p>`),
	})
	got := text(d.Blocks())
	if !strings.Contains(got, "新增的") || !strings.Contains(got, "原有的") {
		t.Errorf("內容不完整:%q", got)
	}
	if strings.Contains(got, "刪掉的") {
		t.Errorf("刪除的內容不該出現:%q", got)
	}
}

// 分頁在段落中間出現時要把段落切開,不能把前後接成一段。
func TestPageBreakSplitsParagraph(t *testing.T) {
	d := open(t, map[string]string{
		"word/document.xml": doc(`<w:p><w:r><w:t>前</w:t><w:br w:type="page"/></w:r><w:r><w:t>後</w:t></w:r></w:p>`),
	})
	bs := d.Blocks()
	if len(bs) != 3 || bs[1].Kind != markdown.Rule {
		t.Fatalf("要「段落 / 分隔線 / 段落」,拿到 %d 個區塊:%q", len(bs), text(bs))
	}
}

func TestNotADocx(t *testing.T) {
	if _, err := Open(build(t, map[string]string{"foo.xml": "<a/>"})); err == nil {
		t.Fatal("沒有 document.xml 應該要失敗")
	}
}
