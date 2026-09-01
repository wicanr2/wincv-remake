package app

import (
	"context"
	"fmt"
	"image"
	"strings"

	"github.com/wicanr2/wincv-remake/internal/cell"
	"github.com/wicanr2/wincv-remake/internal/gopher"
	"github.com/wicanr2/wincv-remake/internal/imgfmt"
	"github.com/wicanr2/wincv-remake/internal/imgview"
	"github.com/wicanr2/wincv-remake/internal/keys"
	"github.com/wicanr2/wincv-remake/internal/markdown"
	"github.com/wicanr2/wincv-remake/internal/render"
	"github.com/wicanr2/wincv-remake/internal/web"
	"path/filepath"
)

// browseLink 是排版之後畫面上的一個可點目標。
//
// 記的是「第幾列」而不是「第幾個區塊」:一個項目在窄畫面上會折成
// 好幾列,而游標要停在整個項目上,捲動也要以列為單位。
type browseLink struct {
	href string
	line int // 起始列
	rows int // 佔幾列
}

// browseView 是網路瀏覽模式的狀態。
//
// gopher 與 http 共用同一份狀態、同一個畫面、同一組按鍵。差別只在
// 「怎麼取回」與「怎麼變成區塊」兩件事,那兩件事各自關在 internal/gopher
// 與 internal/web 裡 —— 這一層看到的都是「一串區塊加一堆連結」。
type browseView struct {
	// url 是目前這一頁的完整位址(含 scheme)。
	url    string
	title  string
	blocks []markdown.Block
	lines  []markdown.Line
	pics   []*markdown.Pic
	links  []browseLink
	cur    int // links 的索引;沒有連結時是 -1
	top    int
	cols   int
	// hist 是上一頁的堆疊。只存位址,回上一頁時重抓 ——
	// 內容不大,而快取整棵樹會讓「重新整理」變得沒有意義。
	hist []string
	// status 是狀態列要顯示的東西(連線中、錯誤)。空的表示正常。
	status string
	// imgs 是這一頁已經取回的圖片,以位址為鍵。排版引擎是同步的,
	// 而取圖是非同步的 —— 先排一次(圖片只有 alt),圖回來了再排一次。
	imgs map[string]image.Image
	// want 是還沒取回的圖片位址。
	want []string
}

// browseResult 是一次取回的結果,由背景 goroutine 送回來。
type browseResult struct {
	url string
	// doc 是網頁;data 是 gopher 的原始位元組。兩者只會有一個。
	doc  *web.Doc
	data []byte
	gurl gopher.URL
	// img 為真表示這是頁面裡的一張內嵌圖,不是新的一頁。
	img bool
	err error
}

// Busy 說明有沒有還沒完成的網路取回。
//
// 外殼(cmd/wincv、mobile)要在 Busy 為真時持續重繪。app 這一層不會
// 自己叫重繪,而取回是非同步的 —— 不持續重繪的話結果回來了畫面也不動,
// 使用者看到的是永遠停在「連線中」。
func (a *App) Busy() bool { return a.gpending != nil }

// OpenURL 開一個位址。gopher:// 與 http(s):// 都吃,沒寫協定時當 gopher。
func (a *App) OpenURL(raw string) bool {
	if _, err := normalizeURL(raw); err != nil {
		a.Message = "位址解不開:" + err.Error()
		return true
	}
	a.bv = browseView{cur: -1}
	a.Mode = ModeBrowse
	a.browseFetch(raw, false)
	return true
}

// normalizeURL 把使用者打的東西變成一個完整位址。
//
// 沒寫協定時當 gopher:這個模式的起點是 gopher,而 http 的位址
// 幾乎都是從別處複製貼上來的,自然會帶著 scheme。
func normalizeURL(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", fmt.Errorf("位址是空的")
	}
	if web.IsHTTP(raw) || strings.HasPrefix(raw, epubScheme) ||
		strings.HasPrefix(raw, pdfScheme) || strings.HasPrefix(raw, docScheme) {
		return raw, nil
	}
	u, err := gopher.ParseURL(raw)
	if err != nil {
		return "", err
	}
	return u.String(), nil
}

// browseFetch 開始取一個位址。push 為真時把目前的位址推進上一頁堆疊。
func (a *App) browseFetch(raw string, push bool) {
	full, err := normalizeURL(raw)
	if err != nil {
		a.bv.status = "位址解不開:" + err.Error()
		return
	}
	if push && a.bv.url != "" {
		a.bv.hist = append(a.bv.hist, a.bv.url)
	}
	// 書是本機檔案,同步做完就好 —— 為它多開一條非同步的路
	// 只會多一種「正在取回」的狀態要處理。
	if strings.HasPrefix(full, epubScheme) {
		a.showBook(full)
		return
	}
	if strings.HasPrefix(full, pdfScheme) {
		a.showPDF(full)
		return
	}
	if strings.HasPrefix(full, docScheme) {
		a.showOffice(full)
		return
	}
	ch := make(chan browseResult, 1)
	a.gpending = ch

	if web.IsHTTP(full) {
		a.bv.status = "連線 " + hostOf(full) + " …"
		client := a.Web
		if client == nil {
			client = &web.Client{}
		}
		go func() {
			d, err := client.Fetch(context.Background(), full)
			ch <- browseResult{url: full, doc: d, err: err}
		}()
		return
	}

	u, err := gopher.ParseURL(full)
	if err != nil {
		a.gpending = nil
		a.bv.status = "位址解不開:" + err.Error()
		return
	}
	a.bv.status = "連線 " + u.Host + " …"
	client := a.Gopher
	if client == nil {
		client = &gopher.Client{}
	}
	go func() {
		data, err := client.Fetch(context.Background(), u)
		ch <- browseResult{url: u.String(), data: data, gurl: u, err: err}
	}()
}

// hostOf 從位址取主機名,給狀態列用。取不出來就原樣顯示。
func hostOf(raw string) string {
	s := raw
	if i := strings.Index(s, "://"); i >= 0 {
		s = s[i+3:]
	}
	if i := strings.IndexAny(s, "/?"); i >= 0 {
		s = s[:i]
	}
	if s == "" {
		return raw
	}
	return s
}

// browsePoll 收取回的結果。沒有結果就立刻回來 —— 這支在每一幀都會被呼叫。
func (a *App) browsePoll() {
	if a.gpending == nil {
		return
	}
	select {
	case r := <-a.gpending:
		a.gpending = nil
		if r.img {
			a.showInlineImage(r)
			return
		}
		a.browseShow(r)
	default:
	}
}

// browseShow 把取回的東西變成畫面。
func (a *App) browseShow(r browseResult) {
	// 截斷不是失敗:內容還是可以看,只是不完整,要說出來。
	truncated := r.err == gopher.ErrTooLarge
	if r.err != nil && !truncated {
		a.bv.status = r.err.Error()
		return
	}

	a.bv.url = r.url
	a.bv.status = ""
	a.bv.top, a.bv.cur, a.bv.cols = 0, -1, 0
	a.bv.lines, a.bv.pics = nil, nil
	a.bv.imgs, a.bv.want = nil, nil

	if r.doc != nil {
		a.showWebDoc(r.doc)
		return
	}

	a.bv.title = browseName(r.gurl)
	switch {
	case gopher.IsImage(r.gurl.Type):
		a.showImageBytes(browseName(r.gurl), r.data)
		return
	case r.gurl.Type == gopher.TypeText:
		a.bv.blocks = gopher.TextBlocks(r.data)
	default:
		// 選單、查詢結果,以及型別不認得的東西。不認得時當選單解:
		// 解不出項目的話畫面會是空的,那比把選單當成純文字倒出來好判讀。
		items := gopher.ParseMenu(r.data)
		if len(items) == 0 {
			a.bv.blocks = gopher.TextBlocks(r.data)
		} else {
			a.bv.blocks = gopher.MenuBlocks(items)
		}
	}
	if truncated {
		a.bv.status = "內容超過上限,只顯示前面的部分"
	}
}

// showWebDoc 把一份網頁放上畫面。
func (a *App) showWebDoc(d *web.Doc) {
	a.bv.url, a.bv.title = d.URL, d.Title
	if len(d.Image) > 0 {
		a.showImageBytes(d.Title, d.Image)
		return
	}
	a.bv.blocks = d.Blocks
	if len(a.bv.blocks) == 0 {
		a.bv.status = "這一頁沒有可以顯示的文字"
	}
	if d.Truncated {
		a.bv.status = "內容超過上限,只顯示前面的部分"
	}
	// 頁面裡的圖片先列出來,排版時發現裝得下才真的去取。
	a.bv.imgs = map[string]image.Image{}
	for _, b := range a.bv.blocks {
		if b.Kind == markdown.Image && web.IsHTTP(b.Src) {
			a.bv.want = append(a.bv.want, b.Src)
		}
	}
}

// showImageBytes 把一段位元組當圖片開,Esc 退回瀏覽模式。
func (a *App) showImageBytes(name string, data []byte) {
	m, err := imgview.Load(name, data)
	if err != nil {
		a.bv.status = "圖解不開:" + err.Error()
		return
	}
	a.Image = m
	a.Mode = ModeImage
	a.gReturn = true
}

// browseImages 取這一頁還沒取回的圖。
//
// 一次取一張,取回一張就重排一次:圖片會改變後面所有內容的行號,
// 而使用者在等的時候看得到文字比看到空白好。取不到的就留 alt,
// 不重試 —— 網頁上壞掉的圖連結非常多,一直重試只是白等。
func (a *App) browseImages() {
	if a.gpending != nil || len(a.bv.want) == 0 || a.Web == nil {
		return
	}
	src := a.bv.want[0]
	a.bv.want = a.bv.want[1:]
	if _, done := a.bv.imgs[src]; done {
		return
	}
	// 先佔位,取不回來也不會再排進佇列。
	a.bv.imgs[src] = nil

	ch := make(chan browseResult, 1)
	a.gpending = ch
	client := a.Web
	go func() {
		b, err := client.FetchImage(context.Background(), src)
		ch <- browseResult{url: src, data: b, img: true, err: err}
	}()
}

// showInlineImage 把取回的內嵌圖填進這一頁,並逼排版重做一次。
//
// 取不到就留著 alt,不重試:網頁上壞掉的圖連結非常多,
// 一直重試只會讓 Busy 一直為真,畫面因此永遠停不下來。
func (a *App) showInlineImage(r browseResult) {
	if r.err != nil || len(r.data) == 0 {
		return
	}
	m, _, err := imgfmt.Decode(imgNameOf(r.url), r.data)
	if err != nil {
		return
	}
	a.bv.imgs[r.url] = m
	// 圖片會改變它後面所有內容的行號,整頁都要重排。
	a.bv.cols, a.bv.lines = 0, nil
}

// imgNameOf 從位址取檔名 —— imgfmt 靠副檔名選解碼器。
func imgNameOf(raw string) string {
	s := raw
	if i := strings.IndexAny(s, "?#"); i >= 0 {
		s = s[:i]
	}
	if i := strings.LastIndex(s, "/"); i >= 0 {
		s = s[i+1:]
	}
	return s
}

// browseImage 是排版引擎要圖時的來源。
//
// 只從已經取回的那一份拿,不在這裡發網路請求 —— 排版是同步的,
// 而且會在捲動時反覆呼叫;在這裡連線會讓畫面卡住。
func (a *App) browseImage(src string) (image.Image, error) {
	if m := a.bv.imgs[src]; m != nil {
		return m, nil
	}
	// PDF 的圖也是本機的。
	if strings.HasPrefix(src, pdfScheme) {
		m, err := a.pdfImage(src)
		if err != nil {
			return nil, err
		}
		if a.bv.imgs == nil {
			a.bv.imgs = map[string]image.Image{}
		}
		a.bv.imgs[src] = m
		return m, nil
	}
	// Office 文件裡的圖同理:內嵌在檔案裡,讀出來就有。
	if strings.HasPrefix(src, docScheme) {
		m, err := a.officeImage(src)
		if err != nil {
			return nil, err
		}
		if a.bv.imgs == nil {
			a.bv.imgs = map[string]image.Image{}
		}
		a.bv.imgs[src] = m
		return m, nil
	}
	// 書裡的圖是本機 zip 的成員,讀完就有,不必等一輪。
	if a.book != nil && !web.IsHTTP(src) {
		m, err := a.bookImage(src)
		if err != nil {
			return nil, err
		}
		if a.bv.imgs == nil {
			a.bv.imgs = map[string]image.Image{}
		}
		a.bv.imgs[src] = m
		return m, nil
	}
	return nil, fmt.Errorf("還沒取回")
}

// browseName 從 gopher 位址取一個像檔名的東西,給看圖模式的標題用。
func browseName(u gopher.URL) string {
	s := u.Sel
	if i := strings.LastIndex(s, "/"); i >= 0 {
		s = s[i+1:]
	}
	if s == "" {
		s = u.Host
	}
	return s
}

// browseLayout 需要時重新排版,並重建連結表。
func (a *App) browseLayout(cols int) {
	if cols == a.bv.cols && a.bv.lines != nil {
		return
	}
	a.bv.cols = cols
	// gopher 的選單裡沒有內嵌圖(圖片是獨立的項目),網頁有。
	// 兩邊走同一條路:載入器只從已經取回的那一份拿,拿不到就留 alt。
	a.bv.lines, a.bv.pics = markdown.Layout(
		a.bv.blocks, cols, a.CellW, a.CellH, a.browseImage, markdown.DefaultTheme())

	a.bv.links = nil
	for i, ln := range a.bv.lines {
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
		if n := len(a.bv.links); n > 0 && a.bv.links[n-1].href == href &&
			a.bv.links[n-1].line+a.bv.links[n-1].rows == i {
			a.bv.links[n-1].rows++
			continue
		}
		a.bv.links = append(a.bv.links, browseLink{href: href, line: i, rows: 1})
	}
	if a.bv.cur >= len(a.bv.links) {
		a.bv.cur = len(a.bv.links) - 1
	}
	if a.bv.cur < 0 && len(a.bv.links) > 0 {
		a.bv.cur = 0
	}
}

// browseKey 處理瀏覽模式的按鍵。
func (a *App) browseKey(k keys.Key) bool {
	rows := a.rows
	if rows < 1 {
		rows = 1
	}
	switch k.Code {
	case keys.Esc:
		a.Mode = ModeBrowser
		a.closeBook()
		a.closePDF()
		a.closeOffice()
		return true
	case keys.Backspace:
		return a.browseBack()
	case keys.Up:
		return a.browseMove(-1, rows)
	case keys.Down:
		return a.browseMove(1, rows)
	case keys.Tab:
		return a.browseMove(1, rows)
	case keys.PgUp:
		a.bv.top -= rows
		a.browseClamp(rows)
		return true
	case keys.PgDn:
		a.bv.top += rows
		a.browseClamp(rows)
		return true
	case keys.Home:
		a.bv.top = 0
		if len(a.bv.links) > 0 {
			a.bv.cur = 0
		}
		return true
	case keys.End:
		a.bv.top = len(a.bv.lines)
		a.browseClamp(rows)
		if len(a.bv.links) > 0 {
			a.bv.cur = len(a.bv.links) - 1
		}
		return true
	case keys.Enter:
		return a.browseFollow()
	}
	if k.Code == keys.Rune && k.Alt && (k.R == 'g' || k.R == 'G') {
		return a.browseAsk()
	}
	return false
}

// browseMove 移動連結游標。沒有連結的頁面(文字檔)就直接捲。
func (a *App) browseMove(d, rows int) bool {
	if len(a.bv.links) == 0 {
		a.bv.top += d
		a.browseClamp(rows)
		return true
	}
	a.bv.cur += d
	if a.bv.cur < 0 {
		a.bv.cur = 0
	}
	if a.bv.cur >= len(a.bv.links) {
		a.bv.cur = len(a.bv.links) - 1
	}
	// 游標要留在畫面內。
	l := a.bv.links[a.bv.cur]
	if l.line < a.bv.top {
		a.bv.top = l.line
	}
	if l.line+l.rows > a.bv.top+rows {
		a.bv.top = l.line + l.rows - rows
	}
	a.browseClamp(rows)
	return true
}

func (a *App) browseClamp(rows int) {
	if max := len(a.bv.lines) - rows; a.bv.top > max {
		a.bv.top = max
	}
	if a.bv.top < 0 {
		a.bv.top = 0
	}
}

// browseFollow 跟著目前的連結走。
func (a *App) browseFollow() bool {
	if a.bv.cur < 0 || a.bv.cur >= len(a.bv.links) {
		return false
	}
	href := a.bv.links[a.bv.cur].href
	if web.IsHTTP(href) || strings.HasPrefix(href, epubScheme) ||
		strings.HasPrefix(href, pdfScheme) {
		a.browseFetch(href, true)
		return true
	}
	u, err := gopher.ParseURL(href)
	if err != nil {
		a.Message = err.Error()
		return true
	}
	// 型別 7 是查詢介面,要先問查什麼再送出去。
	if u.Type == gopher.TypeSearch {
		a.ask("查詢 "+u.Host, "", func(q string) {
			if q == "" {
				return
			}
			u.Search = q
			a.browseFetch(u.String(), true)
		})
		return true
	}
	a.browseFetch(href, true)
	return true
}

// browseBack 回上一頁。
func (a *App) browseBack() bool {
	if len(a.bv.hist) == 0 {
		a.Message = "已經是第一頁"
		return true
	}
	u := a.bv.hist[len(a.bv.hist)-1]
	a.bv.hist = a.bv.hist[:len(a.bv.hist)-1]
	a.browseFetch(u, false)
	return true
}

// browseAsk 問一個新位址。
func (a *App) browseAsk() bool {
	fromBrowser := a.Mode != ModeBrowse
	a.ask("位址(gopher:// 或 http://)", a.bv.url, func(s string) {
		if s == "" {
			return
		}
		if _, err := normalizeURL(s); err != nil {
			a.Message = "位址解不開:" + err.Error()
			return
		}
		// 從檔案清單叫進來的話,這裡是這一輪瀏覽的起點:
		// 換模式並清掉上一輪的歷史,不然 Backspace 會退回上次瀏覽的東西。
		if fromBrowser {
			a.bv = browseView{cur: -1}
			a.Mode = ModeBrowse
			a.browseFetch(s, false)
			return
		}
		a.browseFetch(s, true)
	})
	return true
}

func (a *App) drawBrowse(s *cell.Screen) []*render.Overlay {
	t := markdown.DefaultTheme()
	s.Clear(t.Text, t.BG)
	rows := s.Rows - 1
	if rows < 1 {
		rows = 1
	}
	a.rows = rows
	a.browseLayout(s.Cols)
	a.browseClamp(rows)

	// 目前游標所在的列範圍,用來反白。
	selFrom, selTo := -1, -1
	if a.bv.cur >= 0 && a.bv.cur < len(a.bv.links) {
		l := a.bv.links[a.bv.cur]
		selFrom, selTo = l.line, l.line+l.rows
	}

	var ovs []*render.Overlay
	for i := 0; i < rows; i++ {
		idx := a.bv.top + i
		if idx >= len(a.bv.lines) {
			break
		}
		ln := a.bv.lines[idx]
		if ln.Pic != nil {
			// 一張圖只在它的第一列排一次 overlay,而且要考慮圖被捲出
			// 畫面上緣的情況 —— 那時 overlay 的矩形要往上超出,
			// 由 rasterizer 自己裁掉。
			if (ln.PicRow == 0 || idx == a.bv.top) && ln.Pic.Img != nil {
				top := i - ln.PicRow
				ovs = append(ovs, &render.Overlay{
					Img: ln.Pic.Img,
					Rect: image.Rect(0, top*a.CellH,
						ln.Pic.Cols*a.CellW, (top+ln.Pic.Rows)*a.CellH),
					Fit: true,
				})
			}
			continue
		}
		sel := idx >= selFrom && idx < selTo
		if sel {
			s.Fill(0, i, s.Cols, 1, ' ', cell.Black, cell.LtGray)
		}
		x := 0
		for _, pc := range ln.Pieces {
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
	a.drawBrowseStatus(s)
	return ovs
}

func (a *App) drawBrowseStatus(s *cell.Screen) {
	y := s.Rows - 1
	s.Fill(0, y, s.Cols, 1, ' ', cell.LtGray2, cell.Blue)

	// 標題比位址好認,但位址才說得出「現在在哪一台」。標題有就先給標題,
	// 後面補主機名 —— 網頁的標題常常完全看不出是哪個站。
	left := a.bv.url
	if _, _, ok := parsePDFURL(a.bv.url); ok {
		left = a.bv.title
	} else if book, _, ok := parseBookURL(a.bv.url); ok {
		left = a.bv.title
		if a.book != nil && a.book.Title != a.bv.title {
			left = a.book.Title + " — " + a.bv.title
		}
		if left == "" {
			left = filepath.Base(book)
		}
	} else if a.bv.title != "" && a.bv.title != hostOf(a.bv.url) {
		left = a.bv.title + " — " + hostOf(a.bv.url)
	}
	if a.bv.status != "" {
		left = a.bv.status
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
		"Enter 開啟  Backspace 上一頁  F2 位址  Esc 離開",
		"Enter 開啟  ← 上一頁  F2 位址",
		"F2 位址  Esc 離開",
	}
	pos := ""
	if len(a.bv.links) > 0 {
		pos = fmt.Sprintf("%d/%d  ", a.bv.cur+1, len(a.bv.links))
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
