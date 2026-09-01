package doc97

import (
	"strings"
	"testing"

	"github.com/wicanr2/wincv-remake/internal/markdown"
)

// testdata 的兩份 .doc 是 LibreOffice 產生的真實 Word 97 檔案,
// 重建方式見 testdata/README.md。用真檔而不是自己組的最小檔案是必要的:
// FIB、piece table、屬性頁三者互相指位置,自己組的檔案只會驗到
// 自己對格式的理解,驗不到「真的 Word 檔長這樣」。
func openDoc(t *testing.T, name string) *Doc {
	t.Helper()
	d, err := Open("testdata/" + name)
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

func TestRichDocument(t *testing.T) {
	bs := openDoc(t, "rich.doc").Blocks()

	// 對照 LibreOffice 轉出來的純文字:內容要一字不差。
	want := []string{"第一章 標題", "這是一段粗體與斜體的中文內文。", "1.1 小節",
		"條列一", "條列二", "甲|乙丙|丁", "A plain ASCII paragraph with a link."}
	got := strings.Split(strings.TrimRight(text(bs), "\n"), "\n")
	if len(got) != len(want) {
		t.Fatalf("區塊數不對,拿到 %d:%q", len(got), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("第 %d 個區塊是 %q,要 %q", i, got[i], want[i])
		}
	}

	// 標題靠樣式識別碼認出來,與介面語言無關。
	if bs[0].Kind != markdown.Heading || bs[0].Level != 1 {
		t.Errorf("第一章:kind=%v level=%d", bs[0].Kind, bs[0].Level)
	}
	if bs[2].Kind != markdown.Heading || bs[2].Level != 2 {
		t.Errorf("小節:kind=%v level=%d", bs[2].Kind, bs[2].Level)
	}
	if bs[3].Kind != markdown.List || bs[4].Kind != markdown.List {
		t.Errorf("條列沒有認出來:%v / %v", bs[3].Kind, bs[4].Kind)
	}
	if bs[5].Kind != markdown.Table || len(bs[5].Rows) != 2 || bs[5].Rows[1][1] != "丁" {
		t.Errorf("表格不對:%v", bs[5].Rows)
	}
}

// 中文走 UTF-16 的片段,英文走單位元組的片段。同一份文件裡兩種都有,
// 而單位元組那一種一律是 cp1252 —— 不是文件的語系字碼頁。
func TestBothPieceKinds(t *testing.T) {
	got := text(openDoc(t, "rich.doc").Blocks())
	if !strings.Contains(got, "的中文內文") {
		t.Errorf("UTF-16 片段沒解出來:%q", got)
	}
	if !strings.Contains(got, "A plain ASCII paragraph") {
		t.Errorf("單位元組片段沒解出來:%q", got)
	}
}

func TestCharacterFormatting(t *testing.T) {
	bs := openDoc(t, "rich.doc").Blocks()
	var bold, italic, link string
	for _, b := range bs {
		for _, s := range b.Spans {
			switch {
			case s.Style&markdown.Bold != 0:
				bold += s.Text
			case s.Style&markdown.Italic != 0:
				italic += s.Text
			case s.Style&markdown.Link != 0:
				link += s.Text
			}
		}
	}
	if bold != "粗體" || italic != "斜體" {
		t.Errorf("粗體 %q,斜體 %q", bold, italic)
	}
	if link != "link" {
		t.Errorf("超連結的文字是 %q", link)
	}
}

func TestFootnotes(t *testing.T) {
	bs := openDoc(t, "notes.doc").Blocks()
	got := text(bs)
	if !strings.Contains(got, "[註 1]") {
		t.Errorf("正文少了註腳標記:%q", got)
	}
	if !strings.Contains(got, "這是註腳內容") {
		t.Errorf("文末少了註腳內容:%q", got)
	}
	// 註腳的文字接在正文後面,不能混進正文那一段。
	if strings.Contains(text(bs[:1]), "這是註腳內容") {
		t.Errorf("註腳跑進正文了:%q", got)
	}
}

func TestNotADoc(t *testing.T) {
	if _, err := Open("testdata/rich.html"); err == nil {
		t.Fatal("不是複合文件應該要失敗")
	}
}
