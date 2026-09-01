package pdf

import (
	"fmt"
	"io"
	"os"
	"sync"

	"github.com/pdfcpu/pdfcpu/pkg/api"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"
)

// Doc 是一份打開著的 PDF。
type Doc struct {
	Pages int

	ctx *model.Context
	mu  sync.Mutex
	// pageRefs 是「物件編號 → 第幾頁」,書籤要靠它把目的地換成頁碼。
	pageRefs map[int]int
	fonts    map[string]*Font
}

// Open 打開一份 PDF。
func Open(path string) (*Doc, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("打不開:%w", err)
	}
	defer f.Close()

	conf := model.NewDefaultConfiguration()
	// 寬鬆模式:網路上的 PDF 有很大一部分不完全合規,而嚴格模式會因為
	// 一個無關的欄位不合格就整份拒絕。
	conf.ValidationMode = model.ValidationRelaxed
	// 空密碼:大部分「加密」的 PDF 只是設了權限,使用者密碼是空的。
	// 這樣就打得開,而真的需要密碼的會回錯誤,不會靜靜地解出垃圾。
	conf.UserPW, conf.OwnerPW = "", ""

	st, err := f.Stat()
	if err != nil {
		return nil, err
	}
	ctx, err := api.ReadContext(guard(f, st.Size()), conf)
	if err != nil {
		return nil, fmt.Errorf("這份 PDF 解不開:%w", err)
	}
	if err := ctx.EnsurePageCount(); err != nil {
		return nil, fmt.Errorf("算不出頁數:%w", err)
	}
	if ctx.PageCount < 1 {
		return nil, fmt.Errorf("這份 PDF 沒有頁面")
	}
	return &Doc{Pages: ctx.PageCount, ctx: ctx, fonts: map[string]*Font{}}, nil
}

func (d *Doc) Close() error { return nil }

// guarded 是一層防呆:記下對檔案做過幾次讀取與定位,超過上限就報錯。
//
// [雷] 物件層對**截斷或根本不是 PDF 的檔案**會無止境地掃下去 ——
// 40 個位元組的垃圾就能讓它 100% CPU 轉到天荒地老,而且不是死結
// 是活迴圈,外面看起來只是「這個檔案開很久」。這是檔案管理器,
// 使用者一定會開到那種檔案。
//
// 用操作次數而不是計時:計時要另開執行緒,而放棄一個還在轉的執行緒
// 等於留下一個永遠燒 CPU 的東西。次數是在同一條路徑上就停得下來的。
type guarded struct {
	rs   io.ReadSeeker
	ops  int
	max  int
}

func guard(rs io.ReadSeeker, size int64) io.ReadSeeker {
	// 上限與檔案大小掛鉤:大檔案本來就要多讀幾次。基底值放寬到
	// 兩百萬,正常的檔案差得很遠,而空轉一秒就會撞到。
	return &guarded{rs: rs, max: 2_000_000 + int(size/8)}
}

var errRunaway = fmt.Errorf("這個檔案讓解析器停不下來,應該不是完整的 PDF")

func (g *guarded) count() error {
	g.ops++
	if g.ops > g.max {
		return errRunaway
	}
	return nil
}

func (g *guarded) Read(p []byte) (int, error) {
	if err := g.count(); err != nil {
		return 0, err
	}
	return g.rs.Read(p)
}

func (g *guarded) Seek(off int64, whence int) (int64, error) {
	if err := g.count(); err != nil {
		return 0, err
	}
	return g.rs.Seek(off, whence)
}

// Page 是一頁。
type Page struct {
	doc  *Doc
	dict types.Dict
	res  types.Dict

	// MediaBox 的四個邊。座標由左下往右上長。
	X0, Y0, X1, Y1 float64
	Rotate         int
}

// Width 與 Height 是頁面的尺寸(點)。
func (p *Page) Width() float64  { return p.X1 - p.X0 }
func (p *Page) Height() float64 { return p.Y1 - p.Y0 }

// Page 取第 n 頁,頁碼從 1 起算。
func (d *Doc) Page(n int) (*Page, error) {
	if n < 1 || n > d.Pages {
		return nil, fmt.Errorf("沒有第 %d 頁", n)
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	dict, _, attrs, err := d.ctx.XRefTable.PageDict(n, false)
	if err != nil {
		return nil, fmt.Errorf("第 %d 頁讀不出來:%w", n, err)
	}
	p := &Page{doc: d, dict: dict}
	if attrs != nil {
		p.res = attrs.Resources
		p.Rotate = attrs.Rotate
		if b := attrs.MediaBox; b != nil {
			p.X0, p.Y0, p.X1, p.Y1 = b.LL.X, b.LL.Y, b.UR.X, b.UR.Y
		}
	}
	if p.X1 <= p.X0 || p.Y1 <= p.Y0 {
		// 沒有 MediaBox 的頁面照 A4 算。尺寸只用在版面判斷上,
		// 猜一個常見值比讓整頁的欄位偵測失效好。
		p.X0, p.Y0, p.X1, p.Y1 = 0, 0, 595, 842
	}
	if p.res == nil {
		p.res, _ = deref(d.ctx.XRefTable, p.dict["Resources"]).(types.Dict)
	}
	return p, nil
}

// content 把一頁的內容資料流接起來。
//
// 一頁的內容可以被拆成好幾段串流,而且**可以拆在任何位置**,包括
// 一個運算子的中間。所以要先接起來再解讀,不能一段一段各自解。
func (p *Page) content() []byte {
	x := p.doc.ctx.XRefTable
	var out []byte
	switch o := deref(x, p.dict["Contents"]).(type) {
	case types.Array:
		for _, e := range o {
			b := streamBytes(x, e)
			out = append(out, b...)
			out = append(out, '\n')
		}
	default:
		out = streamBytes(x, p.dict["Contents"])
	}
	return out
}

// Bookmark 是書籤(PDF 自己的目錄)裡的一筆。
type Bookmark struct {
	Title string
	Page  int // 0 表示指不到頁碼
	Level int
}

// Outline 讀書籤。
//
// 這是 PDF 自己帶的目錄,比「一排頁碼」有用得多 —— 一份三百頁的
// 技術文件,書籤就是它的章節結構。
func (d *Doc) Outline() []Bookmark {
	x := d.ctx.XRefTable
	cat, err := x.Catalog()
	if err != nil {
		return nil
	}
	root, ok := deref(x, cat["Outlines"]).(types.Dict)
	if !ok {
		return nil
	}
	d.buildPageRefs()
	var out []Bookmark
	d.walkOutline(deref(x, root["First"]), 0, &out)
	return out
}

// MaxBookmarks 是書籤數的上限,擋住損壞檔案裡的迴圈。
const MaxBookmarks = 5000

func (d *Doc) walkOutline(o types.Object, level int, out *[]Bookmark) {
	x := d.ctx.XRefTable
	for i := 0; i < MaxBookmarks && len(*out) < MaxBookmarks; i++ {
		node, ok := o.(types.Dict)
		if !ok {
			return
		}
		b := Bookmark{Title: textString(deref(x, node["Title"])), Level: level}
		b.Page = d.destPage(node)
		if b.Title != "" {
			*out = append(*out, b)
		}
		if level < 8 {
			d.walkOutline(deref(x, node["First"]), level+1, out)
		}
		o = deref(x, node["Next"])
	}
}

// destPage 算出一筆書籤指到第幾頁。
//
// 目的地有三種寫法:直接的陣列、透過動作(/A)的陣列、以及具名目的地
// (一個字串或名稱,要去名稱樹裡查)。三種都要處理,不然大部分檔案的
// 書籤會全部沒有頁碼。
func (d *Doc) destPage(node types.Dict) int {
	x := d.ctx.XRefTable
	dest := deref(x, node["Dest"])
	if dest == nil {
		if a, ok := deref(x, node["A"]).(types.Dict); ok {
			dest = deref(x, a["D"])
		}
	}
	switch v := dest.(type) {
	case types.Array:
		return d.pageOfDest(v)
	case types.Name:
		return d.namedDest(v.Value())
	case types.StringLiteral:
		s, _ := types.StringLiteralToString(v)
		return d.namedDest(s)
	case types.HexLiteral:
		s, _ := types.HexLiteralToString(v)
		return d.namedDest(s)
	}
	return 0
}

func (d *Doc) pageOfDest(arr types.Array) int {
	if len(arr) == 0 {
		return 0
	}
	if ir, ok := arr[0].(types.IndirectRef); ok {
		return d.pageRefs[ir.ObjectNumber.Value()]
	}
	// 有些檔案直接寫頁碼(從 0 起算)。
	if n, ok := numOf(arr[0]); ok {
		return int(n) + 1
	}
	return 0
}

func (d *Doc) namedDest(name string) int {
	x := d.ctx.XRefTable
	o, err := x.DereferenceDestArray(name)
	if err != nil || o == nil {
		return 0
	}
	return d.pageOfDest(o)
}

// buildPageRefs 建「物件編號 → 頁碼」的對照。
func (d *Doc) buildPageRefs() {
	if d.pageRefs != nil {
		return
	}
	d.pageRefs = map[int]int{}
	for i := 1; i <= d.Pages; i++ {
		ir, err := d.ctx.XRefTable.PageDictIndRef(i)
		if err != nil || ir == nil {
			continue
		}
		d.pageRefs[ir.ObjectNumber.Value()] = i
	}
}

// textString 把 PDF 的文字字串換成 Go 的字串。
//
// 那種字串有兩種編碼,靠開頭的位元組順序記號分辨:有 FE FF 的是
// UTF-16BE,沒有的是 PDFDocEncoding。中文的書籤標題幾乎都是前者。
func textString(o types.Object) string {
	var s string
	switch v := o.(type) {
	case types.StringLiteral:
		s, _ = types.StringLiteralToString(v)
	case types.HexLiteral:
		s, _ = types.HexLiteralToString(v)
	default:
		return ""
	}
	return s
}
