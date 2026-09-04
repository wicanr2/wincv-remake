package markdown

import (
	"github.com/wicanr2/wincv-remake/internal/i18n"
	"image"
	"strings"

	"github.com/wicanr2/wincv-remake/internal/cell"
)

// Piece 是排版後一列上的一段文字。
type Piece struct {
	Text  string
	FG    cell.Color
	BG    cell.Color
	Under bool
	// Href 是連結目標,只有 Link 樣式才有。
	//
	// 排版之前 Span 就帶著它了,但排版之後如果不一起帶下來,
	// 畫面上看得到底線、卻沒有東西可以點 —— 上層拿不到目標。
	Href string
}

// Pic 是一張要畫在格點上的圖。
//
// Cols / Rows 是它佔幾格;實際的像素貼圖由 render 那一層做,
// 這裡只決定格位 —— markdown 這一包不碰像素。
type Pic struct {
	Src, Alt   string
	Img        image.Image
	Err        string
	Cols, Rows int
	Row        int // 在整份文件裡從第幾列開始
}

// Line 是排版後的一列。
type Line struct {
	Pieces []Piece
	// Pic 不為 nil 表示這一列被一張圖佔著。PicRow 是圖的第幾列。
	Pic    *Pic
	PicRow int
}

// Loader 讀取圖片。傳 nil 表示不載圖,圖片只顯示 alt。
//
// 做成介面而不是直接讀檔:markdown 可能來自壓縮檔內部,
// 那時候「相對路徑」要對到壓縮檔裡的成員,不是磁碟上的檔案。
//
// **「哪些位址可以載」是載入器的政策,不是排版引擎的。** 看一份 .md
// 不該變成連外行為,但使用者自己輸入網址進去的瀏覽模式就該載 ——
// 同一個排版引擎服務兩種來源,把政策寫死在這裡會讓其中一種一定是錯的。
// 回傳錯誤的話會原樣顯示在 alt 旁邊,所以訊息要寫給人看。
type Loader func(src string) (image.Image, error)

// Theme 是各種元素的顏色。
type Theme struct {
	Text, Heading, Code, CodeBG, Quote, Link, Marker, Rule, TableHead cell.Color
	BG                                                                cell.Color
}

// DefaultTheme 沿用 image 裡那 29 個具名顏色。
//
// 挑色的原則跟 keyword_*.cfg 一樣:同一類東西同一個色相,
// 靠明度分階層。標題最亮,正文次之,裝飾性的最暗。
//
// 但「最暗」有下限:分隔線用 dkgray(#404040)畫在黑底上等於看不見,
// 而看不見的分隔線與沒有分隔線是同一回事。改用 gray(#808080)。
func DefaultTheme() Theme {
	return Theme{
		Text: cell.LtGray, Heading: cell.LtCyan, Code: cell.LtGreen,
		CodeBG: cell.Black, Quote: cell.Gray, Link: cell.LtBlue,
		Marker: cell.LtYellow, Rule: cell.Gray, TableHead: cell.White,
		BG: cell.Black,
	}
}

// MaxPicRows 是一張圖最多佔幾列。
//
// 沒有上限的話,一張直式圖會把整個畫面吃掉,而使用者要的多半是
// 「文件裡有這張圖」而不是「這張圖填滿螢幕」。要看大圖按 Enter
// 進看圖模式。
const MaxPicRows = 24

// Layout 把區塊排成固定寬度的列。
//
// load 為 nil 時圖片只留 alt 文字。cellW/cellH 是格子的像素大小,
// 用來把圖片的像素尺寸換算成格數 —— 換算要在這裡做,因為
// 「一張圖佔幾列」會影響後面所有內容的行號。
func Layout(blocks []Block, cols, cellW, cellH int, load Loader, t Theme) ([]Line, []*Pic) {
	if cols < 8 {
		cols = 8
	}
	var out []Line
	var pics []*Pic
	blank := func() { out = append(out, Line{}) }

	for bi, b := range blocks {
		if bi > 0 && b.Kind != List && b.Kind != Pre {
			// 清單項目之間不空行,其餘區塊之間空一行。
			//
			// Pre 也不空行:那是「呼叫端已經排好版」的意思,
			// 排版引擎再自己加空白就是在猜它的意圖。
			if prev := blocks[bi-1]; !(prev.Kind == List && b.Kind == List) {
				blank()
			}
		}
		switch b.Kind {
		case Heading:
			fg := t.Heading
			pre := strings.Repeat("#", b.Level) + " "
			for i, ln := range wrapSpans(b.Spans, cols-len(pre)) {
				p := []Piece{{Text: pre, FG: t.Marker}}
				if i > 0 {
					p = []Piece{{Text: strings.Repeat(" ", len(pre)), FG: t.Text}}
				}
				out = append(out, Line{Pieces: append(p, styled(ln, fg, t)...)})
			}
			// 一級與二級標題底下加一條線,層級用線條表示而不是靠顏色。
			if b.Level <= 2 {
				out = append(out, Line{Pieces: []Piece{
					{Text: strings.Repeat(string(cell.HLine), cols), FG: t.Rule},
				}})
			}
		case Para:
			for _, ln := range wrapSpans(b.Spans, cols) {
				out = append(out, Line{Pieces: styled(ln, t.Text, t)})
			}
		case Quote:
			for _, ln := range wrapSpans(b.Spans, cols-2) {
				p := []Piece{{Text: string(cell.VLine) + " ", FG: t.Rule}}
				out = append(out, Line{Pieces: append(p, styled(ln, t.Quote, t)...)})
			}
		case List:
			ind := strings.Repeat("  ", b.Level)
			mark := ind + "• "
			switch {
			case b.Marker != "":
				// 符號與內文之間一定要有空白。標記是各種格式各自帶來的
				// (RTF 帶「1.」、docx 帶「•」),誰有沒有留尾巴那個空格
				// 並不一致 —— 沒補的話會變成「1.內文」黏在一起。
				mark = ind + b.Marker
				if !strings.HasSuffix(mark, " ") {
					mark += " "
				}
			case b.Ordered:
				mark = ind + itoa(b.Num) + ". "
			}
			pad := strings.Repeat(" ", len([]rune(mark)))
			for i, ln := range wrapSpans(b.Spans, cols-len([]rune(mark))) {
				m := mark
				fg := t.Marker
				if i > 0 {
					m, fg = pad, t.Text
				}
				out = append(out, Line{Pieces: append(
					[]Piece{{Text: m, FG: fg}}, styled(ln, t.Text, t)...)})
			}
		case Pre:
			// 與 Code 的差別:不縮排、不套底色、用正文顏色。
			//
			// 給「本來就排好版、不能重新斷行」的內容用(gopher 的文字檔
			// 常常是 70 欄的 ASCII art)。markdown 的解析器不會產生這種區塊,
			// 它只從 layout 這一側被用到。
			for _, ln := range b.Lines {
				ln = expandTabs(ln)
				if len([]rune(ln)) > cols {
					ln = string([]rune(ln)[:cols])
				}
				out = append(out, Line{Pieces: []Piece{{Text: ln, FG: t.Text}}})
			}
		case Code:
			for _, ln := range b.Lines {
				ln = expandTabs(ln)
				if len([]rune(ln)) > cols-2 {
					ln = string([]rune(ln)[:cols-2])
				}
				out = append(out, Line{Pieces: []Piece{
					{Text: "  " + ln, FG: t.Code, BG: t.CodeBG},
				}})
			}
		case Rule:
			out = append(out, Line{Pieces: []Piece{
				{Text: strings.Repeat(string(cell.HLine), cols), FG: t.Rule},
			}})
		case Table:
			out = append(out, table(b, cols, t)...)
		case Image:
			pic := loadPic(b.Src, b.Alt, cols, cellW, cellH, load)
			pics = append(pics, pic)
			pic.Row = len(out)
			for r := 0; r < pic.Rows; r++ {
				out = append(out, Line{Pic: pic, PicRow: r})
			}
			if pic.Img == nil {
				// 載不到就把 alt 與原因寫出來。留白的話使用者只會覺得
				// 「這裡本來就沒東西」,而事實是「有圖但畫不出來」。
				txt := i18n.T("[圖] ") + pic.Alt
				if pic.Err != "" {
					txt += " —— " + pic.Err
				}
				out = append(out, Line{Pieces: []Piece{{Text: clip(txt, cols), FG: t.Quote}}})
			}
		}
	}
	return out, pics
}

func loadPic(src, alt string, cols, cellW, cellH int, load Loader) *Pic {
	p := &Pic{Src: src, Alt: alt}
	switch {
	case load == nil:
		p.Err = i18n.T("沒有載圖器")
	default:
		img, err := load(src)
		if err != nil {
			p.Err = err.Error()
		} else {
			p.Img = img
		}
	}
	if p.Img == nil {
		return p
	}
	b := p.Img.Bounds()
	if b.Dx() <= 0 || b.Dy() <= 0 || cellW <= 0 || cellH <= 0 {
		p.Img, p.Err = nil, i18n.T("尺寸不合理")
		return p
	}
	// 先照原尺寸換算成格數,再縮到畫面容得下。
	c := (b.Dx() + cellW - 1) / cellW
	r := (b.Dy() + cellH - 1) / cellH
	if c > cols {
		r = r * cols / c
		c = cols
	}
	if r > MaxPicRows {
		c = c * MaxPicRows / r
		r = MaxPicRows
	}
	if c < 1 {
		c = 1
	}
	if r < 1 {
		r = 1
	}
	p.Cols, p.Rows = c, r
	return p
}

func table(b Block, cols int, t Theme) []Line {
	if len(b.Rows) == 0 {
		return nil
	}
	n := 0
	for _, r := range b.Rows {
		if len(r) > n {
			n = len(r)
		}
	}
	w := make([]int, n)
	for _, r := range b.Rows {
		for i, c := range r {
			if v := width(c); v > w[i] {
				w[i] = v
			}
		}
	}
	// 太寬就等比例壓,不要讓表格把畫面撐爆。
	total := 1
	for _, v := range w {
		total += v + 3
	}
	if total > cols {
		for i := range w {
			w[i] = w[i] * (cols - 1 - 3*n) / (total - 1 - 3*n)
			if w[i] < 1 {
				w[i] = 1
			}
		}
	}
	var out []Line
	for ri, r := range b.Rows {
		fg := t.Text
		if ri == 0 {
			fg = t.TableHead
		}
		var ps []Piece
		for i := 0; i < n; i++ {
			s := ""
			if i < len(r) {
				s = clip(r[i], w[i])
			}
			align := byte('l')
			if i < len(b.Aligns) {
				align = b.Aligns[i]
			}
			ps = append(ps, Piece{Text: " " + padTo(s, w[i], align) + " ", FG: fg})
			if i < n-1 {
				ps = append(ps, Piece{Text: string(cell.VLine), FG: t.Rule})
			}
		}
		out = append(out, Line{Pieces: ps})
		if ri == 0 {
			var sep strings.Builder
			for i := 0; i < n; i++ {
				sep.WriteString(strings.Repeat(string(cell.HLine), w[i]+2))
				if i < n-1 {
					sep.WriteString(string(cell.HLine))
				}
			}
			out = append(out, Line{Pieces: []Piece{{Text: sep.String(), FG: t.Rule}}})
		}
	}
	return out
}

// styled 把一列的片段轉成畫得出來的 Piece。
//
// 格點畫面沒有字重也沒有斜體(只有一種字模),所以粗體與斜體只能
// 用顏色表示 —— 這是格點的先天限制,不是偷懶。粗體用亮一階的顏色,
// 斜體與刪除線用暗一階,讀者仍然分得出層次。
func styled(sp []Span, base cell.Color, t Theme) []Piece {
	var out []Piece
	for _, s := range sp {
		fg, bg := base, cell.Color(t.BG)
		under := false
		switch {
		case s.Style&Mono != 0:
			fg, bg = t.Code, t.CodeBG
		case s.Style&Link != 0:
			fg, under = t.Link, true
		case s.Style&Bold != 0:
			fg = cell.White
		case s.Style&Strike != 0:
			fg = t.Rule
		case s.Style&Italic != 0:
			fg = t.Quote
		}
		out = append(out, Piece{Text: s.Text, FG: fg, BG: bg, Under: under, Href: s.Href})
	}
	return out
}
