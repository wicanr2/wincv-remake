package app

import (
	"archive/zip"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/wicanr2/wincv-remake/internal/cell"
	"github.com/wicanr2/wincv-remake/internal/keys"
	"github.com/wicanr2/wincv-remake/internal/vfs"
)

// 用真實檔案系統的暫存目錄,因為 app 層要驗的正是「進出目錄、開檔」
// 這些真的會碰檔案系統的行為。
func fixture(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	must(os.MkdirAll(filepath.Join(root, "sub"), 0o755))
	must(os.WriteFile(filepath.Join(root, "a.txt"), []byte("line1\nline2\nline3\n"), 0o644))
	must(os.WriteFile(filepath.Join(root, "b.txt"), []byte("bbb\n"), 0o644))
	must(os.WriteFile(filepath.Join(root, "sub", "c.txt"), []byte("ccc\n"), 0o644))
	// 一個真的二進位檔
	bin := make([]byte, 512)
	for i := range bin {
		bin[i] = byte(i % 5)
	}
	must(os.WriteFile(filepath.Join(root, "z.bin"), bin, 0o644))
	return root
}

func newApp(t *testing.T) (*App, *cell.Screen) {
	a := New(vfs.OS{}, fixture(t))
	s := cell.New(78, 20)
	a.Draw(s) // 先畫一次讓 rows 有值
	return a, s
}

func cursorName(a *App) string {
	if e := a.Browser.Current(); e != nil {
		return e.Name
	}
	return ""
}

func TestEnterDirAndBack(t *testing.T) {
	a, s := newApp(t)
	root := a.Browser.Dir

	// 移到 sub
	for cursorName(a) != "sub" {
		if !a.HandleKey(keys.Named(keys.Down)) {
			t.Fatal("找不到 sub")
		}
		a.Draw(s)
	}
	a.HandleKey(keys.Named(keys.Enter))
	a.Draw(s)
	if filepath.Base(a.Browser.Dir) != "sub" {
		t.Fatalf("Enter 之後應在 sub,現在在 %s", a.Browser.Dir)
	}

	a.HandleKey(keys.Named(keys.Backspace))
	a.Draw(s)
	if a.Browser.Dir != root {
		t.Fatalf("BackSpace 之後應回到 %s,現在在 %s", root, a.Browser.Dir)
	}
	// 回上一層時游標要停在剛才離開的目錄上。
	if cursorName(a) != "sub" {
		t.Errorf("回上一層後游標應停在 sub,現在在 %q", cursorName(a))
	}
}

func TestEnterTextFileOpensViewer(t *testing.T) {
	a, s := newApp(t)
	for cursorName(a) != "a.txt" {
		a.HandleKey(keys.Named(keys.Down))
		a.Draw(s)
	}
	a.HandleKey(keys.Named(keys.Enter))
	if a.Mode != ModeViewer {
		t.Fatal("Enter 在文字檔上應進入檢視器")
	}
	if a.Viewer.Name != "a.txt" || len(a.Viewer.Lines) != 3 {
		t.Errorf("檢視器載入 %q, %d 行", a.Viewer.Name, len(a.Viewer.Lines))
	}

	// Esc 是回上一層,不是離開程式。
	a.Draw(s)
	a.HandleKey(keys.Named(keys.Esc))
	if a.Mode != ModeBrowser {
		t.Error("Esc 應回到瀏覽器")
	}
	if a.Quit {
		t.Error("Esc 不該讓程式結束")
	}
}

// 二進位檔不該把檢視器塞滿亂碼,應該回報而不是進去。
func TestEnterBinaryFileReports(t *testing.T) {
	a, s := newApp(t)
	for cursorName(a) != "z.bin" {
		if !a.HandleKey(keys.Named(keys.Down)) {
			t.Fatal("找不到 z.bin")
		}
		a.Draw(s)
	}
	a.HandleKey(keys.Named(keys.Enter))
	if a.Mode != ModeBrowser {
		t.Error("二進位檔不該進入文字檢視器")
	}
	if !strings.Contains(a.Message, "二進位") {
		t.Errorf("應該有提示訊息,得到 %q", a.Message)
	}
}

func TestMarkKeys(t *testing.T) {
	a, s := newApp(t)
	a.HandleKey(keys.Ch(' '))
	a.Draw(s)
	if n, _ := a.Browser.MarkedStats(); n != 0 {
		t.Error("游標在「..」上按 Space 不該標記到任何東西")
	}

	a.HandleKey(keys.Ch('t'))
	n, _ := a.Browser.MarkedStats()
	if n != 3 { // a.txt b.txt z.bin,不含 sub
		t.Errorf("小寫 t 標記了 %d 筆, 應為 3(只有檔案)", n)
	}

	a.HandleKey(keys.Ch('u'))
	if n, _ := a.Browser.MarkedStats(); n != 0 {
		t.Errorf("u 之後還有 %d 個標記", n)
	}

	a.HandleKey(keys.AltCh('T'))
	n, _ = a.Browser.MarkedStats()
	if n != 4 { // 多一個 sub
		t.Errorf("Alt-T 標記了 %d 筆, 應為 4(含目錄)", n)
	}
}

func TestViewerToggles(t *testing.T) {
	a, s := newApp(t)
	for cursorName(a) != "a.txt" {
		a.HandleKey(keys.Named(keys.Down))
		a.Draw(s)
	}
	a.HandleKey(keys.Named(keys.Enter))
	a.Draw(s)

	if a.Viewer.Wrap {
		t.Error("預設不該開自動換行")
	}
	a.HandleKey(keys.Ch('w'))
	if !a.Viewer.Wrap {
		t.Error("W 應切換自動換行")
	}
	a.HandleKey(keys.Ch('w'))
	if a.Viewer.Wrap {
		t.Error("再按一次 W 應關掉自動換行")
	}

	if !a.Viewer.Ansi {
		t.Error("預設應解讀 ANSI 色碼")
	}
	a.HandleKey(keys.Ch('a'))
	if a.Viewer.Ansi {
		t.Error("A 應切換 ANSI")
	}
}

// 按鍵不分大小寫 —— 原版的單鍵指令都是這樣。
func TestLetterCaseInsensitive(t *testing.T) {
	for _, tc := range []struct {
		k    keys.Key
		want rune
	}{
		{keys.Ch('t'), 'T'},
		{keys.Ch('T'), 'T'},
		{keys.Ch('5'), 0},
		{keys.Named(keys.Enter), 0},
	} {
		if got := tc.k.Letter(); got != tc.want {
			t.Errorf("%v.Letter() = %q, 應為 %q", tc.k, got, tc.want)
		}
	}
}

func TestKeyString(t *testing.T) {
	for _, tc := range []struct {
		k    keys.Key
		want string
	}{
		{keys.Ch('C'), "C"},
		{keys.AltCh('Z'), "Alt-Z"},
		{keys.CtrlCh('N'), "Ctrl-N"},
		{keys.Named(keys.F11), "F11"},
		{keys.Named(keys.Backspace), "BackSpace"},
	} {
		if got := tc.k.String(); got != tc.want {
			t.Errorf("String() = %q, 應為 %q", got, tc.want)
		}
	}
}

// 壓縮檔要能像目錄一樣進出:進去、往下一層、退回來、退出壓縮檔,
// 而且退出後游標要停在那個壓縮檔上。
func TestEnterArchiveLikeDirectory(t *testing.T) {
	root := fixture(t)
	zipPath := filepath.Join(root, "pack.zip")
	func() {
		f, err := os.Create(zipPath)
		if err != nil {
			t.Fatal(err)
		}
		defer f.Close()
		zw := zip.NewWriter(f)
		for name, body := range map[string]string{
			"top.txt":       "top\n",
			"docs/deep.txt": "deep\n",
		} {
			w, err := zw.Create(name)
			if err != nil {
				t.Fatal(err)
			}
			io.WriteString(w, body)
		}
		zw.Close()
	}()

	a := New(vfs.OS{}, root)
	s := cell.New(78, 20)
	a.Draw(s)

	for cursorName(a) != "pack.zip" {
		if !a.HandleKey(keys.Named(keys.Down)) {
			t.Fatal("找不到 pack.zip")
		}
		a.Draw(s)
	}
	a.HandleKey(keys.Named(keys.Enter))
	a.Draw(s)

	var got []string
	for _, e := range a.Browser.Entries {
		got = append(got, e.Name)
	}
	if strings.Join(got, " ") != ".. docs top.txt" {
		t.Fatalf("壓縮檔最上層 = %v, 應為 [.. docs top.txt]", got)
	}

	// 往下一層
	for cursorName(a) != "docs" {
		a.HandleKey(keys.Named(keys.Down))
		a.Draw(s)
	}
	a.HandleKey(keys.Named(keys.Enter))
	a.Draw(s)
	if cursorName(a) != ".." {
		t.Fatalf("進 docs 之後第一筆應是 ..,得到 %q", cursorName(a))
	}
	found := false
	for _, e := range a.Browser.Entries {
		if e.Name == "deep.txt" {
			found = true
		}
	}
	if !found {
		t.Fatal("docs/ 裡看不到 deep.txt")
	}

	// 看壓縮檔裡的檔案
	for cursorName(a) != "deep.txt" {
		a.HandleKey(keys.Named(keys.Down))
		a.Draw(s)
	}
	a.HandleKey(keys.Named(keys.Enter))
	if a.Mode != ModeViewer {
		t.Fatal("壓縮檔裡的文字檔應該打得開")
	}
	if a.Viewer.Lines[0].Text() != "deep" {
		t.Errorf("讀出 %q", a.Viewer.Lines[0].Text())
	}
	a.Draw(s)
	a.HandleKey(keys.Named(keys.Esc))
	a.Draw(s)

	// 退回壓縮檔最上層
	a.HandleKey(keys.Named(keys.Backspace))
	a.Draw(s)
	if cursorName(a) != "docs" {
		t.Errorf("退回上一層後游標應停在 docs,得到 %q", cursorName(a))
	}

	// 再退一次就離開壓縮檔
	a.HandleKey(keys.Named(keys.Backspace))
	a.Draw(s)
	if a.Browser.Dir != root {
		t.Fatalf("應該回到 %s,現在在 %s", root, a.Browser.Dir)
	}
	if cursorName(a) != "pack.zip" {
		t.Errorf("離開壓縮檔後游標應停在 pack.zip,得到 %q", cursorName(a))
	}
}

// 還沒實作的格式要給訊息,不是默默什麼都不做。
func TestEnterUnsupportedArchive(t *testing.T) {
	root := fixture(t)
	os.WriteFile(filepath.Join(root, "x.rar"), []byte("Rar!\x1a\x07\x00"), 0o644)
	a := New(vfs.OS{}, root)
	s := cell.New(78, 20)
	a.Draw(s)
	for cursorName(a) != "x.rar" {
		if !a.HandleKey(keys.Named(keys.Down)) {
			t.Fatal("找不到 x.rar")
		}
		a.Draw(s)
	}
	a.HandleKey(keys.Named(keys.Enter))
	if a.Mode != ModeBrowser {
		t.Error("不支援的格式不該切換模式")
	}
	if !strings.Contains(a.Message, "RAR") {
		t.Errorf("應該說明是什麼格式沒支援,得到 %q", a.Message)
	}
}
