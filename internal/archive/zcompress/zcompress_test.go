package zcompress_test

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"

	"github.com/wicanr2/wincv-remake/internal/archive/zcompress"
)

// 測試資料是 ncompress(參考實作)壓出來的,不是自己編的 ——
// 拿自己的編碼器驗自己的解碼器等於什麼都沒驗。
// 期望的 sha256 是壓縮前的原始檔。
var cases = []struct {
	file string
	size int
	sum  string
	note string
}{
	{"real_small.Z", 2000, "799f9cc5416fe970d46ebaaa5d3a4e6b", "只用到 9-10 位"},
	{"real16.Z", 229880, "a0a61c73569bdbfbd4252b6b0c664bf1", "maxbits=16,走完 9→16 的加寬"},
	// maxbits=12 的字典只有 4096 個,大檔會塞滿並重設 —— 這是唯一
	// 會走到 CLEAR 與「重設後回到 9 位」那條路的案例。
	{"real12.Z", 229880, "a0a61c73569bdbfbd4252b6b0c664bf1", "maxbits=12,會觸發 CLEAR"},
}

func TestDecodeAgainstNcompress(t *testing.T) {
	for _, c := range cases {
		t.Run(c.file, func(t *testing.T) {
			data, err := os.ReadFile(filepath.Join("..", "..", "..", "testdata", "z", c.file))
			if err != nil {
				t.Fatal(err)
			}
			got, err := zcompress.Decode(data)
			if err != nil {
				t.Fatalf("%s(%s): %v", c.file, c.note, err)
			}
			if len(got) != c.size {
				t.Fatalf("解出 %d 個位元組,期望 %d(%s)", len(got), c.size, c.note)
			}
			sum := sha256.Sum256(got)
			if h := hex.EncodeToString(sum[:16]); h != c.sum {
				t.Errorf("內容不符:sha256 前 16 位元組 = %s,期望 %s", h, c.sum)
			}
		})
	}
}

func TestRejectsNonZ(t *testing.T) {
	if _, err := zcompress.Decode([]byte("not a Z file")); err == nil {
		t.Error("不是 .Z 應該報錯")
	}
	if zcompress.IsZ([]byte{0x1F, 0x8B, 0x08}) {
		t.Error("gzip 的 magic 不該被當成 compress")
	}
}
