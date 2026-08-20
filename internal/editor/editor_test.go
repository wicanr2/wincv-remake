package editor

import (
	"strings"
	"testing"

	"github.com/wicanr2/wincv-remake/internal/syntax"
	"github.com/wicanr2/wincv-remake/internal/textenc"
)

func mk(lines ...string) *Model {
	return Load("t", []byte(strings.Join(lines, "\n")+"\n"), textenc.ASCII, nil)
}

func text(m *Model) []string {
	out := make([]string, len(m.Lines))
	for i, l := range m.Lines {
		out[i] = string(l)
	}
	return out
}

func eq(t *testing.T, m *Model, want ...string) {
	t.Helper()
	got := text(m)
	if len(got) != len(want) {
		t.Fatalf("行數 = %d, 應為 %d\n得到 %q\n預期 %q", len(got), len(want), got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("第 %d 行 = %q, 應為 %q", i, got[i], want[i])
		}
	}
}

func TestInsertAndOverwrite(t *testing.T) {
	m := mk("abc")
	m.MoveTo(Pos{0, 1})
	m.InsertRune('X')
	eq(t, m, "aXbc")

	m.Insert = false
	m.MoveTo(Pos{0, 0})
	m.InsertRune('Z')
	eq(t, m, "ZXbc")
}

func TestNewLineAndBackspaceJoin(t *testing.T) {
	m := mk("abcd")
	m.MoveTo(Pos{0, 2})
	m.NewLine()
	eq(t, m, "ab", "cd")
	if m.Cur != (Pos{1, 0}) {
		t.Errorf("斷行後游標 = %+v", m.Cur)
	}
	m.Backspace()
	eq(t, m, "abcd")
	if m.Cur != (Pos{0, 2}) {
		t.Errorf("合併後游標 = %+v, 應回到接點", m.Cur)
	}
}

// 矩形區塊:每一列取同一段欄位,不是連續文字。
func TestRectBlockCopy(t *testing.T) {
	m := mk("abcdef", "ghijkl", "mnopqr")
	m.MoveTo(Pos{0, 1})
	m.MarkBlock(BlockRect)
	m.MoveTo(Pos{2, 4})
	m.MarkBlock(BlockRect)
	if !m.CopyBlock() {
		t.Fatal("複製失敗")
	}
	want := []string{"bcd", "hij", "nop"}
	for i, w := range want {
		if m.Clipboard[i] != w {
			t.Errorf("第 %d 列 = %q, 應為 %q", i, m.Clipboard[i], w)
		}
	}
}

func TestRectBlockDelete(t *testing.T) {
	m := mk("abcdef", "ghijkl")
	m.MoveTo(Pos{0, 1})
	m.MarkBlock(BlockRect)
	m.MoveTo(Pos{1, 4})
	m.MarkBlock(BlockRect)
	m.DeleteBlock()
	eq(t, m, "aef", "gkl")
}

// 矩形貼上是**插入**,右邊的字往後推 —— 這是 PE2 的行為。
func TestRectPasteInserts(t *testing.T) {
	m := mk("abc", "def")
	m.Clipboard, m.ClipKind = []string{"12", "34"}, BlockRect
	m.MoveTo(Pos{0, 1})
	m.PasteBlock()
	eq(t, m, "a12bc", "d34ef")
}

// 「在每一行前面加上 5 個空白」—— changelog 明講的用法。
func TestFillBlockInsertMode(t *testing.T) {
	m := mk("aaa", "bbb", "ccc")
	m.MoveTo(Pos{0, 0})
	m.MarkBlock(BlockRect)
	m.MoveTo(Pos{2, 5})
	m.MarkBlock(BlockRect)
	if !m.FillBlock(' ', true) {
		t.Fatal("填充失敗")
	}
	eq(t, m, "     aaa", "     bbb", "     ccc")
}

func TestFillBlockOverwrite(t *testing.T) {
	m := mk("abcdef", "ghijkl")
	m.MoveTo(Pos{0, 1})
	m.MarkBlock(BlockRect)
	m.MoveTo(Pos{1, 4})
	m.MarkBlock(BlockRect)
	m.FillBlock('.', false)
	eq(t, m, "a...ef", "g...kl")
}

// 整列區塊刪掉的是整行,不是欄位範圍。
func TestLineBlockDelete(t *testing.T) {
	m := mk("one", "two", "three", "four")
	m.MoveTo(Pos{1, 2})
	m.MarkBlock(BlockLine)
	m.MoveTo(Pos{2, 0})
	m.MarkBlock(BlockLine)
	m.DeleteBlock()
	eq(t, m, "one", "four")
}

func TestLineBlockPaste(t *testing.T) {
	m := mk("one", "two")
	m.Clipboard, m.ClipKind = []string{"X", "Y"}, BlockLine
	m.MoveTo(Pos{1, 0})
	m.PasteBlock()
	eq(t, m, "one", "X", "Y", "two")
}

func TestUndo(t *testing.T) {
	m := mk("abc")
	m.MoveTo(Pos{0, 0})
	m.InsertRune('X')
	m.InsertRune('Y')
	eq(t, m, "XYabc")
	m.Undo()
	eq(t, m, "Xabc")
	m.Undo()
	eq(t, m, "abc")
	if m.Undo() {
		t.Error("沒東西可復原時應回 false")
	}
}

func TestFindAndReplaceAll(t *testing.T) {
	m := mk("foo bar", "BAR foo", "baz")
	at, ok := m.Find("bar", Pos{}, false)
	if !ok || at != (Pos{0, 4}) {
		t.Errorf("Find = %+v ok=%v, 應為 {0 4}", at, ok)
	}
	at, ok = m.Find("bar", Pos{0, 5}, false)
	if !ok || at != (Pos{1, 0}) {
		t.Errorf("接著找 = %+v ok=%v, 應為 {1 0}", at, ok)
	}
	if _, ok := m.Find("bar", Pos{}, true); !ok {
		t.Error("分大小寫也該找得到第一個 bar")
	}
	n := m.ReplaceAll("foo", "QUX", false)
	if n != 2 {
		t.Errorf("取代了 %d 處, 應為 2", n)
	}
	eq(t, m, "QUX bar", "BAR QUX", "baz")
}

// Tab 要對齊到 TabWidth 的倍數,全形字算兩格。
func TestColToScreen(t *testing.T) {
	m := mk("")
	m.TabWidth = 8
	line := []rune("a\tb")
	if got := m.ColToScreen(line, 1); got != 1 {
		t.Errorf("'a' 之後 = %d", got)
	}
	if got := m.ColToScreen(line, 2); got != 8 {
		t.Errorf("tab 之後應對齊到 8, 得到 %d", got)
	}
	wide := []rune("中a")
	if got := m.ColToScreen(wide, 1); got != 2 {
		t.Errorf("全形字之後 = %d, 應為 2", got)
	}
}

// 存回去要保留原本的編碼與換行樣式。
func TestRoundTripKeepsEncodingAndEOL(t *testing.T) {
	src := []byte("\xa4\xa4\xa4\xe5\r\n\xa4\xa4\xa4\xe5\r\n") // Big5「中文」兩行,CRLF
	m := Load("t", src, textenc.Unknown, nil)
	if m.Enc != textenc.Big5 {
		t.Fatalf("判讀 = %v", m.Enc)
	}
	if m.EOL != "\r\n" {
		t.Errorf("EOL = %q, 應為 CRLF", m.EOL)
	}
	if got := string(m.Bytes()); got != string(src) {
		t.Errorf("round-trip 不一致\n得到 %q\n原檔 %q", got, src)
	}
}

// 游標可以停在行尾之後(虛擬空白),在那裡打字才把中間補成空白。
// 這是 PE2 這類 DOS 編輯器的特性,矩形區塊要靠它才做得出來。
func TestVirtualSpace(t *testing.T) {
	m := mk("ab")
	m.MoveTo(Pos{0, 6})
	if m.Cur.Col != 6 {
		t.Fatalf("游標應能停在行尾之後,得到 %d", m.Cur.Col)
	}
	eq(t, m, "ab") // 只移動游標不該改內容

	m.InsertRune('X')
	eq(t, m, "ab    X")

	m2 := mk("ab")
	m2.MoveTo(Pos{0, 5})
	m2.Backspace()
	if m2.Cur.Col != 4 {
		t.Errorf("在虛擬空白處退格應只移游標,得到欄 %d", m2.Cur.Col)
	}
	eq(t, m2, "ab")
}

// 在虛擬空白處斷行,不該產生一堆空白。
func TestNewLineInVirtualSpace(t *testing.T) {
	m := mk("ab", "cd")
	m.MoveTo(Pos{0, 9})
	m.NewLine()
	eq(t, m, "ab", "", "cd")
}

func TestCommentLinesToggle(t *testing.T) {
	cfg := &syntax.Config{Name: "c", LineComment: "//"}
	m := Load("a.c", []byte("int a;\nint b;\n\nint c;\n"), textenc.UTF8, cfg)

	if !m.CommentLines(0, 3) {
		t.Fatal("CommentLines 回 false")
	}
	got := string(m.Bytes())
	want := "// int a;\n// int b;\n\n// int c;\n"
	if got != want {
		t.Fatalf("加註解後 = %q,期望 %q", got, want)
	}

	// 全部都已註解 → 再按一次是拿掉
	m.CommentLines(0, 3)
	if got := string(m.Bytes()); got != "int a;\nint b;\n\nint c;\n" {
		t.Fatalf("拿掉註解後 = %q", got)
	}
}

// 半註解的區塊按一次,結果要是「全部註解」,不是各自反轉。
func TestCommentLinesPartialGoesAllOn(t *testing.T) {
	cfg := &syntax.Config{Name: "c", LineComment: "//"}
	m := Load("a.c", []byte("// int a;\nint b;\n"), textenc.UTF8, cfg)
	m.CommentLines(0, 1)
	if got := string(m.Bytes()); got != "// // int a;\n// int b;\n" {
		t.Fatalf("= %q", got)
	}
}

// 沒有語法設定就不要猜一個註解記號上去。
func TestCommentLinesNoConfig(t *testing.T) {
	m := Load("a.txt", []byte("hello\n"), textenc.UTF8, nil)
	if m.CommentLines(0, 0) {
		t.Error("沒有設定時不該動手")
	}
	if string(m.Bytes()) != "hello\n" {
		t.Errorf("內容被改了: %q", m.Bytes())
	}
}
