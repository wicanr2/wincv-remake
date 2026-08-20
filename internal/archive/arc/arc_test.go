package arc_test

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"

	"github.com/wicanr2/wincv-remake/internal/archive/arc"
)

// 期望值是原始檔的 sha256。測試資料由 arc 5.21(參考實作)產生。
var want = map[string]struct {
	size int64
	sum  string
}{
	"m.txt":    {100000, "dad9797d73725309f3e5e2e8886c0589"},
	"s.txt":    {2000, "799f9cc5416fe970d46ebaaa5d3a4e6b"},
	"tiny.txt": {400, ""},
	"z.gz":     {369, "b4088587c1a991cc3358e1c193c7163c"},
}

func TestCrunched(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "..", "testdata", "arc", "t.arc"))
	if err != nil {
		t.Fatal(err)
	}
	fs, err := arc.Read(data)
	if err != nil {
		t.Fatal(err)
	}
	if len(fs) != 3 {
		t.Fatalf("讀到 %d 筆: %v", len(fs), names(fs))
	}
	for _, f := range fs {
		w, ok := want[f.Name]
		if !ok {
			t.Errorf("多出沒預期的 %q", f.Name)
			continue
		}
		if int64(len(f.Data)) != w.size {
			t.Errorf("%s(方法 %d): 解出 %d 個位元組,期望 %d",
				f.Name, f.Method, len(f.Data), w.size)
			continue
		}
		if w.sum == "" {
			continue
		}
		sum := sha256.Sum256(f.Data)
		if h := hex.EncodeToString(sum[:16]); h != w.sum {
			t.Errorf("%s(方法 %d): 內容不符(%s)", f.Name, f.Method, h)
		}
	}
}

// 壓不小的檔案 arc 會原樣存(方法 2),和壓過的成員混在同一個檔裡。
func TestStoredMixedWithCrunched(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "..", "testdata", "arc", "mix.arc"))
	if err != nil {
		t.Fatal(err)
	}
	fs, err := arc.Read(data)
	if err != nil {
		t.Fatal(err)
	}
	if len(fs) != 2 {
		t.Fatalf("讀到 %d 筆: %v", len(fs), names(fs))
	}
	sawStored := false
	for _, f := range fs {
		if f.Method == 2 {
			sawStored = true
		}
		w := want[f.Name]
		sum := sha256.Sum256(f.Data)
		if h := hex.EncodeToString(sum[:16]); h != w.sum {
			t.Errorf("%s(方法 %d): 內容不符(%s)", f.Name, f.Method, h)
		}
	}
	if !sawStored {
		t.Error("沒有讀到不壓縮的那一筆")
	}
}

func names(fs []arc.File) []string {
	var out []string
	for _, f := range fs {
		out = append(out, f.Name)
	}
	return out
}

func TestRejectsNonArc(t *testing.T) {
	if _, err := arc.Read([]byte("not an arc file")); err == nil {
		t.Error("不是 ARC 應該報錯")
	}
}
