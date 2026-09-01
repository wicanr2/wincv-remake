package officedoc

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/wicanr2/wincv-remake/internal/markdown"
)

// testdata 裡的三份檔案是 LibreOffice 產生的真實 Office 檔,
// 重建方式見 testdata/README.md。用真檔的理由:自己組的最小檔案
// 只驗得到自己對格式的理解,驗不到「真的 Office 檔長這樣」——
// 版面配置的繼承、共用字串表、關聯的相對路徑都只在真檔上才會出現。
func open(t *testing.T, name string) *Doc {
	t.Helper()
	d, err := Open(filepath.Join("testdata", name))
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

func TestRealDocx(t *testing.T) {
	d := open(t, "rich.docx")
	if d.Kind != Word || len(d.Parts) != 1 {
		t.Fatalf("kind=%v parts=%d", d.Kind, len(d.Parts))
	}
	got := text(d.Blocks(0))
	for _, want := range []string{"第一章 標題", "粗體", "斜體", "條列一", "甲|乙", "ASCII paragraph"} {
		if !strings.Contains(got, want) {
			t.Errorf("少了 %q:\n%s", want, got)
		}
	}
	bs := d.Blocks(0)
	if bs[0].Kind != markdown.Heading || bs[0].Level != 1 {
		t.Errorf("標題沒認出來:%v level=%d", bs[0].Kind, bs[0].Level)
	}
}

func TestRealPptx(t *testing.T) {
	d := open(t, "deck.pptx")
	if d.Kind != Presentation {
		t.Fatalf("kind=%v", d.Kind)
	}
	if len(d.Parts) != 2 {
		t.Fatalf("要 2 張投影片,拿到 %d:%v", len(d.Parts), d.Parts)
	}
	if d.Parts[0].Title != "第一張投影片" || d.Parts[1].Title != "第二張投影片" {
		t.Errorf("投影片標題:%q / %q", d.Parts[0].Title, d.Parts[1].Title)
	}
	got := text(d.Blocks(0))
	for _, want := range []string{"第一張投影片", "條列一", "次層條列", "備忘稿", "這是講稿"} {
		if !strings.Contains(got, want) {
			t.Errorf("第一張少了 %q:\n%s", want, got)
		}
	}
	// 條列的階層要保留。
	var lv1, lv2 bool
	for _, b := range d.Blocks(0) {
		if b.Kind != markdown.List {
			continue
		}
		switch b.Level {
		case 1:
			lv1 = true
		case 2:
			lv2 = true
		}
	}
	if !lv1 || !lv2 {
		t.Errorf("條列階層沒保留(第一層 %v,第二層 %v)", lv1, lv2)
	}
}

func TestRealXlsx(t *testing.T) {
	d := open(t, "sheet.xlsx")
	if d.Kind != Spreadsheet || len(d.Parts) < 1 {
		t.Fatalf("kind=%v parts=%v", d.Kind, d.Parts)
	}
	var rows [][]string
	for _, b := range d.Blocks(0) {
		if b.Kind == markdown.Table {
			rows = b.Rows
		}
	}
	if len(rows) != 3 {
		t.Fatalf("要 3 列,拿到 %d:%v", len(rows), rows)
	}
	if rows[0][0] != "甲" || rows[1][0] != "1" {
		t.Errorf("內容不對:%v", rows)
	}
	// 日期在檔案裡是一個天數,靠格式碼才知道它是日期。
	if !strings.HasPrefix(rows[1][2], "2023-03-15") {
		t.Errorf("日期沒有還原:%q", rows[1][2])
	}
}

// 用 .doc 存的 RTF 在真實世界很常見。副檔名不保證內容。
func TestDocExtensionHoldingRTF(t *testing.T) {
	dir := t.TempDir()
	name := filepath.Join(dir, "fake.doc")
	if err := os.WriteFile(name, []byte(`{\rtf1\ansi 其實是 RTF\par}`), 0o644); err != nil {
		t.Fatal(err)
	}
	d, err := Open(name)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	if got := text(d.Blocks(0)); !strings.Contains(got, "RTF") {
		t.Errorf("內容是 %q", got)
	}
}

// 這張表就是「支援哪些格式」的答案。加格式時要一起加進來。
func TestFormatsTable(t *testing.T) {
	for ext, kind := range Formats {
		if !strings.HasPrefix(ext, ".") || ext != strings.ToLower(ext) {
			t.Errorf("副檔名要小寫並以點開頭:%q", ext)
		}
		if !Is("x" + ext) {
			t.Errorf("Is 不認得 %q", ext)
		}
		if kind.PartWord() == "" {
			t.Errorf("%q 的分段稱呼是空的", ext)
		}
	}
	for _, no := range []string{"a.txt", "a.pdf", "a.zip", "a", "a.docx.bak"} {
		if Is(no) {
			t.Errorf("Is 不該認 %q", no)
		}
	}
}
