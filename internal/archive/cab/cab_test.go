package cab_test

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"

	"github.com/wicanr2/wincv-remake/internal/archive/cab"
)

// 測試資料是 gcab 造的,內容用 cabextract 對過。期望值是原始檔的 sha256。
var want = map[string]struct {
	size int64
	sum  string
}{
	// big.txt 229880 個位元組 ⇒ MSZIP 會切成七個以上的區塊,
	// 每個區塊的 LZ77 視窗接續前一個區塊的輸出。字典沒接對的話
	// 前 32 KB 仍然正常,之後才開始壞。
	"big.txt":     {229880, "a0a61c73569bdbfbd4252b6b0c664bf1"},
	"small.txt":   {2000, "799f9cc5416fe970d46ebaaa5d3a4e6b"},
	"sub/mid.txt": {100000, "dad9797d73725309f3e5e2e8886c0589"},
}

func read(t *testing.T, name string) []cab.File {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", "..", "testdata", "cab", name))
	if err != nil {
		t.Fatal(err)
	}
	fs, err := cab.Read(data)
	if err != nil {
		t.Fatal(err)
	}
	return fs
}

func TestMSZIP(t *testing.T) {
	fs := read(t, "mszip.cab")
	if len(fs) != 3 {
		t.Fatalf("讀到 %d 筆,期望 3", len(fs))
	}
	for _, f := range fs {
		w, ok := want[f.Name]
		if !ok {
			t.Errorf("多出沒預期的 %q", f.Name)
			continue
		}
		if f.Size != w.size || int64(len(f.Data)) != w.size {
			t.Errorf("%s: size=%d len=%d,期望 %d", f.Name, f.Size, len(f.Data), w.size)
			continue
		}
		sum := sha256.Sum256(f.Data)
		if h := hex.EncodeToString(sum[:16]); h != w.sum {
			t.Errorf("%s: 內容不符(%s)", f.Name, h)
		}
	}
}

func TestStored(t *testing.T) {
	fs := read(t, "store.cab")
	if len(fs) != 1 {
		t.Fatalf("讀到 %d 筆,期望 1", len(fs))
	}
	sum := sha256.Sum256(fs[0].Data)
	if h := hex.EncodeToString(sum[:16]); h != want["small.txt"].sum {
		t.Errorf("不壓縮的 folder 內容不符(%s)", h)
	}
}

// 反過來的證據:目錄分隔字元要正規化成 /,不然 vfs 那層會把
// "sub\mid.txt" 當成單一檔名。
func TestBackslashNormalized(t *testing.T) {
	for _, f := range read(t, "mszip.cab") {
		if f.Name == "sub/mid.txt" {
			return
		}
	}
	t.Error("沒有把 sub\\mid.txt 正規化成 sub/mid.txt")
}

func TestRejectsNonCab(t *testing.T) {
	if _, err := cab.Read([]byte("not a cab file at all............")); err == nil {
		t.Error("不是 CAB 應該報錯")
	}
}
