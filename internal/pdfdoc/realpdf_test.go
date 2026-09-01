package pdfdoc

import (
	"strings"
	"testing"
)

// 借 internal/pdf 的測試檔:那兩份是 LibreOffice 產生的真實 PDF,
// 重建方式見該目錄的 README。手寫的最小 PDF 驗得到組裝邏輯,
// 但驗不到真實檔案的字型形態 —— 而中文 PDF 的字型正是最容易解錯的地方。
const (
	cjkPDF    = "../pdf/testdata/rich.pdf"
	twoColPDF = "../pdf/testdata/twocol.pdf"
)

func pageText(t *testing.T, path string, page int) string {
	t.Helper()
	d, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	lines, err := d.Text(page)
	if err != nil {
		t.Fatal(err)
	}
	var sb strings.Builder
	for _, l := range lines {
		sb.WriteString(l.Text)
		sb.WriteString("\n")
	}
	return sb.String()
}

// 中文 PDF 用的是子集化的 CID 字型:字碼是字型內部的編號,
// 要走 ToUnicode 才變得回文字。不走的話整頁是亂碼,而那是合法的字串,
// 不會有任何錯誤訊息。
func TestRealCJKText(t *testing.T) {
	got := pageText(t, cjkPDF, 1)
	for _, want := range []string{
		"第一章 標題",
		"這是一段粗體與斜體的中文內文。",
		"1.1 小節",
		"A plain ASCII paragraph with a link.",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("少了 %q:\n%s", want, got)
		}
	}
}

// 書籤是 PDF 自己帶的目錄,比一排頁碼有用得多。
func TestRealOutline(t *testing.T) {
	d, err := Open(cjkPDF)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	marks := d.Outline()
	if len(marks) < 2 {
		t.Fatalf("書籤只有 %d 筆", len(marks))
	}
	if marks[0].Title != "第一章 標題" || marks[0].Page != 1 {
		t.Errorf("第一筆書籤是 %q(第 %d 頁)", marks[0].Title, marks[0].Page)
	}
	if marks[1].Level <= marks[0].Level {
		t.Errorf("小節應該比章更深一層:%d / %d", marks[1].Level, marks[0].Level)
	}
}

// 雙欄的內文要一欄讀完再讀下一欄。段落編號是連續的,讀錯順序就會亂掉。
func TestRealTwoColumnOrder(t *testing.T) {
	got := pageText(t, twoColPDF, 1)
	last := 0
	for i := 1; i <= 12; i++ {
		mark := "Paragraph " + itoa(i) + "."
		at := strings.Index(got, mark)
		if at < 0 {
			continue
		}
		if at < last {
			t.Fatalf("第 %d 段出現在前一段之前 —— 欄的順序讀反了\n%s", i, got)
		}
		last = at
	}
	// 一列只能有一欄的字。兩欄併成一列的話,列長會是欄寬的兩倍,
	// 而且每一句都會在中間被另一欄的字插斷。
	if !strings.Contains(got, "Paragraph 1. The quick brown fox jumps over") {
		t.Errorf("第一列不是完整的一句開頭:\n%s", got)
	}
	for _, line := range strings.Split(strings.TrimSpace(got), "\n") {
		if len(line) > 70 {
			t.Fatalf("這一列有 %d 個字元,是兩欄被併起來了:%q", len(line), line)
		}
	}
}

func itoa(n int) string {
	if n < 10 {
		return string(rune('0' + n))
	}
	return string(rune('0'+n/10)) + string(rune('0'+n%10))
}
