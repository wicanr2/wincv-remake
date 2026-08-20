// Package cell 是整個 UI 的唯一渲染介面。
//
// WinCV 的畫面是固定格點的自繪介面:每格一個半形字,全形字佔兩格。
// 上層(檔案列表、瀏覽器、編輯器)只寫字元與屬性,不碰像素;
// 換字型、換縮放、換 backend 都不影響上層。
package cell

// Color 是調色盤的索引。實際 RGB 由 render 層決定,
// 因為 WinCV 的配色可由使用者設定(主選單的「顏色」)。
type Color uint8

// 前 29 個是**語法上色用的具名顏色**,名稱與順序取自 WINCV.IMG 內
// 0x5692d 的斜線分隔清單(counted string,長度 227);隨附的
// keyword_*.cfg 就是用這些名字指定顏色。
//
// 後面 14 個是 image 裡另外定義、但不在那張清單上的顏色 ——
// 檔案清單的副檔名配色就用其中的 DIR-* 系列。掃 image 可以找到
// 46 個色彩 word、43 個有名字(tools/palette.py -all)。
//
// 這裡照抄它的名字與順序 —— 用它自己的詞彙,才不會在
// 「我的 16 色」與「它的 43 色」之間反覆換算出錯。
const (
	Black Color = iota
	DkGray
	Red
	LtRed
	Green
	LtGreen
	Blue
	LtBlue
	Yellow
	MildYellow
	LtYellow
	Magenta
	LtMagenta
	Cyan
	LtCyan
	Gray
	White
	LtGray
	Purple
	LtPurple
	Orange
	LtOrange
	GooseYellow
	BlueGreen
	InkGreen
	MildWhite
	MildGreen
	MildCyan
	MildMagenta
	// 以下不在 keyword_*.cfg 的顏色名清單上,但 image 裡有定義。
	DirGreen             // 目錄
	DirLtGreen           // 圖檔
	DirCyan              // 壓縮檔
	DirLtCyan
	DirYellow
	LtGray1
	NewWhite
	ToolGray
	HistogramLtGray
	LtBlueGreen
	EFCyan
	OriginGreen
	RemovableDiskGreen
	CLccwLtGray
	NumColors
)

// Names 是每個顏色的名字,順序與上面的常數一致。
// keyword_*.cfg 用名字指定顏色,解析時要對回索引。
var Names = [NumColors]string{
	"black", "dkgray", "red", "ltred", "green", "ltgreen",
	"blue", "ltblue", "yellow", "mildyellow", "ltyellow",
	"magenta", "ltmagenta", "cyan", "ltcyan", "gray",
	"white", "ltgray", "purple", "ltpurple", "orange", "ltorange",
	"gooseyellow", "bluegreen", "inkgreen",
	"mildwhite", "mildgreen", "mildcyan", "mildmagenta",
	// 這些名字不會出現在 keyword_*.cfg 裡,但保留 image 用的原名
	"dir-green", "dir-ltgreen", "dir-cyan", "dir-ltcyan", "dir-yellow",
	"ltgray1", "newwhite", "toolgray", "histogram-ltgray",
	"ltbluegreen", "ef-cyan", "origin-green",
	"removeable-disk-green", "c-lccw-ltgray",
}

// NumConfigColors 是 keyword_*.cfg 認得的顏色數。
// 語法設定檔只能用前面這 29 個。
const NumConfigColors = 29

// ByName 把 keyword_*.cfg 裡的顏色名對回索引。認不得回 (0,false)。
func ByName(n string) (Color, bool) {
	for i, v := range Names {
		if v == n {
			return Color(i), true
		}
	}
	return 0, false
}

// Cell 是畫面上的一格。
//
// 全形字存在左半格(Wide=true),右半格是 Cont=true 的佔位格,
// 它的 Ch 沒有意義。這樣「第 N 欄是什麼字」永遠只要看一格。
type Cell struct {
	Ch   rune
	FG   Color
	BG   Color
	Wide bool // 這格是全形字的左半
	Cont bool // 這格是前一格全形字的右半
}

// Screen 是一整面的格點緩衝區。
type Screen struct {
	Cols, Rows int
	cells      []Cell
}

func New(cols, rows int) *Screen {
	s := &Screen{Cols: cols, Rows: rows, cells: make([]Cell, cols*rows)}
	s.Clear(LtGray, Black)
	return s
}

func (s *Screen) inBounds(x, y int) bool {
	return x >= 0 && y >= 0 && x < s.Cols && y < s.Rows
}

// At 回傳可寫入的格。超出範圍回 nil,呼叫端要檢查。
func (s *Screen) At(x, y int) *Cell {
	if !s.inBounds(x, y) {
		return nil
	}
	return &s.cells[y*s.Cols+x]
}

func (s *Screen) Clear(fg, bg Color) {
	for i := range s.cells {
		s.cells[i] = Cell{Ch: ' ', FG: fg, BG: bg}
	}
}

// Resize 換掉緩衝區大小,內容不保留。
func (s *Screen) Resize(cols, rows int) {
	if cols == s.Cols && rows == s.Rows {
		return
	}
	s.Cols, s.Rows = cols, rows
	s.cells = make([]Cell, cols*rows)
	s.Clear(LtGray, Black)
}

// Set 寫一格半形字。
func (s *Screen) Set(x, y int, ch rune, fg, bg Color) {
	if c := s.At(x, y); c != nil {
		*c = Cell{Ch: ch, FG: fg, BG: bg}
	}
}

// IsWide 判斷一個字要不要佔兩格。
//
// 判準對齊 Big5 的顯示行為:CJK 統一漢字、全形標點、注音符號、
// 全形英數等在原版都是雙寬。這裡只涵蓋 WinCV 會遇到的區段,
// 不做完整的 Unicode East Asian Width 表。
func IsWide(r rune) bool {
	switch {
	case r >= 0x1100 && r <= 0x115F: // 韓文字母
		return true
	case r >= 0x2E80 && r <= 0x303E: // 部首補充、康熙部首、CJK 符號
		return true
	case r >= 0x3041 && r <= 0x33FF: // 平假名、片假名、注音、諺文、CJK 相容
		return true
	case r >= 0x3400 && r <= 0x4DBF: // 擴充 A
		return true
	case r >= 0x4E00 && r <= 0x9FFF: // 統一漢字
		return true
	case r >= 0xA000 && r <= 0xA4CF: // 彝文
		return true
	case r >= 0xAC00 && r <= 0xD7A3: // 諺文音節
		return true
	case r >= 0xF900 && r <= 0xFAFF: // 相容漢字
		return true
	case r >= 0xFE30 && r <= 0xFE6F: // CJK 相容形式
		return true
	case r >= 0xFF00 && r <= 0xFF60: // 全形 ASCII
		return true
	case r >= 0xFFE0 && r <= 0xFFE6: // 全形符號
		return true
	}
	return false
}

// Print 從 (x,y) 往右寫一串字,回傳寫掉的格數。
// 遇到列尾就停,不換行 —— 換行是上層的事。
func (s *Screen) Print(x, y int, str string, fg, bg Color) int {
	n := 0
	for _, r := range str {
		if x+n >= s.Cols {
			break
		}
		if IsWide(r) {
			if x+n+1 >= s.Cols {
				break // 放不下全形字就不放,不要切一半
			}
			s.cells[y*s.Cols+x+n] = Cell{Ch: r, FG: fg, BG: bg, Wide: true}
			s.cells[y*s.Cols+x+n+1] = Cell{Ch: r, FG: fg, BG: bg, Cont: true}
			n += 2
		} else {
			if !s.inBounds(x+n, y) {
				break
			}
			s.cells[y*s.Cols+x+n] = Cell{Ch: r, FG: fg, BG: bg}
			n++
		}
	}
	return n
}

// Fill 把一塊矩形填成同一個字與屬性。
func (s *Screen) Fill(x, y, w, h int, ch rune, fg, bg Color) {
	for yy := y; yy < y+h; yy++ {
		for xx := x; xx < x+w; xx++ {
			s.Set(xx, yy, ch, fg, bg)
		}
	}
}

// SetAttr 只改屬性、不動字元。反白游標列用得到。
func (s *Screen) SetAttr(x, y, w int, fg, bg Color) {
	for xx := x; xx < x+w; xx++ {
		if c := s.At(xx, y); c != nil {
			c.FG, c.BG = fg, bg
		}
	}
}
