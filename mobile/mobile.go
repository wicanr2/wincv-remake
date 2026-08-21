// Package mobile 是 Android 版的進入點。
//
// ebitenmobile bind 會把這個套件包成 AAR,Java 那一側拿到一個
// EbitenView 就能顯示。桌面版的進入點在 cmd/wincv,兩者共用
// internal/app 與 internal/render —— 也就是說「畫什麼」與「按鍵怎麼分派」
// 只有一份,平台差異只在輸入與字型來源。
//
// 建置見 tools/build-android.sh,設計與分期見 docs/plan/android.md。
package mobile

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sync"

	"github.com/hajimehoshi/ebiten/v2"
	emobile "github.com/hajimehoshi/ebiten/v2/mobile"

	"github.com/wicanr2/wincv-remake/internal/app"
	"github.com/wicanr2/wincv-remake/internal/bundled"
	"github.com/wicanr2/wincv-remake/internal/cell"
	"github.com/wicanr2/wincv-remake/internal/eten"
	"github.com/wicanr2/wincv-remake/internal/fnt"
	"github.com/wicanr2/wincv-remake/internal/render"
	"github.com/wicanr2/wincv-remake/internal/ttf"
	"github.com/wicanr2/wincv-remake/internal/vfs"
)

var (
	mu   sync.Mutex
	root = ""
	note = ""
)

// SetRoot 由 Java 在開啟 view 之前呼叫,指定要瀏覽的目錄。
//
// 為什麼不讓 Go 自己找:Android 10 起是 scoped storage,
// app 讀得到哪裡是由 Java 那一側的授權決定的,Go 這邊猜不出來。
func SetRoot(dir string) {
	// 順便把螢幕資訊讀出來 —— 這一步會讓 Ebiten 去問 JVM 拿 density 並快取。
	//
	// 為什麼要在這裡做:EbitenView.onLayout 是用「像素 ÷ deviceScale()」
	// 算版面尺寸的,而 deviceScale() 在 JVM 還沒回應時是 0,除出來是 +Inf。
	// SetRoot 由 Activity.onCreate 呼叫,那時 JVM 一定就緒,
	// 所以在這裡讀一次就能讓 onLayout 拿到真的值。
	_ = ebiten.Monitor().DeviceScaleFactor()

	mu.Lock()
	root = dir
	mu.Unlock()
}

func init() {
	emobile.SetGame(&game{})
}

// Dummy 是 gomobile bind 的要求:套件至少要有一個匯出的東西
// 才會產生 Java 綁定。
func Dummy() {}

type game struct {
	a      *app.App
	rast   *render.Rasterizer
	screen *cell.Screen
	canvas *ebiten.Image
	cols   int
	rows   int
	lastW  int // 最後一次合理的外部尺寸,用來擋掉壞的輸入
	lastH  int
	scale  float64
	dirty  bool
	touch  touchState
	ready  bool
}

// start 在第一次 Layout 時才做,因為那時候才知道螢幕多大,
// 也才等得到 Java 呼叫過 SetRoot。
func (g *game) start(w, h int) {
	dir := pickRoot()
	g.a = app.New(vfs.OS{}, dir)
	g.a.Touch = true // 手機一律顯示觸控功能列
	g.a.Scale = 1

	half, cjk, n := loadFonts(dir, w, h)
	note = n
	g.rast = render.New(half, cjk)
	g.rast.MissingMark = true
	if chain, _, _ := ttf.LoadChain(8, 16, 16); len(chain) > 0 {
		g.rast.Fallback = chain
	}
	g.a.CellW, g.a.CellH = g.rast.CellW, g.rast.CellH
	if note != "" {
		g.a.Message = note
	}
	g.ready = true
}

// pickRoot 決定一開始瀏覽哪裡。
//
// Java 沒設就自己找一個讀得到的 —— 第一版是唯讀瀏覽器,
// 讀不到東西的話畫面會是空的,而空畫面看起來像程式壞了。
func pickRoot() string {
	mu.Lock()
	r := root
	mu.Unlock()
	if r != "" {
		if _, err := os.ReadDir(r); err == nil {
			return r
		}
	}
	for _, c := range []string{
		"/storage/emulated/0/Download",
		"/storage/emulated/0",
		"/sdcard",
		"/data/local/tmp",
		"/",
	} {
		if _, err := os.ReadDir(c); err == nil {
			return c
		}
	}
	return "/"
}

// loadFonts 決定半形與全形字模從哪來。
//
// 優先用使用者匯入的原版字型(點陣像素對齊);沒有就拿系統字型現場產。
// **原版的 cvga.fon 與倚天字庫是第三方版權物,不打包進 APK。**
// 回傳的第三個值是要顯示給使用者的說明,不是錯誤 ——
// 「現在用的是系統字型」這件事應該讓人知道,不然他會以為畫面本來就長這樣。
func loadFonts(dir string, w, h int) (render.HalfSource, render.CJKSource, string) {
	fontDir := filepath.Join(dir, "wincv")
	if d, err := os.ReadFile(filepath.Join(fontDir, "cvga.fon")); err == nil {
		if f, err := fnt.Parse(d); err == nil {
			cjk, _ := eten.Load(
				filepath.Join(fontDir, "STDFONT.15"),
				filepath.Join(fontDir, "SPCFONT.15"),
				f.PixWidth*2, f.PixHeight)
			if cjk != nil {
				return f, cjk, ""
			}
			return f, nil, "找到 cvga.fon,但沒有倚天字庫,全形字會用系統字型"
		}
	}

	// 內嵌字型(-tags fonts 建出來的版本才有)。磁碟上放了就用磁碟的,
	// 這裡是後備 —— 順序與桌面版一致。
	if d := bundled.Get("cvga.fon"); d != nil {
		if f, err := fnt.Parse(d); err == nil {
			cjk, err := eten.LoadBytes(bundled.Get("STDFONT.15"),
				bundled.Get("SPCFONT.15"), bundled.Get("SPCFSUPP.15"),
				f.PixWidth*2, f.PixHeight)
			if err == nil {
				return f, cjk, ""
			}
			return f, nil, "內嵌的倚天字庫載不起來,全形字會用系統字型"
		}
	}
	half, path, errs := ttf.LoadHalf(8, 15)
	if half == nil {
		return nil, nil, fmt.Sprintf("找不到任何可用字型(試過 %d 個候選,%d 個載不起來)",
			len(ttf.Candidates()), len(errs))
	}
	return half, nil, "用系統字型 " + filepath.Base(path) + ";把 cvga.fon 放進 wincv/ 可換成原版點陣字"
}

func (g *game) Layout(outW, outH int) (int, int) {
	// 這一步不是防禦性程式碼,是必要的:Android 那一側算出來的尺寸
	// 可能是 +Inf(見 app.SaneLayout 的說明),原樣傳回去會被 Ebiten
	// 當成非法值 panic 掉整個 app。
	monW, monH := ebiten.Monitor().Size()
	outW, outH = app.SaneLayout(outW, outH, g.lastW, g.lastH, monW, monH)
	g.lastW, g.lastH = outW, outH

	if !g.ready {
		g.start(outW, outH)
	}
	if g.rast == nil {
		return outW, outH
	}
	// 手機螢幕密度高,格子放大到看得清楚為止。
	// 目標是一列大約 40-56 格 —— 再少就放不下檔名與大小,
	// 再多在手指上點不準。
	// 手機上維持整數倍:非整數倍會讓點陣字的筆畫粗細不勻,
	// 而在高密度的小螢幕上那個不勻比「格子稍微大一點」更明顯。
	sc := outW / (g.rast.CellW * 48)
	if sc < 1 {
		sc = 1
	}
	if float64(sc) > app.MaxScale {
		sc = int(app.MaxScale)
	}
	g.scale = float64(sc)
	cols, rows := outW/(g.rast.CellW*sc), outH/(g.rast.CellH*sc)
	if cols < 20 {
		cols = 20
	}
	if rows < 8 {
		rows = 8
	}
	if cols != g.cols || rows != g.rows {
		// 印進 logcat(tag GoLog)。截圖看不出格點是不是剛好填滿畫面,
		// 這幾個數字看得出來。
		log.Printf("wincv: 外部 %dx%d dp, 格 %dx%d, 倍率 %d, 畫布 %dx%d px",
			outW, outH, cols, rows, sc, cols*g.rast.CellW*sc, rows*g.rast.CellH*sc)
		g.cols, g.rows = cols, rows
		g.screen = cell.New(cols, rows)
		g.canvas = nil
		g.dirty = true
	}
	return outW, outH
}

func (g *game) Update() error {
	if !g.ready || g.screen == nil {
		return nil
	}
	for _, k := range g.touch.keys(g) {
		if g.a.HandleKey(k) {
			g.dirty = true
		}
	}
	// 同 cmd/wincv:有網路取回還沒完成就持續重繪,
	// 不然結果回來了畫面也不動。
	if g.a.Busy() {
		g.dirty = true
	}
	return nil
}

// cellPx 是放大之後一格佔幾個螢幕像素。
func (g *game) cellPx() (int, int) {
	return int(float64(g.rast.CellW) * g.scale), int(float64(g.rast.CellH) * g.scale)
}

func (g *game) Draw(dst *ebiten.Image) {
	if !g.ready || g.screen == nil {
		return
	}
	if g.dirty || g.canvas == nil {
		ov := g.a.Draw(g.screen)
		img := g.rast.DrawWith(g.screen, ov...)
		if g.canvas == nil || g.canvas.Bounds().Dx() != img.Rect.Dx() ||
			g.canvas.Bounds().Dy() != img.Rect.Dy() {
			g.canvas = ebiten.NewImage(img.Rect.Dx(), img.Rect.Dy())
		}
		g.canvas.WritePixels(img.Pix)
		g.dirty = false
	}
	op := &ebiten.DrawImageOptions{}
	op.GeoM.Scale(g.scale, g.scale)
	dst.DrawImage(g.canvas, op)
}
