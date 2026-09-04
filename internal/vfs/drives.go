package vfs

import (
	"bufio"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Drive 是磁碟窗格裡的一列。
//
// 欄位對齊原版的 DISKBUF:它每一筆存 >DISKBUF-PATH / >DISKBUF-ATTRIB /
// >DISKBUF-LABEL 三樣(見 docs/re/symbols.tsv)。
type Drive struct {
	Label  string // 顯示用的短名,如 "C:" 或 "/" 或 "usb-stick"
	Path   string // 切過去要用的路徑
	Volume bool   // 是不是可卸除/外接的(原版用不同顏色畫)
}

// pseudoFS 是不該出現在磁碟窗格裡的檔案系統型別。
//
// 這些掛載點對使用者沒有「切過去瀏覽」的意義,而且數量很多
// (一台 Linux 桌機上 /proc/mounts 動輒四五十列,其中真正是磁碟的
// 通常不到五個)。不過濾的話,磁碟窗格會被 cgroup 灌爆。
var pseudoFS = map[string]bool{
	"proc": true, "sysfs": true, "devtmpfs": true, "devpts": true,
	"tmpfs": true, "cgroup": true, "cgroup2": true, "securityfs": true,
	"pstore": true, "bpf": true, "debugfs": true, "tracefs": true,
	"configfs": true, "fusectl": true, "hugetlbfs": true, "mqueue": true,
	"efivarfs": true, "autofs": true, "binfmt_misc": true, "squashfs": true,
	"nsfs": true, "overlay": true, "ramfs": true, "rpc_pipefs": true,
	"fuse.portal": true, "fuse.gvfsd-fuse": true, "fuse.snapfuse": true,
}

// removableRoots 是各平台放可卸除媒體的地方。
var removableRoots = []string{"/media", "/run/media", "/mnt", "/Volumes"}

// Drives 列出可以切過去的磁碟或掛載點。
//
// Windows 版在 drives_windows.go,走磁碟機代號。
// 這一版(Linux / macOS)沒有磁碟機代號,拿掛載點當等價物:
// 根目錄、家目錄,加上可卸除媒體。macOS 沒有 /proc/mounts,
// 直接掃 /Volumes 就涵蓋了它的外接磁碟。
func unixDrives() []Drive {
	seen := map[string]bool{}
	var out []Drive
	add := func(label, path string, vol bool) {
		if path == "" || seen[path] {
			return
		}
		if st, err := os.Stat(path); err != nil || !st.IsDir() {
			return
		}
		seen[path] = true
		out = append(out, Drive{Label: label, Path: path, Volume: vol})
	}

	add("/", "/", false)
	if h, err := os.UserHomeDir(); err == nil {
		add("~", h, false)
	}

	// /proc/mounts 只在 Linux 有。讀不到就靠下面的 removableRoots 掃描,
	// 那條路徑在 macOS 上就是全部的答案。
	if f, err := os.Open("/proc/mounts"); err == nil {
		defer f.Close()
		sc := bufio.NewScanner(f)
		for sc.Scan() {
			fs := strings.Fields(sc.Text())
			if len(fs) < 3 || pseudoFS[fs[2]] {
				continue
			}
			mp := unescapeMount(fs[1])
			if mp == "/" || strings.HasPrefix(mp, "/snap/") {
				continue
			}
			add(filepath.Base(mp), mp, isRemovable(mp))
		}
	}

	for _, root := range removableRoots {
		ents, err := os.ReadDir(root)
		if err != nil {
			continue
		}
		for _, e := range ents {
			p := filepath.Join(root, e.Name())
			// /media/<user>/<label> 這種兩層的結構要再進一層。
			if sub, err := os.ReadDir(p); err == nil && !hasFiles(sub) && root != "/Volumes" {
				for _, s := range sub {
					add(s.Name(), filepath.Join(p, s.Name()), true)
				}
			}
			add(e.Name(), p, true)
		}
	}

	sort.SliceStable(out, func(i, j int) bool {
		// 固定的排前面,可卸除的排後面;同類保持發現順序。
		return !out[i].Volume && out[j].Volume
	})
	return out
}

// hasFiles 判斷一個目錄底下有沒有非目錄的東西。
// 用來分辨 /media/<user>/ (底下全是目錄)與 /media/<label>/ (底下有檔案)。
func hasFiles(ents []os.DirEntry) bool {
	for _, e := range ents {
		if !e.IsDir() {
			return true
		}
	}
	return false
}

func isRemovable(mp string) bool {
	for _, r := range removableRoots {
		if strings.HasPrefix(mp, r+"/") {
			return true
		}
	}
	return false
}

// unescapeMount 還原 /proc/mounts 的八進位跳脫。
// 掛載點含空白時會寫成 \040,不還原就會切到一個不存在的路徑。
func unescapeMount(s string) string {
	if !strings.Contains(s, `\`) {
		return s
	}
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == '\\' && i+3 < len(s) {
			v := 0
			ok := true
			for _, c := range s[i+1 : i+4] {
				if c < '0' || c > '7' {
					ok = false
					break
				}
				v = v*8 + int(c-'0')
			}
			if ok {
				b.WriteByte(byte(v))
				i += 3
				continue
			}
		}
		b.WriteByte(s[i])
	}
	return b.String()
}
