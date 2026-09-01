package cfb

import (
	"strings"
	"testing"
)

// 借 doc97 的測試檔:那是 LibreOffice 產生的真實 Word 97 檔案,
// 也就是一個真實的複合文件。自己組一個最小的容器只會驗到自己對
// 格式的理解 —— 而 FAT、迷你 FAT、目錄三者的互相指涉正是容易解錯的地方。
const sample = "../doc97/testdata/rich.doc"

func TestStreams(t *testing.T) {
	f, err := Open(sample)
	if err != nil {
		t.Fatal(err)
	}
	names := strings.Join(f.Names(), " ")
	for _, want := range []string{"WordDocument", "Root Entry"} {
		if !strings.Contains(names, want) {
			t.Errorf("目錄裡少了 %s:%s", want, names)
		}
	}
	wd, err := f.Stream("WordDocument")
	if err != nil {
		t.Fatal(err)
	}
	if len(wd) < 512 {
		t.Fatalf("WordDocument 只讀到 %d 個位元組", len(wd))
	}
	if wd[0] != 0xEC || wd[1] != 0xA5 {
		t.Errorf("WordDocument 開頭不是 Word 的識別碼:% x", wd[:2])
	}
}

// 小於門檻的串流走迷你配置表,大的走一般配置表。同一份檔案裡兩種都有。
func TestBothChainKinds(t *testing.T) {
	f, err := Open(sample)
	if err != nil {
		t.Fatal(err)
	}
	var small, large int
	for _, n := range f.Names() {
		if !f.Has(n) {
			continue
		}
		b, err := f.Stream(n)
		if err != nil {
			t.Errorf("讀 %s 失敗:%v", n, err)
			continue
		}
		if len(b) == 0 {
			continue
		}
		if uint32(len(b)) < f.miniCutoff {
			small++
		} else {
			large++
		}
	}
	if small == 0 || large == 0 {
		t.Errorf("兩種鏈結沒有都走到(小 %d 大 %d)", small, large)
	}
}

func TestNotACompoundFile(t *testing.T) {
	if _, err := Parse([]byte(strings.Repeat("x", 1024))); err == nil {
		t.Fatal("簽章不符應該要失敗")
	}
	if _, err := Parse([]byte("short")); err == nil {
		t.Fatal("太短應該要失敗")
	}
}
