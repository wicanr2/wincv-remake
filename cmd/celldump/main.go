// celldump 把一個畫面渲染成 PNG,不開視窗。
//
// 用途是驗收:remake 的畫面要跟原版做格點比對,而比對不該需要顯示器。
// 這支程式與 Ebiten 那條路徑共用同一個 render.Rasterizer,
// 所以「headless 畫出來的」跟「視窗裡看到的」是同一份像素。
package main

import (
	"flag"
	"fmt"
	"image/png"
	"os"
	"path/filepath"

	"github.com/wicanr2/wincv-remake/internal/browser"
	"github.com/wicanr2/wincv-remake/internal/cell"
	"github.com/wicanr2/wincv-remake/internal/eten"
	"github.com/wicanr2/wincv-remake/internal/fnt"
	"github.com/wicanr2/wincv-remake/internal/hexview"
	"github.com/wicanr2/wincv-remake/internal/imgfmt"
	"github.com/wicanr2/wincv-remake/internal/imgview"
	"github.com/wicanr2/wincv-remake/internal/textenc"
	"github.com/wicanr2/wincv-remake/internal/viewer"
	"github.com/wicanr2/wincv-remake/internal/render"
	"github.com/wicanr2/wincv-remake/internal/vfs"
)

// drawFile 依內容判斷要用看圖、文字檢視還是 16 進位檢視,
// 與 app 層的規則一致。
func drawFile(s *cell.Screen, name string, cw, ch int) (*render.Overlay, error) {
	data, err := os.ReadFile(name)
	if err != nil {
		return nil, err
	}
	base := filepath.Base(name)
	if imgfmt.IsImage(base) {
		m, err := imgview.Load(base, data)
		if err == nil {
			return m.Draw(s, cw, ch), nil
		}
		hexview.Load(base, data).Draw(s)
		return nil, nil
	}
	v := viewer.Load(base, data, textenc.Unknown)
	if v.Enc == textenc.Binary {
		hexview.Load(base, data).Draw(s)
		return nil, nil
	}
	v.Draw(s)
	return nil, nil
}

func browserModel(dir string) (*browser.Model, error) {
	m := browser.New(vfs.OS{}, dir)
	if err := m.Load(dir); err != nil {
		return nil, err
	}
	return m, nil
}

func main() {
	var (
		halfPath = flag.String("half", "original/app/cvga.fon", "半形 .FON")
		stdPath  = flag.String("eten-std", "original/eten/STDFONT.15", "倚天漢字區")
		spcPath  = flag.String("eten-spc", "original/eten/SPCFONT.15", "倚天符號區")
		out      = flag.String("o", "screen.png", "輸出 PNG")
		cols     = flag.Int("cols", 80, "欄數")
		rows     = flag.Int("rows", 25, "列數")
		dir      = flag.String("dir", "", "要瀏覽的目錄。留空則畫字型與配色的示範畫面")
		file     = flag.String("file", "", "要檢視的檔案(文字或 16 進位,依內容自動判斷)")
	)
	flag.Parse()

	half, err := loadHalf(*halfPath)
	if err != nil {
		die(err)
	}
	cjk, err := eten.Load(*stdPath, *spcPath, half.PixWidth*2, half.PixHeight)
	if err != nil {
		fmt.Fprintf(os.Stderr, "警告:載不到倚天字庫,全形字會留白 (%v)\n", err)
	}

	s := cell.New(*cols, *rows)
	var overlay *render.Overlay
	if *file != "" {
		ov, err := drawFile(s, *file, half.PixWidth, half.PixHeight)
		if err != nil {
			die(err)
		}
		overlay = ov
	} else if *dir != "" {
		m, err := browserModel(*dir)
		if err != nil {
			die(err)
		}
		m.Draw(s)
	} else {
		demo(s)
	}

	r := render.New(half, cjkOrNil(cjk))
	img := r.DrawWith(s, overlay)

	f, err := os.Create(*out)
	if err != nil {
		die(err)
	}
	defer f.Close()
	if err := png.Encode(f, img); err != nil {
		die(err)
	}
	w, h := r.Size(s.Cols, s.Rows)
	fmt.Printf("%s  %dx%d px  (%dx%d 格,每格 %dx%d)\n",
		*out, w, h, s.Cols, s.Rows, r.CellW, r.CellH)
}

func cjkOrNil(f *eten.Font) render.CJKSource {
	if f == nil {
		return nil
	}
	return f
}

func loadHalf(p string) (*fnt.Font, error) {
	d, err := os.ReadFile(p)
	if err != nil {
		return nil, err
	}
	return fnt.Parse(d)
}

// demo 畫一個能一眼看出「半形、全形、配色、格點」都對的畫面。
func demo(s *cell.Screen) {
	s.Clear(cell.LtGray, cell.Black)

	s.Fill(0, 0, s.Cols, 1, ' ', cell.Black, cell.LtGray)
	s.Print(1, 0, "1檔案 2目錄/磁碟 3顏色 4檢視 5環境 6其他 7說明", cell.Black, cell.LtGray)

	s.Print(2, 2, "半形 ASCII:", cell.White, cell.Black)
	line := ""
	for c := 0x20; c < 0x7F; c++ {
		line += string(rune(c))
	}
	s.Print(2, 3, line[:min(len(line), s.Cols-2)], cell.LtGray, cell.Black)

	s.Print(2, 5, "全形漢字:", cell.White, cell.Black)
	s.Print(2, 6, "檔案瀏覽器 壓縮檔管理 文字檢視 編碼轉換", cell.LtCyan, cell.Black)
	s.Print(2, 7, "全形標點:，。！？「」『』（）《》～", cell.Yellow, cell.Black)

	s.Print(2, 9, "16 色:", cell.White, cell.Black)
	for i := 0; i < 16; i++ {
		s.Print(2+i*4, 10, fmt.Sprintf("%2d", i), cell.Color(i), cell.Black)
		s.Fill(2+i*4+2, 10, 2, 1, ' ', cell.Black, cell.Color(i))
	}

	s.Print(2, 12, "反白游標列示範(整列換屬性,字不動):", cell.White, cell.Black)
	s.Print(2, 13, "7-zip32  dll     228,864  2004-10-01  10:00", cell.LtGray, cell.Black)
	s.Print(2, 14, "big52gbk txt      69,865  2002-10-21  10:00", cell.LtGray, cell.Black)
	s.SetAttr(2, 14, 44, cell.Black, cell.LtGray)

	s.Print(2, 16, "全形字佔兩格,右半格是佔位:中|文|字", cell.LtGreen, cell.Black)
	s.Print(2, 17, "混排 mixed 中英 test 對齊 align", cell.LtMagenta, cell.Black)
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func die(err error) {
	fmt.Fprintln(os.Stderr, "錯誤:", err)
	os.Exit(1)
}
