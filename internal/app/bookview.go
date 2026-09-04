package app

import (
	"fmt"
	"github.com/wicanr2/wincv-remake/internal/i18n"
	"image"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/wicanr2/wincv-remake/internal/epub"
	"github.com/wicanr2/wincv-remake/internal/imgfmt"
	"github.com/wicanr2/wincv-remake/internal/markdown"
)

// epubScheme 是書內位址的前綴。
//
// 借瀏覽模式的殼:一本書就是「一份目錄加上一堆用連結串起來的章節」,
// 與 gopher 選單是同一個形狀。連結導覽、上一頁、圖片、排版全部共用,
// 這一層只要把「章節」翻成「位址」。
const epubScheme = "epub:"

// bookURL 組出書內位址。chapter 為負數表示目錄那一頁。
func bookURL(path string, chapter int) string {
	if chapter < 0 {
		return epubScheme + path
	}
	return epubScheme + path + "#" + strconv.Itoa(chapter)
}

// parseBookURL 拆開書內位址。
func parseBookURL(raw string) (path string, chapter int, ok bool) {
	if !strings.HasPrefix(raw, epubScheme) {
		return "", 0, false
	}
	rest := strings.TrimPrefix(raw, epubScheme)
	chapter = -1
	// 從**最後**一個 # 切:書的路徑本身可能含 #。
	if i := strings.LastIndex(rest, "#"); i >= 0 {
		if n, err := strconv.Atoi(rest[i+1:]); err == nil {
			chapter, rest = n, rest[:i]
		}
	}
	return rest, chapter, true
}

// IsBook 判斷一個檔名是不是這一包看得懂的書。
func IsBook(name string) bool {
	return strings.EqualFold(filepath.Ext(name), ".epub")
}

// openBook 用瀏覽模式打開一本書,停在目錄那一頁。
func (a *App) openBook(name string) bool {
	full := filepath.Join(a.Browser.Dir, name)
	a.bv = browseView{cur: -1}
	a.Mode = ModeBrowse
	a.browseFetch(bookURL(full, -1), false)
	return true
}

// showBook 把書內位址變成畫面。同步做 —— 讀的是本機的 zip,
// 不像網路那樣可能等上幾十秒,為它多開一條非同步的路只會多一種狀態。
func (a *App) showBook(raw string) {
	path, chapter, _ := parseBookURL(raw)
	if err := a.loadBook(path); err != nil {
		a.bv.status = err.Error()
		return
	}
	a.bv.url = raw
	a.bv.status = ""
	a.bv.top, a.bv.cur, a.bv.cols = 0, -1, 0
	a.bv.lines, a.bv.pics = nil, nil
	a.bv.imgs, a.bv.want = map[string]image.Image{}, nil

	if chapter < 0 {
		a.bv.title = a.book.Title
		a.bv.blocks = bookTOC(a.book, path)
		return
	}
	blocks, err := a.book.Blocks(chapter)
	if err != nil {
		a.bv.status = err.Error()
		return
	}
	a.bv.title = a.book.Chapters[chapter].Title
	// 前後接一列導覽,不然讀完一節之後只能按 Backspace 回目錄再點下一節。
	a.bv.blocks = append(blocks, bookNav(a.book, path, chapter)...)
}

// loadBook 需要時打開書。同一本書不重開 —— 換一節就重解一次 zip
// 會讓翻頁變成等待。
func (a *App) loadBook(path string) error {
	if a.book != nil && a.bookPath == path {
		return nil
	}
	if a.book != nil {
		a.book.Close()
		a.book, a.bookPath = nil, ""
	}
	b, err := epub.Open(path)
	if err != nil {
		return fmt.Errorf(i18n.T("開不了這本書:%w"), err)
	}
	a.book, a.bookPath = b, path
	return nil
}

// closeBook 關掉開著的書。離開瀏覽模式時呼叫。
func (a *App) closeBook() {
	if a.book != nil {
		a.book.Close()
		a.book, a.bookPath = nil, ""
	}
}

// bookTOC 排出目錄那一頁。
func bookTOC(b *epub.Book, path string) []markdown.Block {
	out := []markdown.Block{
		{Kind: markdown.Heading, Level: 1,
			Spans: []markdown.Span{{Text: b.Title}}},
	}
	if b.Author != "" {
		out = append(out, markdown.Block{Kind: markdown.Para,
			Spans: []markdown.Span{{Text: b.Author, Style: markdown.Italic}}})
	}
	for i, c := range b.Chapters {
		out = append(out, markdown.Block{
			Kind:   markdown.List,
			Marker: fmt.Sprintf("%3d. ", i+1),
			Spans: []markdown.Span{{
				Text: c.Title, Style: markdown.Link, Href: bookURL(path, i),
			}},
		})
	}
	return out
}

// bookNav 是章節末尾的上下一節。
func bookNav(b *epub.Book, path string, cur int) []markdown.Block {
	var links []markdown.Span
	if cur > 0 {
		links = append(links, markdown.Span{
			Text: i18n.T("← 上一節"), Style: markdown.Link, Href: bookURL(path, cur-1)})
	}
	links = append(links, markdown.Span{
		Text: i18n.T("目錄"), Style: markdown.Link, Href: bookURL(path, -1)})
	if cur+1 < len(b.Chapters) {
		links = append(links, markdown.Span{
			Text: i18n.T("下一節 →"), Style: markdown.Link, Href: bookURL(path, cur+1)})
	}
	out := []markdown.Block{{Kind: markdown.Rule}}
	// 一列一個連結:游標是以列為單位停的,擠在同一段裡就只選得到第一個。
	for _, sp := range links {
		out = append(out, markdown.Block{
			Kind: markdown.List, Marker: "  ", Spans: []markdown.Span{sp}})
	}
	return out
}

// bookImage 讀書裡的一張圖。
func (a *App) bookImage(src string) (image.Image, error) {
	if a.book == nil {
		return nil, fmt.Errorf(i18n.T("沒有開著的書"))
	}
	data, err := a.book.Image(src)
	if err != nil {
		return nil, err
	}
	m, _, err := imgfmt.Decode(imgNameOf(src), data)
	if err != nil {
		return nil, err
	}
	return m, nil
}
