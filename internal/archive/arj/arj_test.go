package arj_test

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"

	"github.com/wicanr2/wincv-remake/internal/archive/arj"
)

// 測試資料是 arj 3.10(參考實作)壓出來的。期望值是原始檔的 sha256。
var want = map[string]struct {
	size int64
	sum  string
}{
	"big.txt":   {229880, "a0a61c73569bdbfbd4252b6b0c664bf1"},
	"small.txt": {2000, "799f9cc5416fe970d46ebaaa5d3a4e6b"},
	// arj 存的是含路徑的名字,列表指令顯示時才省略掉目錄
	"sub/mid.txt": {100000, "dad9797d73725309f3e5e2e8886c0589"},
}

func check(t *testing.T, file string, wantN int) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", "..", "testdata", "arj", file))
	if err != nil {
		t.Fatal(err)
	}
	fs, err := arj.Read(data)
	if err != nil {
		t.Fatal(err)
	}
	if len(fs) != wantN {
		t.Fatalf("讀到 %d 筆,期望 %d", len(fs), wantN)
	}
	for _, f := range fs {
		w, ok := want[f.Name]
		if !ok {
			t.Errorf("多出沒預期的 %q", f.Name)
			continue
		}
		if int64(len(f.Data)) != w.size {
			t.Errorf("%s: 解出 %d 個位元組,期望 %d", f.Name, len(f.Data), w.size)
			continue
		}
		sum := sha256.Sum256(f.Data)
		if h := hex.EncodeToString(sum[:16]); h != w.sum {
			t.Errorf("%s(方法 %d): 內容不符(%s)", f.Name, f.Method, h)
		}
	}
}

// 方法 1 是 arj 的預設。它與 -lh5- 的 Huffman 一樣,但距離表的參數
// 是 NP=17 / PBIT=5 而不是 14 / 4 —— 照 lh5 解會一開始就錯。
func TestMethod1(t *testing.T) { check(t, "m1.arj", 3) }

// 方法 4 沒有 Huffman,長度與距離用「前綴幾個 1 決定欄位寬度」。
func TestMethod4(t *testing.T) { check(t, "m4.arj", 2) }

// 方法 0 不壓縮。
func TestMethod0(t *testing.T) { check(t, "m0.arj", 1) }

func TestRejectsNonArj(t *testing.T) {
	if _, err := arj.Read([]byte("not an arj file")); err == nil {
		t.Error("不是 ARJ 應該報錯")
	}
}
