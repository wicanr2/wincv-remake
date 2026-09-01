package pdfdoc_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/wicanr2/wincv-remake/internal/pdfdoc"
)

// makePDF 手寫一份最小的 PDF。
//
// 自己造而不是放一個現成的檔案進版控:這樣測試材料的每一個位元組
// 都說得出理由,也沒有第三方檔案的授權問題。xref 的偏移量要真的算 ——
// 算錯的話物件層讀不到任何物件,而症狀是「這份 PDF 沒有頁面」。
func makePDF(t *testing.T, content string) string {
	t.Helper()
	objs := []string{
		"<</Type/Catalog/Pages 2 0 R>>",
		"<</Type/Pages/Kids[3 0 R]/Count 1>>",
		"<</Type/Page/Parent 2 0 R/MediaBox[0 0 612 792]/Contents 4 0 R" +
			"/Resources<</Font<</F1 5 0 R>>>>>>",
		fmt.Sprintf("<</Length %d>>\nstream\n%s\nendstream", len(content), content),
		"<</Type/Font/Subtype/Type1/BaseFont/Helvetica>>",
	}
	var sb strings.Builder
	sb.WriteString("%PDF-1.4\n")
	offsets := make([]int, len(objs)+1)
	for i, o := range objs {
		offsets[i+1] = sb.Len()
		fmt.Fprintf(&sb, "%d 0 obj\n%s\nendobj\n", i+1, o)
	}
	xref := sb.Len()
	fmt.Fprintf(&sb, "xref\n0 %d\n0000000000 65535 f \n", len(objs)+1)
	for i := 1; i <= len(objs); i++ {
		fmt.Fprintf(&sb, "%010d 00000 n \n", offsets[i])
	}
	fmt.Fprintf(&sb, "trailer\n<</Size %d/Root 1 0 R>>\nstartxref\n%d\n%%%%EOF\n",
		len(objs)+1, xref)

	p := filepath.Join(t.TempDir(), "t.pdf")
	if err := os.WriteFile(p, []byte(sb.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestOpenAndText(t *testing.T) {
	// 兩個字串放在同一列,中間隔開 —— 隔多遠決定要不要補一個空格。
	// 用 Td 位移而不是寫成 "(Hello World)":要測的是「靠座標間距
	// 還原空白」這條路,而字串裡的空格字元走的是另一條(它本來就是
	// 一個字元)。標準 14 字型的寬度來自內建的度量表,不必嵌在檔案裡。
	p := makePDF(t, "BT /F1 12 Tf 72 720 Td (Hello) Tj 40 0 Td (World) Tj ET\n"+
		"BT /F1 12 Tf 72 700 Td (Second) Tj 50 0 Td (line) Tj ET")
	d, err := pdfdoc.Open(p)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	if d.Pages != 1 {
		t.Fatalf("頁數 %d", d.Pages)
	}
	lines, err := d.Text(1)
	if err != nil {
		t.Fatal(err)
	}
	if len(lines) != 2 {
		t.Fatalf("解出 %d 列:%+v", len(lines), lines)
	}
	// [雷] PDF 裡沒有空格字元,字與字之間的空白是座標間距。
	// 不還原的話這裡會是 "HelloWorld"。
	if !strings.Contains(lines[0].Text, "Hello") || !strings.Contains(lines[0].Text, "World") {
		t.Fatalf("第一列是 %q", lines[0].Text)
	}
	if !strings.Contains(lines[0].Text, " ") {
		t.Errorf("兩個字之間沒有補空格:%q", lines[0].Text)
	}
	// 由上往下:PDF 的 Y 軸是由下往上長的,不翻過來會整頁倒著讀。
	if lines[0].Y < lines[1].Y {
		t.Errorf("列的順序反了:%v / %v", lines[0].Y, lines[1].Y)
	}
	if !strings.Contains(lines[1].Text, "Second") || !strings.Contains(lines[1].Text, "line") {
		t.Errorf("第二列是 %q", lines[1].Text)
	}
}

// 沒有嵌入字型時還原不出詞間空白 —— 這是已知限制,寫成測試是為了
// 讓它變成一個「知道會這樣」的事實,而不是某天被當成新 bug 重查一次。
func TestNoEmbeddedFontLosesSpaces(t *testing.T) {
	d, err := pdfdoc.Open(makePDF(t, "BT /F1 12 Tf 72 720 Td (Second line) Tj ET"))
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	lines, err := d.Text(1)
	if err != nil || len(lines) != 1 {
		t.Fatalf("%v / %+v", err, lines)
	}
	if lines[0].Text != "Secondline" {
		t.Logf("行為變了(可能是好事):%q", lines[0].Text)
	}
}

// 緊接著的兩段字不該被塞進空格。
func TestNoSpuriousSpace(t *testing.T) {
	p := makePDF(t, "BT /F1 12 Tf 72 720 Td (Con) Tj (cat) Tj ET")
	d, err := pdfdoc.Open(p)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	lines, err := d.Text(1)
	if err != nil || len(lines) != 1 {
		t.Fatalf("%v / %+v", err, lines)
	}
	if strings.Contains(lines[0].Text, " ") {
		t.Fatalf("接在一起的字被拆開了:%q", lines[0].Text)
	}
}

// [雷] 物件層對截斷或根本不是 PDF 的檔案會**無止境地掃下去** ——
// 40 個位元組的垃圾就能讓它 100% CPU 轉到天荒地老。那不是死結是活迴圈,
// 外面看起來只是「這個檔案開很久」。internal/pdf 的操作次數上限擋的就是這個。
// 這是檔案管理器,使用者會開到各種來路不明的檔。
func TestBadFilesDoNotPanic(t *testing.T) {
	dir := t.TempDir()
	for name, body := range map[string]string{
		"empty.pdf":  "",
		"text.pdf":   "這根本不是 PDF",
		"header.pdf": "%PDF-1.4\n然後就沒了",
		"trunc.pdf":  "%PDF-1.4\n1 0 obj\n<</Type/Catalog",
	} {
		p := filepath.Join(dir, name)
		os.WriteFile(p, []byte(body), 0o644)
		func() {
			defer func() {
				if r := recover(); r != nil {
					t.Errorf("%s 讓程式炸了:%v", name, r)
				}
			}()
			if d, err := pdfdoc.Open(p); err == nil {
				d.Close()
				t.Errorf("%s 不該打得開", name)
			}
		}()
	}
}

func TestPageOutOfRange(t *testing.T) {
	d, err := pdfdoc.Open(makePDF(t, "BT /F1 12 Tf 72 720 Td (x) Tj ET"))
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	for _, pg := range []int{0, -1, 2, 999} {
		if _, err := d.Text(pg); err == nil {
			t.Errorf("第 %d 頁不該有東西", pg)
		}
	}
}
