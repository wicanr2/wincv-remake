package app

import (
	"context"
	"fmt"
	"strings"

	"github.com/wicanr2/wincv-remake/internal/cell"
	"github.com/wicanr2/wincv-remake/internal/gopher"
	"github.com/wicanr2/wincv-remake/internal/imgview"
	"github.com/wicanr2/wincv-remake/internal/keys"
	"github.com/wicanr2/wincv-remake/internal/markdown"
	"github.com/wicanr2/wincv-remake/internal/render"
)

// gopherLink 是排版之後畫面上的一個可點目標。
//
// 記的是「第幾列」而不是「第幾個區塊」:一個選單項目在窄畫面上會折成
// 好幾列,而游標要停在整個項目上,捲動也要以列為單位。
type gopherLink struct {
	href string
	line int // 起始列
	rows int // 佔幾列
}

// gopherView 是 gopher 瀏覽模式的狀態。
type gopherView struct {
	url    gopher.URL
	blocks []markdown.Block
	lines  []markdown.Line
	links  []gopherLink
	cur    int // links 的索引;沒有連結時是 -1
	top    int
	cols   int
	// hist 是上一頁的堆疊。只存位址,回上一頁時重抓 ——
	// gopher 的內容不大,而快取整棵樹會讓「重新整理」變得沒有意義。
	hist []gopher.URL
	// status 是狀態列要顯示的東西(連線中、錯誤)。空的表示正常。
	status string
}

// gopherResult 是一次取回的結果,由背景 goroutine 送回來。
type gopherResult struct {
	url  gopher.URL
	data []byte
	err  error
}

// Busy 說明有沒有還沒完成的網路取回。
//
// 外殼(cmd/wincv、mobile)要在 Busy 為真時持續重繪。app 這一層不會
// 自己叫重繪,而取回是非同步的 —— 不持續重繪的話結果回來了畫面也不動,
// 使用者看到的是永遠停在「連線中」。
func (a *App) Busy() bool { return a.gpending != nil }

// OpenGopher 開一個 gopher 位址。
func (a *App) OpenGopher(raw string) bool {
	u, err := gopher.ParseURL(raw)
	if err != nil {
		a.Message = "位址解不開:" + err.Error()
		return true
	}
	a.gv = gopherView{cur: -1}
	a.Mode = ModeGopher
	a.gopherFetch(u, false)
	return true
}

// gopherFetch 開始取一個位址。push 為真時把目前的位址推進上一頁堆疊。
func (a *App) gopherFetch(u gopher.URL, push bool) {
	if push && a.gv.url.Host != "" {
		a.gv.hist = append(a.gv.hist, a.gv.url)
	}
	a.gv.status = "連線 " + u.Host + " …"
	ch := make(chan gopherResult, 1)
	a.gpending = ch
	client := a.Gopher
	if client == nil {
		client = &gopher.Client{}
	}
	go func() {
		data, err := client.Fetch(context.Background(), u)
		ch <- gopherResult{url: u, data: data, err: err}
	}()
}

// gopherPoll 收取回的結果。沒有結果就立刻回來 —— 這支在每一幀都會被呼叫。
func (a *App) gopherPoll() {
	if a.gpending == nil {
		return
	}
	select {
	case r := <-a.gpending:
		a.gpending = nil
		a.gopherShow(r)
	default:
	}
}

// gopherShow 把取回的東西變成畫面。
func (a *App) gopherShow(r gopherResult) {
	// 截斷不是失敗:內容還是可以看,只是不完整,要說出來。
	truncated := r.err == gopher.ErrTooLarge
	if r.err != nil && !truncated {
		a.gv.status = r.err.Error()
		return
	}

	a.gv.url = r.url
	a.gv.status = ""
	a.gv.top, a.gv.cur, a.gv.cols = 0, -1, 0
	a.gv.lines = nil

	switch {
	case gopher.IsImage(r.url.Type):
		m, err := imgview.Load(gopherName(r.url), r.data)
		if err != nil {
			a.gv.status = "圖解不開:" + err.Error()
			return
		}
		a.Image = m
		a.Mode = ModeImage
		a.gReturn = true
		return
	case r.url.Type == gopher.TypeText:
		a.gv.blocks = gopher.TextBlocks(r.data)
	default:
		// 選單、查詢結果,以及型別不認得的東西。不認得時當選單解:
		// 解不出項目的話畫面會是空的,那比把選單當成純文字倒出來好判讀。
		items := gopher.ParseMenu(r.data)
		if len(items) == 0 {
			a.gv.blocks = gopher.TextBlocks(r.data)
		} else {
			a.gv.blocks = gopher.MenuBlocks(items)
		}
	}
	if truncated {
		a.gv.status = "內容超過上限,只顯示前面的部分"
	}
}

// gopherName 從位址取一個像檔名的東西,給看圖模式的標題用。
func gopherName(u gopher.URL) string {
	s := u.Sel
	if i := strings.LastIndex(s, "/"); i >= 0 {
		s = s[i+1:]
	}
	if s == "" {
		s = u.Host
	}
	return s
}

// gopherLayout 需要時重新排版,並重建連結表。
func (a *App) gopherLayout(cols int) {
	if cols == a.gv.cols && a.gv.lines != nil {
		return
	}
	a.gv.cols = cols
	// load 傳 nil:gopher 的選單裡沒有內嵌圖,圖片是獨立的項目。
	a.gv.lines, _ = markdown.Layout(
		a.gv.blocks, cols, a.CellW, a.CellH, nil, markdown.DefaultTheme())

	a.gv.links = nil
	for i, ln := range a.gv.lines {
		href := ""
		for _, p := range ln.Pieces {
			if p.Href != "" {
				href = p.Href
				break
			}
		}
		if href == "" {
			continue
		}
		// 同一個連結折成多列時只記一次,但要把列數加上去 ——
		// 游標要把整個項目反白,不是只反白第一列。
		if n := len(a.gv.links); n > 0 && a.gv.links[n-1].href == href &&
			a.gv.links[n-1].line+a.gv.links[n-1].rows == i {
			a.gv.links[n-1].rows++
			continue
		}
		a.gv.links = append(a.gv.links, gopherLink{href: href, line: i, rows: 1})
	}
	if a.gv.cur >= len(a.gv.links) {
		a.gv.cur = len(a.gv.links) - 1
	}
	if a.gv.cur < 0 && len(a.gv.links) > 0 {
		a.gv.cur = 0
	}
}

// gopherKey 處理瀏覽模式的按鍵。
func (a *App) gopherKey(k keys.Key) bool {
	rows := a.rows
	if rows < 1 {
		rows = 1
	}
	switch k.Code {
	case keys.Esc:
		a.Mode = ModeBrowser
		return true
	case keys.Backspace:
		return a.gopherBack()
	case keys.Up:
		return a.gopherMove(-1, rows)
	case keys.Down:
		return a.gopherMove(1, rows)
	case keys.Tab:
		return a.gopherMove(1, rows)
	case keys.PgUp:
		a.gv.top -= rows
		a.gopherClamp(rows)
		return true
	case keys.PgDn:
		a.gv.top += rows
		a.gopherClamp(rows)
		return true
	case keys.Home:
		a.gv.top = 0
		if len(a.gv.links) > 0 {
			a.gv.cur = 0
		}
		return true
	case keys.End:
		a.gv.top = len(a.gv.lines)
		a.gopherClamp(rows)
		if len(a.gv.links) > 0 {
			a.gv.cur = len(a.gv.links) - 1
		}
		return true
	case keys.Enter:
		return a.gopherFollow()
	}
	if k.Code == keys.Rune && k.Alt && (k.R == 'g' || k.R == 'G') {
		return a.browseAsk()
	}
	return false
}

// gopherMove 移動連結游標。沒有連結的頁面(文字檔)就直接捲。
func (a *App) gopherMove(d, rows int) bool {
	if len(a.gv.links) == 0 {
		a.gv.top += d
		a.gopherClamp(rows)
		return true
	}
	a.gv.cur += d
	if a.gv.cur < 0 {
		a.gv.cur = 0
	}
	if a.gv.cur >= len(a.gv.links) {
		a.gv.cur = len(a.gv.links) - 1
	}
	// 游標要留在畫面內。
	l := a.gv.links[a.gv.cur]
	if l.line < a.gv.top {
		a.gv.top = l.line
	}
	if l.line+l.rows > a.gv.top+rows {
		a.gv.top = l.line + l.rows - rows
	}
	a.gopherClamp(rows)
	return true
}

func (a *App) gopherClamp(rows int) {
	if max := len(a.gv.lines) - rows; a.gv.top > max {
		a.gv.top = max
	}
	if a.gv.top < 0 {
		a.gv.top = 0
	}
}

// gopherFollow 跟著目前的連結走。
func (a *App) gopherFollow() bool {
	if a.gv.cur < 0 || a.gv.cur >= len(a.gv.links) {
		return false
	}
	href := a.gv.links[a.gv.cur].href
	// http(s) 的連結這個客戶端解不了。把位址顯示出來讓人自己處理,
	// 比默默沒反應好 —— 沒反應會讓人以為程式壞了。
	if strings.HasPrefix(href, "http://") || strings.HasPrefix(href, "https://") {
		a.Message = "這是網頁連結,本程式只解 gopher:" + href
		return true
	}
	u, err := gopher.ParseURL(href)
	if err != nil {
		a.Message = err.Error()
		return true
	}
	if u.Type == gopher.TypeSearch {
		a.ask("查詢 "+u.Host, "", func(q string) {
			if q == "" {
				return
			}
			u.Search = q
			a.gopherFetch(u, true)
		})
		return true
	}
	a.gopherFetch(u, true)
	return true
}

// gopherBack 回上一頁。
func (a *App) gopherBack() bool {
	if len(a.gv.hist) == 0 {
		a.Message = "已經是第一頁"
		return true
	}
	u := a.gv.hist[len(a.gv.hist)-1]
	a.gv.hist = a.gv.hist[:len(a.gv.hist)-1]
	a.gopherFetch(u, false)
	return true
}

// browseAsk 問一個新位址。
func (a *App) browseAsk() bool {
	cur := ""
	if a.gv.url.Host != "" {
		cur = a.gv.url.String()
	}
	fromBrowser := a.Mode != ModeGopher
	a.ask("gopher 位址", cur, func(s string) {
		if s == "" {
			return
		}
		u, err := gopher.ParseURL(s)
		if err != nil {
			a.Message = "位址解不開:" + err.Error()
			return
		}
		// 從檔案清單叫進來的話,這裡是這一輪瀏覽的起點:
		// 換模式並清掉上一輪的歷史,不然 Backspace 會退回上次瀏覽的東西。
		if fromBrowser {
			a.gv = gopherView{cur: -1}
			a.Mode = ModeGopher
			a.gopherFetch(u, false)
			return
		}
		a.gopherFetch(u, true)
	})
	return true
}

func (a *App) drawGopher(s *cell.Screen) []*render.Overlay {
	t := markdown.DefaultTheme()
	s.Clear(t.Text, t.BG)
	rows := s.Rows - 1
	if rows < 1 {
		rows = 1
	}
	a.rows = rows
	a.gopherLayout(s.Cols)
	a.gopherClamp(rows)

	// 目前游標所在的列範圍,用來反白。
	selFrom, selTo := -1, -1
	if a.gv.cur >= 0 && a.gv.cur < len(a.gv.links) {
		l := a.gv.links[a.gv.cur]
		selFrom, selTo = l.line, l.line+l.rows
	}

	for i := 0; i < rows; i++ {
		idx := a.gv.top + i
		if idx >= len(a.gv.lines) {
			break
		}
		sel := idx >= selFrom && idx < selTo
		if sel {
			s.Fill(0, i, s.Cols, 1, ' ', cell.Black, cell.LtGray)
		}
		x := 0
		for _, pc := range a.gv.lines[idx].Pieces {
			if x >= s.Cols {
				break
			}
			fg, bg := pc.FG, pc.BG
			if sel {
				fg, bg = cell.Black, cell.LtGray
			}
			n := s.Print(x, i, pc.Text, fg, bg)
			if pc.Under && !sel {
				s.Underline(x, i, n, true)
			}
			x += n
		}
	}
	a.drawGopherStatus(s)
	return nil
}

func (a *App) drawGopherStatus(s *cell.Screen) {
	y := s.Rows - 1
	s.Fill(0, y, s.Cols, 1, ' ', cell.LtGray2, cell.Blue)

	left := a.gv.url.String()
	if a.gv.status != "" {
		left = a.gv.status
	}
	if a.Message != "" {
		left = a.Message
	}
	s.Print(0, y, left, cell.LtGray2, cell.Blue)

	// [雷] 寬度要用 cellWidth 不能用 len([]rune(...)) —— 中文字佔兩格。
	// 用 rune 數會低估,判斷式失準之後提示會安靜地整條消失,
	// 而畫面上看起來只是「這個模式沒有提示」。
	//
	// 由長到短試,擠不下最短的那個就不畫。
	cands := []string{
		"Enter 開啟  Backspace 上一頁  Alt-G 位址  Esc 離開",
		"Enter 開啟  ← 上一頁  Alt-G 位址",
		"Alt-G 位址  Esc 離開",
	}
	pos := ""
	if len(a.gv.links) > 0 {
		pos = fmt.Sprintf("%d/%d  ", a.gv.cur+1, len(a.gv.links))
	}
	for _, c := range cands {
		right := pos + c
		x := s.Cols - cellWidth(right) - 1
		if x > cellWidth(left)+1 {
			s.Print(x, y, right, cell.LtGray2, cell.Blue)
			break
		}
	}
}
