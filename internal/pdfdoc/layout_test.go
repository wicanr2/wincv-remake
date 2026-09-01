package pdfdoc

import (
	"strings"
	"testing"

	"github.com/wicanr2/wincv-remake/internal/pdf"
)

// texts 依「每一列一個字串」造出字。x 是列的起點,每個字寬 size/2。
func texts(size float64, rows []struct {
	x, y float64
	s    string
}) []pdf.Text {
	var out []pdf.Text
	for _, r := range rows {
		x := r.x
		for _, ch := range r.s {
			w := size / 2
			out = append(out, pdf.Text{X: x, Y: r.y, W: w, Size: size, S: string(ch)})
			x += w
		}
	}
	return out
}

type rowSpec = struct {
	x, y float64
	s    string
}

// 雙欄內文要一欄讀完再讀下一欄。同一列上的兩欄接成一句話是最糟的
// 結果 —— 每一句都斷在中間,而畫面上看起來很正常。
func TestTwoColumnsReadDown(t *testing.T) {
	var rows []rowSpec
	for i := 0; i < 20; i++ {
		y := 700 - float64(i)*14
		rows = append(rows, rowSpec{72, y, "LEFTLEFTLEFTLEFTLEFTLEFT"})
		rows = append(rows, rowSpec{250, y, "RIGHTRIGHTRIGHTRIGHTRIGH"})
	}
	lines := layout(texts(10, rows), 0, 595)
	if len(lines) != 40 {
		t.Fatalf("要 40 列,拿到 %d", len(lines))
	}
	for i, l := range lines {
		want := "LEFT"
		if i >= 20 {
			want = "RIGHT"
		}
		if !strings.HasPrefix(l.Text, want) {
			t.Fatalf("第 %d 列是 %q,應該屬於 %s 欄", i, l.Text, want)
		}
	}
}

// 表格也有一條通到底的縱向空白帶,但它要橫著讀。
// 判準是「大部分的列有沒有填滿欄寬」——儲存格不會。
func TestTableIsNotSplitIntoColumns(t *testing.T) {
	var rows []rowSpec
	for i := 0; i < 20; i++ {
		y := 700 - float64(i)*14
		rows = append(rows, rowSpec{72, y, "cell"})
		rows = append(rows, rowSpec{330, y, "cell"})
	}
	lines := layout(texts(10, rows), 0, 595)
	if len(lines) != 20 {
		t.Fatalf("表格應該一列一列讀,拿到 %d 列", len(lines))
	}
	if !strings.Contains(lines[0].Text, "cell") || strings.Count(lines[0].Text, "cell") != 2 {
		t.Errorf("同一列的兩格要在同一列:%q", lines[0].Text)
	}
}

// 列數太少時不分欄:證據不足。
func TestShortPageIsNotSplit(t *testing.T) {
	var rows []rowSpec
	for i := 0; i < 4; i++ {
		y := 700 - float64(i)*14
		rows = append(rows, rowSpec{72, y, "LEFTLEFTLEFTLEFTLEFTLEFT"})
		rows = append(rows, rowSpec{250, y, "RIGHTRIGHTRIGHTRIGHTRIGH"})
	}
	if lines := layout(texts(10, rows), 0, 595); len(lines) != 4 {
		t.Fatalf("要 4 列,拿到 %d", len(lines))
	}
}

// 座標之間的間距要還原成空格。
func TestSpaceFromGap(t *testing.T) {
	ts := []pdf.Text{
		{X: 72, Y: 700, W: 6, Size: 12, S: "a"},
		{X: 78, Y: 700, W: 6, Size: 12, S: "b"},
		{X: 100, Y: 700, W: 6, Size: 12, S: "c"},
	}
	lines := layout(ts, 0, 595)
	if len(lines) != 1 || lines[0].Text != "ab c" {
		t.Fatalf("拿到 %+v", lines)
	}
}

// 寬度是 0 的字型不能拿來算間距:每個字後面都會多一個空格。
func TestZeroWidthDoesNotInsertSpaces(t *testing.T) {
	var ts []pdf.Text
	for i, ch := range "hello" {
		ts = append(ts, pdf.Text{X: 72 + float64(i)*6, Y: 700, W: 0, Size: 12, S: string(ch)})
	}
	lines := layout(ts, 0, 595)
	if len(lines) != 1 || lines[0].Text != "hello" {
		t.Fatalf("拿到 %+v", lines)
	}
}
