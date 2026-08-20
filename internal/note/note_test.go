package note

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/wicanr2/wincv-remake/internal/textenc"
)

func TestRoundTrip(t *testing.T) {
	dir := t.TempDir()
	s, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if s.Len() != 0 {
		t.Fatalf("空目錄應該沒有註解,拿到 %d 筆", s.Len())
	}
	s.Set("a.txt", "第一個檔")
	s.Set("b.txt", "第二個檔")
	if err := s.Save(dir); err != nil {
		t.Fatal(err)
	}

	s2, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got := s2.Get("a.txt"); got != "第一個檔" {
		t.Errorf("a.txt 註解 = %q", got)
	}
	if got := s2.Get("b.txt"); got != "第二個檔" {
		t.Errorf("b.txt 註解 = %q", got)
	}
}

// 清掉最後一筆註解時,不要留下一個空的 dir.doc。
func TestSaveRemovesEmptyFile(t *testing.T) {
	dir := t.TempDir()
	s, _ := Load(dir)
	s.Set("a.txt", "x")
	if err := s.Save(dir); err != nil {
		t.Fatal(err)
	}
	s.Set("a.txt", "")
	if err := s.Save(dir); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, File)); !os.IsNotExist(err) {
		t.Errorf("沒有註解了還留著檔案: %v", err)
	}
}

// 原版的 dir.doc 是 Big5。寫回去要維持 Big5,不然原版讀到亂碼。
func TestKeepsBig5Encoding(t *testing.T) {
	dir := t.TempDir()
	big5 := []byte{'a', '.', 't', 'x', 't', ' ', 0xA4, 0xA4, 0xA4, 0xE5, '\n'} // "中文"
	if err := os.WriteFile(filepath.Join(dir, File), big5, 0o644); err != nil {
		t.Fatal(err)
	}
	s, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got := s.Get("a.txt"); got != "中文" {
		t.Fatalf("讀出來 = %q,期望 \"中文\"", got)
	}
	s.Set("b.txt", "測試")
	if err := s.Save(dir); err != nil {
		t.Fatal(err)
	}
	out, _ := os.ReadFile(filepath.Join(dir, File))
	if enc := textenc.Detect(out); enc != textenc.Big5 {
		t.Errorf("寫回去變成 %v,應該還是 Big5", enc)
	}
	if textenc.Decode(out, textenc.Big5) != "a.txt 中文\nb.txt 測試\n" {
		t.Errorf("內容不對: %q", textenc.Decode(out, textenc.Big5))
	}
}

// 檔名裡有空白時,以第一段空白切 —— 和原版一樣,這個格式沒有跳脫。
func TestFirstSpaceSplits(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, File), []byte("my file.txt 有空白的檔名\n"), 0o644)
	s, _ := Load(dir)
	if got := s.Get("my"); got != "file.txt 有空白的檔名" {
		t.Errorf("拿到 %q", got)
	}
}
