package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/wicanr2/wincv-remake/internal/cell"
	"github.com/wicanr2/wincv-remake/internal/keys"
	"github.com/wicanr2/wincv-remake/internal/vfs"
)

// posApp 建一個目錄,裡面一份 200 行的文字檔與一份 markdown。
func posApp(t *testing.T) (*App, string) {
	t.Helper()
	dir := t.TempDir()
	var b strings.Builder
	for i := 0; i < 200; i++ {
		b.WriteString("line\n")
	}
	os.WriteFile(filepath.Join(dir, "long.txt"), []byte(b.String()), 0o644)
	a := New(vfs.OS{}, dir)
	a.now = func() int64 { return 1 }
	a.Draw(cell.New(80, 25))
	return a, dir
}

func openNamed(t *testing.T, a *App, name string, k keys.Key) {
	t.Helper()
	for i, e := range a.Browser.Entries {
		if e.Name == name {
			a.Browser.MoveTo(i, 20)
		}
	}
	a.HandleKey(k)
	a.Draw(cell.New(80, 25))
}

// 看檔捲到某處、離開、再開同一檔,要回到同一處。
func TestViewerPositionRecalled(t *testing.T) {
	a, _ := posApp(t)
	openNamed(t, a, "long.txt", keys.Named(keys.Enter))
	if a.Mode != ModeViewer {
		t.Fatalf("mode = %v", a.Mode)
	}
	a.HandleKey(keys.Named(keys.PgDn))
	a.HandleKey(keys.Named(keys.PgDn))
	want := a.Viewer.Top
	if want == 0 {
		t.Fatal("PgDn 兩次後 Top 還是 0")
	}
	a.HandleKey(keys.Named(keys.Esc))
	if a.Mode != ModeBrowser {
		t.Fatalf("Esc 後 mode = %v", a.Mode)
	}
	openNamed(t, a, "long.txt", keys.Named(keys.Enter))
	if a.Viewer.Top != want {
		t.Fatalf("重開後 Top=%d,預期 %d", a.Viewer.Top, want)
	}
}

// 編輯器記的是游標(行、欄)與捲動位置。
func TestEditorPositionRecalled(t *testing.T) {
	a, _ := posApp(t)
	openNamed(t, a, "long.txt", keys.Ch('E'))
	if a.Mode != ModeEdit {
		t.Fatalf("mode = %v", a.Mode)
	}
	for i := 0; i < 50; i++ {
		a.HandleKey(keys.Named(keys.Down))
	}
	a.HandleKey(keys.Named(keys.Right))
	line, col := a.Editor.Cur.Line, a.Editor.Cur.Col
	a.HandleKey(keys.Named(keys.Esc))
	openNamed(t, a, "long.txt", keys.Ch('E'))
	if a.Editor.Cur.Line != line || a.Editor.Cur.Col != col {
		t.Fatalf("重開後游標 %+v,預期 {%d %d}", a.Editor.Cur, line, col)
	}
}

// 位置表要跟著 session 走:Snapshot 帶出去、Restore 進另一個 App 之後
// 開同一檔仍然回到同一處。檔案變短時夾回範圍,不會是空白畫面。
func TestPositionsSurviveSession(t *testing.T) {
	a, dir := posApp(t)
	openNamed(t, a, "long.txt", keys.Named(keys.Enter))
	a.HandleKey(keys.Named(keys.PgDn))
	want := a.Viewer.Top
	a.HandleKey(keys.Named(keys.Esc))
	st := a.Snapshot()
	if len(st.Positions) != 1 {
		t.Fatalf("Snapshot 的位置表有 %d 筆", len(st.Positions))
	}

	b := New(vfs.OS{}, dir)
	b.Restore(st)
	b.Draw(cell.New(80, 25))
	openNamed(t, b, "long.txt", keys.Named(keys.Enter))
	if b.Viewer.Top != want {
		t.Fatalf("另一個 App 重開後 Top=%d,預期 %d", b.Viewer.Top, want)
	}
	b.HandleKey(keys.Named(keys.Esc))

	// 檔案縮成 3 行:位置要夾回去。
	os.WriteFile(filepath.Join(dir, "long.txt"), []byte("a\nb\nc\n"), 0o644)
	openNamed(t, b, "long.txt", keys.Named(keys.Enter))
	if b.Viewer.Top != 0 {
		t.Fatalf("檔案變短後 Top=%d,預期 0", b.Viewer.Top)
	}
}

// 說明不是檔案,不進位置表。
func TestHelpNotRemembered(t *testing.T) {
	a, _ := posApp(t)
	a.HandleKey(keys.Named(keys.F1))
	a.HandleKey(keys.Named(keys.PgDn))
	if len(a.positions) != 0 {
		t.Fatalf("說明被記進位置表:%v", a.positions)
	}
}
