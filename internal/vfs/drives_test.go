package vfs

import (
	"os"
	"strings"
	"testing"
)

// 磁碟窗格最容易出的錯是「把 /proc/mounts 整份倒出來」——
// 一台 Linux 桌機動輒四五十列,其中真正是磁碟的通常不到五個。
// 這條盯的是過濾有沒有在做事,以及根目錄一定要在。
func TestDrivesHasRootAndFiltersPseudo(t *testing.T) {
	ds := Drives()
	if len(ds) == 0 {
		t.Fatal("一個磁碟都沒列出來")
	}
	var root bool
	for _, d := range ds {
		if d.Path == "/" || strings.HasSuffix(d.Path, `:\`) {
			root = true
		}
		if d.Label == "" {
			t.Errorf("%q 沒有標籤", d.Path)
		}
		if st, err := os.Stat(d.Path); err != nil || !st.IsDir() {
			t.Errorf("%q 不是可用的目錄", d.Path)
		}
	}
	if !root {
		t.Error("清單裡沒有根目錄")
	}
	// 沒有上限就等於沒過濾。這個數字寬鬆,但擋得住「整份倒出來」。
	if len(ds) > 24 {
		t.Errorf("列出 %d 個,過濾大概沒生效", len(ds))
	}
	seen := map[string]bool{}
	for _, d := range ds {
		if seen[d.Path] {
			t.Errorf("%q 重複出現", d.Path)
		}
		seen[d.Path] = true
	}
}

// 掛載點含空白時 /proc/mounts 寫成 \040。不還原會切到一個不存在的路徑,
// 而症狀是「這個磁碟按了沒反應」,看起來像 UI 沒接好。
func TestUnescapeMount(t *testing.T) {
	for _, c := range []struct{ in, want string }{
		{`/media/u/My\040Disk`, "/media/u/My Disk"},
		{"/mnt/data", "/mnt/data"},
		{`/a\134b`, `/a\b`},
		{`/trailing\04`, `/trailing\04`}, // 不完整的跳脫原樣保留
	} {
		if got := unescapeMount(c.in); got != c.want {
			t.Errorf("unescapeMount(%q) = %q, 想要 %q", c.in, got, c.want)
		}
	}
}
