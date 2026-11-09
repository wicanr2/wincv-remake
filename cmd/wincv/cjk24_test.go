package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/wicanr2/wincv-remake/internal/app"
	"github.com/wicanr2/wincv-remake/internal/fontset"
	"github.com/wicanr2/wincv-remake/internal/vfs"
)

// 這條收據使用未納入版控的真實素材，驗證設定選單要求的「先載入，
// 成功才交換」路徑。一般 CI 沒有素材時跳過。
func TestReloadCJK24UsesGuoqiaoAndKeepsPriorOnFailure(t *testing.T) {
	eten24 := os.Getenv("WINCV_ETEN24_DIR")
	guoqiao := os.Getenv("WINCV_GUOQIAO_DIR")
	if eten24 == "" || guoqiao == "" {
		t.Skip("未提供 24 點 ETEN 與國喬素材目錄")
	}
	appDir := "../../original/app"
	eten15 := "../../original/eten"
	for _, p := range []string{filepath.Join(appDir, "cvga.fon"), filepath.Join(eten15, "STDFONT.15"), filepath.Join(eten24, "STD.24M"), filepath.Join(guoqiao, "stdfont.24f")} {
		if _, err := os.Stat(p); err != nil {
			t.Skipf("缺少外部驗證素材 %s", p)
		}
	}
	levels := fontset.Load(appDir, filepath.Join(eten15, "STDFONT.15"), filepath.Join(eten15, "SPCFONT.15"), filepath.Join(eten24, "STD.24M"), filepath.Join(eten24, "SPCFONT.24"), "", true)
	if len(levels) == 0 {
		t.Fatal("初始字形載入失敗")
	}
	a := app.New(vfs.OS{}, t.TempDir())
	a.MaxZoom = len(levels) - 1
	g := &game{app: a, levels: levels, scale: 1, zoom: -1, cjk24: "eten-m",
		fontDir: appDir, etenStd: filepath.Join(eten15, "STDFONT.15"), etenSpc: filepath.Join(eten15, "SPCFONT.15"), noFallback: true}
	g.setZoom(0)
	t.Setenv("WINCV_HOME", guoqiao)
	a.CJK24 = "guoqiao"
	g.reloadCJK24()
	if g.cjk24 != "guoqiao" || a.CJK24 != "guoqiao" {
		t.Fatalf("國喬沒有套用: game=%q app=%q", g.cjk24, a.CJK24)
	}
	t.Setenv("WINCV_HOME", t.TempDir())
	a.CJK24 = "eten-k"
	g.reloadCJK24()
	if g.cjk24 != "guoqiao" || a.CJK24 != "guoqiao" {
		t.Fatalf("失敗來源覆寫了目前字形: game=%q app=%q", g.cjk24, a.CJK24)
	}
}

// 倚天 2000 的檔名放在素材目錄裡就認得,不必改名成 STD.24*(GitHub issue #11)。
func TestCJK24PathsFindsEten2000Names(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "STDFONT.24K"), make([]byte, 72), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("WINCV_HOME", dir)
	std, _, ok := cjk24Paths("eten-k")
	if !ok || std != filepath.Join(dir, "STDFONT.24K") {
		t.Fatalf("找到的是 %q", std)
	}
}

// 找不到時要講要找的檔名,不能丟出一個使用者看不懂的相對路徑。
func TestReloadCJK24MissingNamesTheFiles(t *testing.T) {
	t.Setenv("WINCV_HOME", t.TempDir())
	a := app.New(vfs.OS{}, t.TempDir())
	g := &game{app: a, cjk24: "eten-m"}
	a.CJK24 = "eten-k"
	g.reloadCJK24()
	if a.CJK24 != "eten-m" {
		t.Fatalf("失敗時應退回原本的來源,現在是 %q", a.CJK24)
	}
	if !strings.Contains(a.Message, "STD.24K / STDFONT.24K") || strings.Contains(a.Message, "original") {
		t.Fatalf("訊息沒講清楚要找哪個檔:%q", a.Message)
	}
}
