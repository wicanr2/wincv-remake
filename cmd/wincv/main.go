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
	"github.com/wicanr2/wincv-remake/internal/cell"
	"github.com/wicanr2/wincv-remake/internal/eten"
	"github.com/wicanr2/wincv-remake/internal/fnt"
	"github.com/wicanr2/wincv-remake/internal/render"
	"github.com/wicanr2/wincv-remake/internal/vfs"
)

type game struct {
	app    *app.App
	screen *cell.Screen
	rast   *render.Rasterizer
	canvas *ebiten.Image
	dirty  bool
	scale  int
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
	// app 那一層只翻旗標,真的去動視窗是這裡的事。每幀比對一次,
	// 使用者從視窗管理員那邊切全螢幕時也能跟上。
	if g.app.Fullscreen != ebiten.IsFullscreen() {
		ebiten.SetFullscreen(g.app.Fullscreen)
	}
	return nil
}

func (g *game) Draw(dst *ebiten.Image) {
	if g.dirty || g.canvas == nil {
		ov := g.app.Draw(g.screen)
		img := g.rast.DrawWith(g.screen, ov)
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

func (g *game) Layout(int, int) (int, int) {
	w, h := g.rast.Size(g.screen.Cols, g.screen.Rows)
	return w * g.scale, h * g.scale
}

func main() {
	var (
		halfPath = flag.String("half", "original/app/cvga.fon", "半形 .FON")
		stdPath  = flag.String("eten-std", "original/eten/STDFONT.15", "倚天漢字區")
		spcPath  = flag.String("eten-spc", "original/eten/SPCFONT.15", "倚天符號區")
		cols     = flag.Int("cols", 80, "欄數")
		rows     = flag.Int("rows", 30, "列數")
		scale    = flag.Int("scale", 2, "整數倍放大")
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

	fd, err := os.ReadFile(*halfPath)
	if err != nil {
		die(fmt.Errorf("讀不到半形字型 %s: %w", *halfPath, err))
	}
	half, err := fnt.Parse(fd)
	if err != nil {
		die(err)
	}
	var cjk render.CJKSource
	if f, err := eten.Load(*stdPath, *spcPath, half.PixWidth*2, half.PixHeight); err == nil {
		cjk = f
	} else {
		fmt.Fprintf(os.Stderr, "警告:載不到倚天字庫,全形字會留白 (%v)\n", err)
	}

	a := app.New(vfs.OS{}, abs)
	a.CellW, a.CellH = half.PixWidth, half.PixHeight
	// 語法上色設定跟半形字型放在一起(原版是同一個安裝目錄)。
	a.LoadSyntax(filepath.Dir(*halfPath))
	a.DictDir = filepath.Dir(*halfPath)
	g := &game{
		app:    a,
		screen: cell.New(*cols, *rows),
		rast:   render.New(half, cjk),
		dirty:  true,
		scale:  *scale,
	}
	w, h := g.rast.Size(*cols, *rows)
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
