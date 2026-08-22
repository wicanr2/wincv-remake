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
	"image/color"
	"math"
	"os"
	"path/filepath"

	"github.com/hajimehoshi/ebiten/v2"

	"github.com/wicanr2/wincv-remake/internal/app"
	"github.com/wicanr2/wincv-remake/internal/bundled"
	"github.com/wicanr2/wincv-remake/internal/cell"
	"github.com/wicanr2/wincv-remake/internal/eten"
	"github.com/wicanr2/wincv-remake/internal/fnt"
	"github.com/wicanr2/wincv-remake/internal/render"
	"github.com/wicanr2/wincv-remake/internal/session"
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
	half render.HalfSource
	cjk  render.CJKSource
	fb   render.CJKSource
}

type game struct {
	app    *app.App
	screen *cell.Screen
	rast   *render.Rasterizer
	canvas *ebiten.Image
	// tex 是把 CPU 端的格點圖搬上 GPU 用的暫存。
	//
	// [雷] 三層各一個,**不能共用**。Ebiten 的 DrawImage 是延遲執行的
	// (指令排進佇列,幀結束才送出),而共用的話下一層會在前一層真的
	// 畫出去之前就把那張圖的內容換掉、甚至 Deallocate 掉 ——
	// 症狀是某一層整片變黑,而且沒有任何錯誤。
	tex   [3]*ebiten.Image
	dirty bool
	scale float64

	levels []level
	zoom   int
	// rasts 是各字級的光柵器(懶建),effIdx / effScale 是
	// pickLevel 挑出來的「實際用哪一級、還要再放大幾倍」。
	rasts    []*render.Rasterizer
	effIdx   int
	effScale float64

	// menuRast 是選單那一層的光柵器。選單可以用與內容不同的字型與
	// 大小,所以它有自己的格點;nil 表示沿用內容的那一份。
	menuRast *render.Rasterizer
	// menuScale 是選單層的放大倍率。與內容分開 —— 兩者的「大小」
	// 是兩件獨立的事,共用一個倍率就等於沒有分離。
	menuScale float64
	// menuFontPath 是使用者指定的選單字型;空的表示用點陣字。
	menuFontPath string
	menuFontSize int
	// menuZoom 是選單層目前用第幾級點陣字,-1 表示跟內容一樣。
	menuZoom int
	// resume 是「記住位置」的開關,lastSt 是上次寫出去的那一份。
	resume bool
	lastSt session.State
	cols   int
	rows   int
}

// setZoom 換字級,並依現在的視窗大小重算欄列數。
func (g *game) setZoom(n int) {
	if n < 0 || n >= len(g.levels) || n == g.zoom {
		return
	}
	g.zoom = n
	g.applyLevel()
	g.canvas = nil
	g.dirty = true
	// 換字級要保住**格數**而不是視窗大小:使用者按放大字體想要的是
	// 「字變大」,不是「同樣大的視窗裡剩下一半的內容」。
	if g.cols > 0 && g.rows > 0 {
		g.applySize(g.cols, g.rows)
	}
}

// rastFor 取第 i 個字級的光柵器,建過就留著。
//
// 留著是必要的:pickLevel 每次都要問各字級的格子多大,而建一個
// 光柵器要解字型、配緩衝區。每幀重建會讓捲動變成幻燈片。
func (g *game) rastFor(i int) *render.Rasterizer {
	if i < 0 || i >= len(g.levels) {
		return nil
	}
	if g.rasts == nil {
		g.rasts = make([]*render.Rasterizer, len(g.levels))
	}
	if g.rasts[i] == nil {
		l := g.levels[i]
		r := render.New(l.half, l.cjk)
		r.Fallback = l.fb
		r.MissingMark = true
		g.rasts[i] = r
	}
	return g.rasts[i]
}

// pickLevel 依「使用者選的字級 + 想要的倍率」決定**實際**用哪一個字級,
// 以及還要再放大幾倍。
//
// 為什麼要挑:原版隨附三種點陣字(8×15 / 10×18 / 12×24),它們之間的
// 比例正好是 1.19 與 1.56。也就是說「放大 1.2 倍」可以用**原生的
// 10×18 字模**達成,而不是把 8×15 的字模拉大 1.2 倍 —— 前者每一個
// 像素都是當年設計出來的,後者必然有一列被複製兩次、一列沒有,
// 於是同一個字的直筆有的 1 px 有的 2 px。
//
// 只往**大**的字級找。往小找等於把字縮小再放大,那比直接放大更糊。
func (g *game) pickLevel() (int, float64) {
	base := g.rastFor(g.zoom)
	if base == nil || base.CellH <= 0 {
		return g.zoom, g.scale
	}
	target := float64(base.CellH) * g.scale

	bestIdx, bestRest := g.zoom, g.scale
	bestErr := math.Abs(g.scale - math.Round(g.scale))
	for i := g.zoom + 1; i < len(g.levels); i++ {
		r := g.rastFor(i)
		if r == nil || r.CellH <= 0 {
			continue
		}
		rest := target / float64(r.CellH)
		// 再大的字級就超過使用者要的尺寸了 —— 那等於縮小,不做。
		if rest < 1-scaleSnap {
			break
		}
		if e := math.Abs(rest - math.Round(rest)); e < bestErr {
			bestIdx, bestRest, bestErr = i, rest, e
		}
	}
	// 差一點點就當剛好。1.02 倍與 1.00 倍的尺寸差不到一個像素,
	// 但前者會讓某一列的像素變成雙倍粗 —— 看得出來的是後者那件事。
	if math.Abs(bestRest-math.Round(bestRest)) <= scaleSnap {
		bestRest = math.Round(bestRest)
	}
	if bestRest < 1 {
		bestRest = 1
	}
	return bestIdx, bestRest
}

// scaleSnap 是「差這麼多就當剛好」的容差。
//
// 5% 是尺寸上看不出來、但銳利度上差很多的那個區間:8×16 的格子
// 放大 1.19 倍與 1.20 倍只差 0.16 個像素,而前者可以整格換成
// 原生的 10×18 字模。
const scaleSnap = 0.05

// applyLevel 依 pickLevel 的結果換掉目前在用的光柵器。
func (g *game) applyLevel() {
	idx, rest := g.pickLevel()
	r := g.rastFor(idx)
	if r == nil {
		return
	}
	if g.rast != r || g.effScale != rest {
		g.canvas = nil
		g.dirty = true
	}
	g.rast, g.effIdx, g.effScale = r, idx, rest
	g.app.CellW, g.app.CellH = r.CellW, r.CellH
}

// resize 依視窗像素大小算出裝得下幾欄幾列。
//
// 「視窗放大時內容多一點」就是這件事:格子大小固定,視窗變大就多幾格,
// 而不是把同樣的內容拉大。要把內容拉大是換字級(Ctrl-+)或放大倍率(Alt-+)。
func (g *game) resize(w, h int) {
	cw, ch := g.cellPx()
	if cw <= 0 || ch <= 0 {
		return
	}
	// 選單列吃掉的是**像素**不是格 —— 它可以用完全不同的字型大小。
	cols, rows := w/cw, (h-g.menuBarPx())/ch
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
	g.saveState()
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
		// 換倍率時重挑字級:1.2 倍用原生的 10×18 字模比把 8×15
		// 拉大 1.2 倍銳利得多(見 pickLevel)。
		g.applyLevel()
		g.applySize(cols, rows)
	}
	// 選單裡的「視窗大小」只留下請求,真的去動視窗是這裡的事。
	// 選單字級改了就重建那一層。
	if g.app.MenuZoom != g.menuZoom {
		g.menuZoom = g.app.MenuZoom
		g.applyMenuZoom()
		g.canvas = nil
		g.dirty = true
	}
	if g.app.WantCols > 0 && g.app.WantRows > 0 {
		g.applySize(g.app.WantCols, g.app.WantRows)
		g.app.WantCols, g.app.WantRows = 0, 0
	}
	return nil
}

// saveState 在位置變了的時候記下來。
//
// 不等到關閉才存:程式被 kill、系統關機、當掉 —— 這些情況下
// 「關閉時才存」等於沒存,而使用者失去的是他最後待的地方。
// 只比對「人在哪裡」那幾個欄位,所以捲一頁不會寫檔。
func (g *game) saveState() {
	if !g.resume {
		return
	}
	st := g.app.Snapshot()
	if st.Dir == g.lastSt.Dir && st.Mode == g.lastSt.Mode && st.File == g.lastSt.File {
		return
	}
	g.lastSt = st
	// 存不了就算了。這是錦上添花的功能,不該在每一幀噴一次錯誤。
	_ = st.Save()
}

// applySize 把視窗調成 cols×rows 格。
//
// 全螢幕時不動視窗 —— 那會把使用者踢出全螢幕,而他要的只是換個字級。
func (g *game) applySize(cols, rows int) {
	if ebiten.IsFullscreen() {
		g.resize(ebiten.WindowSize())
		return
	}
	cw, ch := g.cellPx()
	w, h := cols*cw, rows*ch+g.menuBarPx()
	ebiten.SetWindowSize(w, h)
	g.resize(w, h)
}

// cellPx 是放大之後一格佔幾個螢幕像素。
//
// 無條件進位:1.1 倍時 8×15 的格子是 8.8×16.5 px,取小的那邊會讓
// 最後一列/欄被視窗切掉一半。寧可視窗底部多幾個像素的黑邊。
func (g *game) cellPx() (int, int) {
	return int(math.Ceil(float64(g.rast.CellW) * g.effScale)),
		int(math.Ceil(float64(g.rast.CellH) * g.effScale))
}

// setMenuFont 換選單那一層的字型。path 為空表示沿用內容的字型。
//
// 選單與內容分開的理由:兩者要的東西不一樣。內容要的是「與原版逐像素
// 對齊的點陣字」,那是這個 remake 存在的理由之一;選單只是介面,
// 在高解析度螢幕上用一份清楚的向量字型反而好認。硬綁在一起就得二選一。
func (g *game) setMenuFont(path string, size int, mag float64) error {
	g.menuScale = mag
	if path == "" {
		g.menuRast = nil
		return nil
	}
	if size <= 0 {
		size = g.rast.CellH
	}
	// 半形寬取字高的一半:等寬字型的常見比例,而選單是靠格點排的。
	half := size / 2
	if half < 4 {
		half = 4
	}
	f, err := ttf.Load(path, half, half*2, size)
	if err != nil {
		g.menuRast = nil
		return err
	}
	r := render.New(ttf.NewHalf(f, half, size), f)
	r.MissingMark = true
	// 選單的字也可能缺(中文標籤),同一條後備鏈接上去。
	if chain, _, _ := ttf.LoadChain(half, half*2, size); len(chain) > 0 {
		r.Fallback = chain
	}
	g.menuRast = r
	return nil
}

// applyMenuZoom 依 app.MenuZoom 換選單那一層的點陣字級。
//
// 指定了 -menu-font 時不動:那是使用者明講要用某個字型檔,
// 字級選單改的是「用哪一級點陣字」,兩者不該互相覆蓋。
func (g *game) applyMenuZoom() {
	if g.menuFontPath != "" {
		return
	}
	n := g.menuZoom
	if n < 0 || n >= len(g.levels) {
		g.menuRast = nil
		return
	}
	l := g.levels[n]
	r := render.New(l.half, l.cjk)
	r.Fallback = l.fb
	r.MissingMark = true
	g.menuRast = r
}

// menu 回傳選單層的光柵器、倍率,以及一格的螢幕像素。
//
// [雷] 倍率只在這裡算一次。原本 paint 那邊另外算了一次自己的 msc,
// 兩邊對 menuScale 零值的處理不一樣 —— 結果是 GeoM.Scale(0, 0),
// 選單層被縮成零,畫面上看起來像「選單列沒畫出來」而不是任何錯誤。
func (g *game) menu() (r *render.Rasterizer, sc float64, cw, ch int) {
	r, sc = g.menuRast, g.menuScale
	if r == nil {
		// 沒有指定選單字型時整層沿用內容的字型與倍率。
		r, sc = g.rast, g.effScale
	}
	if sc <= 0 {
		sc = 1
	}
	return r, sc, int(math.Ceil(float64(r.CellW) * sc)),
		int(math.Ceil(float64(r.CellH) * sc))
}

// menuBarPx 是選單列佔掉的螢幕像素高。關著時是 0。
func (g *game) menuBarPx() int {
	if !g.app.MenuBar {
		return 0
	}
	_, _, _, ch := g.menu()
	return ch
}

func (g *game) Draw(dst *ebiten.Image) {
	// 尺寸取自 dst 而不是 ebiten.WindowSize():dst 就是 Layout 回報的
	// 那塊邏輯螢幕,而 WindowSize 在還沒有視窗管理員的環境
	// (Xvfb、剛啟動的那幾幀)回報的可能是別的數字 —— 對不上的話
	// canvas 會比畫面小,而多出來的部分永遠是黑的。
	w, h := dst.Bounds().Dx(), dst.Bounds().Dy()
	if g.dirty || g.canvas == nil ||
		g.canvas.Bounds().Dx() != w || g.canvas.Bounds().Dy() != h {
		g.paint(w, h)
		g.dirty = false
	}
	dst.DrawImage(g.canvas, &ebiten.DrawImageOptions{})
}

// paint 把內容層與選單層合成到 canvas 上。
//
// 兩層各自放大再貼,而不是合成完再一起放大:兩者的倍率是分開的,
// 而且點陣字必須整格複製像素,先合成會讓其中一層被插值糊掉。
func (g *game) paint(w, h int) {
	if w <= 0 || h <= 0 {
		return
	}
	if g.canvas == nil || g.canvas.Bounds().Dx() != w || g.canvas.Bounds().Dy() != h {
		g.canvas = ebiten.NewImage(w, h)
	}
	g.canvas.Fill(color.Black)

	top := g.menuBarPx()

	ov := g.app.Draw(g.screen)
	img := g.rast.DrawWith(g.screen, ov...)
	g.blit(0, img, 0, top, g.effScale)

	if top > 0 {
		mr, msc, mcw, mch := g.menu()
		layer := g.app.MenuLayer(w/mcw, h/mch)
		if layer.Bar != nil {
			g.blit(1, mr.Draw(layer.Bar), 0, 0, msc)
		}
		if layer.Drop != nil {
			// [雷] Rasterizer.Draw 的緩衝區會被下一次呼叫重用,
			// 所以下拉一定要在選單列 blit 完之後才畫。
			g.blit(2, mr.Draw(layer.Drop), layer.DropX*mcw, mch, msc)
		}
	}
}

// blit 把一張格點圖放大 sc 倍之後貼到 canvas 的 (x, y)。
// slot 決定用哪一個暫存材質,見 tex 的說明。
func (g *game) blit(slot int, img *image.RGBA, x, y int, sc float64) {
	if img == nil || img.Rect.Empty() || slot < 0 || slot >= len(g.tex) {
		return
	}
	t := g.tex[slot]
	if t == nil || t.Bounds().Dx() != img.Rect.Dx() || t.Bounds().Dy() != img.Rect.Dy() {
		if t != nil {
			t.Deallocate()
		}
		t = ebiten.NewImage(img.Rect.Dx(), img.Rect.Dy())
		g.tex[slot] = t
	}
	t.WritePixels(img.Pix)
	op := &ebiten.DrawImageOptions{}
	op.GeoM.Scale(sc, sc)
	op.GeoM.Translate(float64(x), float64(y))
	g.canvas.DrawImage(t, op)
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
		scale    = flag.Float64("scale", 2, "放大倍率,0.1 為一階")
		zoom     = flag.Int("zoom", 0, "字級:0=8x15 1=10x18 2=12x24")
		fbFont   = flag.String("fallback", "", "後備字型(TTF/TTC),補倚天沒有的字;留空自動找")
		noFB     = flag.Bool("no-fallback", false, "不要後備字型")
		noResume = flag.Bool("no-resume", false, "不要回到上次的位置")
		menuFont = flag.String("menu-font", "", "選單專用字型(TTF/TTC/OTF);留空沿用內容的點陣字")
		menuSize = flag.Int("menu-size", 0, "選單字高(像素);配 -menu-font 用,留空取內容格高")
		menuMag  = flag.Float64("menu-scale", 0, "選單的放大倍率;留空沿用內容的倍率")
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
	// 命令列指定了目錄就以它為準 —— 使用者說了要去哪,不該被上次的位置蓋掉。
	st := session.State{}
	if !*noResume {
		st = session.Load()
		if flag.NArg() > 0 {
			st.Dir, st.Cursor, st.Mode, st.File = abs, "", "", ""
		}
	}
	// 語法上色設定跟半形字型放在一起(原版是同一個安裝目錄)。
	cfgDir := filepath.Dir(*halfPath)
	a.LoadSyntax(cfgDir)
	a.DictDir = cfgDir

	levels := loadLevels(cfgDir, *stdPath, *spcPath, *fbFont, *noFB)
	if len(levels) == 0 {
		// 沒有原版的 .FON 就用系統字型現場產一份。畫面不是原版的點陣字,
		// 但版面、欄位與按鍵完全一樣 —— 這比「跑不起來」有用得多。
		levels = ttfLevels(*stdPath, *spcPath, *fbFont, *noFB)
		if len(levels) > 0 {
			fmt.Fprintf(os.Stderr,
				"提示:沒有原版的點陣字型(找過 %s),改用系統字型。\n"+
					"      要與原版逐像素相同的畫面,用 -half 指定 cvga.fon。\n", cfgDir)
		}
	}
	if len(levels) == 0 {
		die(fmt.Errorf("一個半形字型都載不到:%s 底下沒有原版的 .FON,"+
			"系統上也找不到可用的 TrueType。\n%s", cfgDir, ttf.MissingHint()))
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
	if st.Cols >= 20 && st.Rows >= 5 && !flagGiven("cols") && !flagGiven("rows") {
		*cols, *rows = st.Cols, st.Rows
	}
	g := &game{app: a, levels: levels, scale: *scale, zoom: -1, dirty: true,
		resume: !*noResume, effScale: *scale}
	g.setZoom(*zoom)
	cw, ch := g.cellPx()
	g.resize(*cols*cw, *rows*ch)

	w, h := g.rast.Size(g.cols, g.rows)
	ebiten.SetWindowSize(int(math.Ceil(float64(w)*g.effScale)),
		int(math.Ceil(float64(h)*g.effScale))+g.menuBarPx())
	ebiten.SetWindowTitle("WinCV")
	ebiten.SetWindowResizingMode(ebiten.WindowResizingModeEnabled)
	// 畫面只在有事情發生時重畫 —— 這是檔案管理程式,不是遊戲。
	ebiten.SetScreenClearedEveryFrame(false)

	g.menuFontPath, g.menuFontSize, g.menuZoom = *menuFont, *menuSize, -1
	if err := g.setMenuFont(*menuFont, *menuSize, *menuMag); err != nil {
		fmt.Fprintln(os.Stderr, "警告:選單字型用不了,沿用內容的字型 —", err)
	}
	a.Restore(st)
	g.lastSt = a.Snapshot()

	err = ebiten.RunGame(g)
	// 關掉時再存一次:這一次連捲動位置都記下來(Update 那邊只在
	// 「人換了地方」時寫,捲一頁不寫檔)。存不了不算錯。
	if !*noResume {
		if e := a.Snapshot().Save(); e != nil {
			fmt.Fprintln(os.Stderr, "記不下這次的位置:", e)
		}
	}
	if err != nil && err != ebiten.Termination {
		die(err)
	}
}

// flagGiven 說明使用者是不是真的在命令列打了這個旗標。
//
// 分得出「沒打」與「打了預設值」才有辦法決定要不要用上次的視窗大小:
// 兩者的變數值一樣,只有 flag 套件知道差別。
func flagGiven(name string) bool {
	found := false
	flag.Visit(func(f *flag.Flag) {
		if f.Name == name {
			found = true
		}
	})
	return found
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
		if f, err := loadEten(stdPath, spcPath, half.PixWidth*2, half.PixHeight); err == nil {
			l.cjk = render.ScaleCJK(f, f.W, f.H, cw*2, ch)
		} else if len(out) == 0 {
			fmt.Fprintf(os.Stderr, "提示:沒有倚天字庫,全形字改用後備字型 (%v)\n", err)
		}
		if !noFB {
			l.fb = loadFallback(fbPath, cw, ch)
		}
		out = append(out, l)
	}
	return out
}

// loadEten 讀倚天字庫:磁碟優先,內嵌是後備。
func loadEten(stdPath, spcPath string, w, h int) (*eten.Font, error) {
	f, err := eten.Load(stdPath, spcPath, w, h)
	if err == nil {
		return f, nil
	}
	if std := bundled.Get("STDFONT.15"); std != nil {
		return eten.LoadBytes(std, bundled.Get("SPCFONT.15"),
			bundled.Get("SPCFSUPP.15"), w, h)
	}
	return nil, err
}

// ttfSizes 是原版隨附的三種半形點陣字型的尺寸。
//
// 沒有那三個 .FON 時照同樣的尺寸從系統字型現場產一份 —— 尺寸一樣,
// 版面、欄位對齊、按鍵行為就完全一樣,只有字形不同。
var ttfSizes = []struct {
	name string
	w, h int
}{{"8x15", 8, 15}, {"10x18", 10, 18}, {"12x24", 12, 24}}

// ttfLevels 用系統字型現場產出三個字級,當作沒有原版 .FON 時的退路。
//
// 為什麼需要:原版的 cvga.fon 是第三方版權物,不能隨產物散布,所以
// 對外的版本解開之後那三個檔一定不在。以前這種情況直接結束程式,
// 而「少一個字型檔」與「這個程式跑不起來」是完全不同的嚴重程度 ——
// Android 版早就是這樣做的(那邊根本沒有可讀的程式目錄),桌面版
// 只是沒接上。
func ttfLevels(stdPath, spcPath, fbPath string, noFB bool) []level {
	var out []level
	var lastErrs []error
	for _, s := range ttfSizes {
		// 半形優先用等寬字型,而且字級要縮到塞得進格子(見 ttf.FindMono
		// 與 ttf.build)。全形用同一份字型但不縮 —— 那邊格子有兩倍寬。
		hf, path, errs := ttf.FindMonoHalf(s.w, s.h)
		if hf == nil {
			lastErrs = errs
			continue
		}
		cw, ch := s.w, s.h+render.LineGap
		l := level{name: "系統字型 " + s.name, half: hf}
		if f, err := ttf.Load(path, s.w, s.w*2, s.h); err == nil {
			l.cjk = f
		}
		// 倚天字庫有的話仍然優先:那才是與原版對齊的全形字形。
		if e, err := loadEten(stdPath, spcPath, s.w*2, s.h); err == nil {
			l.cjk = render.ScaleCJK(e, e.W, e.H, cw*2, ch)
		}
		if !noFB {
			if fb := loadFallback(fbPath, cw, ch); fb != nil {
				l.fb = fb
			}
		}
		out = append(out, l)
	}
	for _, e := range lastErrs {
		fmt.Fprintf(os.Stderr, "警告:字型載不起來 %v\n", e)
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
	// 內嵌的後備接在系統字型後面,不是取代它 —— 系統上裝的字型
	// 通常比較新、比較齊,而內嵌的那份是「這台機器什麼都沒裝」時的保險。
	for _, b := range bundled.Fallbacks() {
		f, err := ttf.LoadBytes(b, cw, cw*2, ch)
		if err != nil {
			continue
		}
		chain = append(chain, f)
	}
	if len(chain) > 0 {
		// 個別字型載不起來(x/image 讀不了某些 TTC)在有其他字型
		// 頂上時只是雜訊。一條都沒載到才是使用者需要知道的事。
		return chain
	}
	for _, e := range errs {
		fmt.Fprintf(os.Stderr, "警告:字型載不起來 %v\n", e)
	}
	fmt.Fprintln(os.Stderr, "警告:"+ttf.MissingHint())
	return nil
}
