package ace_test

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"

	"github.com/wicanr2/wincv-remake/internal/archive/ace"
)

func read(t *testing.T, name string) []ace.File {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", "..", "testdata", "ace", name))
	if err != nil {
		t.Fatal(err)
	}
	fs, err := ace.Read(data)
	if err != nil {
		t.Fatal(err)
	}
	return fs
}

// 每個成員解出來之後都會比對標頭裡的 CRC-32,所以 Read 沒回錯誤
// 就表示內容與壓縮檔自己記的檢查碼相符。下面的 sha256 是再拿
// acefile(獨立實作)對過的值。
var want = map[string]struct {
	size int
	sum  string
}{
	"winappdbg-winappdbg_v1.6/doc/Makefile":            {3136, "d476fb416efdb1c7"},
	"winappdbg-winappdbg_v1.6/winappdbg/plugins/README": {49, "8efd4ce2c5c19a44"},
	"winappdbg-winappdbg_v1.6/distro.bat":              {8637, "ddc6c757b15ac84a"},
	"winappdbg-winappdbg_v1.6/install.bat":             {1848, "47717acbd26023ca"},
	"winappdbg-winappdbg_v1.6/doc/make.bat":            {3194, "ea81a1047b6b2369"},
	"winappdbg-winappdbg_v1.6/epydoc.cfg":              {351, "bc682ec8b2a49092"},
	"winappdbg-winappdbg_v1.6/tools/example.cfg":       {4966, "4bc0d5fdc0ca3180"},
	"winappdbg-winappdbg_v1.6/install.cfg":             {664, "f6ee9501635926b2"},
}

// blocked.ace 是 ACE 2.0 的 blocked 模式,含 8 個有內容的成員
// (7 個 LZ77、1 個 stored)與 21 個目錄項。
//
// 這份樣本走到的是 MODE_LZ77;MODE_LZ77_EXE 與 MODE_LZ77_DELTA
// 由 TestFullCorpus 驗,那份語料太大不進版控。
func TestBlocked(t *testing.T) {
	fs := read(t, "blocked.ace")
	got := 0
	for _, f := range fs {
		w, ok := want[f.Name]
		if !ok {
			if len(f.Data) != 0 {
				t.Errorf("多出沒預期的成員 %q(%d 個位元組)", f.Name, len(f.Data))
			}
			continue
		}
		got++
		if len(f.Data) != w.size {
			t.Errorf("%s: 解出 %d 個位元組,期望 %d", f.Name, len(f.Data), w.size)
			continue
		}
		sum := sha256.Sum256(f.Data)
		if h := hex.EncodeToString(sum[:8]); h != w.sum {
			t.Errorf("%s: 內容不符(%s)", f.Name, h)
		}
	}
	if got != len(want) {
		t.Errorf("只對到 %d 個成員,期望 %d", got, len(want))
	}
}

func TestStored(t *testing.T) {
	fs := read(t, "cafe.ace")
	if len(fs) != 1 {
		t.Fatalf("讀到 %d 筆,期望 1", len(fs))
	}
	if fs[0].CompType != 0 {
		t.Errorf("壓縮法 = %d,期望 0(不壓縮)", fs[0].CompType)
	}
	sum := sha256.Sum256(fs[0].Data)
	if h := hex.EncodeToString(sum[:8]); h != "29acbd14ec3622e5" {
		t.Errorf("內容不符(%s)", h)
	}
}

// TestFullCorpus 對整份 acefile-testdata 語料驗,涵蓋 MODE_LZ77_EXE 與
// MODE_LZ77_DELTA。語料 11 MB,不進版控;要跑的話:
//
//	git clone --depth 1 https://github.com/droe/acefile-testdata
//	WINCV_ACE_CORPUS=<那個目錄> go test ./internal/archive/ace/
//
// 實測結果:268 個成員全部通過 CRC-32,並與 acefile 的輸出逐位元組相同。
func TestFullCorpus(t *testing.T) {
	dir := os.Getenv("WINCV_ACE_CORPUS")
	if dir == "" {
		t.Skip("沒有設 WINCV_ACE_CORPUS,跳過完整語料")
	}
	paths, _ := filepath.Glob(filepath.Join(dir, "*", "*.ace"))
	if len(paths) == 0 {
		t.Fatalf("%s 底下找不到 .ace", dir)
	}
	total := 0
	for _, p := range paths {
		data, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		fs, err := ace.Read(data)
		if err != nil {
			t.Errorf("%s: %v", filepath.Base(p), err)
			continue
		}
		total += len(fs)
	}
	t.Logf("%d 個檔案共 %d 個成員全部通過 CRC-32", len(paths), total)
}

func TestRejectsNonAce(t *testing.T) {
	if _, err := ace.Read([]byte("this is not an ace archive at all")); err == nil {
		t.Error("不是 ACE 應該報錯")
	}
}
