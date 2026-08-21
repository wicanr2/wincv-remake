package markdown

import (
	"image"
	"strings"
	"testing"
)

func textOf(ls []Line) string {
	var b strings.Builder
	for _, l := range ls {
		for _, p := range l.Pieces {
			b.WriteString(p.Text)
		}
		b.WriteByte('\n')
	}
	return b.String()
}

func layout(t *testing.T, src string, cols int) ([]Line, []*Pic) {
	t.Helper()
	return Layout(Parse(src), cols, 8, 16, nil, DefaultTheme())
}

func TestHeadingsAndParagraph(t *testing.T) {
	ls, _ := layout(t, "# 標題\n\n一段文字。\n\n## 次標題\n", 40)
	got := textOf(ls)
	for _, want := range []string{"# 標題", "一段文字。", "## 次標題"} {
		if !strings.Contains(got, want) {
			t.Errorf("少了 %q\n%s", want, got)
		}
	}
}

// setext 標題(底下一行 === 或 ---)不能被當成段落 + 水平線。
func TestSetextHeading(t *testing.T) {
	ls, _ := layout(t, "標題\n====\n\n內文\n", 40)
	got := textOf(ls)
	if !strings.Contains(got, "# 標題") {
		t.Errorf("setext 標題沒有被認出來:\n%s", got)
	}
}

// 程式碼區塊裡的東西一律原樣保留 —— 那正是它的用途。
// 裡面的 # 不是標題,* 不是清單,- 不是水平線。
func TestCodeFenceKeepsContentVerbatim(t *testing.T) {
	src := "```go\n# not a heading\n* not a list\n---\n```\n"
	ls, _ := layout(t, src, 40)
	got := textOf(ls)
	for _, want := range []string{"# not a heading", "* not a list", "---"} {
		if !strings.Contains(got, want) {
			t.Errorf("程式碼區塊的 %q 被改掉了:\n%s", want, got)
		}
	}
}

// 落單的星號不能讓後面整段變成斜體。
func TestUnpairedEmphasisIsLiteral(t *testing.T) {
	sp := inline("2 * 3 = 6")
	var b strings.Builder
	for _, s := range sp {
		b.WriteString(s.Text)
		if s.Style != 0 {
			t.Errorf("%q 不該有樣式 %v", s.Text, s.Style)
		}
	}
	if b.String() != "2 * 3 = 6" {
		t.Errorf("文字被吃掉了: %q", b.String())
	}
}

func TestInlineStyles(t *testing.T) {
	sp := inline("普通 **粗** 與 *斜* 與 `碼` 與 [連結](http://x/y)")
	find := func(txt string) Span {
		for _, s := range sp {
			if s.Text == txt {
				return s
			}
		}
		t.Fatalf("找不到片段 %q,實際 %+v", txt, sp)
		return Span{}
	}
	if find("粗").Style&Bold == 0 {
		t.Error("粗體沒認出來")
	}
	if find("斜").Style&Italic == 0 {
		t.Error("斜體沒認出來")
	}
	if find("碼").Style&Mono == 0 {
		t.Error("行內程式碼沒認出來")
	}
	if l := find("連結"); l.Style&Link == 0 || l.Href != "http://x/y" {
		t.Errorf("連結 = %+v", l)
	}
}

// [![alt](img)](url) 這種巢狀在 README 裡很常見。
// 只找第一個 ] 會切在錯的地方,把後面整行吃掉。
func TestNestedImageLink(t *testing.T) {
	sp := inline("[![badge](b.svg)](https://ci/x) 後面的字")
	if len(sp) < 2 {
		t.Fatalf("解析結果太短: %+v", sp)
	}
	if sp[0].Href != "https://ci/x" || sp[0].Text != "badge" {
		t.Errorf("巢狀連結 = %+v", sp[0])
	}
	if !strings.Contains(sp[len(sp)-1].Text, "後面的字") {
		t.Errorf("後面的文字被吃掉了: %+v", sp)
	}
}

// 中文不靠空白斷字。只按空白折的話,一整段中文會變成一列超長的字。
func TestWrapCJK(t *testing.T) {
	long := strings.Repeat("中文", 30)
	ls, _ := layout(t, long, 20)
	for i, l := range ls {
		w := 0
		for _, p := range l.Pieces {
			w += width(p.Text)
		}
		if w > 20 {
			t.Fatalf("第 %d 列寬 %d,超過 20", i, w)
		}
	}
	if len(ls) < 5 {
		t.Errorf("只折成 %d 列,應該更多", len(ls))
	}
}

// 超長且沒有空白的字串(例如網址)要硬切,不能無限卡住。
func TestWrapVeryLongWord(t *testing.T) {
	ls, _ := layout(t, strings.Repeat("a", 200), 20)
	if len(ls) < 5 || len(ls) > 40 {
		t.Fatalf("折成 %d 列,不合理", len(ls))
	}
	for _, l := range ls {
		w := 0
		for _, p := range l.Pieces {
			w += width(p.Text)
		}
		if w > 20 {
			t.Fatalf("有一列寬 %d", w)
		}
	}
}

func TestTable(t *testing.T) {
	src := "| 名稱 | 大小 |\n|---|---:|\n| a.txt | 12 |\n| b.bin | 3400 |\n"
	ls, _ := layout(t, src, 40)
	got := textOf(ls)
	for _, want := range []string{"名稱", "a.txt", "3400"} {
		if !strings.Contains(got, want) {
			t.Errorf("表格少了 %q:\n%s", want, got)
		}
	}
}

// 圖片載不到時要把 alt 與原因寫出來。留白的話使用者會以為本來就沒東西。
func TestImageFailureIsVisible(t *testing.T) {
	ls, pics := layout(t, "![一張圖](a.png)\n", 40)
	if len(pics) != 1 {
		t.Fatalf("圖片數 = %d", len(pics))
	}
	if !strings.Contains(textOf(ls), "一張圖") {
		t.Errorf("alt 沒顯示:\n%s", textOf(ls))
	}
}

// 遠端圖片不下載 —— 看一份文件不該變成連外行為。
func TestRemoteImageNotFetched(t *testing.T) {
	called := false
	load := func(string) (image.Image, error) {
		called = true
		return nil, nil
	}
	_, pics := Layout(Parse("![x](https://example.com/a.png)"), 40, 8, 16, load, DefaultTheme())
	if called {
		t.Error("去抓了遠端圖片")
	}
	if len(pics) != 1 || pics[0].Err == "" {
		t.Errorf("沒有標明原因: %+v", pics)
	}
}

// 圖片要佔掉整數列,而且後面的文字要接在圖的下面,不能疊在上面。
func TestImageOccupiesRows(t *testing.T) {
	load := func(string) (image.Image, error) {
		return image.NewRGBA(image.Rect(0, 0, 160, 160)), nil
	}
	ls, pics := Layout(Parse("![x](a.png)\n\n後面\n"), 40, 8, 16, load, DefaultTheme())
	if len(pics) != 1 || pics[0].Img == nil {
		t.Fatalf("圖沒載進來: %+v", pics)
	}
	p := pics[0]
	if p.Rows < 2 || p.Cols < 2 {
		t.Fatalf("尺寸 = %d×%d 格", p.Cols, p.Rows)
	}
	n := 0
	for _, l := range ls {
		if l.Pic == p {
			n++
		}
	}
	if n != p.Rows {
		t.Errorf("圖佔了 %d 列,宣告 %d 列", n, p.Rows)
	}
	// 「後面」必須在圖之後
	last := -1
	for i, l := range ls {
		if l.Pic == p {
			last = i
		}
	}
	found := -1
	for i, l := range ls {
		for _, pc := range l.Pieces {
			if strings.Contains(pc.Text, "後面") {
				found = i
			}
		}
	}
	if found <= last {
		t.Errorf("文字在第 %d 列,圖到第 %d 列 —— 疊到一起了", found, last)
	}
}

// 一張很大的圖不能把整個畫面吃掉。
func TestImageRowsCapped(t *testing.T) {
	load := func(string) (image.Image, error) {
		return image.NewRGBA(image.Rect(0, 0, 100, 8000)), nil
	}
	_, pics := Layout(Parse("![x](a.png)"), 40, 8, 16, load, DefaultTheme())
	if pics[0].Rows > MaxPicRows {
		t.Errorf("佔了 %d 列,上限是 %d", pics[0].Rows, MaxPicRows)
	}
}

func TestList(t *testing.T) {
	ls, _ := layout(t, "- 第一\n- 第二\n\n1. 甲\n2. 乙\n", 40)
	got := textOf(ls)
	for _, want := range []string{"• 第一", "• 第二", "1. 甲", "2. 乙"} {
		if !strings.Contains(got, want) {
			t.Errorf("清單少了 %q:\n%s", want, got)
		}
	}
}

// `---` 單獨出現是水平線,接在一行文字後面是二級標題。
// 同一串字元、兩種語意,差別只在前一行有沒有東西。
func TestDashesRuleVsSetext(t *testing.T) {
	ls, _ := layout(t, "一段文字\n\n---\n\n另一段\n", 40)
	if strings.Contains(textOf(ls), "# 一段文字") {
		t.Errorf("空行之後的 --- 被當成標題了:\n%s", textOf(ls))
	}
	ls, _ = layout(t, "一段文字\n---\n\n另一段\n", 40)
	if !strings.Contains(textOf(ls), "## 一段文字") {
		t.Errorf("緊接在文字後的 --- 應該是二級標題:\n%s", textOf(ls))
	}
}

// 標題底下要有一條線。看不見的顏色等於沒有這條線,所以順便盯著顏色。
func TestHeadingRule(t *testing.T) {
	ls, _ := layout(t, "# 標題\n", 20)
	found := false
	for _, l := range ls {
		for _, p := range l.Pieces {
			if strings.Contains(p.Text, "───") {
				found = true
				if p.FG == 1 { // DkGray:#404040,畫在黑底上等於看不見
					t.Error("分隔線用了看不見的顏色")
				}
			}
		}
	}
	if !found {
		t.Errorf("標題底下沒有線:\n%s", textOf(ls))
	}
}
