package fontset

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/wicanr2/wincv-remake/internal/render"
)

// TestLevelsHaveCJK 盯著「每一個字級都要有全形字」。
//
// 為什麼需要:倚天只有 16×15 那一組字模,而半形有三個字級,所以
// 載入尺寸(字庫自己的)與顯示尺寸(格子的)不是同一件事。把後者
// 傳給 eten.Load 的話,除了 8×15 那一級剛好相等以外,其餘字級的
// 字庫會載入失敗、全形字整批變成缺字方塊 —— 而放大倍率會自動
// 換字級(見 pickLevel),所以症狀是「放大到 1.2 倍中文就不見了」,
// 與字型設定看起來毫無關係。
func TestLevelsHaveCJK(t *testing.T) {
	dir := "../../original/app"
	eten := "../../original/eten"
	std := filepath.Join(eten, "STDFONT.15")
	if _, err := os.Stat(std); err != nil {
		t.Skip("沒有倚天字庫,跳過(tools/setup-eten.sh)")
	}
	if _, err := os.Stat(filepath.Join(dir, "cvga.fon")); err != nil {
		t.Skip("沒有原版字型,跳過(tools/setup-wine-oracle.sh)")
	}
	levels := Load(dir, std, filepath.Join(eten, "SPCFONT.15"), "", true)
	if len(levels) != len(Sizes) {
		t.Fatalf("載到 %d 個字級,預期 %d", len(levels), len(Sizes))
	}
	for i, l := range levels {
		if l.CJK == nil {
			t.Errorf("%s 這一級沒有全形字", l.Name)
			continue
		}
		// 「一」是倚天常用區的第一個字,取得到就代表索引也對。
		g := l.CJK.Glyph('一')
		if g == nil {
			t.Errorf("%s 這一級取不到「一」", l.Name)
			continue
		}
		w, h := Sizes[i].w*2, Sizes[i].h+render.LineGap
		if g.W != w || g.H != h {
			t.Errorf("%s 的全形字模是 %d×%d,預期 %d×%d", l.Name, g.W, g.H, w, h)
		}
	}
}
