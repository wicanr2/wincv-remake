package archive

import (
	"archive/zip"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestExtractAll(t *testing.T) {
	dir := t.TempDir()
	a, err := Open(writeZip(t, dir))
	if err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(dir, "out")
	n, err := a.Extract(out, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if n != 3 {
		t.Errorf("解出 %d 個, 應為 3", n)
	}
	b, err := os.ReadFile(filepath.Join(out, "docs", "img", "a.txt"))
	if err != nil || string(b) != "img\n" {
		t.Errorf("巢狀目錄沒解對: %q %v", b, err)
	}
}

func TestExtractSubset(t *testing.T) {
	dir := t.TempDir()
	a, _ := Open(writeZip(t, dir))
	out := filepath.Join(dir, "out2")
	// 指定一個目錄,底下的東西都要跟著出來
	n, err := a.Extract(out, []string{"docs"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Errorf("解出 %d 個, docs 底下應有 2 個", n)
	}
	if _, err := os.Stat(filepath.Join(out, "readme.txt")); err == nil {
		t.Error("沒指定的檔案不該被解出來")
	}
}

// 壓縮檔裡的路徑不可信:`..` 可以寫到目標目錄之外(zip slip)。
func TestExtractRejectsPathEscape(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "evil.zip")
	f, _ := os.Create(p)
	zw := zip.NewWriter(f)
	w, _ := zw.Create("../escaped.txt")
	io.WriteString(w, "pwned")
	zw.Close()
	f.Close()

	a, err := Open(p)
	if err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(dir, "out")
	if _, err := a.Extract(out, nil, nil); err == nil {
		// path.Clean 會把開頭的 ../ 吃掉,所以也可能是「安全地解在裡面」。
		if _, err := os.Stat(filepath.Join(dir, "escaped.txt")); err == nil {
			t.Fatal("檔案被寫到目標目錄之外了")
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "escaped.txt")); err == nil {
		t.Error("檔案被寫到目標目錄之外了")
	}
}

func TestCreateZipRoundTrip(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src")
	os.MkdirAll(filepath.Join(src, "sub"), 0o755)
	os.WriteFile(filepath.Join(src, "a.txt"), []byte("aaa"), 0o644)
	os.WriteFile(filepath.Join(src, "sub", "b.txt"), []byte("bbb"), 0o644)

	out := filepath.Join(dir, "made.zip")
	if err := CreateZip(out, src, []string{"a.txt", "sub"}, nil); err != nil {
		t.Fatal(err)
	}

	a, err := Open(out)
	if err != nil {
		t.Fatal(err)
	}
	got := strings.Join(namesOf(t, a, a.Root()), " ")
	if got != "a.txt sub/" {
		t.Errorf("打包出來的最上層 = %q", got)
	}
	rc, err := a.Open(a.Path("sub/b.txt"))
	if err != nil {
		t.Fatal(err)
	}
	defer rc.Close()
	b, _ := io.ReadAll(rc)
	if string(b) != "bbb" {
		t.Errorf("內容 = %q", b)
	}
}
