package session_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/wicanr2/wincv-remake/internal/session"
)

func TestRoundTrip(t *testing.T) {
	p := filepath.Join(t.TempDir(), "a", "session.json")
	want := session.State{
		Dir: "/home/x/docs", Cursor: "a.txt",
		Mode: "viewer", File: "a.txt", Top: 42,
		Cols: 93, Rows: 22, Zoom: 1, Scale: 1.5,
		MenuBar: session.Bool(false),
	}
	if err := want.SaveTo(p); err != nil {
		t.Fatal(err)
	}
	got := session.LoadFrom(p)
	if got.Dir != want.Dir || got.Cursor != want.Cursor || got.Mode != want.Mode ||
		got.File != want.File || got.Top != want.Top || got.Cols != want.Cols ||
		got.Rows != want.Rows || got.Zoom != want.Zoom || got.Scale != want.Scale {
		t.Fatalf("讀回來是 %+v", got)
	}
	if got.MenuBar == nil || *got.MenuBar {
		t.Fatalf("MenuBar 是 %v", got.MenuBar)
	}
}

// 存檔的目錄不存在時要自己建 —— 第一次跑的時候一定是這個情況。
func TestSaveCreatesDir(t *testing.T) {
	p := filepath.Join(t.TempDir(), "x", "y", "z", "session.json")
	if err := (session.State{Dir: "/tmp"}).SaveTo(p); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(p); err != nil {
		t.Fatal(err)
	}
}

// [雷] 壞掉的檔案不能讓程式開不起來。這是錦上添花的功能,
// 任何一種失敗都只該讓程式從頭開始。
func TestLoadSurvivesGarbage(t *testing.T) {
	dir := t.TempDir()
	for name, body := range map[string]string{
		"empty.json":  "",
		"half.json":   `{"dir":"/tmp","cur`,
		"wrong.json":  `["不是物件"]`,
		"binary.json": "\x00\x01\x02\xff",
	} {
		p := filepath.Join(dir, name)
		os.WriteFile(p, []byte(body), 0o644)
		if got := session.LoadFrom(p); got.Dir != "" {
			t.Errorf("%s 讀出了 %+v", name, got)
		}
	}
	// 不存在的檔案同理
	if got := session.LoadFrom(filepath.Join(dir, "沒有這個檔")); got.Dir != "" {
		t.Errorf("讀出了 %+v", got)
	}
}

// 寫到一半被中斷不能毀掉上一份 —— 先寫暫存再改名。
func TestSaveIsAtomic(t *testing.T) {
	p := filepath.Join(t.TempDir(), "session.json")
	if err := (session.State{Dir: "/first"}).SaveTo(p); err != nil {
		t.Fatal(err)
	}
	if err := (session.State{Dir: "/second"}).SaveTo(p); err != nil {
		t.Fatal(err)
	}
	if got := session.LoadFrom(p); got.Dir != "/second" {
		t.Fatalf("讀回 %q", got.Dir)
	}
	// 暫存檔不該留下來
	if _, err := os.Stat(p + ".tmp"); err == nil {
		t.Error("暫存檔沒有清掉")
	}
}

func TestPathIsAbsolute(t *testing.T) {
	if p := session.Path(); p == "" {
		t.Fatal("沒有路徑")
	}
}
