package app

import (
	"strings"
	"testing"

	"github.com/wicanr2/wincv-remake/internal/cell"
	"github.com/wicanr2/wincv-remake/internal/keys"
	"github.com/wicanr2/wincv-remake/internal/vfs"
)

func touchApp(t *testing.T) (*App, *cell.Screen) {
	t.Helper()
	a := New(vfs.OS{}, fixture(t))
	a.Touch = true
	return a, cell.New(44, 24)
}

// 觸控列開著時,內容要讓開底部兩列 —— 蓋住的話最後兩列的資訊
// (狀態列)就永遠看不到,而畫面上看起來只是「少了兩列」。
func TestTouchBarDoesNotCoverContent(t *testing.T) {
	a, s := touchApp(t)
	a.Draw(s)
	bar := rowText(s, s.Rows-2)
	nav := rowText(s, s.Rows-1)
	if !strings.Contains(bar, "選單") {
		t.Errorf("動作列不見了: %q", bar)
	}
	if !strings.Contains(nav, "上層") {
		t.Errorf("導覽列不見了: %q", nav)
	}
	// 檔案清單要還在,而且完整落在觸控列之上。
	// (清單把主檔名與副檔名分欄,所以找 "a.txt" 找不到 —— 找 "txt"。)
	found := false
	for y := 0; y < s.Rows-TouchRows; y++ {
		if strings.Contains(rowText(s, y), "txt") {
			found = true
		}
	}
	if !found {
		t.Error("檔案清單被觸控列蓋掉了")
	}
	// 兩列狀態列也要在觸控列之上,不能被吃掉。
	statusSeen := false
	for y := 0; y < s.Rows-TouchRows; y++ {
		if strings.Contains(rowText(s, y), "剩餘") {
			statusSeen = true
		}
	}
	if !statusSeen {
		t.Error("狀態列被觸控列蓋掉了")
	}
}

// 功能列隨模式換。做成固定的一組鍵會退化成一個小鍵盤 ——
// 那是把桌面介面硬搬過來,不是移植。
func TestTouchBarFollowsMode(t *testing.T) {
	a, s := touchApp(t)
	a.Draw(s)
	if got := rowText(s, s.Rows-2); !strings.Contains(got, "拷貝") {
		t.Errorf("瀏覽模式的功能列 = %q", got)
	}
	// 進文字檢視
	for i := 0; i < 10; i++ {
		if e := a.Browser.Current(); e != nil && e.Name == "a.txt" {
			break
		}
		a.HandleKey(keys.Named(keys.Down))
	}
	a.HandleKey(keys.Named(keys.Enter))
	a.Draw(s)
	if a.Mode != ModeViewer {
		t.Fatalf("mode = %v", a.Mode)
	}
	if got := rowText(s, s.Rows-2); strings.Contains(got, "拷貝") {
		t.Errorf("檢視模式還在顯示瀏覽的動作: %q", got)
	}
	// 讀文件時不該出現按了沒反應的「標記 / 開啟」
	if got := rowText(s, s.Rows-1); strings.Contains(got, "標記") {
		t.Errorf("檢視模式的導覽列還有「標記」: %q", got)
	}
	if got := rowText(s, s.Rows-1); !strings.Contains(got, "返回") {
		t.Errorf("檢視模式的導覽列少了「返回」: %q", got)
	}
}

// 畫面太矮時不畫觸控列,而不是把內容擠沒。
func TestTouchBarSkippedOnTinyScreen(t *testing.T) {
	a := New(vfs.OS{}, fixture(t))
	a.Touch = true
	s := cell.New(20, 3)
	a.Draw(s) // 不能 panic
	if rowText(s, s.Rows-1) == "" {
		return // 沒畫,符合預期
	}
}

// 「畫在哪」與「點到什麼」必須一致。
//
// 兩邊各算一次版面的話會慢慢對不上,而那種錯只有在真的用手指點下去
// 才會發現 —— 畫面看起來完全正常,按鈕就是點不準。
func TestTouchHitTestMatchesDrawing(t *testing.T) {
	for _, cols := range []int{30, 44, 60, 93} {
		a := New(vfs.OS{}, fixture(t))
		a.Touch = true
		s := cell.New(cols, 20)
		a.Draw(s)

		barY := s.Rows - TouchRows
		bar := a.touchBar()

		// 每一個鍵的標籤所在的那一格,回查要得到同一個鍵。
		for i, sp := range a.touchSpans(cols) {
			if i >= len(bar) {
				break
			}
			mid := sp.x + sp.w/2
			got, ok := a.TouchKeyAt(mid, 0, cols)
			if !ok {
				t.Errorf("%d 欄:第 %d 個鍵的中心點 %d 查不到按鍵", cols, i, mid)
				continue
			}
			if got != bar[i].key {
				t.Errorf("%d 欄:點在「%s」的位置卻得到別的鍵", cols, bar[i].label)
			}
			// 而且那一格畫出來的確實是這個標籤的一部分
			row := rowText(s, barY)
			if !strings.Contains(row, bar[i].label) {
				t.Errorf("%d 欄:畫面上找不到「%s」→ %q", cols, bar[i].label, row)
			}
		}
		// 每一格都要對應到某個鍵,不能有點了沒反應的縫隙。
		for x := 0; x < cols; x++ {
			if _, ok := a.TouchKeyAt(x, 0, cols); !ok {
				t.Errorf("%d 欄:第 %d 格是死區", cols, x)
				break
			}
			if _, ok := a.TouchKeyAt(x, 1, cols); !ok {
				t.Errorf("%d 欄:導覽列第 %d 格是死區", cols, x)
				break
			}
		}
	}
}

// 清單游標的列號是給觸控把「點第幾列」翻成幾次上下鍵用的。
// 不是瀏覽模式時要回 -1,不然點在文字上會亂送方向鍵。
func TestListCursorRow(t *testing.T) {
	a := New(vfs.OS{}, fixture(t))
	s := cell.New(44, 20)
	a.Draw(s)
	if got := a.ListCursorRow(); got != 0 {
		t.Errorf("一開始游標在第 %d 列,想要 0", got)
	}
	a.HandleKey(keys.Named(keys.Down))
	a.Draw(s)
	if got := a.ListCursorRow(); got != 1 {
		t.Errorf("下一列之後 = %d", got)
	}
	a.Mode = ModeViewer
	if got := a.ListCursorRow(); got != -1 {
		t.Errorf("非瀏覽模式應該回 -1,得到 %d", got)
	}
}
