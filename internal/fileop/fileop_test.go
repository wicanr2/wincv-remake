package fileop

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func mk(t *testing.T) (src, dst string) {
	t.Helper()
	root := t.TempDir()
	src, dst = filepath.Join(root, "a"), filepath.Join(root, "b")
	for _, d := range []string{src, dst} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	write(t, src, "one.txt", "111")
	write(t, src, "two.txt", "222")
	return
}

func write(t *testing.T, dir, name, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func read(t *testing.T, p string) string {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestCopy(t *testing.T) {
	src, dst := mk(t)
	r := Copy(src, dst, []string{"one.txt", "two.txt"}, Options{})
	if r.Failed() || len(r.Done) != 2 {
		t.Fatalf("%+v", r)
	}
	if read(t, filepath.Join(dst, "one.txt")) != "111" {
		t.Error("內容不對")
	}
	if read(t, filepath.Join(src, "one.txt")) != "111" {
		t.Error("拷貝不該動到來源")
	}
}

// 沒有人可問覆蓋與否時,預設是**不覆蓋**。
// 默默蓋掉使用者的檔案是不可接受的預設。
func TestOverwriteDefaultsToSkip(t *testing.T) {
	src, dst := mk(t)
	write(t, dst, "one.txt", "existing")
	r := Copy(src, dst, []string{"one.txt"}, Options{})
	if len(r.Skipped) != 1 {
		t.Fatalf("應該跳過,得到 %+v", r)
	}
	if read(t, filepath.Join(dst, "one.txt")) != "existing" {
		t.Error("目的檔被蓋掉了")
	}
}

// 「全部覆蓋」之後就不再問。
func TestOverwriteApplyToAll(t *testing.T) {
	src, dst := mk(t)
	write(t, dst, "one.txt", "x")
	write(t, dst, "two.txt", "y")
	asked := 0
	r := Copy(src, dst, []string{"one.txt", "two.txt"}, Options{
		Confirm: func(string) (bool, bool) { asked++; return true, true },
	})
	if asked != 1 {
		t.Errorf("問了 %d 次,選了「全部覆蓋」之後就不該再問", asked)
	}
	if len(r.Done) != 2 {
		t.Errorf("%+v", r)
	}
	if read(t, filepath.Join(dst, "two.txt")) != "222" {
		t.Error("第二個檔沒被覆蓋")
	}
}

func TestMove(t *testing.T) {
	src, dst := mk(t)
	r := Move(src, dst, []string{"one.txt"}, Options{})
	if r.Failed() {
		t.Fatalf("%+v", r)
	}
	if _, err := os.Stat(filepath.Join(src, "one.txt")); err == nil {
		t.Error("搬移之後來源還在")
	}
	if read(t, filepath.Join(dst, "one.txt")) != "111" {
		t.Error("內容不對")
	}
}

func TestCopyDirRecursive(t *testing.T) {
	src, dst := mk(t)
	sub := filepath.Join(src, "sub", "deep")
	os.MkdirAll(sub, 0o755)
	write(t, sub, "x.txt", "deep")
	r := Copy(src, dst, []string{"sub"}, Options{})
	if r.Failed() {
		t.Fatalf("%+v", r)
	}
	if read(t, filepath.Join(dst, "sub", "deep", "x.txt")) != "deep" {
		t.Error("遞迴拷貝失敗")
	}
}

func TestRename(t *testing.T) {
	src, _ := mk(t)
	if err := Rename(src, "one.txt", "renamed.txt"); err != nil {
		t.Fatal(err)
	}
	if read(t, filepath.Join(src, "renamed.txt")) != "111" {
		t.Error("內容不對")
	}
	// 目的地已存在要拒絕
	if err := Rename(src, "renamed.txt", "two.txt"); err == nil {
		t.Error("改成已存在的名字應該失敗")
	}
	// 檔名不可以帶路徑分隔
	if err := Rename(src, "two.txt", "../escape.txt"); err == nil {
		t.Error("帶路徑分隔的檔名應該被拒絕")
	}
}

// 填 0 刪除:刪之前內容要先被覆寫掉。
func TestDeleteZeroFill(t *testing.T) {
	src, _ := mk(t)
	p := filepath.Join(src, "one.txt")
	// 先驗 zeroFill 本身
	if err := zeroFill(p); err != nil {
		t.Fatal(err)
	}
	if got := read(t, p); got != "\x00\x00\x00" {
		t.Errorf("填 0 之後內容 = %q", got)
	}
	r := Delete(src, []string{"one.txt"}, Options{ZeroFill: true})
	if r.Failed() {
		t.Fatalf("%+v", r)
	}
	if _, err := os.Stat(p); err == nil {
		t.Error("檔案還在")
	}
}

func TestCompare(t *testing.T) {
	src, _ := mk(t)
	write(t, src, "same1.txt", "hello world")
	write(t, src, "same2.txt", "hello world")
	write(t, src, "diff.txt", "hello WORLD")
	write(t, src, "short.txt", "hello")

	same, at, err := Compare(filepath.Join(src, "same1.txt"), filepath.Join(src, "same2.txt"))
	if err != nil || !same || at != -1 {
		t.Errorf("相同的檔案: same=%v at=%d err=%v", same, at, err)
	}
	same, at, err = Compare(filepath.Join(src, "same1.txt"), filepath.Join(src, "diff.txt"))
	if err != nil || same {
		t.Errorf("不同的檔案應回 false: %v %v", same, err)
	}
	if at != 6 {
		t.Errorf("第一個相異位移 = %d, 應為 6", at)
	}
	same, at, err = Compare(filepath.Join(src, "same1.txt"), filepath.Join(src, "short.txt"))
	if err != nil || same {
		t.Errorf("長度不同應回 false")
	}
	if at != 5 {
		t.Errorf("長度不同時的位移 = %d, 應為 5(短的那個的長度)", at)
	}
}

func TestSummary(t *testing.T) {
	r := &Result{Done: []string{"a", "b"}, Skipped: []string{"c"}, Errors: map[string]error{}}
	if got := r.Summary("已拷貝"); !strings.Contains(got, "2 個檔案") || !strings.Contains(got, "跳過 1") {
		t.Errorf("摘要 = %q", got)
	}
}
