package app

import (
	"fmt"
	"image"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/wicanr2/wincv-remake/internal/imgfmt"
	"github.com/wicanr2/wincv-remake/internal/markdown"
	"github.com/wicanr2/wincv-remake/internal/officedoc"
)

// docScheme 是 Office 文件內位址的前綴。與書、PDF 同一個作法:
// 一份文件就是「幾段內容加上一份清單」,借瀏覽模式的連結導覽。
const docScheme = "doc:"

// docURL 組出文件內的位址。part 從 1 起算,0 表示分段清單。
func docURL(path string, part int) string {
	if part < 1 {
		return docScheme + path
	}
	return docScheme + path + "#" + strconv.Itoa(part)
}

func parseDocURL(raw string) (path string, part int, ok bool) {
	if !strings.HasPrefix(raw, docScheme) {
		return "", 0, false
	}
	rest := strings.TrimPrefix(raw, docScheme)
	if i := strings.LastIndex(rest, "#"); i >= 0 {
		if n, err := strconv.Atoi(rest[i+1:]); err == nil {
			return rest[:i], n, true
		}
	}
	return rest, 0, true
}

// IsOfficeDoc 判斷一個檔名是不是 Word / PowerPoint / Excel 文件。
func IsOfficeDoc(name string) bool { return officedoc.Is(name) }

// openOffice 用瀏覽模式打開一份 Office 文件,停在第一段。
func (a *App) openOffice(name string) bool {
	full := filepath.Join(a.Browser.Dir, name)
	a.bv = browseView{cur: -1}
	a.Mode = ModeBrowse
	a.browseFetch(docURL(full, 1), false)
	return true
}

// showOffice 把文件內位址變成畫面。同步做,理由與書、PDF 相同:
// 那是本機檔案,為它多開一條非同步的路只會多一種狀態要處理。
func (a *App) showOffice(raw string) {
	path, part, _ := parseDocURL(raw)
	if err := a.loadOffice(path); err != nil {
		a.bv.status = err.Error()
		return
	}
	a.bv.url = raw
	a.bv.status = ""
	a.bv.top, a.bv.cur, a.bv.cols = 0, -1, 0
	a.bv.lines, a.bv.pics = nil, nil
	a.bv.imgs, a.bv.want = map[string]image.Image{}, nil

	d := a.office
	name := filepath.Base(path)
	if part < 1 || part > len(d.Parts) {
		a.bv.title = name
		a.bv.blocks = docTOC(d, path, name)
		return
	}
	if len(d.Parts) > 1 {
		a.bv.title = fmt.Sprintf("%s — %s %d/%d", name, d.Kind.PartWord(), part, len(d.Parts))
	} else {
		a.bv.title = name
	}
	blocks := docImageRefs(d.Blocks(part - 1))
	a.bv.blocks = append(blocks, docNav(d, path, part)...)
}

// docImageRefs 把區塊裡的圖片參照加上前綴。
//
// 解析那一層產生的是文件內部的名字(它不知道自己會被誰用),
// 而排版那一層要靠前綴分辨「這張圖該去哪裡拿」。
func docImageRefs(bs []markdown.Block) []markdown.Block {
	out := make([]markdown.Block, len(bs))
	copy(out, bs)
	for i := range out {
		if out[i].Kind == markdown.Image && !strings.HasPrefix(out[i].Src, docScheme) {
			out[i].Src = docImgRef(out[i].Src)
		}
	}
	return out
}

func docImgRef(ref string) string { return docScheme + "img/" + ref }

func parseDocImgRef(src string) (string, bool) {
	return strings.CutPrefix(src, docScheme+"img/")
}

func (a *App) loadOffice(path string) error {
	if a.office != nil && a.officePath == path {
		return nil
	}
	a.closeOffice()
	d, err := officedoc.Open(path)
	if err != nil {
		return err
	}
	a.office, a.officePath = d, path
	return nil
}

// closeOffice 關掉開著的文件。離開瀏覽模式時呼叫。
func (a *App) closeOffice() {
	if a.office != nil {
		a.office.Close()
		a.office, a.officePath = nil, ""
	}
}

// docTOC 排出分段清單。
func docTOC(d *officedoc.Doc, path, name string) []markdown.Block {
	title := d.Title
	if title == "" {
		title = name
	}
	out := []markdown.Block{{Kind: markdown.Heading, Level: 1,
		Spans: []markdown.Span{{Text: title}}}}
	for i, p := range d.Parts {
		out = append(out, markdown.Block{
			Kind: markdown.List, Marker: "  ",
			Spans: []markdown.Span{{Text: fmt.Sprintf("%d. %s", i+1, p.Title),
				Style: markdown.Link, Href: docURL(path, i+1)}},
		})
	}
	return out
}

// docNav 排出頁尾的導覽。只有一段的文件不需要 —— 那時候上一段、
// 下一段、清單三個連結都指向同一個地方。
func docNav(d *officedoc.Doc, path string, part int) []markdown.Block {
	if len(d.Parts) <= 1 {
		return nil
	}
	word := d.Kind.PartWord()
	var links []markdown.Span
	if part > 1 {
		links = append(links, markdown.Span{Text: "← 上一" + word,
			Style: markdown.Link, Href: docURL(path, part-1)})
	}
	links = append(links, markdown.Span{Text: word + "清單",
		Style: markdown.Link, Href: docURL(path, 0)})
	if part < len(d.Parts) {
		links = append(links, markdown.Span{Text: "下一" + word + " →",
			Style: markdown.Link, Href: docURL(path, part+1)})
	}
	out := []markdown.Block{{Kind: markdown.Rule}}
	for _, sp := range links {
		out = append(out, markdown.Block{Kind: markdown.List,
			Marker: "  ", Spans: []markdown.Span{sp}})
	}
	return out
}

// officeImage 讀文件裡的一張圖。
func (a *App) officeImage(src string) (image.Image, error) {
	ref, ok := parseDocImgRef(src)
	if !ok || a.office == nil {
		return nil, fmt.Errorf("不是這份文件的圖")
	}
	data, err := a.office.Image(ref)
	if err != nil {
		return nil, err
	}
	m, _, err := imgfmt.Decode(ref, data)
	if err != nil {
		return nil, err
	}
	return m, nil
}
