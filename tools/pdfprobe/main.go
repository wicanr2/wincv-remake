// pdfprobe 回答「真實的 PDF 到底用了什麼」。
//
// 頁面渲染要決定先做哪些功能時,靠的是這個工具的統計而不是規格的
// 功能清單 —— 規格列了七種漸層、四種函式,實際檔案用得到的是少數幾種。
//
//	pdfprobe scan  <目錄>          統計一批 PDF 的漸層 / 圖樣 / 函式型別
//	pdfprobe pages <檔案>          列出哪幾頁用到漸層或圖樣
//	pdfprobe res   <檔案> <頁>     印出該頁的資源字典;加第三個參數就只印含它的內容行
//	pdfprobe lines <檔案> <頁> <起> <迄>  印出內容資料流的某段
//	pdfprobe obj   <檔案> <編號>   印出一個物件
//	pdfprobe px    <PNG> <y> <x…>  沿一條水平線取樣像素(看漸層有沒有真的在變)
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"

	"github.com/pdfcpu/pdfcpu/pkg/api"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"
)

// [雷] 物件層預設會在家目錄底下建設定目錄,兩個行程同時第一次跑會互相踩到。
// 與 internal/pdf 同樣關掉。
func init() { model.ConfigPath = "disable" }

func usage() {
	fmt.Fprintln(os.Stderr, "用法:pdfprobe scan|pages|res|lines|obj|px …(見原始碼開頭)")
	os.Exit(2)
}

func main() {
	if len(os.Args) < 3 {
		usage()
	}
	cmd, arg := os.Args[1], os.Args[2]
	rest := os.Args[3:]
	atoi := func(i int) int {
		if i >= len(rest) {
			usage()
		}
		n, err := strconv.Atoi(rest[i])
		if err != nil {
			usage()
		}
		return n
	}
	switch cmd {
	case "scan":
		scanDir(arg)
	case "pages":
		listPages(arg)
	case "res":
		grep := ""
		if len(rest) > 1 {
			grep = rest[1]
		}
		dumpPage(arg, atoi(0), grep)
	case "lines":
		dumpLines(arg, atoi(0), atoi(1), atoi(2))
	case "obj":
		dumpObj(arg, atoi(0))
	case "px":
		xs := make([]int, 0, len(rest)-1)
		for i := 1; i < len(rest); i++ {
			xs = append(xs, atoi(i))
		}
		dumpPixels(arg, atoi(0), xs)
	default:
		usage()
	}
}

func read(path string) (*model.Context, error) {
	conf := model.NewDefaultConfiguration()
	conf.ValidationMode = model.ValidationRelaxed
	return api.ReadContextFile(path)
}

// counts 記一份檔案(或一頁)裡各型別各出現幾次。
type counts struct {
	shading map[int]int
	pattern map[int]int
	fn      map[int]int
	seen    map[string]bool
}

func newCounts() *counts {
	return &counts{shading: map[int]int{}, pattern: map[int]int{},
		fn: map[int]int{}, seen: map[string]bool{}}
}

func (c *counts) any() bool { return len(c.shading) > 0 || len(c.pattern) > 0 }

func (c *counts) add(o *counts) {
	for k, v := range o.shading {
		c.shading[k] += v
	}
	for k, v := range o.pattern {
		c.pattern[k] += v
	}
	for k, v := range o.fn {
		c.fn[k] += v
	}
}

// walk 走訪一棵物件樹。間接參照只走一次:同一個函式被兩個漸層共用很常見,
// 不去重的話會把「幾個」算成「被引用幾次」,而且遇到互相參照就回不來。
//
// [雷] 深度上限只是防呆,不能拿來當終止條件。上限訂得低(試過 24)時,
// 統計會**隨執行而變** —— Go 的 map 走訪順序每次不同,先碰到某個物件的
// 路徑深淺就不同,淺的那次數得到、深的那次被截掉。而輸出看起來完全正常,
// 只是數字每跑一次差幾個。真正的終止條件是上面的去重。
func (c *counts) walk(x *model.XRefTable, o types.Object, depth int) {
	if depth > 512 {
		return
	}
	switch v := o.(type) {
	case types.IndirectRef:
		k := v.String()
		if c.seen[k] {
			return
		}
		c.seen[k] = true
		if d, err := x.Dereference(v); err == nil {
			c.walk(x, d, depth+1)
		}
	case types.Dict:
		c.dict(x, v, depth)
	case types.StreamDict:
		c.dict(x, v.Dict, depth)
	case types.Array:
		for _, e := range v {
			c.walk(x, e, depth+1)
		}
	}
}

func (c *counts) dict(x *model.XRefTable, d types.Dict, depth int) {
	num := func(k string) (int, bool) {
		switch v := d[k].(type) {
		case types.Integer:
			return v.Value(), true
		case types.Float:
			return int(v.Value()), true
		}
		return 0, false
	}
	if n, ok := num("ShadingType"); ok {
		c.shading[n]++
	}
	if n, ok := num("PatternType"); ok {
		c.pattern[n]++
	}
	if n, ok := num("FunctionType"); ok {
		c.fn[n]++
	}
	for _, v := range d {
		c.walk(x, v, depth+1)
	}
}

func scanDir(dir string) {
	files, _ := filepath.Glob(filepath.Join(dir, "*.pdf"))
	sort.Strings(files)
	total := newCounts()
	hit, opened, failed := 0, 0, 0
	for _, f := range files {
		ctx, err := read(f)
		if err != nil {
			failed++
			continue
		}
		opened++
		c := newCounts()
		// [雷] 不能直接走 XRefTable.Table。解參照會把物件流裡的物件補進
		// 同一張表,等於**一邊走一邊長**,而 Go 的 map 走訪順序每次不同,
		// 於是同一份檔案每跑一次數出來的數字都差幾個。先把編號抄下來排序,
		// 再逐一解參照。
		nums := make([]int, 0, len(ctx.XRefTable.Table))
		for n := range ctx.XRefTable.Table {
			nums = append(nums, n)
		}
		sort.Ints(nums)
		for _, n := range nums {
			o, err := ctx.XRefTable.Dereference(*types.NewIndirectRef(n, 0))
			if err == nil {
				c.walk(ctx.XRefTable, o, 0)
			}
		}
		if c.any() {
			hit++
			fmt.Printf("%-28s shading=%v pattern=%v fn=%v\n",
				filepath.Base(f), c.shading, c.pattern, c.fn)
		}
		total.add(c)
	}
	fmt.Printf("\n檔案 %d,打得開 %d,失敗 %d,含漸層或圖樣 %d\n",
		len(files), opened, failed, hit)
	fmt.Printf("ShadingType 合計 %v\nPatternType 合計 %v\nFunctionType 合計 %v\n",
		total.shading, total.pattern, total.fn)
}

func listPages(path string) {
	ctx, err := read(path)
	if err != nil {
		fmt.Println("讀不到:", err)
		return
	}
	if err := ctx.EnsurePageCount(); err != nil {
		fmt.Println("算不出頁數:", err)
		return
	}
	for p := 1; p <= ctx.PageCount; p++ {
		d, _, _, err := ctx.PageDict(p, false)
		if err != nil || d == nil {
			continue
		}
		c := newCounts()
		c.walk(ctx.XRefTable, d["Resources"], 0)
		if c.any() {
			fmt.Printf("第 %d 頁  shading=%v pattern=%v fn=%v\n", p, c.shading, c.pattern, c.fn)
		}
	}
}
