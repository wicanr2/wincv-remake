package web_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/wicanr2/wincv-remake/internal/markdown"
	"github.com/wicanr2/wincv-remake/internal/web"
)

// blocks 把區塊攤成「種類:文字」的字串陣列,好比對。
func blocks(bs []markdown.Block) []string {
	out := make([]string, 0, len(bs))
	for _, b := range bs {
		var txt string
		switch b.Kind {
		case markdown.Pre:
			txt = strings.Join(b.Lines, "\\n")
		case markdown.Image:
			txt = b.Src + " [" + b.Alt + "]"
		default:
			var sb strings.Builder
			for _, sp := range b.Spans {
				sb.WriteString(sp.Text)
			}
			txt = sb.String()
		}
		out = append(out, kindName(b.Kind)+":"+txt)
	}
	return out
}

func kindName(k markdown.Kind) string {
	switch k {
	case markdown.Para:
		return "P"
	case markdown.Heading:
		return "H"
	case markdown.List:
		return "LI"
	case markdown.Quote:
		return "Q"
	case markdown.Pre:
		return "PRE"
	case markdown.Rule:
		return "HR"
	case markdown.Image:
		return "IMG"
	}
	return "?"
}

func parse(t *testing.T, base, src string) (string, []markdown.Block) {
	t.Helper()
	var u *url.URL
	if base != "" {
		var err error
		if u, err = url.Parse(base); err != nil {
			t.Fatal(err)
		}
	}
	return web.ParseHTML(u, strings.NewReader(src))
}

func TestParseHTMLBasics(t *testing.T) {
	title, bs := parse(t, "", `
<html><head><title>測試頁</title></head>
<body>
  <h1>標題</h1>
  <p>第一段。<b>粗</b>的字。</p>
  <ul><li>甲</li><li>乙</li></ul>
  <hr>
  <blockquote>引一段話</blockquote>
</body></html>`)

	if title != "測試頁" {
		t.Fatalf("標題是 %q", title)
	}
	want := []string{"H:標題", "P:第一段。粗的字。", "LI:甲", "LI:乙", "HR:", "Q:引一段話"}
	got := blocks(bs)
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("解出來是\n  %v\n想要\n  %v", got, want)
	}
	if bs[0].Level != 1 {
		t.Errorf("h1 的階層是 %d", bs[0].Level)
	}
}

// script 與 style 的內容不是給人看的,整段都不能留。
func TestParseHTMLDropsScriptAndStyle(t *testing.T) {
	_, bs := parse(t, "", `<body>
	<style>body{color:red}</style>
	<script>var x = "不該出現";</script>
	<p>只有這一段</p>
	<noscript>也不該出現</noscript>
	</body>`)
	got := strings.Join(blocks(bs), "|")
	if strings.Contains(got, "不該出現") || strings.Contains(got, "color") ||
		strings.Contains(got, "也不該出現") {
		t.Fatalf("腳本或樣式漏進來了:%s", got)
	}
	if got != "P:只有這一段" {
		t.Fatalf("得到 %q", got)
	}
}

// 連結要接成絕對位址 —— 相對位址在下一頁的基準變了之後就失效。
func TestParseHTMLResolvesLinks(t *testing.T) {
	_, bs := parse(t, "https://example.org/a/b.html", `<body>
	<p><a href="c.html">同層</a> <a href="/top">根</a>
	<a href="https://other.org/x">外站</a></p></body>`)
	var hrefs []string
	for _, b := range bs {
		for _, sp := range b.Spans {
			if sp.Href != "" {
				hrefs = append(hrefs, sp.Href)
			}
		}
	}
	want := []string{
		"https://example.org/a/c.html",
		"https://example.org/top",
		"https://other.org/x",
	}
	if strings.Join(hrefs, " ") != strings.Join(want, " ") {
		t.Fatalf("接出來是 %v", hrefs)
	}
}

// 圖片自己成一段,位址也要絕對化。
func TestParseHTMLImages(t *testing.T) {
	_, bs := parse(t, "https://example.org/a/", `<body>
	<p>圖在下面</p><img src="pic.png" alt="一張圖"><p>圖在上面</p></body>`)
	got := blocks(bs)
	want := []string{"P:圖在下面", "IMG:https://example.org/a/pic.png [一張圖]", "P:圖在上面"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("得到 %v", got)
	}
}

// <pre> 裡的空白與換行是內容,不能摺疊。
func TestParseHTMLPreKeepsWhitespace(t *testing.T) {
	_, bs := parse(t, "", "<body><pre>  第一列\n     第二列</pre></body>")
	if len(bs) != 1 || bs[0].Kind != markdown.Pre {
		t.Fatalf("得到 %v", blocks(bs))
	}
	if bs[0].Lines[1] != "     第二列" {
		t.Fatalf("縮排被吃掉了:%q", bs[0].Lines[1])
	}
}

// [雷] HTML 原始碼裡到處是為了可讀性換的行。摺疊沒做好的話,
// 每一個縮排都會變成內容,整頁看起來像被拉開一樣。
func TestParseHTMLCollapsesWhitespace(t *testing.T) {
	_, bs := parse(t, "", "<body><p>一段\n    很長的\n    文字</p></body>")
	got := blocks(bs)
	if len(got) != 1 || got[0] != "P:一段 很長的 文字" {
		t.Fatalf("得到 %v", got)
	}
}

// <br> 換行但不換段。
func TestParseHTMLBreakStaysInParagraph(t *testing.T) {
	_, bs := parse(t, "", "<body><p>上<br>下</p></body>")
	if len(bs) != 1 {
		t.Fatalf("拆成 %d 段:%v", len(bs), blocks(bs))
	}
}

// 巢狀清單的縮排層數要對。
func TestParseHTMLNestedList(t *testing.T) {
	_, bs := parse(t, "", `<body><ul><li>外</li><ul><li>內</li></ul></ul></body>`)
	var levels []int
	for _, b := range bs {
		if b.Kind == markdown.List {
			levels = append(levels, b.Level)
		}
	}
	if len(levels) != 2 || levels[0] != 1 || levels[1] != 2 {
		t.Fatalf("層數是 %v", levels)
	}
}

// 標籤沒關的頁面不能讓整份解析爛掉 —— 網路上這種頁面很多。
func TestParseHTMLSurvivesBrokenMarkup(t *testing.T) {
	_, bs := parse(t, "", "<body><p>一<b>二<p>三</body>")
	got := strings.Join(blocks(bs), "|")
	for _, want := range []string{"一", "二", "三"} {
		if !strings.Contains(got, want) {
			t.Fatalf("%q 不見了:%s", want, got)
		}
	}
}

// --- Fetch ---------------------------------------------------------------

func serve(t *testing.T, h http.HandlerFunc) string {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	return srv.URL
}

func TestFetchHTML(t *testing.T) {
	base := serve(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write([]byte(`<title>首頁</title><h1>你好</h1><p><a href="/next">下一頁</a></p>`))
	})
	c := &web.Client{Timeout: 5 * time.Second}
	d, err := c.Fetch(context.Background(), base)
	if err != nil {
		t.Fatal(err)
	}
	if d.Title != "首頁" {
		t.Errorf("標題是 %q", d.Title)
	}
	got := strings.Join(blocks(d.Blocks), "|")
	if !strings.Contains(got, "你好") || !strings.Contains(got, "下一頁") {
		t.Fatalf("內容是 %q", got)
	}
	// 連結要以最終位址為基準接成絕對位址。
	var href string
	for _, b := range d.Blocks {
		for _, sp := range b.Spans {
			if sp.Href != "" {
				href = sp.Href
			}
		}
	}
	if href != base+"/next" {
		t.Fatalf("連結是 %q,想要 %q", href, base+"/next")
	}
}

// Big5 的站台要解得出來,而且宣告在 header 或 meta 都算。
func TestFetchBig5(t *testing.T) {
	big5 := []byte{0xa4, 0xe5, 0xa6, 0x72} // 「文字」
	for _, tc := range []struct{ name, ctype, meta string }{
		{"header 宣告", "text/html; charset=big5", ""},
		{"meta 宣告", "text/html", `<meta charset="Big5">`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			base := serve(t, func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", tc.ctype)
				w.Write([]byte(tc.meta + "<p>"))
				w.Write(big5)
				w.Write([]byte("</p>"))
			})
			c := &web.Client{Timeout: 5 * time.Second}
			d, err := c.Fetch(context.Background(), base)
			if err != nil {
				t.Fatal(err)
			}
			if got := strings.Join(blocks(d.Blocks), "|"); !strings.Contains(got, "文字") {
				t.Fatalf("解出來是 %q", got)
			}
		})
	}
}

// 圖片位址直接交出位元組,不解析成區塊。
func TestFetchImage(t *testing.T) {
	png := []byte("\x89PNG\r\n\x1a\n擺著當內容")
	base := serve(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		w.Write(png)
	})
	c := &web.Client{Timeout: 5 * time.Second}
	d, err := c.Fetch(context.Background(), base+"/a.png")
	if err != nil {
		t.Fatal(err)
	}
	if string(d.Image) != string(png) {
		t.Fatalf("拿到 %d 個位元組", len(d.Image))
	}
	if len(d.Blocks) != 0 {
		t.Error("圖片不該解析成區塊")
	}
}

// 超過上限時內容要截斷,而且**要說出來** —— 靜靜地少一半最難發現。
func TestFetchTruncates(t *testing.T) {
	base := serve(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.Write([]byte(strings.Repeat("x", 5000)))
	})
	c := &web.Client{Timeout: 5 * time.Second, MaxBytes: 100}
	d, err := c.Fetch(context.Background(), base)
	if err != nil {
		t.Fatal(err)
	}
	if !d.Truncated {
		t.Fatal("截斷了卻沒有說")
	}
	if n := len(d.Blocks[0].Lines[0]); n != 100 {
		t.Fatalf("留下 %d 個字元,想要 100", n)
	}
}

// 重導向之後,相對連結要以**最終**位址為基準。
func TestFetchFollowsRedirect(t *testing.T) {
	var base string
	base = serve(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/old" {
			http.Redirect(w, r, "/new/here", http.StatusFound)
			return
		}
		w.Header().Set("Content-Type", "text/html")
		w.Write([]byte(`<a href="x.html">相對</a>`))
	})
	c := &web.Client{Timeout: 5 * time.Second}
	d, err := c.Fetch(context.Background(), base+"/old")
	if err != nil {
		t.Fatal(err)
	}
	if d.URL != base+"/new/here" {
		t.Fatalf("最終位址是 %q", d.URL)
	}
	var href string
	for _, b := range d.Blocks {
		for _, sp := range b.Spans {
			if sp.Href != "" {
				href = sp.Href
			}
		}
	}
	if href != base+"/new/x.html" {
		t.Fatalf("相對連結接成 %q,想要 %q", href, base+"/new/x.html")
	}
}

func TestFetchRejectsNonHTTP(t *testing.T) {
	c := &web.Client{}
	if _, err := c.Fetch(context.Background(), "gopher://example.org/"); err == nil {
		t.Fatal("gopher 位址不該由這一包處理")
	}
}

func TestFetchReportsStatus(t *testing.T) {
	base := serve(t, func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "沒有這一頁", http.StatusNotFound)
	})
	c := &web.Client{Timeout: 5 * time.Second}
	_, err := c.Fetch(context.Background(), base)
	if err == nil || !strings.Contains(err.Error(), "404") {
		t.Fatalf("錯誤是 %v", err)
	}
}

func TestIsHTTP(t *testing.T) {
	for _, s := range []string{"http://a", "HTTPS://a", " https://a "} {
		if !web.IsHTTP(s) {
			t.Errorf("%q 應該算 http", s)
		}
	}
	for _, s := range []string{"gopher://a", "a.org", "ftp://a"} {
		if web.IsHTTP(s) {
			t.Errorf("%q 不該算 http", s)
		}
	}
}
