package rtf

import (
	"strconv"
	"strings"
	"testing"

	"github.com/wicanr2/wincv-remake/internal/markdown"
)

func parse(t *testing.T, src string) *Doc {
	t.Helper()
	d, err := Parse([]byte(src))
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func text(bs []markdown.Block) string {
	var sb strings.Builder
	for _, b := range bs {
		for _, s := range b.Spans {
			sb.WriteString(s.Text)
		}
		for _, r := range b.Rows {
			sb.WriteString(strings.Join(r, "|"))
		}
		sb.WriteString("\n")
	}
	return sb.String()
}

func TestParagraphsAndStyles(t *testing.T) {
	d := parse(t, `{\rtf1\ansi one \b two\b0  three\par four\par}`)
	bs := d.Blocks()
	if len(bs) != 2 {
		t.Fatalf("要 2 段,拿到 %d:%q", len(bs), text(bs))
	}
	var bold string
	for _, s := range bs[0].Spans {
		if s.Style&markdown.Bold != 0 {
			bold += s.Text
		}
	}
	if bold != "two" {
		t.Errorf("粗體的部分是 %q", bold)
	}
}

// 群組是 RTF 唯一的作用域機制:離開群組,裡面的格式設定就沒了。
func TestGroupScoping(t *testing.T) {
	d := parse(t, `{\rtf1\ansi {\b inside} outside\par}`)
	bs := d.Blocks()
	for _, s := range bs[0].Spans {
		if strings.Contains(s.Text, "outside") && s.Style&markdown.Bold != 0 {
			t.Errorf("群組外面不該還是粗體:%+v", s)
		}
	}
}

// 一份繁體中文 RTF 只在字型表裡寫 fcharset136,正文全是 \'hh。
func TestBig5ViaFontCharset(t *testing.T) {
	d := parse(t, `{\rtf1\ansi\ansicpg1252{\fonttbl{\f0\fnil\fcharset0 Arial;}{\f1\fnil\fcharset136 PMingLiU;}}`+
		`\f1\'a4\'a4\'a4\'e5\f0 abc\par}`)
	got := text(d.Blocks())
	if !strings.Contains(got, "中文") {
		t.Fatalf("Big5 沒有解出來:%q", got)
	}
	if !strings.Contains(got, "abc") {
		t.Errorf("換回西文字型之後的內容不見了:%q", got)
	}
}

// uw 組出一個 \uN 控制字。
//
// 用組的而不是直接寫在原始碼裡:反斜線接 u 再接四個十六進位數字這個形狀,
// 在通往檔案的路上會被跳脫規則搶走,寫進去的就不是原本要寫的東西。
func uw(n int) string { return "\\u" + strconv.Itoa(n) + " " }

func TestUnicodeEscapeAndSkip(t *testing.T) {
	// \uc1 表示每個 \u 後面有一個替代字元要跳過。
	d := parse(t, `{\rtf1\ansi\uc1 `+uw(20013)+`?`+uw(25991)+`?!\par}`)
	if got := strings.TrimSpace(text(d.Blocks())); got != "中文!" {
		t.Errorf("拿到 %q", got)
	}
}

// 大於 32767 的字碼在 RTF 裡是用負數寫的。
func TestUnicodeNegativeAndSurrogate(t *testing.T) {
	d := parse(t, `{\rtf1\ansi\uc0 `+uw(-10179)+uw(-8704)+`\par}`)
	got := strings.TrimSpace(text(d.Blocks()))
	if got != "\U0001F600" {
		t.Errorf("代理對沒有組回來:%q(% x)", got, got)
	}
}

func TestTable(t *testing.T) {
	d := parse(t, `{\rtf1\ansi\ansicpg65001\trowd\intbl 甲\cell 乙\cell\row`+
		`\trowd\intbl 丙\cell 丁\cell\row\pard 之後\par}`)
	bs := d.Blocks()
	if bs[0].Kind != markdown.Table {
		t.Fatalf("第一個區塊不是表格:%q", text(bs))
	}
	if len(bs[0].Rows) != 2 || bs[0].Rows[1][1] != "丁" {
		t.Errorf("表格內容不對:%v", bs[0].Rows)
	}
	if bs[1].Kind != markdown.Para {
		t.Errorf("表格後面應該接一般段落,拿到 %v", bs[1].Kind)
	}
}

func TestHyperlinkField(t *testing.T) {
	d := parse(t, `{\rtf1\ansi\ansicpg65001 {\field{\*\fldinst HYPERLINK "https://a.example/" }{\fldrslt 按這裡}}後面\par}`)
	bs := d.Blocks()
	var link, plain string
	for _, s := range bs[0].Spans {
		if s.Style&markdown.Link != 0 {
			link += s.Text
			if s.Href != "https://a.example/" {
				t.Errorf("網址是 %q", s.Href)
			}
		} else {
			plain += s.Text
		}
	}
	if link != "按這裡" || plain != "後面" {
		t.Errorf("連結 %q,一般 %q", link, plain)
	}
}

// 清單的符號在 listtext 群組裡,那是給不懂清單表的讀取器看的備援。
func TestListTextBecomesMarker(t *testing.T) {
	d := parse(t, `{\rtf1\ansi\ansicpg65001\ls1\ilvl0{\listtext 1.\tab}第一項\par\ls1\ilvl1{\listtext 2.\tab}第二項\par}`)
	bs := d.Blocks()
	if bs[0].Kind != markdown.List || bs[0].Marker != "1." {
		t.Errorf("第一項:kind=%v marker=%q", bs[0].Kind, bs[0].Marker)
	}
	if bs[1].Level != 2 {
		t.Errorf("第二項的層級是 %d", bs[1].Level)
	}
	if got := text(bs); strings.Contains(got, "1.第一項") {
		t.Errorf("項目符號不該混進內文:%q", got)
	}
}

func TestSkippedDestinations(t *testing.T) {
	d := parse(t, `{\rtf1\ansi\ansicpg65001{\colortbl;\red255\green0\blue0;}{\info{\title 標題}}`+
		`{\*\generator Word;}{\*\bkmkstart X}正文\par}`)
	got := strings.TrimSpace(text(d.Blocks()))
	if got != "正文" {
		t.Errorf("該丟掉的東西跑進正文了:%q", got)
	}
}

func TestPictureAndDedup(t *testing.T) {
	png := "89504e470d0a1a0a0000"
	d := parse(t, `{\rtf1\ansi{\*\shppict{\pict\pngblip `+png+`}}{\*\nonshppict{\pict\wmetafile8 0102}}\par}`)
	var imgs []markdown.Block
	for _, b := range d.Blocks() {
		if b.Kind == markdown.Image {
			imgs = append(imgs, b)
		}
	}
	if len(imgs) != 1 {
		t.Fatalf("圖片應該只有一張,拿到 %d:%q", len(imgs), text(d.Blocks()))
	}
	got, err := d.Image(imgs[0].Src)
	if err != nil {
		t.Fatal(err)
	}
	if string(got[:4]) != "\x89PNG" {
		t.Errorf("解出來的不是 PNG:% x", got[:4])
	}
}

func TestFootnote(t *testing.T) {
	d := parse(t, `{\rtf1\ansi\ansicpg65001 正文{\*\footnote \chftn 註腳內容}後面\par}`)
	got := text(d.Blocks())
	if !strings.Contains(got, "[註 1]") {
		t.Errorf("少了註腳標記:%q", got)
	}
	if !strings.Contains(got, "註腳內容") {
		t.Errorf("少了註腳內容:%q", got)
	}
	// 註腳內容不能留在正文那一段裡。
	if strings.Contains(text(d.Blocks()[:1]), "註腳內容") {
		t.Errorf("註腳跑進正文了:%q", got)
	}
}

func TestHeadingFromStylesheet(t *testing.T) {
	d := parse(t, `{\rtf1\ansi\ansicpg65001{\stylesheet{\s1 heading 1;}{\s2 標題 2;}}`+
		`\s1 大標\par\s2 小標\par\pard 內文\par}`)
	bs := d.Blocks()
	if bs[0].Kind != markdown.Heading || bs[0].Level != 1 {
		t.Errorf("第一段:%v level=%d", bs[0].Kind, bs[0].Level)
	}
	if bs[1].Kind != markdown.Heading || bs[1].Level != 2 {
		t.Errorf("第二段:%v level=%d", bs[1].Kind, bs[1].Level)
	}
	if bs[2].Kind != markdown.Para {
		t.Errorf("第三段應該是一般段落,拿到 %v", bs[2].Kind)
	}
}

func TestNotRTF(t *testing.T) {
	if _, err := Parse([]byte("hello")); err == nil {
		t.Fatal("不是 RTF 應該要失敗")
	}
}
