// Package datadir 回答「原版素材放在哪裡」。
//
// 要找的是原版的半形 `.FON`、倚天字庫、語法上色設定(`keyword_*.cfg`)
// 與字典資料。這些是第三方版權物,**不隨產物散布**,由使用者自備 ——
// 也就是說「使用者把檔案放在哪裡」這件事,程式必須答得出來。
//
// [雷] 原本只認一個相對路徑 `original/app/`,那是相對於**當下的工作目錄**。
// 從 repo 根目錄跑當然找得到,而打包安裝之後:Windows 上按兩下的工作目錄是
// 桌面或 system32,macOS 上是 `/`,Linux 上是啟動器給的任何一個地方。
// 結果是使用者把字型放在執行檔旁邊卻完全沒有作用,而畫面照樣出得來
// (改用系統字型),看起來像「這個功能沒做」而不像「檔案沒找到」。
package datadir

import (
	"os"
	"path/filepath"
	"strings"
)

// EnvHome 是指定素材目錄的環境變數。
//
// 有環境變數而不是只有命令列旗標:按兩下啟動、桌面捷徑、Android 上
// 都沒有地方打旗標,而那正是工作目錄最不可預測的幾種情形。
const EnvHome = "WINCV_HOME"

// subdirs 是每一個位置底下還要看的子目錄。
//
// 原版的安裝目錄把半形字型與倚天字庫分開放(`original/app` 與
// `original/eten`),而使用者多半會把所有東西丟在同一個資料夾。
// 兩種擺法都要認,不然「檔案明明在那裡卻沒作用」。
var subdirs = []string{
	"",
	"wincv",
	filepath.Join("original", "app"),
	filepath.Join("original", "eten"),
}

// Dirs 依序列出要找素材的目錄。前面的贏。
//
// 順序的道理:使用者明講的(環境變數)> 放在程式旁邊的(最多人會這樣做)
// > 個人設定目錄(與 session.json 同一套慣例)> 工作目錄(開發時從 repo
// 根目錄跑的情形)。
func Dirs() []string {
	var roots []string
	addRoot := func(parts ...string) {
		if parts[0] != "" {
			roots = append(roots, filepath.Join(parts...))
		}
	}

	addRoot(os.Getenv(EnvHome))

	if exe, err := os.Executable(); err == nil {
		if resolved, err := filepath.EvalSymlinks(exe); err == nil {
			exe = resolved
		}
		addRoot(filepath.Dir(exe))
	}
	if cfg, err := os.UserConfigDir(); err == nil {
		addRoot(cfg, "wincv")
	}
	if home, err := os.UserHomeDir(); err == nil {
		addRoot(home, ".wincv")
	}
	// 工作目錄。開發時 `tools/go.sh run ./cmd/wincv` 是從 repo 根目錄跑的,
	// 素材就在 original/ 底下。
	addRoot(".")

	var out []string
	seen := map[string]bool{}
	for _, r := range roots {
		for _, sub := range subdirs {
			p := filepath.Join(r, sub)
			if !seen[p] {
				seen[p] = true
				out = append(out, p)
			}
		}
	}
	return out
}

// Find 找出第一個放著 name 的目錄。找不到時第二個回傳值為假,
// 第一個仍然回一個「最合理的位置」讓錯誤訊息有東西可講。
func Find(name string) (string, bool) {
	dirs := Dirs()
	for _, d := range dirs {
		if st, err := os.Stat(filepath.Join(d, name)); err == nil && !st.IsDir() {
			return d, true
		}
	}
	if len(dirs) > 0 {
		return dirs[len(dirs)-1], false
	}
	return ".", false
}

// Resolve 把一個檔名變成完整路徑。已經是路徑(帶目錄分隔號)的照原樣回,
// 使用者明講的位置永遠贏。
func Resolve(name string) string {
	if name == "" {
		return ""
	}
	if filepath.Dir(name) != "." {
		return name
	}
	d, _ := Find(name)
	return filepath.Join(d, name)
}

// Resolve2 是給命令列旗標用的:使用者給了就照他給的,沒給就自己找。
func Resolve2(given, name string) string {
	if given != "" {
		return given
	}
	return Resolve(name)
}

// Explain 把搜尋順序排成幾列,給錯誤訊息用。
//
// 「找不到檔案」的訊息如果不說找過哪裡,使用者只能猜 —— 而這裡正是
// 每個平台都不一樣、最猜不到的地方。
func Explain() string {
	var b strings.Builder
	for _, d := range Dirs() {
		b.WriteString("        ")
		b.WriteString(d)
		b.WriteString("\n")
	}
	return b.String()
}
