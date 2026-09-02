package datadir

import (
	"os"
	"path/filepath"
	"testing"
)

func write(t *testing.T, dir, name string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, name), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
}

// 環境變數指定的位置要贏過其他所有地方 —— 那是使用者明講的。
func TestEnvHomeWins(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "cvga.fon")
	t.Setenv(EnvHome, dir)
	if got, ok := Find("cvga.fon"); !ok || got != dir {
		t.Errorf("Find = %q (%v),期待 %q", got, ok, dir)
	}
	if got := Resolve("cvga.fon"); got != filepath.Join(dir, "cvga.fon") {
		t.Errorf("Resolve = %q", got)
	}
}

// 找不到的時候仍然要回一個位置,錯誤訊息才有東西可講。
func TestFindFallsBackToSomething(t *testing.T) {
	t.Setenv(EnvHome, t.TempDir())
	got, ok := Find("這個檔案不存在.fon")
	if ok {
		t.Fatal("不該找得到")
	}
	if got == "" {
		t.Error("找不到時也要回一個位置")
	}
}

// 使用者明講路徑的時候不准改寫。
func TestResolveKeepsExplicitPath(t *testing.T) {
	for _, p := range []string{
		filepath.Join("some", "where", "cvga.fon"),
		filepath.Join(string(filepath.Separator), "abs", "cvga.fon"),
	} {
		if got := Resolve(p); got != p {
			t.Errorf("Resolve(%q) = %q,不該動它", p, got)
		}
	}
	if got := Resolve(""); got != "" {
		t.Errorf("空字串要照樣回空字串,拿到 %q", got)
	}
}

// 搜尋順序裡一定要有「執行檔旁邊」與「工作目錄」這兩個。
// 前者是打包安裝之後唯一穩定的位置,後者是開發時從 repo 跑的位置。
func TestDirsCoverExeAndCwd(t *testing.T) {
	t.Setenv(EnvHome, "")
	dirs := Dirs()
	exe, err := os.Executable()
	if err != nil {
		t.Skip("這個平台問不到執行檔位置")
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolved
	}
	want := map[string]bool{
		filepath.Dir(exe):                false,
		filepath.Join("original", "app"): false,
		".":                              false,
	}
	for _, d := range dirs {
		if _, ok := want[d]; ok {
			want[d] = true
		}
	}
	for d, ok := range want {
		if !ok {
			t.Errorf("搜尋順序裡少了 %q\n實際:%v", d, dirs)
		}
	}
}

// 同一個目錄不該出現兩次:每一個位置都要 stat 一次,重複只是白花時間,
// 而且會讓「找過哪些地方」的訊息看起來像壞掉。
func TestDirsAreDeduped(t *testing.T) {
	t.Setenv(EnvHome, ".")
	seen := map[string]int{}
	for _, d := range Dirs() {
		seen[d]++
	}
	for d, n := range seen {
		if n > 1 {
			t.Errorf("%q 出現 %d 次", d, n)
		}
	}
}
