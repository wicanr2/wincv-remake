package lzh_test

import (
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/wicanr2/wincv-remake/internal/archive/lzh"
)

// 期望值來自 7z(p7zip)對同一個檔案的解壓縮結果,不是自己的輸出。
// 拿自己的輸出當期望值等於什麼都沒驗。
var want = map[string]struct {
	size int64
	sum  string
}{
	// testdata/lzh/init1181.lzh:level 1 標頭、沒有擴充標頭、全部 -lh5-
	"118BOARD.DOC": {8625, "d26f46c3e43be61d"},
	"INIT118.ASM":  {31572, "9464e17080074373"},
	"INIT118.COM":  {4175, "01a059bc898060e5"},
	"INIT118.DOC":  {5198, "8c709e098ca085e0"},

	// testdata/lzh/dosbox-sample.lha:level 1 + 目錄擴充標頭(0x02),
	// 分隔字元是 0xFF。dosbox.diff 那一筆的碼長表用滿全部 510 個字碼,
	// 是 nc 常數少算一格時唯一會壞掉的情況。
	"DOSBox/C.info":        {8504, "6a662e50967d5c38"},
	"DOSBox/dosbox.diff":   {41241, "ac9fe9afc6031d00"},
	"DOSBox/dosbox.readme": {3097, "2627623613a7c37e"},
}

func TestDecodeAgainst7z(t *testing.T) {
	for _, name := range []string{"init1181.lzh", "dosbox-sample.lha"} {
		t.Run(name, func(t *testing.T) {
			f, err := os.Open(filepath.Join("..", "..", "..", "testdata", "lzh", name))
			if err != nil {
				t.Fatal(err)
			}
			defer f.Close()
			es, err := lzh.List(f)
			if err != nil {
				t.Fatal(err)
			}
			if len(es) == 0 {
				t.Fatal("一筆都沒讀到")
			}
			for _, e := range es {
				if e.IsDir {
					continue
				}
				w, ok := want[e.Name]
				if !ok {
					t.Errorf("多出沒預期的成員 %q", e.Name)
					continue
				}
				if _, err := f.Seek(e.Offset, io.SeekStart); err != nil {
					t.Fatal(err)
				}
				got, err := lzh.Decode(io.LimitReader(f, e.Packed), e.Method, e.Original)
				if err != nil {
					t.Errorf("%s: %v", e.Name, err)
					continue
				}
				if int64(len(got)) != w.size {
					t.Errorf("%s: 解出 %d 個位元組,期望 %d", e.Name, len(got), w.size)
					continue
				}
				sum := sha256.Sum256(got)
				if h := hex.EncodeToString(sum[:8]); h != w.sum {
					t.Errorf("%s: 內容不符,sha256 前 8 位元組 = %s,期望 %s", e.Name, h, w.sum)
				}
			}
		})
	}
}

// 目錄項的名字要帶著路徑,分隔字元(0xFF)要正規化成 /。
func TestDirEntry(t *testing.T) {
	f, err := os.Open(filepath.Join("..", "..", "..", "testdata", "lzh", "dosbox-sample.lha"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	es, _ := lzh.List(f)
	found := false
	for _, e := range es {
		if e.IsDir {
			found = true
			if e.Name != "DOSBox/C/" {
				t.Errorf("目錄名 = %q,期望 \"DOSBox/C/\"", e.Name)
			}
		}
	}
	if !found {
		t.Error("沒認出目錄項")
	}
}

func TestSupported(t *testing.T) {
	for _, m := range []string{"-lh0-", "-lh5-", "-lh6-", "-lh7-", "-lhd-"} {
		if !lzh.Supported(m) {
			t.Errorf("%s 應該支援", m)
		}
	}
	// -lh1- 是另一套(動態 Huffman),還沒做 —— 要誠實回報不支援,
	// 不能當成 lh5 去解,那會解出垃圾而不是報錯。
	if lzh.Supported("-lh1-") {
		t.Error("-lh1- 還沒實作,不該回報支援")
	}
}
