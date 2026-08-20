package render

import (
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/wicanr2/wincv-remake/internal/cell"
)

// 色表與名字表要一樣長,不然 keyword_*.cfg 對名字時會越界。
func TestPaletteMatchesNames(t *testing.T) {
	if len(DefaultPalette) != int(cell.NumColors) {
		t.Fatalf("色表有 %d 個,常數說 %d 個", len(DefaultPalette), cell.NumColors)
	}
	if len(cell.Names) != int(cell.NumColors) {
		t.Fatalf("名字表有 %d 個", len(cell.Names))
	}
	if cell.NumConfigColors > int(cell.NumColors) {
		t.Fatal("設定檔可用的顏色數不該超過總數")
	}
	for i, n := range cell.Names {
		if n == "" {
			t.Errorf("第 %d 個顏色沒有名字", i)
		}
	}
}

// 檔案清單量到的顏色。改動色表時這幾個一定要跟著對。
func TestMeasuredFileListColors(t *testing.T) {
	cases := []struct {
		c    cell.Color
		hex  string
		note string
	}{
		{cell.DirGreen, "14BE00", "目錄"},
		{cell.LtRed, "FF0000", "exe / com"},
		{cell.LtMagenta, "FF00FF", "bat / cmd"},
		{cell.DirCyan, "1EBEBE", "壓縮檔"},
		{cell.DirLtGreen, "00F000", "圖檔"},
		{cell.LtGray, "C0C0C0", "其他"},
	}
	for _, tc := range cases {
		got := DefaultPalette[tc.c]
		if h := hex3(got.R, got.G, got.B); h != tc.hex {
			t.Errorf("%s(%s):色表是 #%s,量到的是 #%s",
				tc.note, cell.Names[tc.c], h, tc.hex)
		}
	}
}

func hex3(r, g, b uint8) string {
	const d = "0123456789ABCDEF"
	out := []byte{d[r>>4], d[r&15], d[g>>4], d[g&15], d[b>>4], d[b&15]}
	return string(out)
}

// 色表要與 image 裡的定義一致。有原版素材時才跑。
func TestPaletteAgainstImage(t *testing.T) {
	img := "../../original/app/WINCV.IMG"
	if _, err := os.Stat(img); err != nil {
		t.Skip("沒有 WINCV.IMG,跳過")
	}
	out, err := exec.Command("python3", "../../tools/palette.py", img).Output()
	if err != nil {
		t.Skipf("跑不動 tools/palette.py: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	if len(lines) != cell.NumConfigColors {
		t.Fatalf("抽出 %d 行,期望 %d", len(lines), cell.NumConfigColors)
	}
	for i, line := range lines {
		c := DefaultPalette[i]
		want := "{0x" + hex3(c.R, c.G, c.B)[0:2] + ", 0x" + hex3(c.R, c.G, c.B)[2:4] +
			", 0x" + hex3(c.R, c.G, c.B)[4:6] + ", 0xFF}"
		if !strings.Contains(line, want) {
			t.Errorf("第 %d 個(%s):image 說 %q,色表是 %s",
				i, cell.Names[i], strings.TrimSpace(line), want)
		}
	}
}
