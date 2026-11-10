// docsum-net 把網頁、gopher 選單與 EPUB 解成區塊,印出每個區塊的所有欄位,
// 當 Rust 改寫(rust/crates/web、gopher、epub)的對照真值。
//
//	tools/go.sh run ./tools/docsum-net < 清單
//
// 清單每列「模式 TAB 路徑」,模式是:
//
//	html    經 web.Client.Fetch 取回(本機 127.0.0.1 的伺服器,Content-Type: text/html),
//	        走判型別、解碼、拆區塊、接相對連結的整條路。位址是 http://SRV/f/<序號>/page.html,
//	        輸出裡的「127.0.0.1:埠」一律寫成 SRV。
//	raw     web.ParseHTML(nil, 原始位元組):EPUB 章節走的那條路(不解碼、不接連結)。
//	gopher  gopher.ParseMenu 的每一列、MenuBlocks、TextBlocks。
//	epub    epub.Open,印書名、作者、章節,再逐章 Blocks,逐張圖 Image。
//
// 每一列以 TAB 分欄,字串先照 Go 的 range 規則把壞位元組換成 U+FFFD,
// 再把 \ TAB 換行 CR 寫成 \\ \t \n \r:
//
//	F 模式 路徑                                        每個輸入一行
//	E 訊息                                             錯誤
//	D 位址 標題 截斷 圖的位元組數                       html 的 Doc
//	T 標題                                             raw 的標題
//	I 序號 型別 顯示 selector 主機 埠 連結 網頁位址 位址  gopher 的一列
//	K 書名 作者 章數                                    epub
//	C 序號 標題 href                                   epub 的章節
//	X 序號                                             epub 的第幾章,之後是區塊
//	M src 位元組數|錯誤                                 epub 的圖
//	G 名稱                                             區塊組的開頭(menu / text / doc)
//	B 序號 種類 階層 語言 有序 序號 記號 src alt 對齊 列數 每個區塊
//	S 樣式 文字 href                                   區塊的 Span
//	L 文字                                             區塊的 Lines
//	R 格...                                            表格的一列
//
// Rust 端是 rust/crates/epub/examples/docsum.rs(epub 依賴 web,另外帶 gopher),輸出同樣格式。
package main

import (
	"bufio"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/wicanr2/wincv-remake/internal/epub"
	"github.com/wicanr2/wincv-remake/internal/gopher"
	"github.com/wicanr2/wincv-remake/internal/markdown"
	"github.com/wicanr2/wincv-remake/internal/web"
)

var escaper = strings.NewReplacer(`\`, `\\`, "\t", `\t`, "\n", `\n`, "\r", `\r`)

// esc:壞位元組各換一個 U+FFFD(Go 的排版引擎以字元走訪時看到的就是它),再逃脫。
func esc(s string) string { return escaper.Replace(string([]rune(s))) }

func b01(b bool) int {
	if b {
		return 1
	}
	return 0
}

var kindName = map[markdown.Kind]string{
	markdown.Para: "Para", markdown.Heading: "Heading", markdown.Code: "Code", markdown.Quote: "Quote",
	markdown.List: "List", markdown.Rule: "Rule", markdown.Image: "Image", markdown.Table: "Table",
	markdown.Pre: "Pre",
}

func blocks(w func(string, ...any), name string, bs []markdown.Block) {
	w("G\t%s\n", name)
	for i, b := range bs {
		w("B\t%d\t%s\t%d\t%s\t%d\t%d\t%s\t%s\t%s\t%s\t%d\n", i, kindName[b.Kind], b.Level, esc(b.Lang),
			b01(b.Ordered), b.Num, esc(b.Marker), esc(b.Src), esc(b.Alt), esc(string(b.Aligns)), len(b.Rows))
		for _, sp := range b.Spans {
			w("S\t%d\t%s\t%s\n", sp.Style, esc(sp.Text), esc(sp.Href))
		}
		for _, ln := range b.Lines {
			w("L\t%s\n", esc(ln))
		}
		for _, r := range b.Rows {
			cells := make([]string, len(r))
			for j, c := range r {
				cells[j] = esc(c)
			}
			w("R\t%s\n", strings.Join(cells, "\t"))
		}
	}
}

// chapterErr 把讀檔的錯誤分類:zip 函式庫自己的訊息兩邊不同,只留類別。
func chapterErr(err error) string {
	s := err.Error()
	for _, p := range []string{"讀不到 ", "沒有第 ", "書裡沒有 "} {
		if strings.HasPrefix(s, p) {
			return s
		}
	}
	return "讀取錯誤"
}

func main() {
	var (
		mu    sync.Mutex
		files []string
	)
	srv := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		parts := strings.Split(r.URL.Path, "/") // "", "f", 序號, page.html
		mu.Lock()
		nfiles := len(files)
		var name string
		n, err := strconv.Atoi(parts[len(parts)-2])
		if err == nil && n >= 0 && n < nfiles {
			name = files[n]
		}
		mu.Unlock()
		if len(parts) != 4 || name == "" {
			http.NotFound(rw, r)
			return
		}
		b, err := os.ReadFile(name)
		if err != nil {
			http.NotFound(rw, r)
			return
		}
		rw.Header().Set("Content-Type", "text/html")
		rw.Write(b)
	}))
	defer srv.Close()
	host := strings.TrimPrefix(srv.URL, "http://")
	client := &web.Client{Timeout: 60 * time.Second}
	ctx := context.Background()

	out := bufio.NewWriter(os.Stdout)
	defer out.Flush()
	in := bufio.NewScanner(os.Stdin)
	in.Buffer(make([]byte, 1<<20), 1<<20)
	for in.Scan() {
		mode, path, ok := strings.Cut(in.Text(), "\t")
		if !ok {
			continue
		}
		mu.Lock()
		files = append(files, path)
		mu.Unlock()
		n := len(files) - 1
		var lines []string
		w := func(f string, a ...any) { lines = append(lines, fmt.Sprintf(f, a...)) }
		w("F\t%s\t%s\n", mode, esc(path))
		switch mode {
		case "html":
			d, err := client.Fetch(ctx, fmt.Sprintf("%s/f/%d/page.html", srv.URL, n))
			if err != nil {
				w("E\t%s\n", esc(err.Error()))
				break
			}
			w("D\t%s\t%s\t%d\t%d\n", esc(d.URL), esc(d.Title), b01(d.Truncated), len(d.Image))
			blocks(w, "doc", d.Blocks)
		case "raw":
			b, err := os.ReadFile(path)
			if err != nil {
				w("E\t讀不到\n")
				break
			}
			title, bs := web.ParseHTML(nil, strings.NewReader(string(b)))
			w("T\t%s\n", esc(title))
			blocks(w, "doc", bs)
		case "gopher":
			b, err := os.ReadFile(path)
			if err != nil {
				w("E\t讀不到\n")
				break
			}
			items := gopher.ParseMenu(b)
			for i, it := range items {
				web, wok := it.WebURL()
				w("I\t%d\t%d\t%s\t%s\t%s\t%s\t%d\t%d\t%s\t%s\n", i, it.Type, esc(it.Display), esc(it.Sel),
					esc(it.Host), esc(it.Port), b01(it.IsLink()), b01(wok), esc(web), esc(it.URL().String()))
			}
			blocks(w, "menu", gopher.MenuBlocks(items))
			blocks(w, "text", gopher.TextBlocks(b))
		case "epub":
			bk, err := epub.Open(path)
			if err != nil {
				msg := err.Error()
				if strings.HasPrefix(msg, "打不開:") {
					msg = "打不開"
				}
				w("E\t%s\n", esc(msg))
				break
			}
			w("K\t%s\t%s\t%d\n", esc(bk.Title), esc(bk.Author), len(bk.Chapters))
			for i, c := range bk.Chapters {
				w("C\t%d\t%s\t%s\n", i, esc(c.Title), esc(c.Href))
			}
			for i := range bk.Chapters {
				w("X\t%d\n", i)
				bs, err := bk.Blocks(i)
				if err != nil {
					w("E\t%s\n", esc(chapterErr(err)))
					continue
				}
				blocks(w, "doc", bs)
				for _, b := range bs {
					if b.Kind != markdown.Image {
						continue
					}
					if data, err := bk.Image(b.Src); err != nil {
						w("M\t%s\t%s\n", esc(b.Src), esc(chapterErr(err)))
					} else {
						w("M\t%s\t%d\n", esc(b.Src), len(data))
					}
				}
			}
			bk.Close()
		default:
			w("E\t不認得的模式\n")
		}
		for _, ln := range lines {
			out.WriteString(strings.ReplaceAll(ln, host, "SRV"))
		}
	}
}
