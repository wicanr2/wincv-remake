package ttf

import (
	"path/filepath"
	"strings"
	"testing"
)

// 字型放在哪裡完全由平台決定,而任何一台機器上都只驗得到一個平台。
// host 把 GOOS、環境變數與家目錄變成參數,四個平台的路徑組法就都驗得動。
func fake(goos string, env map[string]string) host {
	return host{
		goos: goos,
		env:  func(k string) string { return env[k] },
		home: filepath.FromSlash("/home/u"),
	}
}

func has(list []string, want string) bool {
	for _, p := range list {
		if p == want {
			return true
		}
	}
	return false
}

// mustHave 比對時用 filepath.Join 組期望值:被測程式也是用它組的,
// 而在 Linux 上跑 Windows 分支時分隔號會是 "/"。要驗的是「路徑從哪裡來」,
// 不是分隔號長什麼樣 —— 真的在 Windows 上跑的時候兩邊都會是 "\"。
func mustHave(t *testing.T, list []string, want ...string) {
	t.Helper()
	for _, w := range want {
		if !has(list, w) {
			t.Errorf("少了 %s\n實際:%v", w, list)
		}
	}
}

// Windows 的系統目錄不一定在 C:,而「只為我安裝」的字型根本不在系統目錄裡。
func TestWindowsFontDirs(t *testing.T) {
	h := fake("windows", map[string]string{
		"SystemRoot":   `D:\WinNT`,
		"LOCALAPPDATA": `C:\Users\u\AppData\Local`,
	})
	dirs := h.fontDirs()
	mustHave(t, dirs,
		filepath.Join(`D:\WinNT`, "Fonts"),
		filepath.Join(`C:\Users\u\AppData\Local`, "Microsoft", "Windows", "Fonts"))

	// SystemRoot 沒設就退回 windir,兩個都沒有才用預設。
	if got := fake("windows", map[string]string{"windir": `E:\Win`}).winFontsDir(); got != filepath.Join(`E:\Win`, "Fonts") {
		t.Errorf("windir 沒被採用,拿到 %s", got)
	}
	if got := fake("windows", nil).winFontsDir(); got != `C:\Windows\Fonts` { // 預設值是字面值,不經過 Join
		t.Errorf("預設值不對:%s", got)
	}

	// 寫死的候選清單也要跟著系統目錄走,不能自己拼 C:\。
	list := candidatesFor(h)
	mustHave(t, list,
		filepath.Join(`D:\WinNT`, "Fonts", "msjh.ttc"),
		filepath.Join(`D:\WinNT`, "Fonts", "consola.ttf"))
	for _, p := range list {
		if strings.HasPrefix(p, `C:\Windows`) {
			t.Errorf("候選清單裡還有寫死的 C:\\Windows:%s", p)
		}
	}
}

// macOS 從 Catalina 起把附帶的字型搬進 Supplemental。
func TestDarwinFontDirs(t *testing.T) {
	dirs := fake("darwin", nil).fontDirs()
	mustHave(t, dirs,
		"/System/Library/Fonts",
		"/System/Library/Fonts/Supplemental",
		"/Library/Fonts",
		"/home/u/Library/Fonts")
}

// Android 10 之後系統被拆成好幾個分割區,字型不再只在 /system/fonts。
func TestAndroidFontDirs(t *testing.T) {
	dirs := fake("android", nil).fontDirs()
	mustHave(t, dirs, "/system/fonts", "/product/fonts", "/system_ext/fonts")
	// 候選清單要對每一個目錄各展開一次,不能只認 /system/fonts。
	mustHave(t, candidatesFor(fake("android", nil)),
		"/system/fonts/DroidSansMono.ttf",
		"/product/fonts/NotoSansCJK-Regular.ttc")
}

// Linux:XDG 的兩個變數要照規格解讀,而容器裡主機的字型掛在別的地方。
func TestLinuxFontDirs(t *testing.T) {
	dirs := fake("linux", map[string]string{
		"XDG_DATA_HOME": "/home/u/.xdgdata",
		"XDG_DATA_DIRS": "/opt/share:/srv/share",
	}).fontDirs()
	mustHave(t, dirs,
		"/usr/share/fonts",
		"/usr/local/share/fonts",
		"/home/u/.xdgdata/fonts",
		"/opt/share/fonts",
		"/srv/share/fonts",
		"/run/host/fonts")
	// XDG_DATA_HOME 有設的時候不該再用 ~/.local/share。
	if has(dirs, filepath.FromSlash("/home/u/.local/share/fonts")) {
		t.Error("XDG_DATA_HOME 有設,不該再掃 ~/.local/share/fonts")
	}
	// 沒設才退回預設值。
	mustHave(t, fake("linux", nil).fontDirs(),
		"/home/u/.local/share/fonts", "/home/u/.fonts")
}

// 沒有家目錄(服務帳號、容器)時不可以組出 "Library/Fonts" 這種相對路徑 ——
// 那會從當下的工作目錄開始掃,掃到的東西完全不可預期。
func TestNoHomeDirProducesNoRelativePaths(t *testing.T) {
	for _, goos := range []string{"linux", "darwin", "windows", "android"} {
		h := host{goos: goos, env: func(string) string { return "" }}
		for _, p := range h.fontDirs() {
			// filepath.IsAbs 在 Linux 上不認得 "C:\..."(那是 Windows 的
			// 絕對路徑),所以 Windows 那一輪另外判。
			abs := filepath.IsAbs(p)
			if goos == "windows" && len(p) > 2 && p[1] == ':' {
				abs = true
			}
			if !abs {
				t.Errorf("%s:掃描目錄不是絕對路徑:%q", goos, p)
			}
		}
	}
}

// 掃描認得的檔名關鍵字要涵蓋每一個平台。只有 Noto 的話,掃描在
// Windows 與 macOS 上一個字型都找不到,而那正是掃描存在的理由。
func TestWantedCoversEveryPlatform(t *testing.T) {
	for _, c := range []struct {
		platform string
		names    []string
	}{
		{"windows", []string{"msjh.ttc", "mingliu.ttc", "simsun.ttc", "consola.ttf", "malgun.ttf"}},
		{"macOS", []string{"PingFang.ttc", "Menlo.ttc", "Hiragino Sans GB.ttc", "Songti.ttc"}},
		{"Android", []string{"DroidSansMono.ttf", "DroidSansFallback.ttf"}},
		{"Linux", []string{"NotoSansCJK-Regular.ttc", "DejaVuSansMono.ttf"}},
	} {
		for _, n := range c.names {
			base := strings.ToLower(n)
			base = strings.NewReplacer("-", "", "_", "", " ", "").Replace(base)
			found := false
			for _, w := range wanted {
				if strings.Contains(base, strings.ReplaceAll(w, "-", "")) {
					found = true
					break
				}
			}
			if !found {
				t.Errorf("%s 的 %s 不在掃描認得的關鍵字裡", c.platform, n)
			}
		}
	}
}
