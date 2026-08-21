package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/wicanr2/wincv-remake/internal/cell"
	"github.com/wicanr2/wincv-remake/internal/keys"
	"github.com/wicanr2/wincv-remake/internal/session"
	"github.com/wicanr2/wincv-remake/internal/vfs"
)

func newScreen() *cell.Screen { return cell.New(80, 25) }

// 關掉時記下目錄與游標,再開起來要回到同一個地方。
func TestSnapshotRestoreDirAndCursor(t *testing.T) {
	dir := t.TempDir()
	for _, n := range []string{"a.txt", "b.txt", "c.txt"} {
		os.WriteFile(filepath.Join(dir, n), []byte("內容\n"), 0o644)
	}
	a := New(vfs.OS{}, dir)
	a.Draw(newScreen())
	// 走到 b.txt
	for a.Browser.Current() == nil || a.Browser.Current().Name != "b.txt" {
		if !a.HandleKey(keys.Named(keys.Down)) {
			t.Fatal("走不到 b.txt")
		}
	}
	st := a.Snapshot()
	if st.Dir != dir || st.Cursor != "b.txt" {
		t.Fatalf("快照是 %+v", st)
	}

	b := New(vfs.OS{}, t.TempDir())
	b.Draw(newScreen())
	b.Restore(st)
	if b.Browser.Dir != dir {
		t.Fatalf("沒回到原目錄:%s", b.Browser.Dir)
	}
	if c := b.Browser.Current(); c == nil || c.Name != "b.txt" {
		t.Fatalf("游標沒回到 b.txt:%+v", c)
	}
}

// 開著的文件與捲動位置也要回得去。
func TestSnapshotRestoreOpenFile(t *testing.T) {
	dir := t.TempDir()
	var sb strings.Builder
	for i := 0; i < 200; i++ {
		sb.WriteString("這是第幾列\n")
	}
	os.WriteFile(filepath.Join(dir, "long.txt"), []byte(sb.String()), 0o644)

	a := New(vfs.OS{}, dir)
	a.Draw(newScreen())
	a.focusOn("long.txt")
	a.HandleKey(keys.Named(keys.Enter))
	if a.Mode != ModeViewer {
		t.Fatalf("沒進檢視模式:%v", a.Mode)
	}
	a.Draw(newScreen())
	for i := 0; i < 3; i++ {
		a.HandleKey(keys.Named(keys.PgDn))
	}
	st := a.Snapshot()
	if st.Mode != "viewer" || st.File != "long.txt" || st.Top == 0 {
		t.Fatalf("快照是 %+v", st)
	}

	b := New(vfs.OS{}, t.TempDir())
	b.Draw(newScreen())
	b.Restore(st)
	if b.Mode != ModeViewer {
		t.Fatalf("沒回到檢視模式:%v", b.Mode)
	}
	if b.Viewer == nil || b.Viewer.Name != "long.txt" {
		t.Fatal("開的不是同一個檔")
	}
	if b.Viewer.Top != st.Top {
		t.Fatalf("捲到第 %d 列,想要 %d", b.Viewer.Top, st.Top)
	}
}

// [雷] 上次看的那個檔案很可能正是這次被刪掉的那一個。
// 還原是錦上添花,不能讓程式開不起來。
func TestRestoreSurvivesMissingTargets(t *testing.T) {
	dir := t.TempDir()
	a := New(vfs.OS{}, dir)
	a.Draw(newScreen())
	for _, st := range []session.State{
		{Dir: "/沒有這個目錄", Mode: "viewer", File: "x.txt"},
		{Dir: dir, Mode: "viewer", File: "不存在.txt", Top: 999},
		{Dir: dir, Mode: "edit", File: "不存在.txt"},
		{Dir: dir, Mode: "image", File: "不存在.png"},
		{Dir: dir, Mode: "認不得的模式", File: "x"},
		{},
	} {
		func() {
			defer func() {
				if r := recover(); r != nil {
					t.Errorf("%+v 讓程式炸了:%v", st, r)
				}
			}()
			a.Restore(st)
		}()
	}
}

// 檔案在兩次執行之間變短了,捲動位置要夾回去 ——
// 不夾的話畫面是一片空白,而使用者只會覺得檔案不見了。
func TestRestoreClampsScroll(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "short.txt"), []byte("一\n二\n"), 0o644)
	a := New(vfs.OS{}, dir)
	a.Draw(newScreen())
	a.Restore(session.State{Dir: dir, Mode: "viewer", File: "short.txt", Top: 900})
	if a.Viewer == nil {
		t.Fatal("檔案沒開起來")
	}
	if a.Viewer.Top != 0 {
		t.Fatalf("捲到第 %d 列", a.Viewer.Top)
	}
}

// 壓縮檔內部的位置存了也回不去,要退到它外面那一層。
func TestSnapshotLeavesArchive(t *testing.T) {
	a := New(vfs.OS{}, t.TempDir())
	a.Draw(newScreen())
	st := a.Snapshot()
	if strings.Contains(st.Dir, "!") {
		t.Fatalf("存了壓縮檔內部的路徑:%q", st.Dir)
	}
}

// 說明是內嵌的,不是檔案 —— 存下來下次會找不到那個「檔名」。
func TestSnapshotSkipsHelp(t *testing.T) {
	a := New(vfs.OS{}, t.TempDir())
	a.HandleKey(keys.Named(keys.F1))
	st := a.Snapshot()
	if st.File == "使用說明" || st.Mode == "markdown" {
		t.Fatalf("把說明存進去了:%+v", st)
	}
}

// 畫面設定也要回得去。
func TestSnapshotRestoreDisplay(t *testing.T) {
	a := New(vfs.OS{}, t.TempDir())
	a.MaxZoom = 2
	a.Zoom, a.Scale, a.MenuBar = 2, 1.5, false
	st := a.Snapshot()

	b := New(vfs.OS{}, t.TempDir())
	b.MaxZoom = 2
	b.Restore(st)
	if b.Zoom != 2 || b.Scale != 1.5 || b.MenuBar {
		t.Fatalf("還原後 Zoom=%d Scale=%v MenuBar=%v", b.Zoom, b.Scale, b.MenuBar)
	}
}
