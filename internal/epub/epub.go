// Package epub 讀 EPUB 電子書。
//
// EPUB 就是一個 ZIP:裡面是 XHTML 的章節、圖片,加上一份說明「照什麼
// 順序讀」的 OPF。所以這一包幾乎沒有自己的解碼工作 —— ZIP 交給
// archive/zip,XHTML 交給 internal/web 的 HTML 解析器(那是同一件事:
// 把標記壓成一串排版區塊),這裡只負責把兩者接起來、找出章節順序。
//
// 支援 EPUB 2 與 3。兩者的差別在目錄檔(2 用 NCX、3 用 nav.xhtml),
// 而閱讀順序都在 OPF 的 spine 裡,所以主要路徑是同一條。
package epub

import (
	"archive/zip"
	"encoding/xml"
	"fmt"
	"io"
	"path"
	"strings"

	"github.com/wicanr2/wincv-remake/internal/markdown"
	"github.com/wicanr2/wincv-remake/internal/web"
)

// MaxChapterBytes 是單一章節的上限。
//
// 有些書會把整本塞進一個 XHTML,那種檔案排版起來會讓畫面停很久。
const MaxChapterBytes = 8 << 20

// Book 是一本打開著的書。用完要 Close。
type Book struct {
	Title    string
	Author   string
	Chapters []Chapter

	zr   *zip.Reader
	rc   io.Closer
	root string // OPF 所在的目錄,章節與圖片的相對路徑以它為基準
	// files 是壓縮檔成員的索引,以正規化過的路徑為鍵。
	files map[string]*zip.File
}

// Chapter 是閱讀順序上的一章。
type Chapter struct {
	// Title 取自目錄;目錄沒寫就留空,由上層編號。
	Title string
	// Href 是 zip 內的路徑(已經接上 root)。
	Href string
}

// Open 打開一本書。
func Open(name string) (*Book, error) {
	zr, err := zip.OpenReader(name)
	if err != nil {
		return nil, fmt.Errorf("打不開:%w", err)
	}
	b, err := newBook(&zr.Reader, zr)
	if err != nil {
		zr.Close()
		return nil, err
	}
	return b, nil
}

// OpenBytes 從記憶體裡的一份 EPUB 打開。壓縮檔內部的 .epub 走這條。
func OpenBytes(data []byte) (*Book, error) {
	zr, err := zip.NewReader(strings.NewReader(string(data)), int64(len(data)))
	if err != nil {
		return nil, fmt.Errorf("打不開:%w", err)
	}
	return newBook(zr, nil)
}

func newBook(zr *zip.Reader, rc io.Closer) (*Book, error) {
	b := &Book{zr: zr, rc: rc, files: map[string]*zip.File{}}
	for _, f := range zr.File {
		b.files[norm(f.Name)] = f
	}
	opfPath, err := b.findOPF()
	if err != nil {
		return nil, err
	}
	b.root = path.Dir(opfPath)
	if b.root == "." {
		b.root = ""
	}
	if err := b.readOPF(opfPath); err != nil {
		return nil, err
	}
	if len(b.Chapters) == 0 {
		return nil, fmt.Errorf("這本書沒有可讀的章節")
	}
	// 目錄沒寫標題的就編號。空白的章節名在清單上是一整排空行,
	// 看起來像壞掉;實際上只是那本書的目錄沒寫到那一節。
	for i := range b.Chapters {
		if b.Chapters[i].Title == "" {
			b.Chapters[i].Title = fmt.Sprintf("第 %d 節", i+1)
		}
	}
	return b, nil
}

func (b *Book) Close() error {
	if b.rc != nil {
		return b.rc.Close()
	}
	return nil
}

// container 是 META-INF/container.xml 的結構。
type container struct {
	Rootfiles []struct {
		FullPath string `xml:"full-path,attr"`
	} `xml:"rootfiles>rootfile"`
}

// findOPF 找出 OPF 的位置。
//
// 規格說一定在 META-INF/container.xml 裡指著。有些書的 container.xml
// 壞掉或不見,那時退回「掃一個 .opf 出來」—— 讀不了整本書
// 和少一個索引檔是完全不同的嚴重程度。
func (b *Book) findOPF() (string, error) {
	if f := b.files["meta-inf/container.xml"]; f != nil {
		var c container
		if err := b.unmarshal(f, &c); err == nil {
			for _, rf := range c.Rootfiles {
				if rf.FullPath != "" && b.files[norm(rf.FullPath)] != nil {
					return rf.FullPath, nil
				}
			}
		}
	}
	for _, f := range b.zr.File {
		if strings.HasSuffix(strings.ToLower(f.Name), ".opf") {
			return f.Name, nil
		}
	}
	return "", fmt.Errorf("找不到 OPF,這可能不是 EPUB")
}

// opf 是套件檔的結構。
type opf struct {
	Metadata struct {
		Title   []string `xml:"title"`
		Creator []string `xml:"creator"`
	} `xml:"metadata"`
	Manifest struct {
		Items []struct {
			ID         string `xml:"id,attr"`
			Href       string `xml:"href,attr"`
			MediaType  string `xml:"media-type,attr"`
			Properties string `xml:"properties,attr"`
		} `xml:"item"`
	} `xml:"manifest"`
	Spine struct {
		TOC   string `xml:"toc,attr"`
		Items []struct {
			IDRef  string `xml:"idref,attr"`
			Linear string `xml:"linear,attr"`
		} `xml:"itemref"`
	} `xml:"spine"`
}

func (b *Book) readOPF(p string) error {
	f := b.files[norm(p)]
	if f == nil {
		return fmt.Errorf("讀不到 %s", p)
	}
	var o opf
	if err := b.unmarshal(f, &o); err != nil {
		return fmt.Errorf("OPF 解不開:%w", err)
	}
	if len(o.Metadata.Title) > 0 {
		b.Title = strings.TrimSpace(o.Metadata.Title[0])
	}
	if len(o.Metadata.Creator) > 0 {
		b.Author = strings.TrimSpace(o.Metadata.Creator[0])
	}

	byID := map[string]string{}
	var navHref, ncxHref string
	for _, it := range o.Manifest.Items {
		byID[it.ID] = it.Href
		if strings.Contains(it.Properties, "nav") {
			navHref = it.Href
		}
		if strings.Contains(it.MediaType, "dtbncx") {
			ncxHref = it.Href
		}
	}
	if ncxHref == "" && o.Spine.TOC != "" {
		ncxHref = byID[o.Spine.TOC]
	}
	// 目錄只用來取章節標題。取不到就用編號 —— 沒有標題還是讀得下去,
	// 讀不到內容才是問題。
	titles := b.readTOC(navHref, ncxHref)

	for _, ref := range o.Spine.Items {
		// linear="no" 是封面、版權頁那類「不在正文順序上」的東西。
		// 仍然收進來:使用者按順序翻到那裡不該撞到一片空白。
		href := byID[ref.IDRef]
		if href == "" {
			continue
		}
		full := b.resolve(href)
		if b.files[norm(full)] == nil {
			continue
		}
		b.Chapters = append(b.Chapters, Chapter{Title: titles[norm(full)], Href: full})
	}
	return nil
}

// readTOC 從目錄檔取「路徑 → 標題」。EPUB 3 用 nav.xhtml,2 用 NCX。
func (b *Book) readTOC(navHref, ncxHref string) map[string]string {
	out := map[string]string{}
	if ncxHref != "" {
		if f := b.files[norm(b.resolve(ncxHref))]; f != nil {
			var n struct {
				Points []struct {
					Label   string `xml:"navLabel>text"`
					Content struct {
						Src string `xml:"src,attr"`
					} `xml:"content"`
				} `xml:"navMap>navPoint"`
			}
			if b.unmarshal(f, &n) == nil {
				base := path.Dir(b.resolve(ncxHref))
				for _, p := range n.Points {
					src := stripFrag(p.Content.Src)
					if src == "" {
						continue
					}
					// [雷] 取**第一個**。目錄常常有好幾個 navPoint 指向
					// 同一個檔案的不同 #片段(整本切成幾個大檔的書都是
					// 這樣),後寫的會蓋掉先寫的 —— 那本書的第二節就會
					// 掛著它裡面最後一章的名字。
					k := norm(path.Join(base, src))
					if _, dup := out[k]; !dup {
						out[k] = strings.TrimSpace(p.Label)
					}
				}
			}
		}
	}
	if navHref != "" && len(out) == 0 {
		if f := b.files[norm(b.resolve(navHref))]; f != nil {
			// nav.xhtml 是 HTML 不是 XML —— 借 web 的解析器,
			// 連結的位址與文字正好就是目錄要的兩欄。
			data, err := b.read(f)
			if err == nil {
				_, blocks := web.ParseHTML(nil, strings.NewReader(string(data)))
				base := path.Dir(b.resolve(navHref))
				for _, blk := range blocks {
					for _, sp := range blk.Spans {
						if sp.Href == "" {
							continue
						}
						src := stripFrag(sp.Href)
						if src == "" {
							continue
						}
						k := norm(path.Join(base, src))
						if _, dup := out[k]; !dup {
							out[k] = strings.TrimSpace(sp.Text)
						}
					}
				}
			}
		}
	}
	return out
}

// Blocks 把第 i 章排成區塊。
func (b *Book) Blocks(i int) ([]markdown.Block, error) {
	if i < 0 || i >= len(b.Chapters) {
		return nil, fmt.Errorf("沒有第 %d 章", i+1)
	}
	f := b.files[norm(b.Chapters[i].Href)]
	if f == nil {
		return nil, fmt.Errorf("讀不到 %s", b.Chapters[i].Href)
	}
	data, err := b.read(f)
	if err != nil {
		return nil, err
	}
	// base 傳 nil:章節裡的連結是 zip 內的相對路徑,不是網址。
	// 圖片的路徑在 Image 那邊接,連結目前不跨章跳(見 app 那一層)。
	_, blocks := web.ParseHTML(nil, strings.NewReader(string(data)))
	// 圖片的 Src 接成 zip 內的絕對路徑,Image 才找得到。
	dir := path.Dir(b.Chapters[i].Href)
	for j := range blocks {
		if blocks[j].Kind == markdown.Image && blocks[j].Src != "" {
			blocks[j].Src = norm(path.Join(dir, blocks[j].Src))
		}
	}
	return blocks, nil
}

// Image 讀出書裡的一張圖。路徑是 Blocks 給的那個。
func (b *Book) Image(p string) ([]byte, error) {
	f := b.files[norm(p)]
	if f == nil {
		return nil, fmt.Errorf("書裡沒有 %s", p)
	}
	return b.read(f)
}

// resolve 把 OPF 裡的相對路徑接成 zip 內的路徑。
func (b *Book) resolve(href string) string {
	href = stripFrag(href)
	if b.root == "" {
		return href
	}
	return path.Join(b.root, href)
}

func (b *Book) read(f *zip.File) ([]byte, error) {
	rc, err := f.Open()
	if err != nil {
		return nil, err
	}
	defer rc.Close()
	data, err := io.ReadAll(io.LimitReader(rc, MaxChapterBytes))
	if err != nil {
		return nil, err
	}
	return data, nil
}

func (b *Book) unmarshal(f *zip.File, v any) error {
	data, err := b.read(f)
	if err != nil {
		return err
	}
	// 先照規格解。OPF 與 NCX 都是正規的 XML,而**嚴格模式才會正確
	// 處理命名空間**。
	//
	// [雷] 反過來說:關掉 Strict 之後,一份宣告了多個 xmlns 的 OPF
	// (真實的書幾乎都是:opf、dc、dcterms、xsi 加一個預設的)會
	// **整份解成空的而且不回報任何錯誤** —— 症狀是「這本書沒有章節」,
	// 看起來像檔案壞了。只宣告一個 xmlns 的檔案不會觸發,
	// 所以手寫的測試資料剛好會通過。
	if err := decodeXML(data, v, true); err == nil {
		return nil
	}
	// 真的壞掉的檔案(未閉合的標籤、HTML 實體)再用寬鬆模式試一次。
	return decodeXML(data, v, false)
}

func decodeXML(data []byte, v any, strict bool) error {
	dec := xml.NewDecoder(strings.NewReader(string(data)))
	dec.Strict = strict
	if !strict {
		dec.AutoClose = xml.HTMLAutoClose
		dec.Entity = xml.HTMLEntity
	}
	return dec.Decode(v)
}

// norm 把 zip 內的路徑正規化,好當索引的鍵。
//
// 大小寫不敏感:同一本書的 OPF 寫 Text/ch1.xhtml、zip 裡是 text/ch1.xhtml
// 這種事很常見,而區分大小寫的話整本書都讀不出來。
func norm(p string) string {
	p = strings.ReplaceAll(p, "\\", "/")
	p = path.Clean(strings.TrimPrefix(p, "./"))
	return strings.ToLower(strings.TrimPrefix(p, "/"))
}

func stripFrag(s string) string {
	if i := strings.IndexAny(s, "#?"); i >= 0 {
		return s[:i]
	}
	return s
}
