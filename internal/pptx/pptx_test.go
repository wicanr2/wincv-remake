package pptx

import (
	"archive/zip"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/wicanr2/wincv-remake/internal/markdown"
)

const ns = `xmlns:p="http://schemas.openxmlformats.org/presentationml/2006/main"
 xmlns:a="http://schemas.openxmlformats.org/drawingml/2006/main"
 xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships"
 xmlns:mc="http://schemas.openxmlformats.org/markup-compatibility/2006"`

func build(t *testing.T, parts map[string]string) string {
	t.Helper()
	name := filepath.Join(t.TempDir(), "t.pptx")
	f, err := os.Create(name)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	zw := zip.NewWriter(f)
	all := map[string]string{
		"[Content_Types].xml": `<?xml version="1.0"?><Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types">
		  <Default Extension="xml" ContentType="application/xml"/><Default Extension="png" ContentType="image/png"/>
		  <Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/></Types>`,
		"_rels/.rels": `<?xml version="1.0"?><Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">
		  <Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="ppt/presentation.xml"/></Relationships>`,
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

func slide(body string) string {
	return `<?xml version="1.0"?><p:sld ` + ns + `><p:cSld><p:spTree>` + body + `</p:spTree></p:cSld></p:sld>`
}

// sp 組一個圖形。ph 是佔位屬性(可以是空的),xy 是位置(可以是空的)。
func sp(ph, xy, paras string) string {
	phEl := ""
	if ph != "" {
		phEl = `<p:ph ` + ph + `/>`
	}
	pos := ""
	if xy != "" {
		pos = `<p:spPr><a:xfrm><a:off ` + xy + `/></a:xfrm></p:spPr>`
	}
	return `<p:sp><p:nvSpPr><p:nvPr>` + phEl + `</p:nvPr></p:nvSpPr>` + pos +
		`<p:txBody>` + paras + `</p:txBody></p:sp>`
}

func para(pPr, text string) string {
	return `<a:p>` + pPr + `<a:r><a:t>` + text + `</a:t></a:r></a:p>`
}

func open(t *testing.T, parts map[string]string) *Deck {
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
		for _, r := range b.Rows {
			sb.WriteString(strings.Join(r, "|"))
		}
		sb.WriteString("\n")
	}
	return sb.String()
}

// 放映順序寫在 sldIdLst,不是檔名的數字。搬動過投影片的簡報兩者不一樣。
func TestSlideOrderFollowsIDList(t *testing.T) {
	d := open(t, map[string]string{
		"ppt/presentation.xml": `<?xml version="1.0"?><p:presentation ` + ns + `><p:sldIdLst>
		  <p:sldId id="258" r:id="rB"/><p:sldId id="256" r:id="rA"/></p:sldIdLst></p:presentation>`,
		"ppt/_rels/presentation.xml.rels": `<?xml version="1.0"?><Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">
		  <Relationship Id="rA" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/slide" Target="slides/slide1.xml"/>
		  <Relationship Id="rB" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/slide" Target="slides/slide2.xml"/></Relationships>`,
		"ppt/slides/slide1.xml": slide(sp(`type="title"`, "", para("", "第一個檔名"))),
		"ppt/slides/slide2.xml": slide(sp(`type="title"`, "", para("", "第一張放映"))),
	})
	if len(d.Slides) != 2 {
		t.Fatalf("要 2 張,拿到 %d", len(d.Slides))
	}
	if d.Slides[0].Title != "第一張放映" || d.Slides[1].Title != "第一個檔名" {
		t.Errorf("順序不對:%q / %q", d.Slides[0].Title, d.Slides[1].Title)
	}
}

// 只有一張投影片時的共用骨架。
func oneSlide(body string, extra map[string]string) map[string]string {
	m := map[string]string{
		"ppt/presentation.xml": `<?xml version="1.0"?><p:presentation ` + ns + `><p:sldIdLst>
		  <p:sldId id="256" r:id="rA"/></p:sldIdLst></p:presentation>`,
		"ppt/_rels/presentation.xml.rels": `<?xml version="1.0"?><Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">
		  <Relationship Id="rA" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/slide" Target="slides/slide1.xml"/></Relationships>`,
		"ppt/slides/slide1.xml": body,
	}
	for k, v := range extra {
		m[k] = v
	}
	return m
}

func TestTitleAndBulletLevels(t *testing.T) {
	d := open(t, oneSlide(slide(
		sp(`type="title"`, "", para("", "標題"))+
			sp(`type="body" idx="1"`, "",
				para(`<a:pPr lvl="0"/>`, "第一層")+
					para(`<a:pPr lvl="1"/>`, "第二層")+
					para(`<a:pPr><a:buNone/></a:pPr>`, "沒有符號")),
	), nil))
	bs := d.Slides[0].Blocks
	if bs[0].Kind != markdown.Heading || bs[0].Spans[0].Text != "標題" {
		t.Fatalf("第一個區塊應該是標題:%q", text(bs))
	}
	if bs[1].Kind != markdown.List || bs[1].Level != 1 {
		t.Errorf("第一層條列不對:%+v", bs[1])
	}
	if bs[2].Kind != markdown.List || bs[2].Level != 2 {
		t.Errorf("第二層條列不對:%+v", bs[2])
	}
	if bs[3].Kind != markdown.Para {
		t.Errorf("buNone 應該變成一般段落,拿到 %v", bs[3].Kind)
	}
}

// 投影片自己常常只寫 idx 不寫 type,型別要回頭問版面配置。
func TestPlaceholderTypeFromLayout(t *testing.T) {
	d := open(t, oneSlide(slide(sp(`idx="0"`, "", para("", "我是標題"))),
		map[string]string{
			"ppt/slides/_rels/slide1.xml.rels": `<?xml version="1.0"?><Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">
			  <Relationship Id="rL" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/slideLayout" Target="../slideLayouts/slideLayout1.xml"/></Relationships>`,
			"ppt/slideLayouts/slideLayout1.xml": `<?xml version="1.0"?><p:sldLayout ` + ns + `><p:cSld><p:spTree>
			  <p:sp><p:nvSpPr><p:nvPr><p:ph type="title" idx="0"/></p:nvPr></p:nvSpPr></p:sp></p:spTree></p:cSld></p:sldLayout>`,
		}))
	if d.Slides[0].Title != "我是標題" {
		t.Fatalf("標題沒有從版面配置認出來:%q / %q", d.Slides[0].Title, text(d.Slides[0].Blocks))
	}
}

func TestTableAndImageAndNotes(t *testing.T) {
	png := "\x89PNG\r\n\x1a\n內容"
	body := slide(
		sp(`type="title"`, "", para("", "T")) +
			`<p:graphicFrame><a:graphic><a:graphicData><a:tbl>
			  <a:tr><a:tc><a:txBody><a:p><a:r><a:t>甲</a:t></a:r></a:p></a:txBody></a:tc>
			       <a:tc><a:txBody><a:p><a:r><a:t>乙</a:t></a:r></a:p></a:txBody></a:tc></a:tr>
			  <a:tr><a:tc gridSpan="2"><a:txBody><a:p><a:r><a:t>橫跨</a:t></a:r></a:p></a:txBody></a:tc></a:tr>
			</a:tbl></a:graphicData></a:graphic></p:graphicFrame>` +
			`<p:pic><p:nvPicPr><p:cNvPr id="3" name="圖.png"/></p:nvPicPr>
			  <p:blipFill><a:blip r:embed="rI"/></p:blipFill></p:pic>`)
	d := open(t, oneSlide(body, map[string]string{
		"ppt/media/image1.png": png,
		"ppt/slides/_rels/slide1.xml.rels": `<?xml version="1.0"?><Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">
		  <Relationship Id="rI" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/image" Target="../media/image1.png"/>
		  <Relationship Id="rN" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/notesSlide" Target="../notesSlides/notesSlide1.xml"/></Relationships>`,
		"ppt/notesSlides/notesSlide1.xml": `<?xml version="1.0"?><p:notes ` + ns + `><p:cSld><p:spTree>` +
			sp(`type="body" idx="1"`, "", para("", "講稿內容")) + `</p:spTree></p:cSld></p:notes>`,
	}))
	bs := d.Slides[0].Blocks
	got := text(bs)
	var tbl *markdown.Block
	var img *markdown.Block
	for i := range bs {
		switch bs[i].Kind {
		case markdown.Table:
			tbl = &bs[i]
		case markdown.Image:
			img = &bs[i]
		}
	}
	if tbl == nil || tbl.Rows[0][0] != "甲" || tbl.Rows[1][1] != "" {
		t.Errorf("表格不對:%q", got)
	}
	if img == nil {
		t.Fatalf("少了圖片:%q", got)
	}
	if b, err := d.Image(img.Src); err != nil || string(b) != png {
		t.Errorf("取不回圖片內容:%v", err)
	}
	if !strings.Contains(got, "備忘稿") || !strings.Contains(got, "講稿內容") {
		t.Errorf("備忘稿沒有接上:%q", got)
	}
}

// 每個圖形都寫明位置時依位置排;少一個就整批照原順序。
func TestPositionOrdering(t *testing.T) {
	withPos := open(t, oneSlide(slide(
		sp("", `x="0" y="5000"`, para("", "下面"))+
			sp("", `x="0" y="1000"`, para("", "上面")),
	), nil))
	if got := text(withPos.Slides[0].Blocks); strings.Index(got, "上面") > strings.Index(got, "下面") {
		t.Errorf("有位置時要由上而下:%q", got)
	}
	noPos := open(t, oneSlide(slide(
		sp("", `x="0" y="5000"`, para("", "下面"))+
			sp("", "", para("", "沒位置")),
	), nil))
	got := text(noPos.Slides[0].Blocks)
	if strings.Index(got, "下面") > strings.Index(got, "沒位置") {
		t.Errorf("少一個位置就該照原順序:%q", got)
	}
}

func TestNotAPptx(t *testing.T) {
	if _, err := Open(build(t, map[string]string{"x.xml": "<a/>"})); err == nil {
		t.Fatal("沒有 presentation.xml 應該要失敗")
	}
}

// 沒有佔位資訊的簡報靠位置與長度猜標題。條件不合就不猜 ——
// 把一段內文抬成標題比少一個標題糟。
func TestTitleGuessWithoutPlaceholders(t *testing.T) {
	short := open(t, oneSlide(slide(
		sp("", `x="0" y="1000"`, para("", "短短的標題"))+
			sp("", `x="0" y="5000"`, para("", "內文一")+para("", "內文二")),
	), nil))
	if short.Slides[0].Title != "短短的標題" {
		t.Errorf("該猜的沒猜到:%q", short.Slides[0].Title)
	}

	long := strings.Repeat("很長的一段內文", 12)
	multi := open(t, oneSlide(slide(
		sp("", `x="0" y="1000"`, para("", long)),
	), nil))
	if multi.Slides[0].Title != "" {
		t.Errorf("太長的段落不該被當成標題:%q", multi.Slides[0].Title)
	}
	if got := text(multi.Slides[0].Blocks); !strings.Contains(got, long) {
		t.Errorf("沒被當成標題就要留在內文裡:%q", got)
	}
}
