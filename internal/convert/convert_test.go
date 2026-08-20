package convert

import (
	"strings"
	"testing"

	"github.com/wicanr2/wincv-remake/internal/textenc"
)

func TestEOL(t *testing.T) {
	// 混用兩種換行的檔案也要被統一 —— 這正是原版拿來修壞掉檔案的用法。
	mixed := []byte("a\r\nb\nc\rd")
	if got := string(ToEOL(mixed, EOLUnix)); got != "a\nb\nc\nd" {
		t.Errorf("轉 UNIX = %q", got)
	}
	if got := string(ToEOL(mixed, EOLPC)); got != "a\r\nb\r\nc\r\nd" {
		t.Errorf("轉 PC = %q", got)
	}
	if got := string(ToEOL([]byte("a\r\nb\r\n"), EOLUnix)); got != "a\nb\n" {
		t.Errorf("CRLF 轉 UNIX = %q", got)
	}
	if DetectEOL([]byte("a\r\nb\r\n")) != EOLPC {
		t.Error("CRLF 應判成 PC")
	}
	if DetectEOL([]byte("a\nb\n")) != EOLUnix {
		t.Error("LF 應判成 UNIX")
	}
	if DetectEOL([]byte("a\rb\r")) != EOLMac {
		t.Error("CR 應判成 Mac")
	}
}

// 轉碼是原版的招牌功能,round-trip 一定要對得回來。
func TestRecodeRoundTrip(t *testing.T) {
	const zh = "檔案瀏覽壓縮管理"
	b5, err := Recode([]byte(zh), textenc.UTF8, textenc.Big5)
	if err != nil {
		t.Fatal(err)
	}
	if textenc.Detect(b5) != textenc.Big5 {
		t.Error("轉出來的不是 Big5")
	}
	back, err := Recode(b5, textenc.Big5, textenc.UTF8)
	if err != nil {
		t.Fatal(err)
	}
	if string(back) != zh {
		t.Errorf("轉回來 = %q, 應為 %q", back, zh)
	}
}

// 繁簡互轉一定會遇到對不過去的字,不可以整份失敗。
func TestRecodeLossy(t *testing.T) {
	// 日文假名在 Big5 有,在 EUC-KR 沒有
	src := []byte("あいう漢字")
	got := RecodeLossy(src, textenc.UTF8, textenc.EUCKR, "?")
	if len(got) == 0 {
		t.Fatal("整份轉不出來")
	}
	back := textenc.Decode(got, textenc.EUCKR)
	if !strings.Contains(back, "漢") {
		t.Errorf("轉得過去的字也掉了: %q", back)
	}
}

func TestStripANSI(t *testing.T) {
	in := "\x1b[1;31m紅\x1b[0m白\x1b[44m底\x1b[m"
	if got := StripANSI(in); got != "紅白底" {
		t.Errorf("= %q, 應為 \"紅白底\"", got)
	}
}

// script/style 的內容也要拿掉,不能只拆標籤留下程式碼。
func TestStripHTML(t *testing.T) {
	in := `<html><head><style>body{color:red}</style>
<script>var x = 1 < 2;</script></head>
<body><p>第一段</p><p>第二段<br>換行</p>
<a href="#">連結</a> &amp; &lt;實體&gt;</body></html>`
	got := StripHTML(in)
	for _, bad := range []string{"color:red", "var x", "<p>", "href"} {
		if strings.Contains(got, bad) {
			t.Errorf("結果裡不該有 %q:\n%s", bad, got)
		}
	}
	for _, want := range []string{"第一段", "第二段", "換行", "連結", "& <實體>"} {
		if !strings.Contains(got, want) {
			t.Errorf("結果裡看不到 %q:\n%s", want, got)
		}
	}
}

// &amp;lt; 表示的是字面上的 "&lt;",不是 "<"。
func TestEntityOrder(t *testing.T) {
	if got := StripHTML("<p>&amp;lt;</p>"); got != "&lt;" {
		t.Errorf("= %q, 應為 \"&lt;\"", got)
	}
}

func TestRenameCase(t *testing.T) {
	for _, tc := range []struct {
		in   string
		c    NameCase
		want string
	}{
		{"MyFile.TXT", CaseLower, "myfile.txt"},
		{"MyFile.TXT", CaseUpper, "MYFILE.TXT"},
		{"myFILE.TXT", CaseTitle, "Myfile.txt"},
		{"noext", CaseUpper, "NOEXT"},
		{".hidden", CaseUpper, ".HIDDEN"},
		{"a.b.c", CaseLower, "a.b.c"},
	} {
		if got := RenameCase(tc.in, tc.c); got != tc.want {
			t.Errorf("RenameCase(%q,%v) = %q, 應為 %q", tc.in, tc.c, got, tc.want)
		}
	}
}

func TestNumberedRename(t *testing.T) {
	for _, tc := range []struct {
		pat  string
		n    int
		want string
	}{
		{"pic###.jpg", 7, "pic007.jpg"},
		{"pic###.jpg", 123, "pic123.jpg"},
		{"pic#.jpg", 5, "pic5.jpg"},
		{"pic#.jpg", 42, "pic42.jpg"}, // 位數不夠就長出來,不截斷
		{"noplaceholder.jpg", 3, "noplaceholder.jpg"},
	} {
		if got := NumberedRename(tc.pat, tc.n); got != tc.want {
			t.Errorf("NumberedRename(%q,%d) = %q, 應為 %q", tc.pat, tc.n, got, tc.want)
		}
	}
}
