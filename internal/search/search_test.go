package search

import (
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/text/encoding/traditionalchinese"
)

func fixture(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	os.MkdirAll(filepath.Join(root, "sub"), 0o755)
	os.WriteFile(filepath.Join(root, "readme.txt"), []byte("hello world\nsecond line\n"), 0o644)
	os.WriteFile(filepath.Join(root, "notes.md"), []byte("nothing here\n"), 0o644)
	os.WriteFile(filepath.Join(root, "sub", "deep-readme.txt"), []byte("deep hello\n"), 0o644)
	// Big5 中文檔
	b5, _ := traditionalchinese.Big5.NewEncoder().Bytes([]byte("第一行\n檔案瀏覽器\n第三行\n"))
	os.WriteFile(filepath.Join(root, "cht.txt"), b5, 0o644)
	// 註解檔
	note, _ := traditionalchinese.Big5.NewEncoder().Bytes([]byte("readme.txt  說明檔\nnotes.md    筆記\n"))
	os.WriteFile(filepath.Join(root, "dir.doc"), note, 0o644)
	// 二進位檔
	bin := make([]byte, 512)
	for i := range bin {
		bin[i] = byte(i % 5)
	}
	os.WriteFile(filepath.Join(root, "blob.bin"), bin, 0o644)
	return root
}

func TestByName(t *testing.T) {
	root := fixture(t)
	hits, err := Run(root, "readme", Options{Kind: ByName})
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 1 || hits[0].Name != "readme.txt" {
		t.Errorf("非遞迴 = %+v", hits)
	}
	hits, _ = Run(root, "readme", Options{Kind: ByName, Recursive: true})
	if len(hits) != 2 {
		t.Errorf("遞迴應找到 2 個: %+v", hits)
	}
}

func TestByContent(t *testing.T) {
	root := fixture(t)
	hits, err := Run(root, "hello", Options{Kind: ByContent})
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 1 {
		t.Fatalf("= %+v", hits)
	}
	if hits[0].Line != 1 || hits[0].Text != "hello world" {
		t.Errorf("行號與內容 = %d %q", hits[0].Line, hits[0].Text)
	}
}

// Big5 檔案裡的中文要搜得到。少了編碼判讀這一步,它會**安靜地**找不到。
func TestByContentBig5(t *testing.T) {
	root := fixture(t)
	hits, err := Run(root, "檔案瀏覽", Options{Kind: ByContent})
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 1 {
		t.Fatalf("Big5 中文沒搜到: %+v", hits)
	}
	if hits[0].Line != 2 {
		t.Errorf("行號 = %d, 應為 2", hits[0].Line)
	}
	if hits[0].Text != "檔案瀏覽器" {
		t.Errorf("命中的那一行 = %q", hits[0].Text)
	}
}

// 二進位檔不該被當文字搜。
func TestByContentSkipsBinary(t *testing.T) {
	root := fixture(t)
	hits, _ := Run(root, "\x01", Options{Kind: ByContent})
	for _, h := range hits {
		if h.Name == "blob.bin" {
			t.Error("二進位檔不該被搜")
		}
	}
}

func TestByComment(t *testing.T) {
	root := fixture(t)
	hits, err := Run(root, "說明", Options{Kind: ByComment})
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 1 || hits[0].Name != "readme.txt" {
		t.Errorf("= %+v", hits)
	}
	if hits[0].Text != "說明檔" {
		t.Errorf("註解 = %q", hits[0].Text)
	}
}

func TestMaxHits(t *testing.T) {
	root := t.TempDir()
	for i := 0; i < 20; i++ {
		os.WriteFile(filepath.Join(root, string(rune('a'+i))+"-x.txt"), []byte("x"), 0o644)
	}
	hits, _ := Run(root, "x", Options{Kind: ByName, MaxHits: 5})
	if len(hits) != 5 {
		t.Errorf("上限 5,得到 %d", len(hits))
	}
}
