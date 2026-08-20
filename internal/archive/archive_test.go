package archive

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func writeZip(t *testing.T, dir string) string {
	t.Helper()
	p := filepath.Join(dir, "a.zip")
	f, err := os.Create(p)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	zw := zip.NewWriter(f)
	// 刻意「只存檔案、不存目錄項」—— 很多壓縮工具就是這樣,
	// 中間層目錄必須由我們自己補出來。
	for name, body := range map[string]string{
		"readme.txt":     "hello\n",
		"docs/guide.txt": "guide\n",
		"docs/img/a.txt": "img\n",
	} {
		h := &zip.FileHeader{Name: name, Method: zip.Deflate}
		h.Modified = time.Date(2011, 11, 25, 10, 0, 0, 0, time.UTC)
		w, err := zw.CreateHeader(h)
		if err != nil {
			t.Fatal(err)
		}
		io.WriteString(w, body)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return p
}

func writeTarGz(t *testing.T, dir string) string {
	t.Helper()
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	for _, e := range []struct{ name, body string }{
		{"x.txt", "xxx\n"},
		{"sub/y.txt", "yyy\n"},
	} {
		if err := tw.WriteHeader(&tar.Header{
			Name: e.name, Size: int64(len(e.body)), Mode: 0o644,
			ModTime: time.Date(2011, 1, 1, 0, 0, 0, 0, time.UTC),
		}); err != nil {
			t.Fatal(err)
		}
		io.WriteString(tw, e.body)
	}
	tw.Close()

	p := filepath.Join(dir, "b.tar.gz")
	f, err := os.Create(p)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	gw := gzip.NewWriter(f)
	gw.Write(buf.Bytes())
	gw.Close()
	return p
}

func writeTxtGz(t *testing.T, dir string) string {
	t.Helper()
	p := filepath.Join(dir, "note.txt.gz")
	f, err := os.Create(p)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	gw := gzip.NewWriter(f)
	io.WriteString(gw, "just a text file\n")
	gw.Close()
	return p
}

func namesOf(t *testing.T, a *FS, dir string) []string {
	t.Helper()
	es, err := a.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, e := range es {
		n := e.Name
		if e.IsDir {
			n += "/"
		}
		out = append(out, n)
	}
	return out
}

func TestZipBrowse(t *testing.T) {
	dir := t.TempDir()
	a, err := Open(writeZip(t, dir))
	if err != nil {
		t.Fatal(err)
	}

	got := strings.Join(namesOf(t, a, a.Root()), " ")
	if got != "docs/ readme.txt" {
		t.Errorf("最上層 = %q, 應為 \"docs/ readme.txt\"", got)
	}

	got = strings.Join(namesOf(t, a, a.Path("docs")), " ")
	if got != "guide.txt img/" {
		t.Errorf("docs/ = %q, 應為 \"guide.txt img/\"", got)
	}

	got = strings.Join(namesOf(t, a, a.Path("docs/img")), " ")
	if got != "a.txt" {
		t.Errorf("docs/img/ = %q, 應為 \"a.txt\"", got)
	}
}

func TestZipOpenFile(t *testing.T) {
	dir := t.TempDir()
	a, err := Open(writeZip(t, dir))
	if err != nil {
		t.Fatal(err)
	}
	rc, err := a.Open(a.Path("docs/guide.txt"))
	if err != nil {
		t.Fatal(err)
	}
	defer rc.Close()
	b, _ := io.ReadAll(rc)
	if string(b) != "guide\n" {
		t.Errorf("讀出 %q", b)
	}
}

// .tar.gz 要展開成裡面的多個檔,不是一個叫 b.tar 的檔。
func TestTarGzExpands(t *testing.T) {
	dir := t.TempDir()
	a, err := Open(writeTarGz(t, dir))
	if err != nil {
		t.Fatal(err)
	}
	got := strings.Join(namesOf(t, a, a.Root()), " ")
	if got != "sub/ x.txt" {
		t.Errorf("最上層 = %q, 應為 \"sub/ x.txt\"", got)
	}
	rc, err := a.Open(a.Path("sub/y.txt"))
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(rc)
	if string(b) != "yyy\n" {
		t.Errorf("讀出 %q", b)
	}
}

// .txt.gz 裡面不是 tar,應該只有一個檔,名字去掉 .gz。
func TestPlainGzIsSingleFile(t *testing.T) {
	dir := t.TempDir()
	a, err := Open(writeTxtGz(t, dir))
	if err != nil {
		t.Fatal(err)
	}
	got := namesOf(t, a, a.Root())
	if len(got) != 1 || got[0] != "note.txt" {
		t.Fatalf("最上層 = %v, 應為 [note.txt]", got)
	}
	rc, _ := a.Open(a.Path("note.txt"))
	b, _ := io.ReadAll(rc)
	if string(b) != "just a text file\n" {
		t.Errorf("讀出 %q", b)
	}
}

func TestDetectFormatLongestWins(t *testing.T) {
	for _, tc := range []struct {
		name string
		want string
	}{
		{"a.tar.gz", "GZIP"},
		{"a.gz", "GZIP"},
		{"a.zip", "ZIP"},
		{"a.RAR", "RAR"},
		{"a.txt", ""},
	} {
		f, ok := DetectFormat(tc.name)
		if tc.want == "" {
			if ok {
				t.Errorf("%s 不該被當成壓縮檔,卻判成 %s", tc.name, f.Name)
			}
			continue
		}
		if !ok || f.Name != tc.want {
			t.Errorf("%s 判成 %q(ok=%v), 應為 %q", tc.name, f.Name, ok, tc.want)
		}
	}
}

// 還沒實作的格式要給清楚的訊息,不是含糊的失敗。
func TestUnsupportedFormatMessage(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "x.lzh")
	os.WriteFile(p, []byte("dummy"), 0o644)
	_, err := Open(p)
	if err == nil {
		t.Fatal("LHA 目前應該回錯誤")
	}
	if !strings.Contains(err.Error(), "LHA") || !strings.Contains(err.Error(), "還沒實作") {
		t.Errorf("訊息不夠清楚: %v", err)
	}
}

// 用真實的 .7z(自行產生)。
func TestSevenZip(t *testing.T) {
	a, err := Open("testdata/sample.7z")
	if err != nil {
		t.Fatal(err)
	}
	got := strings.Join(namesOf(t, a, a.Root()), " ")
	if got != "docs/ top.txt" {
		t.Errorf("最上層 = %q, 應為 \"docs/ top.txt\"", got)
	}
	rc, err := a.Open(a.Path("docs/deep.txt"))
	if err != nil {
		t.Fatal(err)
	}
	defer rc.Close()
	b, _ := io.ReadAll(rc)
	if string(b) != "deep\n" {
		t.Errorf("讀出 %q", b)
	}
}

// 用真實世界的 .rar(1999 年的檔,solid 壓縮),來源見 testdata/README.md。
func TestRar(t *testing.T) {
	a, err := Open("testdata/sample.rar")
	if err != nil {
		t.Fatal(err)
	}
	got := namesOf(t, a, a.Root())
	if len(got) != 2 {
		t.Fatalf("最上層 = %v, 應有 2 筆", got)
	}
	rc, err := a.Open(a.Path("EOB3FIX.DOC"))
	if err != nil {
		t.Fatal(err)
	}
	defer rc.Close()
	b, _ := io.ReadAll(rc)
	if len(b) != 303 {
		t.Errorf("EOB3FIX.DOC 讀出 %d bytes, 壓縮檔目錄說是 303", len(b))
	}
}

// 這一條是進度看板:哪些做了、哪些還沒。改了 Formats 就會動到這裡,
// 逼人順手更新狀態,而不是讓表格悄悄過期。
func TestFormatCoverage(t *testing.T) {
	done, total := 0, len(Formats)
	for _, f := range Formats {
		if f.Supported {
			done++
		}
		if !f.Supported && f.Note == "" {
			t.Errorf("%s 還沒支援,但沒寫下一步要怎麼做", f.Name)
		}
	}
	t.Logf("壓縮格式支援 %d/%d", done, total)
	if done < 6 {
		t.Errorf("已支援 %d 種,ZIP/TAR/GZ/BZ2/RAR/7z 這六種應該都要在", done)
	}
}
