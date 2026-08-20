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
	"github.com/wicanr2/wincv-remake/internal/syntax"
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

// 二進位檔按 Enter 直接開 16 進位檢視。
// 原版 0.5 版起就是這個行為(changelog:「按 enter 看檔時自動將可能為
// 執行檔的檔案以 16 進位方式看檔」),不是丟一個訊息就算了。
func TestEnterBinaryFileOpensHex(t *testing.T) {
	a, s := newApp(t)
	for cursorName(a) != "z.bin" {
		if !a.HandleKey(keys.Named(keys.Down)) {
			t.Fatal("找不到 z.bin")
		}
		a.Draw(s)
	}
	a.HandleKey(keys.Named(keys.Enter))
	if a.Mode != ModeHex {
		t.Fatalf("二進位檔應開 16 進位檢視,現在模式 %v", a.Mode)
	}
	if a.Hex == nil || len(a.Hex.Data) != 512 {
		t.Errorf("16 進位檢視載入的資料不對")
	}
	if a.Hex.Lines() != 32 { // 512 / 16
		t.Errorf("512 bytes 應為 32 列,得到 %d", a.Hex.Lines())
	}
	a.Draw(s)
	a.HandleKey(keys.Named(keys.Esc))
	if a.Mode != ModeBrowser {
		t.Error("Esc 應回到瀏覽器")
	}
}

// 文字檔可以用 H 切到 16 進位,再按 H 切回來。
func TestTextToHexAndBack(t *testing.T) {
	a, s := newApp(t)
	for cursorName(a) != "a.txt" {
		a.HandleKey(keys.Named(keys.Down))
		a.Draw(s)
	}
	a.HandleKey(keys.Named(keys.Enter))
	a.Draw(s)
	if a.Mode != ModeViewer {
		t.Fatal("應先進文字檢視")
	}
	a.HandleKey(keys.Ch('h'))
	if a.Mode != ModeHex {
		t.Fatal("H 應切到 16 進位")
	}
	if a.Hex.Name != "a.txt" || len(a.Hex.Data) != len("line1\nline2\nline3\n") {
		t.Errorf("16 進位載入的是 %q, %d bytes", a.Hex.Name, len(a.Hex.Data))
	}
	a.Draw(s)
	a.HandleKey(keys.Ch('h'))
	if a.Mode != ModeViewer {
		t.Error("再按 H 應切回文字檢視")
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
	os.WriteFile(filepath.Join(root, "x.ace"), []byte("dummy"), 0o644)
	a := New(vfs.OS{}, root)
	s := cell.New(78, 20)
	a.Draw(s)
	for cursorName(a) != "x.ace" {
		if !a.HandleKey(keys.Named(keys.Down)) {
			t.Fatal("找不到 x.ace")
		}
		a.Draw(s)
	}
	a.HandleKey(keys.Named(keys.Enter))
	if a.Mode != ModeBrowser {
		t.Error("不支援的格式不該切換模式")
	}
	if !strings.Contains(a.Message, "ACE") {
		t.Errorf("應該說明是什麼格式沒支援,得到 %q", a.Message)
	}
}

// R 改名:輸入列 → Enter → 檔案真的改名 → 游標停在新名字上。
func TestRenameFlow(t *testing.T) {
	a, s := newApp(t)
	for cursorName(a) != "a.txt" {
		a.HandleKey(keys.Named(keys.Down))
		a.Draw(s)
	}
	dir := a.Browser.Dir

	a.HandleKey(keys.Ch('r'))
	if !a.Prompting() {
		t.Fatal("R 應該開輸入列")
	}
	// 清掉預設值再輸入新名字
	for i := 0; i < 20; i++ {
		a.HandleKey(keys.Named(keys.Backspace))
	}
	for _, r := range "new.txt" {
		a.HandleKey(keys.Ch(r))
	}
	a.HandleKey(keys.Named(keys.Enter))
	if a.Prompting() {
		t.Fatal("Enter 之後輸入列應該關掉")
	}
	if _, err := os.Stat(filepath.Join(dir, "new.txt")); err != nil {
		t.Fatalf("檔案沒改名: %v", err)
	}
	a.Draw(s)
	if cursorName(a) != "new.txt" {
		t.Errorf("游標應停在新名字上,得到 %q", cursorName(a))
	}
}

// Esc 取消輸入列,不可以動到檔案。
func TestPromptCancel(t *testing.T) {
	a, s := newApp(t)
	for cursorName(a) != "a.txt" {
		a.HandleKey(keys.Named(keys.Down))
		a.Draw(s)
	}
	dir := a.Browser.Dir
	a.HandleKey(keys.Ch('r'))
	a.HandleKey(keys.Ch('X'))
	a.HandleKey(keys.Named(keys.Esc))
	if a.Prompting() {
		t.Error("Esc 應該關掉輸入列")
	}
	if _, err := os.Stat(filepath.Join(dir, "a.txt")); err != nil {
		t.Error("取消之後原檔應該還在")
	}
}

// D 刪除要先問過。答 N 不可以刪掉東西。
func TestDeleteAsksFirst(t *testing.T) {
	a, s := newApp(t)
	for cursorName(a) != "b.txt" {
		a.HandleKey(keys.Named(keys.Down))
		a.Draw(s)
	}
	dir := a.Browser.Dir
	a.HandleKey(keys.Ch('d'))
	if !a.Prompting() {
		t.Fatal("D 應該先問")
	}
	a.HandleKey(keys.Ch('n'))
	if _, err := os.Stat(filepath.Join(dir, "b.txt")); err != nil {
		t.Fatal("答 N 之後檔案不該被刪")
	}

	a.HandleKey(keys.Ch('d'))
	a.HandleKey(keys.Ch('y'))
	if _, err := os.Stat(filepath.Join(dir, "b.txt")); err == nil {
		t.Error("答 Y 之後檔案應該被刪掉")
	}
}

// Alt-C 比對:要剛好兩個標記。
func TestCompareNeedsExactlyTwo(t *testing.T) {
	a, s := newApp(t)
	a.HandleKey(keys.AltCh('C'))
	if !strings.Contains(a.Message, "兩個") {
		t.Errorf("沒標記時應該提示,得到 %q", a.Message)
	}

	// 造兩個內容相同的檔
	dir := a.Browser.Dir
	os.WriteFile(filepath.Join(dir, "s1.txt"), []byte("same"), 0o644)
	os.WriteFile(filepath.Join(dir, "s2.txt"), []byte("same"), 0o644)
	a.Browser.Load(dir)
	a.Draw(s)
	n := 0
	for i := range a.Browser.Entries {
		if strings.HasPrefix(a.Browser.Entries[i].Name, "s") &&
			strings.HasSuffix(a.Browser.Entries[i].Name, ".txt") {
			a.Browser.Entries[i].Marked = true
			n++
		}
	}
	if n != 2 {
		t.Fatalf("標記了 %d 個", n)
	}
	a.HandleKey(keys.AltCh('C'))
	if !strings.Contains(a.Message, "相同") {
		t.Errorf("兩個相同的檔案應回報相同,得到 %q", a.Message)
	}
}

// 壓縮檔裡不能做檔案操作,要給明確訊息而不是默默失敗。
func TestFileOpsBlockedInsideArchive(t *testing.T) {
	root := fixture(t)
	zipPath := filepath.Join(root, "p.zip")
	f, _ := os.Create(zipPath)
	zw := zip.NewWriter(f)
	w, _ := zw.Create("x.txt")
	io.WriteString(w, "x")
	zw.Close()
	f.Close()

	a := New(vfs.OS{}, root)
	s := cell.New(78, 20)
	a.Draw(s)
	for cursorName(a) != "p.zip" {
		if !a.HandleKey(keys.Named(keys.Down)) {
			t.Fatal("找不到 p.zip")
		}
		a.Draw(s)
	}
	a.HandleKey(keys.Named(keys.Enter))
	a.Draw(s)

	for _, k := range []keys.Key{keys.Ch('c'), keys.Ch('m'), keys.Ch('r'), keys.Ch('d')} {
		a.HandleKey(k)
		if a.Prompting() {
			a.HandleKey(keys.Named(keys.Esc))
			t.Errorf("%v 在壓縮檔裡不該開輸入列", k)
			continue
		}
		if !strings.Contains(a.Message, "壓縮檔") {
			t.Errorf("%v 應該說明壓縮檔裡不能做,得到 %q", k, a.Message)
		}
	}
}

// Z 解壓縮:游標停在壓縮檔上 → 輸入目的地 → 檔案真的解出來。
func TestExtractFlow(t *testing.T) {
	root := fixture(t)
	zp := filepath.Join(root, "pack.zip")
	f, _ := os.Create(zp)
	zw := zip.NewWriter(f)
	for name, body := range map[string]string{"top.txt": "top\n", "d/deep.txt": "deep\n"} {
		w, _ := zw.Create(name)
		io.WriteString(w, body)
	}
	zw.Close()
	f.Close()

	a := New(vfs.OS{}, root)
	s := cell.New(78, 20)
	a.Draw(s)
	for cursorName(a) != "pack.zip" {
		if !a.HandleKey(keys.Named(keys.Down)) {
			t.Fatal("找不到 pack.zip")
		}
		a.Draw(s)
	}
	a.HandleKey(keys.Ch('z'))
	if !a.Prompting() {
		t.Fatal("Z 應該問目的地")
	}
	for i := 0; i < 200; i++ {
		a.HandleKey(keys.Named(keys.Backspace))
	}
	out := filepath.Join(root, "unpacked")
	for _, r := range out {
		a.HandleKey(keys.Ch(r))
	}
	a.HandleKey(keys.Named(keys.Enter))

	if b, err := os.ReadFile(filepath.Join(out, "d", "deep.txt")); err != nil || string(b) != "deep\n" {
		t.Errorf("沒解出來: %q %v", b, err)
	}
	if !strings.Contains(a.Message, "2") {
		t.Errorf("訊息 = %q", a.Message)
	}
}

// Alt-Z 打包:標記檔案 → 輸入檔名 → zip 真的建出來且讀得回去。
func TestCreateArchiveFlow(t *testing.T) {
	a, s := newApp(t)
	dir := a.Browser.Dir
	a.HandleKey(keys.Ch('t')) // 標記所有檔案
	a.Draw(s)

	a.HandleKey(keys.AltCh('Z'))
	if !a.Prompting() {
		t.Fatal("Alt-Z 應該問檔名")
	}
	for i := 0; i < 100; i++ {
		a.HandleKey(keys.Named(keys.Backspace))
	}
	for _, r := range "out.zip" {
		a.HandleKey(keys.Ch(r))
	}
	a.HandleKey(keys.Named(keys.Enter))

	zp := filepath.Join(dir, "out.zip")
	if _, err := os.Stat(zp); err != nil {
		t.Fatalf("zip 沒建出來: %v (訊息 %q)", err, a.Message)
	}
	zr, err := zip.OpenReader(zp)
	if err != nil {
		t.Fatal(err)
	}
	defer zr.Close()
	names := map[string]bool{}
	for _, f := range zr.File {
		names[f.Name] = true
	}
	for _, want := range []string{"a.txt", "b.txt", "z.bin"} {
		if !names[want] {
			t.Errorf("zip 裡少了 %s (有 %v)", want, names)
		}
	}
}

// W 尋找:選種類 → 輸入關鍵字 → 結果清單 → Enter 跳到那個檔案。
func TestFindFlow(t *testing.T) {
	a, s := newApp(t)
	a.Draw(s)

	a.HandleKey(keys.Ch('w'))
	if !a.Prompting() {
		t.Fatal("W 應該先問種類")
	}
	a.HandleKey(keys.Ch('s')) // 找字串
	if !a.Prompting() {
		t.Fatal("選了種類之後應該問關鍵字")
	}
	for _, r := range "line2" {
		a.HandleKey(keys.Ch(r))
	}
	a.HandleKey(keys.Named(keys.Enter))

	if a.Mode != ModeFind {
		t.Fatalf("應該進結果清單,現在模式 %v(訊息 %q)", a.Mode, a.Message)
	}
	if len(a.Find.Hits) != 1 {
		t.Fatalf("命中 %d 筆: %+v", len(a.Find.Hits), a.Find.Hits)
	}
	h := a.Find.Hits[0]
	if h.Name != "a.txt" || h.Line != 2 {
		t.Errorf("命中 = %+v", h)
	}
	a.Draw(s)
	a.HandleKey(keys.Named(keys.Enter))
	if a.Mode != ModeBrowser {
		t.Error("Enter 應該跳回瀏覽器")
	}
	if cursorName(a) != "a.txt" {
		t.Errorf("游標應停在命中的檔案上,得到 %q", cursorName(a))
	}
}

// Ctrl-O 轉換:換行樣式真的被改掉。
func TestConvertEOLFlow(t *testing.T) {
	a, s := newApp(t)
	dir := a.Browser.Dir
	for cursorName(a) != "a.txt" {
		a.HandleKey(keys.Named(keys.Down))
		a.Draw(s)
	}
	a.HandleKey(keys.CtrlCh('O'))
	if !a.Prompting() {
		t.Fatal("Ctrl-O 應該開選單")
	}
	a.HandleKey(keys.Ch('p')) // 轉 PC 換行
	b, err := os.ReadFile(filepath.Join(dir, "a.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), "\r\n") {
		t.Errorf("沒轉成 CRLF: %q", b)
	}

	a.HandleKey(keys.CtrlCh('O'))
	a.HandleKey(keys.Ch('u')) // 轉回 UNIX
	b, _ = os.ReadFile(filepath.Join(dir, "a.txt"))
	if strings.Contains(string(b), "\r") {
		t.Errorf("沒轉回 LF: %q", b)
	}
}

// Esc 要能從選單那一步退出來,不可以卡住。
func TestTwoStepPromptsCancel(t *testing.T) {
	a, s := newApp(t)
	a.Draw(s)
	a.HandleKey(keys.Ch('w'))
	a.HandleKey(keys.Named(keys.Esc))
	if a.Prompting() {
		t.Error("尋找的種類選單 Esc 不掉")
	}
	a.HandleKey(keys.CtrlCh('O'))
	a.HandleKey(keys.Named(keys.Esc))
	if a.Prompting() {
		t.Error("轉換的選單 Esc 不掉")
	}
	// Esc 之後一般按鍵要恢復正常
	if !a.HandleKey(keys.Named(keys.Down)) {
		t.Error("Esc 之後方向鍵應該又能用")
	}
}

// --- F1 選單 / F8 / F11 / Alt-E 註解 / P 改路徑 ---------------------------

func TestMenuOpensAndRuns(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "a.txt"), []byte("hello\n"), 0o644)
	a := New(vfs.OS{}, dir)
	a.HandleKey(keys.Named(keys.F1))
	if !a.Menuing() {
		t.Fatal("F1 應該打開選單")
	}
	// 選單開著時,一般的瀏覽器按鍵不該漏過去
	a.HandleKey(keys.Ch('D'))
	if a.Prompting() {
		t.Error("選單開著時 D 不該觸發刪除")
	}
	a.HandleKey(keys.Named(keys.Esc))
	if a.Menuing() {
		t.Error("Esc 應該關掉選單")
	}
}

// 選單裡直接按該功能的鍵,等同選它 —— 選單同時是說明書。
func TestMenuHotkeyPassesThrough(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "a.txt"), []byte("hello\n"), 0o644)
	a := New(vfs.OS{}, dir)
	a.Draw(cell.New(80, 25))
	a.HandleKey(keys.Named(keys.Down)) // 離開 ".."
	a.HandleKey(keys.Named(keys.F1))
	a.HandleKey(keys.Ch('R')) // 更名
	if a.Menuing() {
		t.Error("按了功能鍵之後選單應該關掉")
	}
	if !a.Prompting() {
		t.Error("R 應該開更名輸入列")
	}
}

func TestFullscreenToggle(t *testing.T) {
	a := New(vfs.OS{}, t.TempDir())
	if a.Fullscreen {
		t.Fatal("預設不該是全螢幕")
	}
	a.HandleKey(keys.Named(keys.F11))
	if !a.Fullscreen {
		t.Error("F11 沒切成全螢幕")
	}
	a.HandleKey(keys.Named(keys.F11))
	if a.Fullscreen {
		t.Error("F11 沒切回來")
	}
}

// F8 關掉中文解讀之後,一個位元組畫一格:Big5 的「中」會變成兩格。
func TestEnglishOnlySplitsDoubleBytes(t *testing.T) {
	dir := t.TempDir()
	big5 := []byte{0xA4, 0xA4, 0xA4, 0xE5, '\n'} // "中文"
	os.WriteFile(filepath.Join(dir, "a.txt"), big5, 0o644)
	a := New(vfs.OS{}, dir)
	a.Draw(cell.New(80, 25))
	a.HandleKey(keys.Named(keys.Down))
	a.HandleKey(keys.Named(keys.Enter))
	if a.Mode != ModeViewer {
		t.Fatalf("沒進檢視器,mode=%v", a.Mode)
	}
	if got := a.Viewer.Lines[0].Text(); got != "中文" {
		t.Fatalf("中文顯示 = %q", got)
	}
	a.HandleKey(keys.Named(keys.F8))
	got := []rune(a.Viewer.Lines[0].Text())
	if len(got) != 4 {
		t.Fatalf("英文顯示應該是 4 格,拿到 %d 格 %q", len(got), string(got))
	}
	if got[0] != 0xA4 || got[3] != 0xE5 {
		t.Errorf("位元組沒有原樣對到字碼: %v", got)
	}
}

func TestNoteEditFlow(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "a.txt"), []byte("x"), 0o644)
	a := New(vfs.OS{}, dir)
	a.Draw(cell.New(80, 25))
	a.HandleKey(keys.Named(keys.Down))

	a.HandleKey(keys.AltCh('E'))
	if !a.Prompting() {
		t.Fatal("Alt-E 應該開註解輸入列")
	}
	for _, r := range "說明" {
		a.HandleKey(keys.Ch(r))
	}
	a.HandleKey(keys.Named(keys.Enter))

	b, err := os.ReadFile(filepath.Join(dir, "dir.doc"))
	if err != nil {
		t.Fatalf("沒寫出 dir.doc: %v", err)
	}
	if !strings.Contains(string(b), "a.txt") {
		t.Errorf("dir.doc 內容不對: %q", b)
	}
	// 註解要立刻反映在瀏覽器上,不用重開
	if a.Browser.Notes["a.txt"] != "說明" {
		t.Errorf("Notes 沒更新: %v", a.Browser.Notes)
	}

	// 再按一次 Alt-E,輸入列要帶出現有的註解
	a.HandleKey(keys.AltCh('E'))
	a.HandleKey(keys.Named(keys.Enter))
	if a.Browser.Notes["a.txt"] != "說明" {
		t.Errorf("原樣按 Enter 不該改掉註解: %v", a.Browser.Notes)
	}
}

func TestChangeDirToFileFocusesIt(t *testing.T) {
	dir := t.TempDir()
	sub := filepath.Join(dir, "sub")
	os.Mkdir(sub, 0o755)
	os.WriteFile(filepath.Join(sub, "target.txt"), []byte("x"), 0o644)
	a := New(vfs.OS{}, dir)
	a.Draw(cell.New(80, 25))

	a.HandleKey(keys.Ch('P'))
	if !a.Prompting() {
		t.Fatal("P 應該開改路徑輸入列")
	}
	// 輸入列預設帶目前路徑,先清掉
	for i := 0; i < 200; i++ {
		a.HandleKey(keys.Named(keys.Backspace))
	}
	for _, r := range filepath.Join(sub, "target.txt") {
		a.HandleKey(keys.Ch(r))
	}
	a.HandleKey(keys.Named(keys.Enter))

	if a.Browser.Dir != sub {
		t.Fatalf("沒切到 %s,現在在 %s", sub, a.Browser.Dir)
	}
	if e := a.Browser.Current(); e == nil || e.Name != "target.txt" {
		t.Errorf("游標沒停在 target.txt 上: %+v", e)
	}
}

// --- 編輯器 F6 尋找/取代 -------------------------------------------------

func openEd(t *testing.T, dir, name, body string) *App {
	t.Helper()
	os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644)
	a := New(vfs.OS{}, dir)
	a.Draw(cell.New(80, 25))
	a.HandleKey(keys.Named(keys.Down)) // 離開 ".."
	a.HandleKey(keys.Ch('E'))
	if a.Mode != ModeEdit {
		t.Fatalf("沒進編輯器,mode=%v", a.Mode)
	}
	return a
}

func typeIn(a *App, s string) {
	for _, r := range s {
		a.HandleKey(keys.Ch(r))
	}
	a.HandleKey(keys.Named(keys.Enter))
}

func TestEditFindOnly(t *testing.T) {
	a := openEd(t, t.TempDir(), "a.txt", "one\ntwo\nthree two\n")
	a.HandleKey(keys.Named(keys.F6))
	typeIn(a, "two")  // 尋找
	typeIn(a, "")     // 取代為:空 = 只尋找
	if a.Prompting() {
		t.Fatal("只尋找不該再問確認")
	}
	if a.Editor.Cur.Line != 1 || a.Editor.Cur.Col != 0 {
		t.Fatalf("游標沒停在第一處: %+v", a.Editor.Cur)
	}
	a.HandleKey(keys.CtrlCh('N')) // 續找
	if a.Editor.Cur.Line != 2 || a.Editor.Cur.Col != 6 {
		t.Fatalf("Ctrl-N 沒跳到第二處: %+v", a.Editor.Cur)
	}
	a.HandleKey(keys.CtrlCh('N'))
	if !strings.Contains(a.Message, "找不到") {
		t.Errorf("找完了應該說找不到,拿到 %q", a.Message)
	}
}

func TestEditReplaceOneByOne(t *testing.T) {
	a := openEd(t, t.TempDir(), "a.txt", "aa\naa\naa\n")
	a.HandleKey(keys.Named(keys.F6))
	typeIn(a, "aa")
	typeIn(a, "bb")
	if !a.Prompting() {
		t.Fatal("有取代字串時應該逐一問")
	}
	a.HandleKey(keys.Ch('y')) // 換第一處
	a.HandleKey(keys.Ch('n')) // 跳過第二處
	a.HandleKey(keys.Ch('y')) // 換第三處
	if got := string(a.Editor.Bytes()); got != "bb\naa\nbb\n" {
		t.Errorf("= %q,期望 \"bb\\naa\\nbb\\n\"", got)
	}
}

func TestEditReplaceAll(t *testing.T) {
	a := openEd(t, t.TempDir(), "a.txt", "aa\naa\naa\n")
	a.HandleKey(keys.Named(keys.F6))
	typeIn(a, "aa")
	typeIn(a, "bb")
	a.HandleKey(keys.Ch('a'))
	if got := string(a.Editor.Bytes()); got != "bb\nbb\nbb\n" {
		t.Errorf("= %q", got)
	}
	if !strings.Contains(a.Message, "3") {
		t.Errorf("沒回報換了幾處: %q", a.Message)
	}
}

// 編輯器裡的 `;` 是一個可以打出來的字,不是指令 ——
// 「`;` 或 Alt-E 註解」那條證據在看圖段落,不適用於編輯器。
func TestSemicolonIsLiteralInEditor(t *testing.T) {
	a := openEd(t, t.TempDir(), "a.c", "int a\n")
	a.HandleKey(keys.Named(keys.End))
	a.HandleKey(keys.Ch(';'))
	if got := string(a.Editor.Bytes()); got != "int a;\n" {
		t.Errorf("= %q,分號應該被打進去", got)
	}
}

func TestEditorCtrlEComments(t *testing.T) {
	dir := t.TempDir()
	a := openEd(t, dir, "a.c", "int a;\n")
	a.Editor.Syntax = &syntax.Config{Name: "c", LineComment: "//"}
	a.HandleKey(keys.CtrlCh('E'))
	if got := string(a.Editor.Bytes()); got != "// int a;\n" {
		t.Errorf("= %q", got)
	}
	a.HandleKey(keys.CtrlCh('E'))
	if got := string(a.Editor.Bytes()); got != "int a;\n" {
		t.Errorf("再按一次沒拿掉: %q", got)
	}
}
