package syntax

import (
	"strings"
	"testing"

	"github.com/wicanr2/wincv-remake/internal/cell"
)

func cfg(t *testing.T, body string) *Config {
	t.Helper()
	return ParseConfig("test", []byte(body))
}

const cBody = `QuoteColor      ltyellow
NumberColor     ltmagenta
LineCommentStart //
========= Ansi C keywords
break      ltred
int        ltorange
return     ltred
`

func colorsOf(c *Config, line string) []string {
	toks := c.Highlight(line)
	r := []rune(line)
	var out []string
	for _, t := range toks {
		name := "-"
		if t.Colored {
			name = cell.Names[t.Color]
		}
		out = append(out, string(r[t.Start:t.End])+":"+name)
	}
	return out
}

func TestParseHeader(t *testing.T) {
	c := cfg(t, cBody)
	if !c.HasQuote || cell.Names[c.QuoteColor] != "ltyellow" {
		t.Errorf("QuoteColor = %v", c.QuoteColor)
	}
	if !c.HasNumber || cell.Names[c.NumberColor] != "ltmagenta" {
		t.Errorf("NumberColor = %v", c.NumberColor)
	}
	if c.LineComment != "//" {
		t.Errorf("LineCommentStart = %q", c.LineComment)
	}
	if len(c.Keywords) != 3 {
		t.Errorf("關鍵字 %d 個: %v", len(c.Keywords), c.Keywords)
	}
}

func TestHighlightKeywordAndNumber(t *testing.T) {
	c := cfg(t, cBody)
	got := strings.Join(colorsOf(c, "int x = 42;"), " ")
	if !strings.Contains(got, "int:ltorange") {
		t.Errorf("關鍵字沒上色: %s", got)
	}
	if !strings.Contains(got, "42:ltmagenta") {
		t.Errorf("數字沒上色: %s", got)
	}
}

// 註解裡的關鍵字不該被上色 —— 判斷順序錯了就會。
func TestCommentSwallowsKeywords(t *testing.T) {
	c := cfg(t, cBody)
	toks := c.Highlight("x = 1; // return break")
	last := toks[len(toks)-1]
	if !last.Colored || cell.Names[last.Color] != "gray" {
		t.Fatalf("註解沒被當成一段: %+v", last)
	}
	r := []rune("x = 1; // return break")
	if string(r[last.Start:]) != "// return break" {
		t.Errorf("註解範圍 = %q", string(r[last.Start:]))
	}
}

// 字串裡的關鍵字與符號也不該被上色。
func TestQuoteSwallowsContent(t *testing.T) {
	c := cfg(t, cBody)
	got := strings.Join(colorsOf(c, `s = "return 42";`), " ")
	if strings.Contains(got, "return:ltred") {
		t.Errorf("字串裡的關鍵字被上色了: %s", got)
	}
	if !strings.Contains(got, `"return 42":ltyellow`) {
		t.Errorf("字串沒被當成一段: %s", got)
	}
}

// OnlyBeginLineComment=true 時,行中的註解符號不算註解。
func TestOnlyBeginLineComment(t *testing.T) {
	c := cfg(t, `LineCommentStart ;
OnlyBeginLineComment true
=========
`)
	toks := c.Highlight("key = value ; 這不是註解")
	for _, tk := range toks {
		if tk.Colored && cell.Names[tk.Color] == "gray" {
			t.Error("行中的 ; 不該被當成註解")
		}
	}
	toks = c.Highlight("  ; 這才是註解")
	found := false
	for _, tk := range toks {
		if tk.Colored && cell.Names[tk.Color] == "gray" {
			found = true
		}
	}
	if !found {
		t.Error("行首(允許前置空白)的 ; 應該是註解")
	}
}

// 用原版真實的設定檔跑一遍。
func TestRealConfigSet(t *testing.T) {
	s, err := LoadSet("../../original/app")
	if err != nil {
		t.Skipf("找不到原版設定: %v", err)
	}
	// keyword.cfg 用 "." 讓 .c .h .rc 共用 keyword_c.cfg
	for _, ext := range []string{"c", "h", "cpp", "rc", "java", "xml", "ini", "iss", "txt", "bat"} {
		if s.For("x."+ext) == nil {
			t.Errorf(".%s 沒有對應的設定", ext)
		}
	}
	cc := s.For("main.c")
	if cc == nil {
		t.Fatal("找不到 C 的設定")
	}
	if len(cc.Keywords) < 100 {
		t.Errorf("C 的關鍵字只有 %d 個", len(cc.Keywords))
	}
	// keyword_c.cfg 裡明列的幾個
	for _, w := range []string{"break", "auto", "return"} {
		if _, ok := cc.Keywords[w]; !ok {
			t.Errorf("C 的關鍵字表裡沒有 %q", w)
		}
	}
	// .h 與 .c 應該是同一份設定物件
	if s.For("a.h") != cc {
		t.Error(".h 應該沿用 .c 的設定(keyword.cfg 用 \".\" 表示)")
	}
}

// 跨行註解要跨得過行:第一行開了 /* 沒關,第二行整行都還是註解。
func TestBlockCommentSpansLines(t *testing.T) {
	c := cfg(t, cBody)
	if c.BlockStart != "/*" || c.BlockEnd != "*/" {
		t.Fatalf("C 家族應預設 /* */,得到 %q %q", c.BlockStart, c.BlockEnd)
	}
	toks, in := c.HighlightState("int a; /* 開始", false)
	if !in {
		t.Error("行末還在註解裡,應回 true")
	}
	last := toks[len(toks)-1]
	if !last.Colored || cell.Names[last.Color] != "gray" {
		t.Errorf("行尾應是註解色: %+v", last)
	}

	toks, in = c.HighlightState("return break 都在註解裡", true)
	if !in {
		t.Error("這一行沒有結束標記,應該還在註解裡")
	}
	for _, tk := range toks {
		if tk.Colored && cell.Names[tk.Color] != "gray" {
			t.Errorf("註解裡不該有其他顏色: %+v", tk)
		}
	}

	toks, in = c.HighlightState("結束 */ int x;", true)
	if in {
		t.Error("看到 */ 之後就不該還在註解裡")
	}
	got := strings.Join(colorsOf2(c, "結束 */ int x;", true), " ")
	if !strings.Contains(got, "int:ltorange") {
		t.Errorf("註解結束後的關鍵字要正常上色: %s", got)
	}
}

func colorsOf2(c *Config, line string, in bool) []string {
	toks, _ := c.HighlightState(line, in)
	r := []rune(line)
	var out []string
	for _, t := range toks {
		name := "-"
		if t.Colored {
			name = cell.Names[t.Color]
		}
		out = append(out, string(r[t.Start:t.End])+":"+name)
	}
	return out
}

// 單行內開又關的註解,後面的程式碼要正常上色。
func TestBlockCommentInline(t *testing.T) {
	c := cfg(t, cBody)
	got := strings.Join(colorsOf2(c, "int a; /* x */ return b;", false), " ")
	if !strings.Contains(got, "return:ltred") {
		t.Errorf("註解關掉之後的關鍵字沒上色: %s", got)
	}
}
