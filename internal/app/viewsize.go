package app

import (
	"fmt"
	"math"

	"github.com/wicanr2/wincv-remake/internal/render"
)

// MinScale / MaxScale / ScaleStep 是放大倍率的範圍與階距。
//
// 上限 4 是實用的界線:8×16 的格子放到 4 倍是 32×64 px,
// 在 4K 螢幕上一列還放得下 60 格。再大就沒有欄數可用了。
//
// [取捨] 階距 0.1 換來的是「調得準」,代價是**點陣字不再等倍複製**。
// 1.0/2.0/3.0/4.0 這幾階每個原始像素都變成同樣大小的方塊;1.1 倍時
// 有些原始像素佔 1 個螢幕像素、有些佔 2 個,筆畫粗細會不勻。
// 要與原版逐像素對齊請用整數倍(Alt-0 回到 1.0)。
const (
	MinScale  = 1.0
	MaxScale  = 4.0
	ScaleStep = 0.1
)

// shiftOverlays 把一組圖片往下移 dy 個像素。
//
// 選單列讓格點整個下移一列,而 overlay 記的是像素座標,不會自己跟著動。
func shiftOverlays(ov []*render.Overlay, dy int) {
	if dy == 0 {
		return
	}
	for _, o := range ov {
		if o == nil {
			continue
		}
		o.Rect.Min.Y += dy
		o.Rect.Max.Y += dy
	}
}

// 視窗大小的預設檔位。原版的 792×506 client area 換算成格子是 93×21
// (docs/ui/main-screen.md)。
//
// 第一個檔位是 93×22 而不是 93×21:選單列自己吃掉一列(原版那條是
// Win32 的原生選單,掛在 client area 之外,不佔字元格),多給一列
// 之後內容區才是原版的 21 列。
var sizePresets = []struct {
	name       string
	cols, rows int
}{
	{"原版版面 93×21(+選單列)", 93, 22},
	{"80×25", 80, 25},
	{"100×30", 100, 30},
	{"120×40", 120, 40},
}

// setScale 換放大倍率。回傳是否真的變了。
//
// 與字級(Zoom)分開:字級換的是**點陣字本身**(8×15 / 10×18 / 12×24),
// 換過去字形會重新設計、細節更多;放大倍率是把同一份字模拉大,
// 細節不會增加,但任何字級都能再放大。兩個都要有 ——
// 只有三種點陣字,單靠字級在 4K 螢幕上還是太小。
func (a *App) setScale(n float64) bool {
	// 先量化到 0.1:0.1 在二進位裡除不盡,一路累加下去
	// 會走到 1.2000000000000002 這種值,顯示成「1.2×」卻與 1.2 不相等,
	// 於是「已經是這個倍率了」的判斷永遠不成立。
	n = math.Round(n*10) / 10
	if n < MinScale {
		n = MinScale
	}
	if n > MaxScale {
		n = MaxScale
	}
	if n == a.Scale {
		return false
	}
	a.Scale = n
	a.Message = fmt.Sprintf("放大倍率 %.1f×", n)
	return true
}

// requestSize 請視窗層把視窗調成 cols×rows 格。
//
// app 這一層算不出視窗要幾個像素(那取決於字級與倍率,兩個都在視窗層),
// 所以只留下請求,由 cmd/wincv 每幀檢查後執行 —— 跟 Fullscreen 同一個作法。
func (a *App) requestSize(cols, rows int) bool {
	a.WantCols, a.WantRows = cols, rows
	a.Message = fmt.Sprintf("視窗調成 %d×%d 格", cols, rows)
	return true
}

// sizeMenuItems 是「視窗大小」子選單的內容。
func (a *App) sizeMenuItems() []menuItem {
	out := make([]menuItem, 0, len(sizePresets)+4)
	for _, p := range sizePresets {
		cols, rows := p.cols, p.rows
		out = append(out, menuItem{label: p.name, run: func() bool {
			return a.requestSize(cols, rows)
		}})
	}
	out = append(out, menuItem{sep: true})
	out = append(out, menuItem{label: "放大倍率 +", run: func() bool {
		return a.setScale(a.Scale + ScaleStep)
	}})
	out = append(out, menuItem{label: "放大倍率 -", run: func() bool {
		return a.setScale(a.Scale - ScaleStep)
	}})
	out = append(out, menuItem{label: "自訂欄列數…", run: a.startCustomSize})
	return out
}

// startCustomSize 問使用者要幾欄幾列。
func (a *App) startCustomSize() bool {
	a.ask("欄×列", fmt.Sprintf("%d×%d", a.thumbCols, a.rows+3), func(s string) {
		var c, r int
		// 逗號、x、×、空白都收 —— 使用者不會記得該用哪一個。
		norm := make([]rune, 0, len(s))
		for _, ch := range s {
			switch ch {
			case 'x', 'X', '×', ',', ' ', '*':
				norm = append(norm, ' ')
			default:
				norm = append(norm, ch)
			}
		}
		if _, err := fmt.Sscanf(string(norm), "%d %d", &c, &r); err != nil {
			a.Message = "看不懂,格式是「欄×列」,例如 100×30"
			return
		}
		if c < 20 || r < 5 || c > 500 || r > 200 {
			a.Message = "超出範圍(欄 20-500、列 5-200)"
			return
		}
		a.requestSize(c, r)
	})
	return true
}
