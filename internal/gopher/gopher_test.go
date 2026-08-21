package gopher_test

import (
	"context"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/wicanr2/wincv-remake/internal/gopher"
	"github.com/wicanr2/wincv-remake/internal/markdown"
)

func TestParseURL(t *testing.T) {
	cases := []struct {
		in                     string
		host, port, sel, query string
		typ                    byte
	}{
		{"gopher://example.org", "example.org", "", "", "", 0},
		{"gopher://example.org/", "example.org", "", "", "", 0},
		{"example.org", "example.org", "", "", "", 0},
		{"gopher://example.org:7070/", "example.org", "7070", "", "", 0},
		{"gopher://example.org/1/dir", "example.org", "", "/dir", "", '1'},
		{"gopher://example.org/0/a.txt", "example.org", "", "/a.txt", "", '0'},
		{"example.org/1/dir", "example.org", "", "/dir", "", '1'},
		{"gopher://example.org/7/find?貓", "example.org", "", "/find", "貓", '7'},
		{"gopher://example.org/7/find\t貓", "example.org", "", "/find", "貓", '7'},
		{"gopher://[::1]:70/1/", "[::1]", "70", "/", "", '1'},
	}
	for _, c := range cases {
		t.Run(c.in, func(t *testing.T) {
			u, err := gopher.ParseURL(c.in)
			if err != nil {
				t.Fatalf("解不開: %v", err)
			}
			if u.Host != c.host || u.Port != c.port || u.Sel != c.sel ||
				u.Search != c.query || u.Type != c.typ {
				t.Fatalf("得到 host=%q port=%q type=%q sel=%q search=%q",
					u.Host, u.Port, string(u.Type), u.Sel, u.Search)
			}
		})
	}
}

func TestParseURLRejects(t *testing.T) {
	for _, in := range []string{"", "   ", "http://example.org/", "gopher:///1/x"} {
		if u, err := gopher.ParseURL(in); err == nil {
			t.Errorf("%q 應該報錯,卻解成 %+v", in, u)
		}
	}
}

// 型別沒寫的時候要當成選單 —— 那是 gopher 的進入點。
func TestURLStringDefaultsToMenu(t *testing.T) {
	u, _ := gopher.ParseURL("gopher://example.org")
	if got := u.String(); got != "gopher://example.org/1" {
		t.Fatalf("得到 %q", got)
	}
}

// 預設埠不寫進字串,非預設埠要寫。
func TestURLStringPort(t *testing.T) {
	u, _ := gopher.ParseURL("gopher://example.org:70/1/x")
	if got := u.String(); got != "gopher://example.org/1/x" {
		t.Fatalf("預設埠不該出現: %q", got)
	}
	u, _ = gopher.ParseURL("gopher://example.org:7070/1/x")
	if got := u.String(); got != "gopher://example.org:7070/1/x" {
		t.Fatalf("非預設埠要出現: %q", got)
	}
}

const sampleMenu = "iWelcome to the test server\t\terror.host\t1\r\n" +
	"i\t\terror.host\t1\r\n" +
	"1Documents\t/docs\texample.org\t70\r\n" +
	"0Read me\t/readme.txt\texample.org\t70\r\n" +
	"Ia picture\t/pic.png\texample.org\t70\r\n" +
	"hProject page\tURL:https://example.org/\texample.org\t70\r\n" +
	"i沒有欄位的說明列\r\n" +
	".\r\n"

func TestParseMenu(t *testing.T) {
	items := gopher.ParseMenu([]byte(sampleMenu))
	if len(items) != 7 {
		t.Fatalf("解出 %d 列,期望 7", len(items))
	}
	if items[2].Type != '1' || items[2].Display != "Documents" ||
		items[2].Sel != "/docs" || items[2].Host != "example.org" || items[2].Port != "70" {
		t.Fatalf("第 3 列不對: %+v", items[2])
	}
	if !items[2].IsLink() {
		t.Error("目錄應該是連結")
	}
	if items[0].IsLink() {
		t.Error("資訊列不該是連結")
	}
	// error.host 是排版用的假主機,不是真的目標。
	if items[1].IsLink() {
		t.Error("error.host 的列不該是連結")
	}
	if web, ok := items[5].WebURL(); !ok || web != "https://example.org/" {
		t.Errorf("網頁連結解錯: %q %v", web, ok)
	}
}

// [雷] 欄位不足的列不能丟掉 —— 很多站台的說明列只寫 "i文字" 就換行。
// 丟掉的話整段說明消失,而畫面上看起來只是「這個站沒寫什麼」。
func TestParseMenuKeepsShortLines(t *testing.T) {
	items := gopher.ParseMenu([]byte(sampleMenu))
	last := items[len(items)-1]
	if last.Display != "沒有欄位的說明列" {
		t.Fatalf("欄位不足的列被丟掉了,最後一列是 %+v", last)
	}
}

// "." 之後的東西不是內容。
func TestParseMenuStopsAtTerminator(t *testing.T) {
	items := gopher.ParseMenu([]byte("iA\t\t\t\r\n.\r\niB\t\t\t\r\n"))
	if len(items) != 1 {
		t.Fatalf("解出 %d 列,期望 1", len(items))
	}
}

func TestMenuBlocks(t *testing.T) {
	blocks := gopher.MenuBlocks(gopher.ParseMenu([]byte(sampleMenu)))
	if len(blocks) != 7 {
		t.Fatalf("%d 個區塊,期望 7", len(blocks))
	}
	// 全部都是 List —— 排版引擎在連續的 List 之間不插空行,選單要單倍行距。
	for i, b := range blocks {
		if b.Kind != markdown.List {
			t.Fatalf("第 %d 個區塊是 %v,不是 List", i, b.Kind)
		}
	}
	if got := blocks[2].Marker; got != "[目錄] " {
		t.Errorf("目錄的標籤是 %q", got)
	}
	if sp := blocks[2].Spans[0]; sp.Style&markdown.Link == 0 ||
		sp.Href != "gopher://example.org/1/docs" {
		t.Errorf("連結不對: %+v", sp)
	}
	// 資訊列不給 Href,也不給型別標籤 —— 不該長得像可以點的東西。
	if sp := blocks[0].Spans[0]; sp.Href != "" || sp.Style&markdown.Link != 0 {
		t.Errorf("資訊列不該是連結: %+v", sp)
	}
}

// 排版之後 Href 要還在,不然畫得出底線卻沒有東西可以點。
func TestLinkSurvivesLayout(t *testing.T) {
	blocks := gopher.MenuBlocks(gopher.ParseMenu([]byte(sampleMenu)))
	lines, _ := markdown.Layout(blocks, 60, 8, 16, nil, markdown.DefaultTheme())
	var hrefs []string
	for _, ln := range lines {
		for _, p := range ln.Pieces {
			if p.Href != "" {
				hrefs = append(hrefs, p.Href)
			}
		}
	}
	if len(hrefs) == 0 {
		t.Fatal("排版之後一個連結目標都沒有")
	}
	if hrefs[0] != "gopher://example.org/1/docs" {
		t.Fatalf("第一個連結是 %q", hrefs[0])
	}
}

// gopher 的文字內容常常是排好版的,不能重新斷行。
func TestTextBlocksIsPreformatted(t *testing.T) {
	raw := []byte("  第一列\r\n     縮排的第二列\r\n.\r\n")
	blocks := gopher.TextBlocks(raw)
	if len(blocks) != 1 || blocks[0].Kind != markdown.Pre {
		t.Fatalf("得到 %+v", blocks)
	}
	if len(blocks[0].Lines) != 2 {
		t.Fatalf("%d 列,期望 2(結束符不算內容)", len(blocks[0].Lines))
	}
	if blocks[0].Lines[1] != "     縮排的第二列" {
		t.Fatalf("縮排被吃掉了: %q", blocks[0].Lines[1])
	}
}

// Big5 的站台要判讀得出來 —— gopher 沒有 charset 欄位。
func TestTextBlocksDetectsBig5(t *testing.T) {
	big5 := []byte{0xa4, 0xe5, 0xa6, 0x72, 0xc0, 0xc9} // 「文字檔」
	blocks := gopher.TextBlocks(big5)
	if len(blocks) != 1 {
		t.Fatalf("得到 %d 個區塊", len(blocks))
	}
	if got := blocks[0].Lines[0]; got != "文字檔" {
		t.Fatalf("解出 %q", got)
	}
}

// serve 起一個只回固定內容的 gopher 伺服器,回傳位址與收到的 selector。
func serve(t *testing.T, reply string) (net.Addr, <-chan string) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	got := make(chan string, 1)
	go func() {
		c, err := ln.Accept()
		if err != nil {
			return
		}
		defer c.Close()
		buf := make([]byte, 512)
		n, _ := c.Read(buf)
		got <- strings.TrimRight(string(buf[:n]), "\r\n")
		_, _ = c.Write([]byte(reply))
	}()
	return ln.Addr(), got
}

func TestFetch(t *testing.T) {
	addr, got := serve(t, sampleMenu)
	host, port, _ := net.SplitHostPort(addr.String())

	c := &gopher.Client{Timeout: 5 * time.Second}
	u := gopher.URL{Host: host, Port: port, Type: '1', Sel: "/docs"}
	data, err := c.Fetch(context.Background(), u)
	if err != nil {
		t.Fatalf("取不到: %v", err)
	}
	if sel := <-got; sel != "/docs" {
		t.Fatalf("伺服器收到的 selector 是 %q", sel)
	}
	if len(gopher.ParseMenu(data)) != 7 {
		t.Fatal("回來的內容解不出選單")
	}
}

// 型別 7 的查詢字串接在 selector 後面,中間一個 tab。
func TestFetchSearchSendsTab(t *testing.T) {
	addr, got := serve(t, ".\r\n")
	host, port, _ := net.SplitHostPort(addr.String())

	c := &gopher.Client{Timeout: 5 * time.Second}
	u := gopher.URL{Host: host, Port: port, Type: '7', Sel: "/find", Search: "貓"}
	if _, err := c.Fetch(context.Background(), u); err != nil {
		t.Fatal(err)
	}
	if sel := <-got; sel != "/find\t貓" {
		t.Fatalf("送出的是 %q,期望 \"/find\\t貓\"", sel)
	}
}

// 沒有 Content-Length,伺服器可以一直送。上限要真的截斷並說出來。
func TestFetchTruncates(t *testing.T) {
	addr, _ := serve(t, strings.Repeat("x", 5000))
	host, port, _ := net.SplitHostPort(addr.String())

	c := &gopher.Client{Timeout: 5 * time.Second, MaxBytes: 100}
	data, err := c.Fetch(context.Background(), gopher.URL{Host: host, Port: port})
	if err != gopher.ErrTooLarge {
		t.Fatalf("錯誤是 %v,期望 ErrTooLarge", err)
	}
	if len(data) != 100 {
		t.Fatalf("截到 %d 個位元組,期望 100", len(data))
	}
}

func TestFetchNeedsHost(t *testing.T) {
	c := &gopher.Client{}
	if _, err := c.Fetch(context.Background(), gopher.URL{}); err == nil {
		t.Fatal("沒有主機名應該報錯")
	}
}
