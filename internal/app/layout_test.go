package app

import (
	"math"
	"testing"
)

// Android 上真的發生過的那組值:EbitenView 用 deviceScale=0 去除,
// 得到 +Inf,Ebiten 的 Layout 介面收 int,轉出來是 INT64_MIN。
func TestSaneLayoutRejectsInfConvertedToInt(t *testing.T) {
	bad := int(math.Inf(1))
	w, h := SaneLayout(bad, bad, 0, 0, 0, 0)
	if w <= 0 || h <= 0 {
		t.Fatalf("回了非正數 %d×%d —— Ebiten 會 panic", w, h)
	}
	if w != fallbackW || h != fallbackH {
		t.Fatalf("沒有上一次的好值也沒有螢幕尺寸時應該用保底值,得到 %d×%d", w, h)
	}
}

func TestSaneLayout(t *testing.T) {
	bad := int(math.Inf(1))
	cases := []struct {
		name                           string
		w, h, lastW, lastH, monW, monH int
		wantW, wantH                   int
	}{
		{"正常值直接用", 1080, 2340, 0, 0, 0, 0, 1080, 2340},
		{"壞值退回上一次的好值", bad, bad, 393, 851, 0, 0, 393, 851},
		{"沒有上一次就用螢幕尺寸", bad, bad, 0, 0, 393, 851, 393, 851},
		{"零也是壞值", 0, 0, 393, 851, 0, 0, 393, 851},
		{"負值是壞值", -5, -5, 393, 851, 0, 0, 393, 851},
		{"只有一邊壞掉也整組換掉", 1080, 0, 393, 851, 0, 0, 393, 851},
		{"大得不合理也是壞值", 1 << 30, 1 << 30, 393, 851, 0, 0, 393, 851},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			w, h := SaneLayout(c.w, c.h, c.lastW, c.lastH, c.monW, c.monH)
			if w != c.wantW || h != c.wantH {
				t.Fatalf("要 %d×%d,得到 %d×%d", c.wantW, c.wantH, w, h)
			}
		})
	}
}
