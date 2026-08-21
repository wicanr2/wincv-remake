package render

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/wicanr2/wincv-remake/internal/cell"
	"github.com/wicanr2/wincv-remake/internal/fnt"
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

// stubHalf 是一個什麼字都畫成全黑方塊的半形來源,測繪製路徑用。
type stubHalf struct{ w, h int }

func (s stubHalf) Size() (int, int) { return s.w, s.h }
func (s stubHalf) Glyph(code byte) *fnt.Glyph {
	g := &fnt.Glyph{W: s.w, H: s.h, Bits: make([]bool, s.w*s.h)}
	// 只填上半部:最後一列留白,底線才畫得出可辨識的差別。
	for i := 0; i < s.w*(s.h-2); i++ {
		g.Bits[i] = true
	}
	return g
}

// 全形格不畫底線:倚天 16×15 的字模十五列全是字身,底線壓在筆畫上
// 而且同色,看起來是「字糊掉了」而不是「字底下有線」。
func TestUnderlineSkipsWideCells(t *testing.T) {
	r := New(stubHalf{8, 16}, nil)

	// 正對照:同一格畫兩次,只差在有沒有底線。比對最後一列 ——
	// 拿「底線那列」跟「字模那列」比是比不出東西的,兩者同色。
	lastRow := func(ch rune, under bool, wide int) []string {
		s := cell.New(4, 1)
		// 用 Print 不用 Set:Wide 與 Cont 這兩個標記是 Print 設的,
		// Set 只放一個字元 —— 拿 Set 放全形字測出來的是「一個沒有標記
		// 全形的格子」,而那正好會通過任何一種只看 Wide 的檢查。
		s.Print(0, 0, string(ch), cell.White, cell.Black)
		if under {
			s.Underline(0, 0, wide, true)
		}
		img := r.Draw(s)
		var out []string
		for x := 0; x < r.CellW*wide; x++ {
			out = append(out, fmt.Sprint(img.At(x, r.CellH-1)))
		}
		return out
	}

	plain, lined := lastRow('A', false, 1), lastRow('A', true, 1)
	if strings.Join(plain, ",") == strings.Join(lined, ",") {
		t.Fatal("半形格應該畫得出底線")
	}

	wplain, wlined := lastRow('漢', false, 2), lastRow('漢', true, 2)
	if strings.Join(wplain, ",") != strings.Join(wlined, ",") {
		t.Fatal("全形格不該畫底線")
	}
}
