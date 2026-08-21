package browser

import (
	"strings"
	"testing"

	"github.com/wicanr2/wincv-remake/internal/cell"
	"github.com/wicanr2/wincv-remake/internal/vfs"
)

// 捲軸的滑塊要能碰到上下兩端。用總筆數當分母算位置的話,捲到底時
// 滑塊還差一截碰不到下緣 —— 畫面上看起來像「捲不動了」。
func TestScrollbarThumbReachesBothEnds(t *testing.T) {
	m := New(sample(), "/w")
	// 塞到遠多於一頁
	for i := 0; i < 200; i++ {
		m.Entries = append(m.Entries, Entry{})
	}
	s := cell.New(80, 12)
	rows := m.Draw(s)
	x := s.Cols - 1

	thumbSpan := func() (top, bot int) {
		top, bot = -1, -1
		for y := 2; y <= rows-1; y++ {
			if s.At(x, y).Ch == cell.Block {
				if top < 0 {
					top = y
				}
				bot = y
			}
		}
		return
	}

	m.Top = 0
	m.Draw(s)
	if top, _ := thumbSpan(); top != 2 {
		t.Errorf("捲到最上面時滑塊從第 %d 列開始,應該貼齊第 2 列", top)
	}

	// 游標也要跟著移到最後 —— Draw 會 clamp,不然 Top 會被拉回去。
	m.Cursor = len(m.Entries) - 1
	m.Top = len(m.Entries) - rows
	m.Draw(s)
	if _, bot := thumbSpan(); bot != rows-1 {
		t.Errorf("捲到最下面時滑塊停在第 %d 列,應該貼齊第 %d 列", bot, rows-1)
	}
}

// 全部看得到就不該畫滑塊 —— 有滑塊等於告訴使用者「下面還有東西」。
func TestScrollbarNoThumbWhenEverythingFits(t *testing.T) {
	m := New(sample(), "/w")
	s := cell.New(80, 20)
	rows := m.Draw(s)
	x := s.Cols - 1
	for y := 2; y <= rows-1; y++ {
		if s.At(x, y).Ch == cell.Block {
			t.Fatalf("全部看得到卻在第 %d 列畫了滑塊", y)
		}
	}
	if s.At(x, 1).Ch != cell.ArrowUp || s.At(x, rows).Ch != cell.ArrowDown {
		t.Error("上下箭頭沒畫")
	}
}

// 清單很長時滑塊會被算成 0 格而消失。至少要留一格。
func TestScrollbarThumbAtLeastOneCell(t *testing.T) {
	m := New(sample(), "/w")
	for i := 0; i < 5000; i++ {
		m.Entries = append(m.Entries, Entry{})
	}
	s := cell.New(80, 12)
	rows := m.Draw(s)
	x := s.Cols - 1
	n := 0
	for y := 2; y <= rows-1; y++ {
		if s.At(x, y).Ch == cell.Block {
			n++
		}
	}
	if n < 1 {
		t.Error("清單太長時滑塊消失了")
	}
}

// 磁碟窗格開著時,檔案清單要整個往右讓 —— 不讓的話兩邊會疊在一起,
// 而疊出來的畫面「看起來只是有點怪」,不像 bug。
func TestDrivePaneShiftsList(t *testing.T) {
	m := New(sample(), "/w")
	s := cell.New(80, 12)
	m.Draw(s)
	before := rowTextOf(s, 1)

	m.DrivePane = 10
	m.Drives = []vfs.Drive{{Label: "/", Path: "/"}, {Label: "usb", Path: "/media/usb", Volume: true}}
	m.Draw(s)
	after := rowTextOf(s, 1)

	if before == after {
		t.Fatal("開了磁碟窗格,清單卻沒有移位")
	}
	// 前 10 欄現在是磁碟窗格
	if got := s.At(0, 1); got.Ch != '/' {
		t.Errorf("磁碟窗格第一列 = %q, 想要 /", got.Ch)
	}
	if s.At(0, 1).BG != m.Theme.CursorBG {
		t.Error("磁碟窗格的游標列沒有反白")
	}
	if s.At(0, 2).FG != m.Theme.DriveVolumeFG {
		t.Error("可卸除磁碟沒有用不同顏色")
	}
}

// 狀態列上緣要有那條 2 px 分隔線。它是格子的屬性,不是一整列,
// 所以只有逐格看得到 —— 很容易在改版面時整條消失而沒人發現。
func TestStatusHasRule(t *testing.T) {
	m := New(sample(), "/w")
	s := cell.New(80, 12)
	m.Draw(s)
	y := m.statusY(s)
	for x := 0; x < s.Cols; x++ {
		if !s.At(x, y).Rule {
			t.Fatalf("狀態列第 %d 格上緣沒有分隔線", x)
		}
	}
	if s.At(0, y-1).Rule {
		t.Error("分隔線畫到清單的最後一列上了")
	}
}

func rowTextOf(s *cell.Screen, y int) string {
	var out []rune
	for x := 0; x < s.Cols; x++ {
		if c := s.At(x, y); !c.Cont {
			out = append(out, c.Ch)
		}
	}
	return string(out)
}

// 窄畫面上路徑列與右邊的統計會撞在一起。撞上去的症狀是路徑被蓋掉一半,
// 看起來像「路徑本身是錯的」—— 這在手機那種寬度是常態,不是邊角情況。
func TestPathBarDoesNotCollide(t *testing.T) {
	m := New(sample(), "/very/long/path/that/will/not/fit/anywhere")
	for _, cols := range []int{30, 40, 44, 60, 100} {
		s := cell.New(cols, 12)
		m.Draw(s)
		// 路徑那一段(到 *.* 為止)不可以覆蓋到右邊那一塊。
		// 判準:整列不能有「非空白之後又出現路徑字元」的交錯,
		// 直接檢查右段起點前一格是空白就夠。
		row := rowTextOf(s, 0)
		// rowTextOf 會跳過全形字的右半格,所以要量顯示寬度不是 rune 數。
		if w := width(row); w != cols {
			t.Fatalf("%d 欄:路徑列寬 %d → %q", cols, w, row)
		}
		if !strings.Contains(row, "*.*") {
			t.Errorf("%d 欄:遮罩 *.* 不見了 → %q", cols, row)
		}
		if !strings.Contains(row, "標記") {
			t.Errorf("%d 欄:右邊的統計不見了 → %q", cols, row)
		}
		// 路徑放不下時要留尾端(現在在哪個目錄),前面用刪節號。
		if cols <= 44 && !strings.Contains(row, "…") {
			t.Errorf("%d 欄:路徑沒有截斷 → %q", cols, row)
		}
	}
}
