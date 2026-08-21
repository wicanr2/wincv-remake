// wincv 是 remake 的主程式。
//
// 這一層只做三件事:把 Ebiten 的按鍵事件翻成 keys.Key、
// 呼叫 app 更新狀態、把 render 產出的像素貼上畫面。
// 所有邏輯都在 internal/,這裡不該有任何判斷。
package main

import (
	"flag"
	"fmt"
	"image"
	"os"
	"path/filepath"

	"github.com/hajimehoshi/ebiten/v2"

	"github.com/wicanr2/wincv-remake/internal/app"
	"github.com/wicanr2/wincv-remake/internal/bundled"
	"github.com/wicanr2/wincv-remake/internal/cell"
	"github.com/wicanr2/wincv-remake/internal/eten"
	"github.com/wicanr2/wincv-remake/internal/fnt"
	"github.com/wicanr2/wincv-remake/internal/render"
	"github.com/wicanr2/wincv-remake/internal/ttf"
	"github.com/wicanr2/wincv-remake/internal/vfs"
)

// level 是一個字級:一種半形點陣字型,加上配合它的全形來源。
//
// 原版隨附三種尺寸的 `.FON`(8x15 / 10x18 / 12x24),放大縮小字體就是
// 在這幾種之間換。全形字只有倚天的 15 點那一份,其餘尺寸由它縮放
// (24 點的漢字在倚天光碟裡是 ETUNPACK 壓縮的,還解不開)。
type level struct {
	name string
	half *fnt.Font
	cjk  render.CJKSource
	fb   render.CJKSource
}

type game struct {
	app    *app.App
	screen *cell.Screen
	rast   *render.Rasterizer
	canvas *ebiten.Image
	dirty  bool
	scale  int

	levels []level
	zoom   int
	cols   int
	rows   int
}

// setZoom 換字級,並依現在的視窗大小重算欄列數。
func (g *game) setZoom(n int) {
	if n < 0 || n >= len(g.levels) || n == g.zoom {
		return
	}
	g.zoom = n
	l := g.levels[n]
	g.rast = render.New(l.half, l.cjk)
	g.rast.Fallback = l.fb
	g.rast.MissingMark = true
	g.app.CellW, g.app.CellH = g.rast.CellW, g.rast.CellH
	g.canvas = nil
	g.dirty = true
	// 換字級要保住**格數**而不是視窗大小:使用者按放大字體想要的是
	// 「字變大」,不是「同樣大的視窗裡剩下一半的內容」。
	if g.cols > 0 && g.rows > 0 {
		g.applySize(g.cols, g.rows)
	}
}

// resize 依視窗像素大小算出裝得下幾欄幾列。
//
// 「視窗放大時內容多一點」就是這件事:格子大小固定,視窗變大就多幾格,
// 而不是把同樣的內容拉大。要把內容拉大是換字級(Ctrl-+)或整數倍放大。
func (g *game) resize(w, h int) {
	cw, ch := g.rast.CellW*g.scale, g.rast.CellH*g.scale
	if cw <= 0 || ch <= 0 {
		return
	}
	cols, rows := w/cw, h/ch
	if cols < 20 {
		cols = 20
	}
	if rows < 5 {
		rows = 5
	}
	if cols == g.cols && rows == g.rows {
		return
	}
	g.cols, g.rows = cols, rows
	g.screen = cell.New(cols, rows)
	g.canvas = nil
	g.dirty = true
}

func (g *game) Update() error {
	if g.app.Quit {
		return ebiten.Termination
	}
	// 編輯器要收字元事件才打得出 shift 後的符號與中文。
	// 編輯器與輸入列都要收字元事件。
	for _, k := range translate(g.app.Mode == app.ModeEdit || g.app.Prompting()) {
		if g.app.HandleKey(k) {
			g.dirty = true
		}
	}
	// 有還沒完成的網路取回時要一直重繪。app 那一層不會自己叫重繪,
	// 而結果是非同步回來的 —— 不這樣做的話畫面會永遠停在「連線中」。
	if g.app.Busy() {
		g.dirty = true
	}
	// app 那一層只翻旗標,真的去動視窗是這裡的事。每幀比對一次,
	// 使用者從視窗管理員那邊切全螢幕時也能跟上。
	if g.app.Fullscreen != ebiten.IsFullscreen() {
		ebiten.SetFullscreen(g.app.Fullscreen)
	}
	if g.app.Zoom != g.zoom {
		g.setZoom(g.app.Zoom)
	}
	// 整數倍放大改變時,視窗的像素大小要跟著改,否則格數會少掉一半。
	if g.app.Scale != g.scale && g.app.Scale >= app.MinScale && g.app.Scale <= app.MaxScale {
		cols, rows := g.cols, g.rows
		g.scale = g.app.Scale
		g.canvas = nil
		g.dirty = true
		g.applySize(cols, rows)
	}
	// 選單裡的「視窗大小」只留下請求,真的去動視窗是這裡的事。
	if g.app.WantCols > 0 && g.app.WantRows > 0 {
		g.applySize(g.app.WantCols, g.app.WantRows)
		g.app.WantCols, g.app.WantRows = 0, 0
	}
	return nil
}

// applySize 把視窗調成 cols×rows 格。
//
// 全螢幕時不動視窗 —— 那會把使用者踢出全螢幕,而他要的只是換個字級。
func (g *game) applySize(cols, rows int) {
	if ebiten.IsFullscreen() {
		g.resize(ebiten.WindowSize())
		return
	}
	w := cols * g.rast.CellW * g.scale
	h := rows * g.rast.CellH * g.scale
	ebiten.SetWindowSize(w, h)
	g.resize(w, h)
}

func (g *game) Draw(dst *ebiten.Image) {
	if g.dirty || g.canvas == nil {
		ov := g.app.Draw(g.screen)
		img := g.rast.DrawWith(g.screen, ov...)
		if g.canvas == nil ||
			g.canvas.Bounds() != (image.Rectangle{Max: img.Rect.Max}) {
			g.canvas = ebiten.NewImage(img.Rect.Dx(), img.Rect.Dy())
		}
		g.canvas.WritePixels(img.Pix)
		g.dirty = false
	}
	op := &ebiten.DrawImageOptions{}
	op.GeoM.Scale(float64(g.scale), float64(g.scale))
	dst.DrawImage(g.canvas, op)
}

// Layout 每幀被呼叫,參數是視窗的實際大小。回傳同樣的數字表示
// 「邏輯像素 = 視窗像素」,放大交給我們自己做,這樣點陣字才不會被插值糊掉。
func (g *game) Layout(outW, outH int) (int, int) {
	if outW > 0 && outH > 0 {
		g.resize(outW, outH)
	}
	return outW, outH
}

func main() {
	var (
		halfPath = flag.String("half", "original/app/cvga.fon", "半形 .FON")
		stdPath  = flag.String("eten-std", "original/eten/STDFONT.15", "倚天漢字區")
		spcPath  = flag.String("eten-spc", "original/eten/SPCFONT.15", "倚天符號區")
		cols     = flag.Int("cols", 80, "欄數")
		rows     = flag.Int("rows", 30, "列數")
		scale    = flag.Int("scale", 2, "整數倍放大")
		zoom     = flag.Int("zoom", 0, "字級:0=8x15 1=10x18 2=12x24")
		fbFont   = flag.String("fallback", "", "後備字型(TTF/TTC),補倚天沒有的字;留空自動找")
		noFB     = flag.Bool("no-fallback", false, "不要後備字型")
	)
	flag.Parse()

	dir := "."
	if flag.NArg() > 0 {
		dir = flag.Arg(0)
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		die(err)
	}

	a := app.New(vfs.OS{}, abs)
	// 語法上色設定跟半形字型放在一起(原版是同一個安裝目錄)。
	cfgDir := filepath.Dir(*halfPath)
	a.LoadSyntax(cfgDir)
	a.DictDir = cfgDir

	levels := loadLevels(cfgDir, *stdPath, *spcPath, *fbFont, *noFB)
	if len(levels) == 0 {
		die(fmt.Errorf("一個半形字型都載不到(找過 %s)", cfgDir))
	}
	a.MaxZoom = len(levels) - 1
	if *zoom > a.MaxZoom {
		*zoom = a.MaxZoom
	}
	a.Zoom = *zoom

	if *scale < app.MinScale {
		*scale = app.MinScale
	}
	if *scale > app.MaxScale {
		*scale = app.MaxScale
	}
	a.Scale = *scale
	g := &game{app: a, levels: levels, scale: *scale, zoom: -1, dirty: true}
	g.setZoom(*zoom)
	g.resize(*cols*g.rast.CellW*g.scale, *rows*g.rast.CellH*g.scale)

	w, h := g.rast.Size(g.cols, g.rows)
	ebiten.SetWindowSize(w*g.scale, h*g.scale)
	ebiten.SetWindowTitle("WinCV")
	ebiten.SetWindowResizingMode(ebiten.WindowResizingModeEnabled)
	// 畫面只在有事情發生時重畫 —— 這是檔案管理程式,不是遊戲。
	ebiten.SetScreenClearedEveryFrame(false)

	if err := ebiten.RunGame(g); err != nil && err != ebiten.Termination {
		die(err)
	}
}

func die(err error) {
	fmt.Fprintln(os.Stderr, "錯誤:", err)
	os.Exit(1)
}

// fonNames 是原版隨附的三種半形點陣字型,由小到大。
var fonNames = []string{"cvga.fon", "CVGA1018.FON", "cvga1224.FON"}

// loadLevels 把能載到的字級都準備好。載不到的就跳過,但要說出來 ——
// 少一個字級的症狀是「Ctrl-+ 沒反應」,那看起來像壞掉而不像缺檔案。
func loadLevels(dir, stdPath, spcPath, fbPath string, noFB bool) []level {
	var out []level
	for _, name := range fonNames {
		// 磁碟優先,內嵌是後備 —— 使用者放在執行檔旁邊的字型永遠贏。
		d, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			if d = bundled.Get(name); d == nil {
				continue
			}
		}
		half, err := fnt.Parse(d)
		if err != nil {
			fmt.Fprintf(os.Stderr, "警告:%s 解不開 (%v)\n", name, err)
			continue
		}
		cw, ch := half.PixWidth, half.PixHeight+render.LineGap
		l := level{name: name, half: half}

		// 全形字:倚天只有 15 點那一份,其餘字級由它縮放。
		f, err := eten.Load(stdPath, spcPath, half.PixWidth*2, half.PixHeight)
		if err != nil {
			if std := bundled.Get("STDFONT.15"); std != nil {
				f, err = eten.LoadBytes(std, bundled.Get("SPCFONT.15"),
					bundled.Get("SPCFSUPP.15"), half.PixWidth*2, half.PixHeight)
			}
		}
		if err == nil {
			l.cjk = render.ScaleCJK(f, f.W, f.H, cw*2, ch)
		} else if len(out) == 0 {
			fmt.Fprintf(os.Stderr, "警告:載不到倚天字庫,全形字要靠後備字型 (%v)\n", err)
		}
		if !noFB {
			l.fb = loadFallback(fbPath, cw, ch)
		}
		out = append(out, l)
	}
	return out
}

// loadFallback 掛上後備字型,補倚天(Big5 索引)沒有的字:
// 简体字、韓文、希臘文、多數符號。找不到就沒有,缺字會畫成空框。
func loadFallback(path string, cw, ch int) render.CJKSource {
	if path != "" {
		f, err := ttf.Load(path, cw, cw*2, ch)
		if err != nil {
			fmt.Fprintf(os.Stderr, "警告:載不到後備字型 %s (%v)\n", path, err)
			return nil
		}
		return f
	}
	chain, _, errs := ttf.LoadChain(cw, cw*2, ch)
	for _, e := range errs {
		fmt.Fprintf(os.Stderr, "警告:字型載不起來 %v\n", e)
	}
	if len(chain) == 0 {
		return nil
	}
	return chain
}
