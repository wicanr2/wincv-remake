package app

import (
	"fmt"
	"github.com/wicanr2/wincv-remake/internal/i18n"
	"image"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/wicanr2/wincv-remake/internal/imgfmt"
	"github.com/wicanr2/wincv-remake/internal/imgview"
	"github.com/wicanr2/wincv-remake/internal/markdown"
	"github.com/wicanr2/wincv-remake/internal/pdfdoc"
)

// pdfScheme 是 PDF 內位址的前綴。與書同一個作法:一份 PDF 就是
// 「一份頁碼清單加上一頁一頁的內容」,借瀏覽模式的連結導覽。
const pdfScheme = "pdf:"

func pdfURL(path string, page int) string {
	if page < 1 {
		return pdfScheme + path
	}
	return pdfScheme + path + "#" + strconv.Itoa(page)
}

func parsePDFURL(raw string) (path string, page int, ok bool) {
	if !strings.HasPrefix(raw, pdfScheme) {
		return "", 0, false
	}
	rest := strings.TrimPrefix(raw, pdfScheme)
	page = 0
	if i := strings.LastIndex(rest, "#"); i >= 0 {
		if n, err := strconv.Atoi(rest[i+1:]); err == nil {
			page, rest = n, rest[:i]
		}
	}
	return rest, page, true
}

// IsPDF 判斷一個檔名是不是 PDF。
func IsPDF(name string) bool {
	return strings.EqualFold(filepath.Ext(name), ".pdf")
}

// openPDF 用瀏覽模式打開一份 PDF,停在第一頁。
//
// 不從目錄開始:PDF 的「目錄」只是一排頁碼,對一份幾頁的文件毫無用處,
// 而使用者要的就是第一頁。頁末有導覽,要跳頁再回目錄。
func (a *App) openPDF(name string) bool {
	full := filepath.Join(a.Browser.Dir, name)
	a.bv = browseView{cur: -1}
	a.Mode = ModeBrowse
	a.browseFetch(pdfURL(full, 1), false)
	return true
}

// showPDF 把 PDF 內位址變成畫面。同步做,理由與書相同。
func (a *App) showPDF(raw string) {
	path, page, _ := parsePDFURL(raw)
	if err := a.loadPDF(path); err != nil {
		a.bv.status = err.Error()
		return
	}
	a.bv.url = raw
	a.bv.status = ""
	a.bv.top, a.bv.cur, a.bv.cols = 0, -1, 0
	a.bv.lines, a.bv.pics = nil, nil
	a.bv.imgs, a.bv.want = map[string]image.Image{}, nil

	name := filepath.Base(path)
	if page < 1 {
		a.bv.title = name
		a.bv.blocks = pdfTOC(a.pdf, path, name)
		return
	}
	a.bv.title = fmt.Sprintf(i18n.T("%s — 第 %d/%d 頁"), name, page, a.pdf.Pages)
	a.bv.blocks = pdfPageBlocks(a.pdf, path, page)
}

func (a *App) loadPDF(path string) error {
	if a.pdf != nil && a.pdfPath == path {
		return nil
	}
	a.closePDF()
	d, err := pdfdoc.Open(path)
	if err != nil {
		return err
	}
	a.pdf, a.pdfPath = d, path
	return nil
}

func (a *App) closePDF() {
	if a.pdf != nil {
		a.pdf.Close()
		a.pdf, a.pdfPath = nil, ""
	}
}

// pdfPageBlocks 排出一頁。
func pdfPageBlocks(d *pdfdoc.Doc, path string, page int) []markdown.Block {
	var out []markdown.Block
	lines, err := d.Text(page)
	if err != nil {
		out = append(out, markdown.Block{Kind: markdown.Para,
			Spans: []markdown.Span{{Text: err.Error()}}})
	}
	if len(lines) > 0 {
		// 用 Pre:文字的位置是從座標算回來的,重新斷行會把好不容易
		// 對回去的縮排再弄亂一次。
		texts := make([]string, 0, len(lines))
		for _, ln := range lines {
			texts = append(texts, strings.Repeat(" ", clampIndent(ln.Indent))+ln.Text)
		}
		out = append(out, markdown.Block{Kind: markdown.Pre, Lines: texts})
	} else if err == nil {
		out = append(out, markdown.Block{Kind: markdown.Para,
			Spans: []markdown.Span{{Text: i18n.T("(這一頁沒有可以取出的文字)")}}})
	}

	// 圖片放在頁末,標明是這一頁的。
	//
	// 不放回文字中間是因為**位置資訊拿不到**:PDF 把圖畫在哪裡是
	// content stream 裡的座標變換,而抽圖的那一層只交出點陣資料。
	// 硬猜位置會讓圖片插在錯誤的段落之間,那比放在頁末更難讀。
	if names, err := d.ImageNames(page); err == nil && len(names) > 0 {
		out = append(out, markdown.Block{Kind: markdown.Rule})
		out = append(out, markdown.Block{Kind: markdown.Para,
			Spans: []markdown.Span{{Text: fmt.Sprintf(i18n.T("本頁圖片(%d 張)"), len(names)),
				Style: markdown.Bold}}})
		for _, n := range names {
			out = append(out, markdown.Block{
				Kind: markdown.Image, Src: pdfImgRef(page, n), Alt: n})
		}
	}
	return append(out, pdfNav(d, path, page)...)
}

// pdfImgRef 是圖片在這一份 PDF 裡的參照。
//
// 帶頁碼是必要的:不同頁可以有同名的 XObject,只用名字會取到別頁的圖。
func pdfImgRef(page int, name string) string {
	return fmt.Sprintf("%spage/%d/%s", pdfScheme, page, name)
}

func parsePDFImgRef(ref string) (page int, name string, ok bool) {
	rest, found := strings.CutPrefix(ref, pdfScheme+"page/")
	if !found {
		return 0, "", false
	}
	num, name, found := strings.Cut(rest, "/")
	if !found {
		return 0, "", false
	}
	n, err := strconv.Atoi(num)
	if err != nil {
		return 0, "", false
	}
	return n, name, true
}

// pdfTOC 排出目錄。
//
// PDF 自己帶的書籤才是這份文件的章節結構;一排頁碼只是退路,
// 對一份三百頁的技術文件毫無用處。兩者都給,書籤在前。
func pdfTOC(d *pdfdoc.Doc, path, name string) []markdown.Block {
	out := []markdown.Block{{Kind: markdown.Heading, Level: 1,
		Spans: []markdown.Span{{Text: name}}}}
	if marks := d.Outline(); len(marks) > 0 {
		for _, m := range marks {
			sp := markdown.Span{Text: m.Title}
			if m.Page >= 1 && m.Page <= d.Pages {
				sp.Style, sp.Href = markdown.Link, pdfURL(path, m.Page)
			}
			out = append(out, markdown.Block{
				Kind: markdown.List, Level: clampLevel(m.Level + 1),
				Marker: "  ", Spans: []markdown.Span{sp},
			})
		}
		out = append(out, markdown.Block{Kind: markdown.Rule})
		out = append(out, markdown.Block{Kind: markdown.Heading, Level: 2,
			Spans: []markdown.Span{{Text: i18n.T("頁碼")}}})
	}
	for i := 1; i <= d.Pages; i++ {
		out = append(out, markdown.Block{
			Kind: markdown.List, Marker: "  ",
			Spans: []markdown.Span{{Text: fmt.Sprintf(i18n.T("第 %d 頁"), i),
				Style: markdown.Link, Href: pdfURL(path, i)}},
		})
	}
	return out
}

func pdfNav(d *pdfdoc.Doc, path string, page int) []markdown.Block {
	var links []markdown.Span
	if page > 1 {
		links = append(links, markdown.Span{Text: i18n.T("← 上一頁"),
			Style: markdown.Link, Href: pdfURL(path, page-1)})
	}
	links = append(links, markdown.Span{Text: i18n.T("頁碼清單"),
		Style: markdown.Link, Href: pdfURL(path, 0)})
	if page < d.Pages {
		links = append(links, markdown.Span{Text: i18n.T("下一頁 →"),
			Style: markdown.Link, Href: pdfURL(path, page+1)})
	}
	out := []markdown.Block{{Kind: markdown.Rule}}
	for _, sp := range links {
		out = append(out, markdown.Block{Kind: markdown.List,
			Marker: "  ", Spans: []markdown.Span{sp}})
	}
	return out
}

// PageImageDPI 是「看整頁」時的解析度。
//
// 150 比螢幕的 96 高一階:看圖模式可以放大,而放大一張 96 dpi 的圖
// 會糊掉。再高的話一頁 A4 就超過三千萬個像素,在手機上會吃掉記憶體。
const PageImageDPI = 150

// showPDFPageImage 把目前這一頁畫成圖。
//
// 取文字看的是內容,看整頁看的是版面 —— 表格、圖表、公式、簽名,
// 那些東西的意義在位置上,抽成文字就沒了。
func (a *App) showPDFPageImage() bool {
	path, page, ok := parsePDFURL(a.bv.url)
	if !ok || page < 1 {
		a.bv.status = i18n.T("這裡沒有可以畫的頁面")
		return true
	}
	if err := a.loadPDF(path); err != nil {
		a.bv.status = err.Error()
		return true
	}
	r, err := a.pdf.Render(page, PageImageDPI)
	if err != nil {
		a.bv.status = err.Error()
		return true
	}
	name := fmt.Sprintf(i18n.T("%s — 第 %d 頁"), filepath.Base(path), page)
	a.Image = imgview.FromImage(name, "PDF", r.Img, 0)
	// PDF 是向量的:放大時用更高的解析度重畫,而不是把 150 dpi 的點陣圖
	// 拉大。差別在放大之後看不看得到更多東西 —— 表格的細線、小字的註腳,
	// 點陣放大只會讓它們變成更大的糊塊。
	a.Image.Rerender = func(zoom float64) (image.Image, error) {
		return a.renderPDFPage(path, page, zoom)
	}
	a.Mode = ModeImage
	a.gReturn = true
	// 字形不是原檔那一套時要講出來:畫面看起來正常,差別在字形,
	// 不講的話使用者會以為看到的就是原樣。
	if len(r.Substituted) > 0 {
		a.Message = i18n.T("有字型畫不出來,改用系統字型:") + strings.Join(r.Substituted, "、")
	}
	if len(r.Missing) > 0 {
		a.Message = i18n.T("有字型畫不出來,那些字沒有顯示:") + strings.Join(r.Missing, "、")
	}
	return true
}

// MaxPageImagePixels 是重畫一頁的像素上限。
//
// 8 倍解析度的 A4 是一億三千萬個像素,一張 RGBA 就要 500 MB —— 手機會
// 直接被系統殺掉,桌面也會卡住好幾秒。超過就不重畫,退回點陣放大:
// 那時使用者看到的是糊的,但畫面還在動。
const MaxPageImagePixels = 40 << 20

// renderPDFPage 以指定倍率重畫一頁。
func (a *App) renderPDFPage(path string, page int, zoom float64) (image.Image, error) {
	if err := a.loadPDF(path); err != nil {
		return nil, err
	}
	dpi := PageImageDPI * zoom
	if w, h, err := a.pdf.PageSize(page); err == nil {
		px := w / 72 * dpi * h / 72 * dpi
		if px > MaxPageImagePixels {
			return nil, fmt.Errorf(i18n.T("這一頁放大到 %g 倍會超過記憶體上限"), zoom)
		}
	}
	r, err := a.pdf.Render(page, dpi)
	if err != nil {
		return nil, err
	}
	return r.Img, nil
}

// pdfPageStep 在整頁模式下換頁。
//
// 為什麼要攔:看圖模式的「上一張 / 下一張」走的是**目錄裡的圖檔清單**
// (`imageNeighbours`),而 PDF 的整頁圖不在那份清單上 —— 游標停在
// 那個 .pdf 上,按下去會跳到目錄裡不相干的另一張圖,或什麼都不做。
// 在整頁模式下,「下一張」的自然意思就是下一頁。
//
// 換頁時 bv 的內容也一起換掉:`Esc` 退回瀏覽模式時看到的要是**同一頁**
// 的文字,不是進來之前那一頁。
func (a *App) pdfPageStep(d int) bool {
	path, page, ok := parsePDFURL(a.bv.url)
	if !ok || page < 1 || a.pdf == nil {
		return false
	}
	n := page + d
	if n < 1 || n > a.pdf.Pages {
		a.Message = i18n.Sprintf("已經是第 %d 頁", page)
		return true
	}
	// 倍率跟著人走:放大到 2 倍在讀表格的人翻頁之後還在讀表格,
	// 每翻一頁都要重新放大的話,放大這個功能等於只能用在一頁上。
	zoom, fit := 1.0, true
	if a.Image != nil {
		zoom, fit = a.Image.Zoom, a.Image.Fit
	}
	a.showPDF(pdfURL(path, n))
	if !a.showPDFPageImage() {
		return false
	}
	if a.Image != nil && !fit {
		a.Image.SetZoom(zoom)
	}
	return true
}

// inPDFPage 說現在看的是不是 PDF 的整頁圖。
func (a *App) inPDFPage() bool {
	return a.gReturn && strings.HasPrefix(a.bv.url, pdfScheme)
}

// pdfImage 讀 PDF 裡的一張圖。
func (a *App) pdfImage(ref string) (image.Image, error) {
	page, name, ok := parsePDFImgRef(ref)
	if !ok || a.pdf == nil {
		return nil, fmt.Errorf(i18n.T("不是這份 PDF 的圖"))
	}
	data, err := a.pdf.Image(page, name)
	if err != nil {
		return nil, err
	}
	m, _, err := imgfmt.Decode(name, data)
	if err != nil {
		return nil, err
	}
	return m, nil
}

// clampLevel 夾住清單的縮排層數。書籤可以巢狀很多層,而畫面的寬度有限。
func clampLevel(n int) int {
	if n < 1 {
		return 1
	}
	if n > 4 {
		return 4
	}
	return n
}

// clampIndent 夾住縮排。座標換算出來的縮排偶爾會很離譜
// (置中的標題、跑到頁面右半邊的欄),整列被推出畫面就什麼都看不到。
func clampIndent(n int) int {
	if n < 0 {
		return 0
	}
	if n > 20 {
		return 20
	}
	return n
}
