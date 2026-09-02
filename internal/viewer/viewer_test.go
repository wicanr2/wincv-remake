package viewer

import (
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/wicanr2/wincv-remake/internal/cell"
	"github.com/wicanr2/wincv-remake/internal/textenc"
)

func TestLineSplitting(t *testing.T) {
	for _, tc := range []struct {
		name string
		in   string
		want []string
	}{
		{"LF", "a\nb\nc\n", []string{"a", "b", "c"}},
		{"CRLF", "a\r\nb\r\nc\r\n", []string{"a", "b", "c"}},
		{"CR", "a\rb\rc", []string{"a", "b", "c"}},
		{"沒有結尾換行", "a\nb", []string{"a", "b"}},
		{"空行保留", "a\n\nb\n", []string{"a", "", "b"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := Load("t", []byte(tc.in), textenc.ASCII)
			if len(m.Lines) != len(tc.want) {
				t.Fatalf("行數 = %d, 應為 %d (%q)", len(m.Lines), len(tc.want), tc.in)
			}
			for i, w := range tc.want {
				if got := m.Lines[i].Text(); got != w {
					t.Errorf("第 %d 行 = %q, 應為 %q", i, got, w)
				}
			}
		})
	}
}

// BBS 簽名檔的慣例是「1 代表亮色」,不是粗體。
func TestAnsiBrightIsColorNotBold(t *testing.T) {
	m := Load("t", []byte("\x1b[1;31m紅\x1b[0m白\n"), textenc.UTF8)
	if len(m.Lines) != 1 {
		t.Fatalf("行數 = %d", len(m.Lines))
	}
	sp := m.Lines[0].Spans
	if len(sp) < 2 {
		t.Fatalf("應該切成至少兩段,得到 %d 段: %+v", len(sp), sp)
	}
	if sp[0].FG != cell.LtRed {
		t.Errorf("`ESC[1;31m` 的前景 = %d, 應為亮紅 %d", sp[0].FG, cell.LtRed)
	}
	if sp[1].FG != DefaultTheme().FG {
		t.Errorf("`ESC[0m` 之後前景 = %d, 應回到預設 %d", sp[1].FG, DefaultTheme().FG)
	}
	if got := m.Lines[0].Text(); got != "紅白" {
		t.Errorf("純文字 = %q, 應為 \"紅白\" —— 控制碼不該留在內容裡", got)
	}
}

func TestAnsiBackground(t *testing.T) {
	m := Load("t", []byte("\x1b[44mblue bg\x1b[0m\n"), textenc.ASCII)
	sp := m.Lines[0].Spans
	if sp[0].BG != cell.Blue {
		t.Errorf("`ESC[44m` 的背景 = %d, 應為藍 %d", sp[0].BG, cell.Blue)
	}
}

// 關掉 ANSI 之後控制碼要被吃掉,不可以變成一堆可見的怪字元。
func TestAnsiOffStripsCodes(t *testing.T) {
	raw := "\x1b[1;31m紅\x1b[0m白"
	m := Load("t", []byte(raw), textenc.UTF8)
	m.SetAnsi(false, raw)
	if got := m.Lines[0].Text(); got != "紅白" {
		t.Errorf("關掉 ANSI 後 = %q, 應為 \"紅白\"", got)
	}
	if len(m.Lines[0].Spans) != 1 {
		t.Errorf("關掉 ANSI 後不該再分段,得到 %d 段", len(m.Lines[0].Spans))
	}
}

func TestWidthCountsWideAsTwo(t *testing.T) {
	m := Load("t", []byte("中a文"), textenc.UTF8)
	if got := m.Lines[0].Width(); got != 5 {
		t.Errorf("「中a文」寬 = %d, 應為 5", got)
	}
}

func TestSearch(t *testing.T) {
	body := "alpha\nBETA\ngamma\nbeta again\ndelta\n"
	m := Load("t", []byte(body), textenc.ASCII)
	n := m.Search("beta", 3)
	if n != 2 {
		t.Fatalf("找到 %d 筆, 應為 2(不分大小寫)", n)
	}
	if m.Hits[0] != 1 || m.Hits[1] != 3 {
		t.Errorf("命中行號 = %v, 應為 [1 3]", m.Hits)
	}
	m.NextHit(3)
	if m.HitIdx != 1 {
		t.Errorf("NextHit 之後 HitIdx = %d", m.HitIdx)
	}
	m.NextHit(3)
	if m.HitIdx != 0 {
		t.Errorf("NextHit 應該繞回第一筆,得到 %d", m.HitIdx)
	}
	m.PrevHit(3)
	if m.HitIdx != 1 {
		t.Errorf("PrevHit 應該繞到最後一筆,得到 %d", m.HitIdx)
	}
}

func TestScrollClamp(t *testing.T) {
	m := Load("t", []byte(strings.Repeat("x\n", 100)), textenc.ASCII)
	const rows = 10
	m.ScrollBy(-5, rows)
	if m.Top != 0 {
		t.Errorf("往上衝過頭 Top = %d, 應為 0", m.Top)
	}
	m.ScrollBy(9999, rows)
	if m.Top != len(m.Lines)-1 {
		t.Errorf("往下衝過頭 Top = %d, 應為 %d", m.Top, len(m.Lines)-1)
	}
	m.Home(rows)
	if m.Top != 0 || m.Left != 0 {
		t.Errorf("Home 之後 Top=%d Left=%d", m.Top, m.Left)
	}
}

// 自動換行時,一個全形字不可以被拆到兩列。
func TestWrapDoesNotSplitWideChar(t *testing.T) {
	m := Load("t", []byte("aaa中文"), textenc.UTF8)
	m.Wrap = true
	s := cell.New(4, 5) // 寬 4 格:aaa 之後只剩 1 格,放不下全形
	m.Draw(s)
	// 第 0 列應該是 "aaa" 加一個空白,全形字被推到第 1 列
	if c := s.At(3, 0); c.Ch != ' ' {
		t.Errorf("第 0 列第 4 格 = %q, 全形字不該被塞進只剩 1 格的地方", c.Ch)
	}
	if c := s.At(0, 1); c.Ch != '中' || !c.Wide {
		t.Errorf("第 1 列第 1 格 = %q(wide=%v), 應為全形「中」", c.Ch, c.Wide)
	}
}

// 用原版自己的 Big5 檔跑一遍,確認判讀 + 解碼 + 分行整條鏈可用。
func TestRealBig5File(t *testing.T) {
	b, err := os.ReadFile("../../original/app/WinCV.txt")
	if err != nil {
		t.Skip("找不到 WinCV.txt")
	}
	m := Load("WinCV.txt", b, textenc.Unknown)
	if m.Enc != textenc.Big5 {
		t.Errorf("判讀 = %v, 應為 Big5", m.Enc)
	}
	if len(m.Lines) < 50 {
		t.Errorf("只切出 %d 行,原檔應有上百行", len(m.Lines))
	}
	var joined strings.Builder
	for _, l := range m.Lines {
		joined.WriteString(l.Text())
	}
	all := joined.String()
	for _, want := range []string{"WinCV", "版權聲明", "註冊", "發展環境"} {
		if !strings.Contains(all, want) {
			t.Errorf("解出來看不到 %q", want)
		}
	}
	// 這個檔滿是 ANSI 色碼,控制碼不可以留在內容裡。
	if strings.Contains(all, "\x1b[") {
		t.Error("內容裡還留著 ANSI 控制碼")
	}
}

// --- 游標與光棒 -----------------------------------------------------------

func barModel(n int) *Model {
	var sb strings.Builder
	for i := 0; i < n; i++ {
		fmt.Fprintf(&sb, "第 %d 行\n", i+1)
	}
	return Load("t.txt", []byte(sb.String()), textenc.UTF8)
}

// 光棒預設開著:使用者要的就是「一眼看到自己在哪一行」。
func TestBarOnByDefault(t *testing.T) {
	if !barModel(3).Bar {
		t.Error("光棒預設應該是開的")
	}
}

// 游標那一列整列反白,別的列不動。
func TestBarPaintsCursorRow(t *testing.T) {
	m := barModel(10)
	s := cell.New(20, 6) // 5 列內容 + 1 列狀態
	rows := m.Draw(s)
	m.MoveBy(2, rows)
	s = cell.New(20, 6)
	m.Draw(s)

	if m.Cur != 2 {
		t.Fatalf("游標在第 %d 行", m.Cur)
	}
	for x := 0; x < s.Cols; x++ {
		if got := s.At(x, 2).BG; got != m.Theme.BarBG {
			t.Fatalf("游標列第 %d 格底色 = %v,想要 %v", x, got, m.Theme.BarBG)
		}
	}
	for _, y := range []int{0, 1, 3, 4} {
		if got := s.At(0, y).BG; got == m.Theme.BarBG {
			t.Errorf("第 %d 列不該是光棒色", y)
		}
	}
}

// 光棒關掉之後一格都不能被染色 —— ANSI 簽名檔的底色是內容的一部分。
func TestBarOffLeavesColoursAlone(t *testing.T) {
	m := barModel(10)
	m.Bar = false
	s := cell.New(20, 6)
	m.Draw(s)
	for y := 0; y < 5; y++ {
		for x := 0; x < s.Cols; x++ {
			if s.At(x, y).BG == m.Theme.BarBG {
				t.Fatalf("關掉光棒之後 (%d,%d) 還是光棒色", x, y)
			}
		}
	}
}

// 游標走出畫面才捲動。還在畫面裡就硬把它拉到正中間的話,
// 每按一次方向鍵整頁都在動。
func TestCursorScrollsOnlyAtTheEdge(t *testing.T) {
	m := barModel(100)
	const rows = 5
	for i := 0; i < rows-1; i++ {
		m.MoveBy(1, rows)
		if m.Top != 0 {
			t.Fatalf("游標到第 %d 行就捲動了(Top=%d)", m.Cur, m.Top)
		}
	}
	m.MoveBy(1, rows) // 第 5 次:游標到第 5 行,超出畫面
	if m.Top != 1 {
		t.Errorf("游標走出畫面時 Top 應該是 1,拿到 %d", m.Top)
	}
	// 往回走同理。
	for i := 0; i < 4; i++ {
		m.MoveBy(-1, rows)
	}
	if m.Cur != 1 || m.Top != 1 {
		t.Errorf("往回走之後 Cur=%d Top=%d,預期 1 / 1", m.Cur, m.Top)
	}
	m.MoveBy(-1, rows)
	if m.Top != 0 {
		t.Errorf("游標回到第 0 行時 Top 應該是 0,拿到 %d", m.Top)
	}
}

// 翻頁翻的是畫面,游標在畫面上的相對位置不變。
// 「游標往下移一整頁」會讓游標從頂端跑到底端而畫面只捲一列 —— 那不是翻頁。
func TestPageKeepsCursorOffset(t *testing.T) {
	m := barModel(100)
	const rows = 10
	m.PageBy(1, rows)
	if m.Top != 10 || m.Cur != 10 {
		t.Errorf("翻一頁之後 Top=%d Cur=%d,預期 10 / 10", m.Top, m.Cur)
	}
	m.MoveBy(3, rows) // 游標移到畫面第 3 列
	m.PageBy(1, rows)
	if m.Cur-m.Top != 3 {
		t.Errorf("翻頁之後游標在畫面第 %d 列,預期還是第 3 列", m.Cur-m.Top)
	}
}

// 捲動畫面時游標要跟著留在畫面內,不然光棒會不見。
func TestScrollKeepsCursorVisible(t *testing.T) {
	m := barModel(100)
	const rows = 5
	m.ScrollBy(20, rows)
	if m.Cur < m.Top || m.Cur > m.Top+rows-1 {
		t.Errorf("捲動之後游標 %d 不在畫面 [%d, %d] 內", m.Cur, m.Top, m.Top+rows-1)
	}
}

// 狀態列報的是游標在第幾行,不是畫面捲到第幾行 ——
// 原版的工具列也是報游標(「1 字 1 行/ 626」)。
func TestStatusReportsCursorLine(t *testing.T) {
	m := barModel(100)
	const rows = 5
	m.MoveBy(30, rows)
	s := cell.New(40, rows+1)
	m.Draw(s)
	var sb strings.Builder
	for x := 0; x < s.Cols; x++ {
		if c := s.At(x, s.Rows-1); c != nil && c.Ch != 0 {
			sb.WriteRune(c.Ch)
		}
	}
	got := strings.TrimRight(sb.String(), " ")
	t.Logf("狀態列 = %q", got)
	for _, want := range []string{"31/100", "31%"} {
		if !strings.Contains(got, want) {
			t.Errorf("狀態列 = %q,應該含 %q", got, want)
		}
	}
}

// 搜尋命中之後光棒要停在那一行,不然「找到了」看不出來停在哪。
func TestSearchMovesCursor(t *testing.T) {
	m := barModel(100)
	const rows = 5
	if n := m.Search("第 42 行", rows); n != 1 {
		t.Fatalf("找到 %d 筆", n)
	}
	if m.Cur != 41 {
		t.Errorf("命中之後游標在第 %d 行,預期 41", m.Cur)
	}
}
