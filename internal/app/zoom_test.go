package app

import (
	"strings"
	"testing"

	"github.com/wicanr2/wincv-remake/internal/cell"
	"github.com/wicanr2/wincv-remake/internal/keys"
	"github.com/wicanr2/wincv-remake/internal/vfs"
)

func ctrlRune(r rune) keys.Key { return keys.Key{Code: keys.Rune, R: r, Ctrl: true} }

// 字級由外層(cmd/wincv)決定有幾級,app 只負責夾在 [0, MaxZoom] 之間。
// 這個測試盯的是邊界:在最大級再放大、在 0 級再縮小都不能越界,
// 而且「沒變」要回傳 false —— 外層靠這個回傳值決定要不要重載字型。
func TestZoomClampsToRange(t *testing.T) {
	a := New(vfs.OS{}, fixture(t))
	a.MaxZoom = 2

	for i, want := range []int{1, 2, 2} {
		changed := a.HandleKey(ctrlRune('+'))
		if a.Zoom != want {
			t.Fatalf("第 %d 次放大後 Zoom = %d, 想要 %d", i+1, a.Zoom, want)
		}
		if last := i == 2; changed == last {
			t.Fatalf("第 %d 次放大 changed = %v", i+1, changed)
		}
	}
	if !a.HandleKey(ctrlRune('0')) || a.Zoom != 0 {
		t.Fatalf("Ctrl-0 之後 Zoom = %d, 想要 0", a.Zoom)
	}
	if a.HandleKey(ctrlRune('-')) || a.Zoom != 0 {
		t.Fatalf("在 0 級縮小之後 Zoom = %d, 想要 0", a.Zoom)
	}
}

// MaxZoom 沒設(只有一種字型可載)時,放大鍵不能把 Zoom 推到 1 ——
// 外層會拿它去索引字型陣列。
func TestZoomWithSingleLevel(t *testing.T) {
	a := New(vfs.OS{}, fixture(t))
	if a.HandleKey(ctrlRune('+')) || a.Zoom != 0 {
		t.Fatalf("只有一級時放大 Zoom = %d, 想要 0", a.Zoom)
	}
}

// 關於畫面:內容要真的畫出來,而且任意鍵都關得掉。
// 署名是這個 repo 存在的理由之一,漏掉不能只靠人眼看截圖。
func TestAboutScreen(t *testing.T) {
	a := New(vfs.OS{}, fixture(t))
	if !a.openAbout() || !a.Abouting() {
		t.Fatal("openAbout 沒開起來")
	}
	s := cell.New(78, 20)
	a.Draw(s)
	var sb strings.Builder
	for y := 0; y < s.Rows; y++ {
		sb.WriteString(rowText(s, y))
		sb.WriteByte('\n')
	}
	got := sb.String()
	for _, want := range []string{"WinCV Remake", "Lcc Wizard", "王俊又", "為保存台灣中文軟體盡一份心力"} {
		if !strings.Contains(got, want) {
			t.Errorf("關於畫面少了 %q", want)
		}
	}
	if !a.HandleKey(keys.Key{Code: keys.Rune, R: 'x'}) || a.Abouting() {
		t.Error("按鍵之後關於畫面沒關掉")
	}
}
