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

// 放大倍率一階 0.1,而且要真的停在 0.1 的整數倍上。
//
// [雷] 0.1 在二進位裡除不盡。1.0 一路加十次 0.1 會走到
// 0.9999999999999999,顯示成「1.0×」卻與 1.0 不相等 —— 於是
// 「已經是這個倍率了」永遠不成立,每按一次都回傳 true,
// 外層每次都重建視窗與畫布。所以每一階都要量化回去。
func TestScaleStepsByTenth(t *testing.T) {
	a := New(vfs.OS{}, fixture(t))
	if a.Scale != 1 {
		t.Fatalf("預設倍率是 %v", a.Scale)
	}
	for i, want := range []float64{1.1, 1.2, 1.3, 1.4, 1.5} {
		a.HandleKey(altRune('+'))
		if a.Scale != want {
			t.Fatalf("第 %d 次放大之後是 %v,想要 %v", i+1, a.Scale, want)
		}
	}
	for i, want := range []float64{1.4, 1.3} {
		a.HandleKey(altRune('-'))
		if a.Scale != want {
			t.Fatalf("第 %d 次縮小之後是 %v,想要 %v", i+1, a.Scale, want)
		}
	}
	if !a.HandleKey(altRune('0')) || a.Scale != 1 {
		t.Fatalf("Alt-0 之後是 %v,想要 1", a.Scale)
	}
}

// 上下限要夾住,而且「沒變」要回傳 false —— 外層靠這個決定要不要重建視窗。
func TestScaleClamps(t *testing.T) {
	a := New(vfs.OS{}, fixture(t))
	for i := 0; i < 40; i++ {
		a.HandleKey(altRune('+'))
	}
	if a.Scale != MaxScale {
		t.Fatalf("一路放大之後是 %v,想要 %v", a.Scale, MaxScale)
	}
	if a.HandleKey(altRune('+')) {
		t.Error("已經在上限了還回報有變")
	}
	for i := 0; i < 40; i++ {
		a.HandleKey(altRune('-'))
	}
	if a.Scale != MinScale {
		t.Fatalf("一路縮小之後是 %v,想要 %v", a.Scale, MinScale)
	}
	if a.HandleKey(altRune('-')) {
		t.Error("已經在下限了還回報有變")
	}
}

// 訊息要說得出現在是幾倍 —— 倍率沒有別的地方看得到。
func TestScaleReportsValue(t *testing.T) {
	a := New(vfs.OS{}, fixture(t))
	a.HandleKey(altRune('+'))
	if !strings.Contains(a.Message, "1.1") {
		t.Fatalf("訊息是 %q", a.Message)
	}
}
