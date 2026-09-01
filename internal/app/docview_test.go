package app

import (
	"archive/zip"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/wicanr2/wincv-remake/internal/cell"
	"github.com/wicanr2/wincv-remake/internal/keys"
	"github.com/wicanr2/wincv-remake/internal/markdown"
	"github.com/wicanr2/wincv-remake/internal/vfs"
)

// writeDocx 在 dir 底下寫一份最小的 .docx。
func writeDocx(t *testing.T, dir, name, body string) {
	t.Helper()
	f, err := os.Create(filepath.Join(dir, name))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	zw := zip.NewWriter(f)
	parts := map[string]string{
		"[Content_Types].xml": `<?xml version="1.0"?><Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types"><Default Extension="xml" ContentType="application/xml"/></Types>`,
		"_rels/.rels": `<?xml version="1.0"?><Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">
		  <Relationship Id="r1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="word/document.xml"/></Relationships>`,
		"word/document.xml": `<?xml version="1.0"?><w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:body>` +
			body + `</w:body></w:document>`,
	}
	for k, v := range parts {
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
}

// docApp 開一個以 dir 為起點的 App。
func docApp(t *testing.T, dir string) *App {
	t.Helper()
	a := New(vfs.OS{}, dir)
	a.CellW, a.CellH = 8, 16
	a.Draw(cell.New(70, 20))
	return a
}

func blocksText(bs []markdown.Block) string {
	var sb strings.Builder
	for _, b := range bs {
		for _, s := range b.Spans {
			sb.WriteString(s.Text)
		}
		sb.WriteString("\n")
	}
	return sb.String()
}

// [雷] .docx 是 zip。按 Enter 要看到文件,不是走進去看到 word/document.xml。
func TestEnterDocxOpensDocument(t *testing.T) {
	dir := t.TempDir()
	writeDocx(t, dir, "報告.docx", `<w:p><w:r><w:t>文件內容</w:t></w:r></w:p>`)
	a := docApp(t, dir)
	a.focusOn("報告.docx")
	if !a.enter() {
		t.Fatal("按 Enter 沒有反應")
	}
	if a.Mode != ModeBrowse {
		t.Fatalf("應該進瀏覽模式,拿到 %v(訊息:%s)", a.Mode, a.Message)
	}
	if got := blocksText(a.bv.blocks); !strings.Contains(got, "文件內容") {
		t.Fatalf("畫面上沒有文件內容:%q / 狀態:%s", got, a.bv.status)
	}
	// 只有一段的文件不需要導覽列。
	if strings.Contains(blocksText(a.bv.blocks), "章節清單") {
		t.Errorf("單段文件不該有導覽:%q", blocksText(a.bv.blocks))
	}
}

func TestDocURLRoundTrip(t *testing.T) {
	for _, tc := range []struct {
		path string
		part int
	}{{"/tmp/a.docx", 0}, {"/tmp/a.pptx", 3}, {"/tmp/怪#名字.xlsx", 2}} {
		raw := docURL(tc.path, tc.part)
		p, n, ok := parseDocURL(raw)
		if !ok || p != tc.path || n != tc.part {
			t.Errorf("%q → %q,%d,%v", raw, p, n, ok)
		}
	}
}

// 離開瀏覽模式要把文件關掉,不然檔案一直開著。
func TestEscClosesDocument(t *testing.T) {
	dir := t.TempDir()
	writeDocx(t, dir, "a.docx", `<w:p><w:r><w:t>x</w:t></w:r></w:p>`)
	a := docApp(t, dir)
	a.focusOn("a.docx")
	a.enter()
	if a.office == nil {
		t.Fatal("文件沒有開起來")
	}
	a.HandleKey(keys.Named(keys.Esc))
	if a.office != nil {
		t.Error("離開瀏覽模式之後文件還開著")
	}
}
