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
	"time"

	"github.com/wicanr2/wincv-remake/internal/app"
	"github.com/wicanr2/wincv-remake/internal/browser"
	"github.com/wicanr2/wincv-remake/internal/bundled"
	"github.com/wicanr2/wincv-remake/internal/cell"
	"github.com/wicanr2/wincv-remake/internal/editor"
	"github.com/wicanr2/wincv-remake/internal/eten"
	"github.com/wicanr2/wincv-remake/internal/fnt"
	"github.com/wicanr2/wincv-remake/internal/hexview"
	"github.com/wicanr2/wincv-remake/internal/imgfmt"
	"github.com/wicanr2/wincv-remake/internal/imgview"
	"github.com/wicanr2/wincv-remake/internal/keys"
	"github.com/wicanr2/wincv-remake/internal/render"
	"github.com/wicanr2/wincv-remake/internal/syntax"
	"github.com/wicanr2/wincv-remake/internal/textenc"
	"github.com/wicanr2/wincv-remake/internal/ttf"
	"github.com/wicanr2/wincv-remake/internal/vfs"
	"github.com/wicanr2/wincv-remake/internal/viewer"
)

// drawFile 依內容判斷要用看圖、文字檢視還是 16 進位檢視,
// 與 app 層的規則一致。
func drawFile(s *cell.Screen, name string, cw, ch int) ([]*render.Overlay, error) {
	data, err := os.ReadFile(name)
	if err != nil {
		return nil, err
	}
	base := filepath.Base(name)
	if imgfmt.IsImage(base) {
		m, err := imgview.Load(base, data)
		if err == nil {
			return []*render.Overlay{m.Draw(s, cw, ch)}, nil
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

func drawEdit(s *cell.Screen, name, cfgDir string) error {
	data, err := os.ReadFile(name)
	if err != nil {
		return err
	}
	var cfg *syntax.Config
	if set, err := syntax.LoadSet(cfgDir); err == nil {
		cfg = set.For(name)
	}
	editor.Load(filepath.Base(name), data, textenc.Unknown, cfg).Draw(s)
	return nil
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
		halfPath  = flag.String("half", "original/app/cvga.fon", "半形 .FON")
		stdPath   = flag.String("eten-std", "original/eten/STDFONT.15", "倚天漢字區")
		spcPath   = flag.String("eten-spc", "original/eten/SPCFONT.15", "倚天符號區")
		out       = flag.String("o", "screen.png", "輸出 PNG")
		cols      = flag.Int("cols", 80, "欄數")
		rows      = flag.Int("rows", 25, "列數")
		dir       = flag.String("dir", "", "要瀏覽的目錄。留空則畫字型與配色的示範畫面")
		file      = flag.String("file", "", "要檢視的檔案(文字或 16 進位,依內容自動判斷)")
		edit      = flag.String("edit", "", "用編輯器開這個檔案(含語法上色)")
		cfgDir    = flag.String("cfg", "original/app", "語法上色設定所在的目錄")
		appDir    = flag.String("app", "", "跑完整的 app(含選單、模式切換)並瀏覽這個目錄")
		keyStr    = flag.String("keys", "", "先送這一串按鍵再截圖,逗號分隔,例如 F1,Down,Down")
		fbFont    = flag.String("fallback", "", "後備字型(TTF/TTC),補倚天沒有的字;留空自動找")
		noFB      = flag.Bool("no-fallback", false, "不要後備字型")
		touch     = flag.Bool("touch", false, "顯示觸控功能列(Android 版介面草案)")
		gopherURL = flag.String("gopher", "", "開一個 gopher 位址(會真的連外)")
	)
	flag.Parse()

	half, err := loadHalf(*halfPath)
	if err != nil {
		die(err)
	}
	cjk, err := eten.Load(*stdPath, *spcPath, half.PixWidth*2, half.PixHeight)
	if err != nil {
		if std := bundled.Get("STDFONT.15"); std != nil {
			cjk, err = eten.LoadBytes(std, bundled.Get("SPCFONT.15"),
				bundled.Get("SPCFSUPP.15"), half.PixWidth*2, half.PixHeight)
		}
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "警告:載不到倚天字庫,全形字會留白 (%v)\n", err)
	}

	// 光柵器要先建,因為 overlay 的座標必須用**格子**的大小(CellH)
	// 而不是字身高(PixHeight)。兩者差一個 LineGap,用錯的話每列偏 1 px,
	// 到第 40 列就累積成兩列半 —— 而單一張圖鋪滿整個畫面時看不出來,
	// 要到 markdown 一頁好幾張圖才會現形。
	r := render.New(half, cjkOrNil(cjk))
	r.MissingMark = true
	if !*noFB {
		attachFallback(r, *fbFont, half.PixWidth, half.PixHeight)
	}

	s := cell.New(*cols, *rows)
	var overlay []*render.Overlay
	if *edit != "" {
		if err := drawEdit(s, *edit, *cfgDir); err != nil {
			die(err)
		}
	} else if *file != "" {
		ov, err := drawFile(s, *file, r.CellW, r.CellH)
		if err != nil {
			die(err)
		}
		overlay = ov
	} else if *appDir != "" {
		ov, err := drawApp(s, *appDir, *cfgDir, *keyStr, r.CellW, r.CellH, *touch, *gopherURL)
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

	img := r.DrawWith(s, overlay...)

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

// loadHalf 讀半形字型。磁碟優先,內嵌後備 —— 順序與 cmd/wincv 一致。
func loadHalf(p string) (*fnt.Font, error) {
	d, err := os.ReadFile(p)
	if err != nil {
		if d = bundled.Get(filepath.Base(p)); d == nil {
			return nil, err
		}
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

	// 29 個具名顏色,值取自 WINCV.IMG(見 render.DefaultPalette)。
	// 名字用該色自己畫,右邊補一個色塊 —— 深色在黑底上看不清楚,
	// 只印名字驗不到。
	s.Print(2, 9, "29 色(名稱與 RGB 都取自 image):", cell.White, cell.Black)
	const perRow = 3
	for i := 0; i < int(cell.NumColors); i++ {
		col, row := i%perRow, i/perRow
		x, y := 2+col*24, 10+row
		s.Print(x, y, fmt.Sprintf("%2d %-12s", i, cell.Names[i]), cell.Color(i), cell.Black)
		s.Fill(x+16, y, 4, 1, ' ', cell.Black, cell.Color(i))
	}

	y := 10 + (int(cell.NumColors)+perRow-1)/perRow + 1
	s.Print(2, y, "反白游標列示範(整列換屬性,字不動):", cell.White, cell.Black)
	s.Print(2, y+1, "7-zip32  dll     228,864  2004-10-01  10:00", cell.LtGray, cell.Black)
	s.Print(2, y+2, "big52gbk txt      69,865  2002-10-21  10:00", cell.LtGray, cell.Black)
	s.SetAttr(2, y+2, 44, cell.Black, cell.LtGray)

	s.Print(2, y+4, "全形字佔兩格,右半格是佔位:中|文|字", cell.LtGreen, cell.Black)
	s.Print(2, y+5, "混排 mixed 中英 test 對齊 align", cell.LtMagenta, cell.Black)
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

// drawApp 跑完整的 app,送一串按鍵之後畫出當下的畫面。
//
// 這條路徑和 cmd/wincv 走的是同一份 app 程式碼,只是沒有 Ebiten ——
// 所以選單、對話框、模式切換都可以在沒有顯示器的地方檢查。
func drawApp(s *cell.Screen, dir, cfgDir, keyStr string, cw, ch int, touch bool, gopherURL string) ([]*render.Overlay, error) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return nil, err
	}
	a := app.New(vfs.OS{}, abs)
	a.CellW, a.CellH = cw, ch
	a.Touch = touch
	a.LoadSyntax(cfgDir)
	a.DictDir = cfgDir

	ks, err := keys.ParseAll(keyStr)
	if err != nil {
		return nil, err
	}
	// 先畫一次,讓 app 知道畫面有幾列(翻頁與選單定位要用)
	a.Draw(s)

	// settle 一直畫到沒有未完成的非同步動作為止。
	//
	// 不等的話,任何會觸發網路取回的按鍵之後截到的都是「連線中」——
	// 而那看起來像功能壞了,不像截圖截太早。
	settle := func() {
		for i := 0; i < 600 && a.Busy(); i++ {
			a.Draw(s)
			time.Sleep(50 * time.Millisecond)
		}
	}

	if gopherURL != "" {
		a.OpenURL(gopherURL)
		settle()
	}

	for _, k := range ks {
		a.HandleKey(k)
		a.Draw(s)
		settle()
	}
	return a.Draw(s), nil
}

// attachFallback 掛上後備字型。找不到就算了 —— 缺字會畫成空心框,
// 不會安靜地變成空白。
func attachFallback(r *render.Rasterizer, path string, cw, ch int) {
	if path != "" {
		f, err := ttf.Load(path, cw, cw*2, ch)
		if err != nil {
			fmt.Fprintf(os.Stderr, "警告:載不到後備字型 %s (%v)\n", path, err)
			return
		}
		r.Fallback = f
		return
	}
	chain, used, errs := ttf.LoadChain(cw, cw*2, ch)
	for _, e := range errs {
		fmt.Fprintf(os.Stderr, "警告:字型載不起來 %v\n", e)
	}
	if len(chain) > 0 {
		r.Fallback = chain
		if os.Getenv("WINCV_VERBOSE") != "" {
			fmt.Fprintf(os.Stderr, "後備字型:%v\n", used)
		}
	} else {
		fmt.Fprintln(os.Stderr, "警告:找不到後備字型,倚天字庫以外的字會畫成空框")
	}
}
