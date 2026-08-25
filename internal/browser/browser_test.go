package browser

import (
	"io"
	"strings"
	"testing"
	"time"

	"github.com/wicanr2/wincv-remake/internal/cell"
	"github.com/wicanr2/wincv-remake/internal/vfs"
)

// fakeFS 讓測試不碰真實檔案系統。
type fakeFS struct{ entries map[string][]vfs.Entry }

func (f fakeFS) ReadDir(dir string) ([]vfs.Entry, error) {
	return append([]vfs.Entry{}, f.entries[dir]...), nil
}
func (f fakeFS) Open(string) (io.ReadCloser, error) { return nil, nil }
func (f fakeFS) Label(dir string) string            { return dir }

func ts(s string) time.Time {
	t, _ := time.Parse("2006-01-02 15:04", s)
	return t
}

func sample() fakeFS {
	return fakeFS{entries: map[string][]vfs.Entry{
		"/w": {
			{Name: "bszip.dll", Size: 31232, ModTime: ts("2001-03-19 10:00")},
			{Name: "docs", IsDir: true, ModTime: ts("2011-01-01 08:00")},
			{Name: "7-zip32.dll", Size: 228864, ModTime: ts("2004-10-01 12:00")},
			{Name: "CAB32.DLL", Size: 82432, ModTime: ts("1999-01-18 09:00")},
			{Name: "big52gbk.txt", Size: 69865, ModTime: ts("2002-10-21 11:00")},
			{Name: "sub", IsDir: true, ModTime: ts("2010-05-05 05:05")},
		},
	}}
}

func names(m *Model) []string {
	var out []string
	for _, e := range m.Entries {
		out = append(out, e.Name)
	}
	return out
}

func eq(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// 目錄排在檔案前面,名稱比較不分大小寫 —— 原版跑在檔名大小寫不敏感的
// Windows 上,排出來就是 7-zip32、big52gbk、bszip、CAB32 這種混合大小寫字典序。
func TestSortDirsFirstCaseInsensitive(t *testing.T) {
	m := New(sample(), "/w")
	want := []string{"..", "docs", "sub", "7-zip32.dll", "big52gbk.txt", "bszip.dll", "CAB32.DLL"}
	if got := names(m); !eq(got, want) {
		t.Errorf("排序不符\n得到 %v\n預期 %v", got, want)
	}
}

func TestSortBySizeKeepsDirsFirst(t *testing.T) {
	m := New(sample(), "/w")
	m.SortKey = vfs.BySize
	m.Resort()
	got := names(m)
	if got[0] != ".." || got[1] != "docs" || got[2] != "sub" {
		t.Errorf("換排序後目錄仍應在前,得到 %v", got)
	}
	if got[3] != "bszip.dll" {
		t.Errorf("依大小遞增第一個檔案應是 bszip.dll(31232),得到 %v", got[3])
	}
}

// Resort 之後游標要跟著原本那一筆走,標記也不能掉。
func TestResortKeepsCursorAndMarks(t *testing.T) {
	m := New(sample(), "/w")
	m.MoveTo(4, 20) // big52gbk.txt
	if m.Current().Name != "big52gbk.txt" {
		t.Fatalf("前提不成立,游標在 %s", m.Current().Name)
	}
	m.Current().Marked = true
	m.SortKey = vfs.BySize
	m.Resort()
	if m.Current().Name != "big52gbk.txt" {
		t.Errorf("重排後游標跑掉了,現在在 %s", m.Current().Name)
	}
	if !m.Current().Marked {
		t.Error("重排後標記掉了")
	}
}

// ".." 不能被標記,否則「標記後對所有標記檔動作」會動到上層目錄。
func TestUpEntryCannotBeMarked(t *testing.T) {
	m := New(sample(), "/w")
	m.MoveTo(0, 20)
	m.ToggleMark(20)
	if m.Entries[0].Marked {
		t.Error("「..」被標記了")
	}
	if m.Cursor != 1 {
		t.Errorf("Space 之後游標應往下移一格,現在在 %d", m.Cursor)
	}
}

// 原版 'T' 只標檔案,Alt-T 才連目錄。
func TestMarkAll(t *testing.T) {
	m := New(sample(), "/w")
	m.MarkAll(false)
	for _, e := range m.Entries {
		if e.Up && e.Marked {
			t.Error("「..」不該被標記")
		}
		if e.IsDir && !e.Up && e.Marked {
			t.Errorf("T 不該標記目錄,但 %s 被標了", e.Name)
		}
		if !e.IsDir && !e.Marked {
			t.Errorf("T 應標記所有檔案,但 %s 沒被標", e.Name)
		}
	}
	n, bytes := m.MarkedStats()
	if n != 4 {
		t.Errorf("標記數 = %d, 應為 4", n)
	}
	if want := int64(31232 + 228864 + 82432 + 69865); bytes != want {
		t.Errorf("標記位元組 = %d, 應為 %d", bytes, want)
	}

	m.MarkAll(true)
	for _, e := range m.Entries {
		if !e.Up && !e.Marked {
			t.Errorf("Alt-T 應連目錄一起標,但 %s 沒被標", e.Name)
		}
	}

	m.UnmarkAll()
	if n, _ := m.MarkedStats(); n != 0 {
		t.Errorf("UnmarkAll 之後還有 %d 個標記", n)
	}
}

// 游標超出可視範圍時,Top 要跟著捲動;不可以捲過頭。
func TestScrolling(t *testing.T) {
	f := fakeFS{entries: map[string][]vfs.Entry{"/w": nil}}
	for i := 0; i < 50; i++ {
		f.entries["/w"] = append(f.entries["/w"],
			vfs.Entry{Name: string(rune('a'+i%26)) + string(rune('0'+i/26)) + ".txt"})
	}
	m := New(f, "/w")
	const rows = 10
	m.MoveTo(0, rows)
	if m.Top != 0 {
		t.Errorf("游標在頂端時 Top 應為 0,得到 %d", m.Top)
	}
	m.MoveTo(9, rows)
	if m.Top != 0 {
		t.Errorf("游標在第 10 列(仍可見)時 Top 不該動,得到 %d", m.Top)
	}
	m.MoveTo(10, rows)
	if m.Top != 1 {
		t.Errorf("游標移到第 11 列時 Top 應為 1,得到 %d", m.Top)
	}
	m.End(rows)
	if m.Cursor != len(m.Entries)-1 {
		t.Errorf("End 應到最後一筆")
	}
	if m.Top != len(m.Entries)-rows {
		t.Errorf("End 之後 Top = %d, 應為 %d", m.Top, len(m.Entries)-rows)
	}
	m.MoveBy(999, rows)
	if m.Cursor != len(m.Entries)-1 {
		t.Errorf("往下衝過頭應停在最後一筆,得到 %d", m.Cursor)
	}
	m.MoveBy(-999, rows)
	if m.Cursor != 0 || m.Top != 0 {
		t.Errorf("往上衝過頭應停在第一筆,得到 cursor=%d top=%d", m.Cursor, m.Top)
	}
}

func TestComma(t *testing.T) {
	for _, tc := range []struct {
		in   int64
		want string
	}{
		{0, "0"}, {7, "7"}, {999, "999"}, {1000, "1,000"},
		{228864, "228,864"}, {17366865, "17,366,865"}, {-1234, "-1,234"},
	} {
		if got := comma(tc.in); got != tc.want {
			t.Errorf("comma(%d) = %q, 應為 %q", tc.in, got, tc.want)
		}
	}
}

// 截斷不可以把全形字切成一半 —— 切一半在格點畫面上會壞掉一整列。
func TestTruncateWide(t *testing.T) {
	if got := truncate("中文字", 5); width(got) > 5 || got != "中文" {
		t.Errorf(`truncate("中文字",5) = %q (寬 %d), 應為 "中文"`, got, width(got))
	}
	if got := truncate("a中b", 2); got != "a" {
		t.Errorf(`truncate("a中b",2) = %q, 應為 "a"(放不下全形就不放)`, got)
	}
}

// 游標列換底色但字不動,而且**第 0 欄不跟著換** ——
// 那一欄是指示欄,原版量到的是一整條 #000080,游標經過也不變。
// (docs/ui/oracle-window.png 的第 2 列:800000 佔滿,000080 剛好 128 px = 一格)
func TestDrawCursorRowKeepsMarkColumn(t *testing.T) {
	m := New(sample(), "/w")
	s := cell.New(80, 12)
	m.MoveTo(2, 10)
	m.Draw(s)

	y := 1 + (m.Cursor - m.Top)
	if bg := s.At(0, y).BG; bg != m.Theme.MarkColBG {
		t.Errorf("游標列第 0 格背景 = %v, 想要指示欄底色 %v", bg, m.Theme.MarkColBG)
	}
	var line strings.Builder
	for x := 1; x < s.Cols-1; x++ { // 最後一欄是捲軸,不屬於游標列
		c := s.At(x, y)
		if c.BG != m.Theme.CursorBG {
			t.Fatalf("游標列第 %d 格背景不是游標色", x)
		}
		if !c.Cont {
			line.WriteRune(c.Ch)
		}
	}
	if !strings.Contains(line.String(), "sub") {
		t.Errorf("游標列應該是 sub,實際畫出 %q", line.String())
	}
}

// 日期欄在游標列上不變色,時間欄會 —— 兩欄的顏色本來就不同,
// 這個差別只有拿原版截圖逐格量才看得出來,很容易在重構時被抹平。
func TestDateKeepsColourUnderCursor(t *testing.T) {
	m := New(sample(), "/w")
	s := cell.New(80, 12)
	m.MoveTo(2, 10)
	m.Draw(s)
	y := 1 + (m.Cursor - m.Top)

	var dateSeen, timeSeen bool
	for x := 1; x < s.Cols-1; x++ {
		switch s.At(x, y).FG {
		case m.Theme.DateFG:
			dateSeen = true
		case m.Theme.CursorFG:
			timeSeen = true
		}
	}
	if !dateSeen {
		t.Error("游標列上找不到日期色 —— 日期被游標的白色蓋掉了")
	}
	if !timeSeen {
		t.Error("游標列上找不到游標色")
	}
}

// 主檔名欄可以拉寬:拉寬之後長檔名直接顯示在清單裡,不必看最右欄。
func TestResizeName(t *testing.T) {
	m := New(fakeFS{entries: map[string][]vfs.Entry{
		"/w": {{Name: "a-very-long-filename.txt", Size: 1}},
	}}, "/w")
	if err := m.Load("/w"); err != nil {
		t.Fatal(err)
	}
	m.MoveTo(len(m.Entries)-1, 4)
	s := cell.New(80, 6)
	m.Draw(s)
	if !strings.Contains(row(s, 2), "a-very-l ") {
		t.Fatalf("預設 8 格應該截成 a-very-l:%q", row(s, 2))
	}
	if !m.ResizeName(12) {
		t.Fatal("加寬沒生效")
	}
	m.Draw(s)
	if !strings.Contains(row(s, 2), "a-very-long-filename txt") {
		t.Fatalf("加寬到 20 格後:%q", row(s, 2))
	}
	// 夾邊界:再窄不能低於原版的 8。
	m.ResizeName(-100)
	if m.nameW() != MinNameW {
		t.Fatalf("下限應該是 %d,得到 %d", MinNameW, m.nameW())
	}
	if m.ResizeName(-1) {
		t.Error("到了下限還回 true")
	}
}

func row(s *cell.Screen, y int) string {
	var b strings.Builder
	for x := 0; x < s.Cols; x++ {
		if c := s.At(x, y); c != nil && !c.Cont {
			b.WriteRune(c.Ch)
		}
	}
	return b.String()
}
