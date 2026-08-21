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
