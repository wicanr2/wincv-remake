package app

import "fmt"

// MinScale / MaxScale 是整數倍放大的範圍。
//
// 上限 4 是實用的界線:8×16 的格子放到 4 倍是 32×64 px,
// 在 4K 螢幕上一列還放得下 60 格。再大就沒有欄數可用了。
const (
	MinScale = 1
	MaxScale = 4
)

// 視窗大小的預設檔位。原版的 792×506 client area 換算成格子是 93×21
// (docs/ui/main-screen.md),放在第一個。
var sizePresets = []struct {
	name       string
	cols, rows int
}{
	{"原版版面 93×21", 93, 21},
	{"80×25", 80, 25},
	{"100×30", 100, 30},
	{"120×40", 120, 40},
}

// setScale 換整數倍放大。回傳是否真的變了。
//
// 與字級(Zoom)分開:字級換的是**點陣字本身**(8×15 / 10×18 / 12×24),
// 換過去字形會重新設計、細節更多;整數倍放大是把同一份字模每個像素
// 複製 n×n,細節不會增加,但任何字級都能再放大。兩個都要有 ——
// 只有三種點陣字,單靠字級在 4K 螢幕上還是太小。
func (a *App) setScale(n int) bool {
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
	a.Message = fmt.Sprintf("放大倍率 %d×", n)
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
		return a.setScale(a.Scale + 1)
	}})
	out = append(out, menuItem{label: "放大倍率 -", run: func() bool {
		return a.setScale(a.Scale - 1)
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
