// Package pdfdoc 從 PDF 取出文字與圖片。
//
// 做的是「取出」不是「還原版面」。PDF 描述的是「把這個字放在這個座標」,
// 沒有段落、沒有欄、沒有閱讀順序 —— 那些是排版的結果而不是資料。
// 所以這裡的工作是把散落的字重新組回列,把列分回欄,再一頁一頁交出去。
//
// 文字的解讀在 internal/pdf:內容資料流的解譯、字型編碼與 ToUnicode
// 對照表。那一層決定了中文能不能讀 —— PDF 裡的字串是字型內部的編號,
// 不是文字。圖片交給 pdfcpu,它會把 CMYK JPEG、索引色點陣這些一律
// 轉成 png 或 jpg,省掉自己處理十幾種色彩空間與濾鏡的組合。
//
// 一個已知限制:沒有嵌入字型、也不是那 14 個核心字型的 PDF,詞間空白
// 還原不出來。那種檔案裡每個字元的座標都相同,而前進量要查字型的
// 寬度表才算得出來 —— 線索兩邊都沒有。
package pdfdoc

import (
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"sync"

	"github.com/pdfcpu/pdfcpu/pkg/api"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"

	"github.com/wicanr2/wincv-remake/internal/pdf"
)

// MaxImageBytes 是單張圖的上限。
const MaxImageBytes = 32 << 20

// Doc 是一份打開著的 PDF。
type Doc struct {
	Path  string
	Pages int

	mu   sync.Mutex
	d    *pdf.Doc
	imgs map[int]map[string][]byte // 頁碼 → 名稱 → 內容
}

// Open 打開一份 PDF。
func Open(path string) (*Doc, error) {
	d, err := pdf.Open(path)
	if err != nil {
		return nil, err
	}
	return &Doc{Path: path, Pages: d.Pages, d: d, imgs: map[int]map[string][]byte{}}, nil
}

func (d *Doc) Close() error { return d.d.Close() }

// Line 是一頁上的一列文字。
type Line struct {
	// Y 是列在頁面上的縱座標(PDF 座標,由下往上長)。
	Y float64
	// Indent 是這一列從左邊算起空了多少個字寬。
	Indent int
	Text   string
}

// Text 取一頁的文字,依閱讀順序排好。頁碼從 1 起算。
func (d *Doc) Text(page int) (lines []Line, err error) {
	if page < 1 || page > d.Pages {
		return nil, fmt.Errorf("沒有第 %d 頁", page)
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	p, err := d.d.Page(page)
	if err != nil {
		return nil, err
	}
	return layout(p.Texts(), p.X0, p.X1), nil
}

// Bookmark 是書籤裡的一筆。
type Bookmark = pdf.Bookmark

// Outline 讀 PDF 自己帶的目錄。沒有書籤的檔案回 nil。
func (d *Doc) Outline() []Bookmark {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.d.Outline()
}

// ImageNames 回傳一頁上有哪些圖。
func (d *Doc) ImageNames(page int) ([]string, error) {
	m, err := d.pageImages(page)
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(m))
	for n := range m {
		names = append(names, n)
	}
	sort.Strings(names)
	return names, nil
}

// Image 取一頁上的一張圖。回傳的是可以直接解碼的 png 或 jpg。
func (d *Doc) Image(page int, name string) ([]byte, error) {
	m, err := d.pageImages(page)
	if err != nil {
		return nil, err
	}
	b, ok := m[name]
	if !ok {
		return nil, fmt.Errorf("第 %d 頁沒有 %s", page, name)
	}
	return b, nil
}

// pageImages 抽一頁的圖,結果留著。
//
// 按頁抽而不是一次抽全部:一份幾百頁的技術文件圖片加起來可以是幾百 MB,
// 而使用者一次只看一頁。
func (d *Doc) pageImages(page int) (m map[string][]byte, err error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if got, ok := d.imgs[page]; ok {
		return got, nil
	}
	defer func() {
		if r := recover(); r != nil {
			m, err = nil, fmt.Errorf("第 %d 頁的圖抽不出來(%v)", page, r)
		}
	}()

	f, err := os.Open(d.Path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	conf := model.NewDefaultConfiguration()
	conf.ValidationMode = model.ValidationRelaxed
	maps, err := api.ExtractImagesRaw(f, []string{fmt.Sprint(page)}, conf)
	if err != nil {
		return nil, fmt.Errorf("抽不出圖:%w", err)
	}
	out := map[string][]byte{}
	for _, pm := range maps {
		for objNr, im := range pm {
			data, err := io.ReadAll(io.LimitReader(im, MaxImageBytes))
			if err != nil || len(data) == 0 {
				continue
			}
			name := im.Name
			if name == "" {
				name = fmt.Sprintf("obj%d", objNr)
			}
			// 名稱要帶副檔名,解碼器靠它選格式。
			out[name+"."+im.FileType] = data
		}
	}
	d.imgs[page] = out
	return out, nil
}

// TrimTrailing 把一列尾端的空白去掉。
func trimRight(s string) string { return strings.TrimRight(s, " \t") }
