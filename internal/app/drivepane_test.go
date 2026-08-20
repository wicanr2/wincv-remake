package app

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/wicanr2/wincv-remake/internal/cell"
	"github.com/wicanr2/wincv-remake/internal/keys"
	"github.com/wicanr2/wincv-remake/internal/vfs"
)

func altRune(r rune) keys.Key { return keys.Key{Code: keys.Rune, R: r, Alt: true} }

// Alt-D 開關磁碟窗格,而且開的時候游標要停在**目前所在的那個磁碟**上。
func TestDrivePaneToggleAndFocus(t *testing.T) {
	a := New(vfs.OS{}, fixture(t))
	s := cell.New(80, 20)
	a.Draw(s)

	if !a.HandleKey(altRune('D')) {
		t.Fatal("Alt-D 沒有被處理")
	}
	if a.Browser.DrivePane == 0 || len(a.Browser.Drives) == 0 {
		t.Fatalf("窗格沒開:pane=%d drives=%d", a.Browser.DrivePane, len(a.Browser.Drives))
	}
	cur := a.Browser.Drives[a.Browser.DriveCursor]
	if !hasPathPrefix(a.Browser.Dir, cur.Path) {
		t.Errorf("游標停在 %q,但目前在 %q", cur.Path, a.Browser.Dir)
	}

	a.HandleKey(altRune('D'))
	if a.Browser.DrivePane != 0 || a.Browser.Drives != nil || a.DriveFocus {
		t.Error("再按一次沒有關掉")
	}
}

// Tab 把焦點交給窗格,Enter 切過去,Esc 交回來。
func TestDrivePaneNavigation(t *testing.T) {
	root := t.TempDir()
	sub := filepath.Join(root, "sub")
	os.MkdirAll(sub, 0o755)
	a := New(vfs.OS{}, sub)
	a.Draw(cell.New(80, 20))

	a.HandleKey(altRune('D'))
	// 換成兩筆自己控制的,免得測試被這台機器的掛載點影響。
	a.Browser.Drives = []vfs.Drive{{Label: "root", Path: root}, {Label: "sub", Path: sub}}
	a.Browser.DriveCursor = 1

	if !a.HandleKey(keys.Named(keys.Tab)) || !a.DriveFocus {
		t.Fatal("Tab 沒有把焦點交給磁碟窗格")
	}
	a.HandleKey(keys.Named(keys.Up))
	if a.Browser.DriveCursor != 0 {
		t.Fatalf("上鍵之後游標在 %d", a.Browser.DriveCursor)
	}
	if !a.HandleKey(keys.Named(keys.Enter)) {
		t.Fatal("Enter 沒有被處理")
	}
	if a.Browser.Dir != root {
		t.Errorf("沒有切過去,還在 %q", a.Browser.Dir)
	}
	if a.DriveFocus {
		t.Error("切過去之後焦點應該交還給清單")
	}

	a.HandleKey(keys.Named(keys.Tab))
	a.HandleKey(keys.Named(keys.Esc))
	if a.DriveFocus {
		t.Error("Esc 沒有把焦點交回清單")
	}
}

// 焦點在窗格時,方向鍵不能同時動到檔案清單的游標 ——
// 兩邊一起動的話,按一次上鍵會有兩個地方在跑。
func TestDrivePaneKeysDoNotLeak(t *testing.T) {
	a := New(vfs.OS{}, fixture(t))
	a.Draw(cell.New(80, 20))
	a.HandleKey(altRune('D'))
	a.Browser.Drives = []vfs.Drive{{Label: "a", Path: "/"}, {Label: "b", Path: "/"}}
	a.Browser.DriveCursor = 0
	a.Browser.Cursor = 0
	a.HandleKey(keys.Named(keys.Tab))

	a.HandleKey(keys.Named(keys.Down))
	if a.Browser.Cursor != 0 {
		t.Errorf("檔案清單的游標也跟著動了,現在在 %d", a.Browser.Cursor)
	}
	if a.Browser.DriveCursor != 1 {
		t.Errorf("磁碟窗格的游標沒動,在 %d", a.Browser.DriveCursor)
	}
}

// 最長前綴比對:根目錄是每個路徑的前綴,先比到它就永遠選不到別的。
// 而且前綴必須停在路徑分隔上,否則 /media 會是 /mediafoo 的前綴。
func TestHasPathPrefix(t *testing.T) {
	for _, c := range []struct {
		p, prefix string
		want      bool
	}{
		{"/media/usb/x", "/media/usb", true},
		{"/media/usb", "/media/usb", true},
		{"/mediafoo/x", "/media", false},
		{"/home/u", "/", true},
		{`C:\a\b`, `C:\a`, true},
		{"/a", "/ab", false},
	} {
		if got := hasPathPrefix(c.p, c.prefix); got != c.want {
			t.Errorf("hasPathPrefix(%q,%q) = %v, 想要 %v", c.p, c.prefix, got, c.want)
		}
	}
}
