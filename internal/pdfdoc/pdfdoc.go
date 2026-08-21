// Package pdfdoc 從 PDF 取出文字與圖片。
//
// 做的是「取出」不是「還原版面」。PDF 描述的是「把這個字放在這個座標」,
// 沒有段落、沒有欄、沒有閱讀順序 —— 那些是排版的結果而不是資料。
// 想在字元格點上重現原始版面,等於要寫一個 PDF 渲染器,而純 Go 沒有
// 堪用的;接 C 函式庫又會破掉四平台交叉編譯。
//
// 所以這裡做的是:把同一列的文字片段依 X 座標接起來(順帶把 PDF 用
// 座標間距表示的空白還原成空格),一頁一頁交出去。
//
// 兩個已知限制,都是這個做法的必然結果而不是 bug:
//
//   - **分欄的文件會左右欄交錯。** 同一條掃描線上的字被接成一列,
//     而分欄版面的同一條線上有兩欄的內容。要分欄得先偵測欄界,
//     那已經是版面分析,不是取文字。
//   - **沒有嵌入字型的 PDF 還原不出詞間空白。** 那種檔案裡每個字元
//     的座標都相同(前進量要查字型的寬度表才算得出來,而標準 14 字型
//     沒有嵌在檔案裡),空格字元本身又不會出現在文字串流中 ——
//     線索兩邊都沒有。真實世界的 PDF 幾乎都嵌入字型,不受影響。
//
// 兩個第三方套件各司其職:文字用 rsc.io/pdf(小、夠用),圖片用
// pdfcpu(它會把 CMYK JPEG、索引色點陣這些一律轉成 png 或 jpg,
// 省掉自己處理十幾種 ColorSpace 與 Filter 的組合)。
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
	rscpdf "rsc.io/pdf"
)

// MaxImageBytes 是單張圖的上限。
const MaxImageBytes = 32 << 20

// spaceRatio 是「多寬的間距算一個空格」,以字級為單位。
const spaceRatio = 0.125

// Doc 是一份打開著的 PDF。
type Doc struct {
	Path  string
	Pages int

	mu   sync.Mutex
	r    *rscpdf.Reader
	imgs map[int]map[string][]byte // 頁碼 → 名稱 → 內容
}

// Open 打開一份 PDF。
func Open(path string) (d *Doc, err error) {
	// [雷] rsc.io/pdf 遇到看不懂的東西是 **panic 而不是回錯誤**
	// (例如 DCTDecode 這個到處都是的濾鏡)。這是檔案管理器,
	// 使用者會開到各種來路不明的 PDF —— 一個 panic 就是整個程式沒了。
	defer func() {
		if r := recover(); r != nil {
			d, err = nil, fmt.Errorf("這份 PDF 解不開(%v)", r)
		}
	}()
	r, err := rscpdf.Open(path)
	if err != nil {
		return nil, fmt.Errorf("打不開:%w", err)
	}
	n := r.NumPage()
	if n < 1 {
		return nil, fmt.Errorf("這份 PDF 沒有頁面")
	}
	return &Doc{Path: path, Pages: n, r: r, imgs: map[int]map[string][]byte{}}, nil
}

func (d *Doc) Close() error { return nil }

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
	defer func() {
		if r := recover(); r != nil {
			lines, err = nil, fmt.Errorf("第 %d 頁解不開(%v)", page, r)
		}
	}()
	d.mu.Lock()
	p := d.r.Page(page)
	d.mu.Unlock()
	if p.V.IsNull() {
		return nil, nil
	}
	return layoutText(p.Content().Text), nil
}

// layoutText 把散落的文字片段組成一列一列。
func layoutText(ts []rscpdf.Text) []Line {
	if len(ts) == 0 {
		return nil
	}
	// 依 Y 分列。同一列的 Y 不會完全相同(上下標、不同字級),
	// 所以用字級的一半當容差 —— 固定值在放大過的頁面上會把
	// 兩列併成一列,在小字頁面上會把一列拆成兩列。
	type row struct {
		y     float64
		items []rscpdf.Text
	}
	var rows []row
	for _, t := range ts {
		tol := t.FontSize * 0.4
		if tol < 1 {
			tol = 1
		}
		placed := false
		for i := range rows {
			if abs(rows[i].y-t.Y) <= tol {
				rows[i].items = append(rows[i].items, t)
				placed = true
				break
			}
		}
		if !placed {
			rows = append(rows, row{t.Y, []rscpdf.Text{t}})
		}
	}
	// 由上往下:PDF 的 Y 軸由下往上長。
	sort.SliceStable(rows, func(i, j int) bool { return rows[i].y > rows[j].y })

	var out []Line
	for _, r := range rows {
		sort.SliceStable(r.items, func(a, b int) bool { return r.items[a].X < r.items[b].X })
		var sb strings.Builder
		var prevEnd, size float64
		for i, t := range r.items {
			if t.FontSize > size {
				size = t.FontSize
			}
			// [雷] PDF 裡**沒有空格字元**,字與字之間的空白是靠座標
			// 間距表示的。不還原的話整頁會變成
			// 「Trace-basedJust-in-TimeTypeSpecialization」這樣。
			//
			// 門檻取「半個空格」:一個空格大約是字級的四分之一寬,
			// 超過一半就當作有空白。取低一點是刻意的 —— 多一個空格
			// 只是版面鬆一點,少一個空格會讓兩個字黏成一個看不懂的詞。
			if i > 0 && t.X-prevEnd > t.FontSize*spaceRatio {
				sb.WriteByte(' ')
			}
			sb.WriteString(t.S)
			prevEnd = t.X + t.W
		}
		text := strings.TrimRight(sb.String(), " ")
		if strings.TrimSpace(text) == "" {
			continue
		}
		indent := 0
		if size > 0 && len(r.items) > 0 {
			// 左邊界換算成字寬。頁面左緣通常有 50-70pt 的邊界,
			// 扣掉之後剩下的才是真正的縮排。
			if x := r.items[0].X - 54; x > 0 {
				indent = int(x / (size * 0.5))
			}
		}
		out = append(out, Line{Y: r.y, Indent: indent, Text: text})
	}
	return out
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
	// 寬鬆模式:網路上的 PDF 有很大一部分不完全合規,而嚴格模式
	// 會因為一個無關的欄位不合格就整份拒絕。
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

func abs(f float64) float64 {
	if f < 0 {
		return -f
	}
	return f
}
