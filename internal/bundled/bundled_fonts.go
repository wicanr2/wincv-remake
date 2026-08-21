//go:build fonts

package bundled

import (
	"embed"
	"io/fs"
	"path"
	"sort"
	"strings"
)

// assets 由 tools/build-full.sh 在建置前放進來,建完就清掉。
// 目錄本身進 .gitignore —— 第三方版權物不進版控。
//
//go:embed assets
var assets embed.FS

// Available 說明這個執行檔有沒有內嵌字型。
const Available = true

// Get 取一份內嵌字型。名字是原始檔名(如 "cvga.fon"、"STDFONT.15")。
func Get(name string) []byte {
	b, err := assets.ReadFile(path.Join("assets", name))
	if err != nil {
		return nil
	}
	return b
}

// fallbackPrefix 標出哪些內嵌檔案是 Unicode 後備字型。
//
// 用檔名前綴而不是寫死清單:build-full.sh 換掉打包的字型時,
// 這裡不必跟著改 —— 那兩份東西分開改就一定會有一天對不上。
const fallbackPrefix = "fb-"

// Fallbacks 回傳內嵌的 Unicode 後備字型,依檔名排序。
//
// 排序決定「誰先補到這個字」,所以 build-full.sh 是用數字前綴命名的
// (fb-10-cjk.ttc、fb-20-symbols.ttf …)。
func Fallbacks() [][]byte {
	entries, err := fs.ReadDir(assets, "assets")
	if err != nil {
		return nil
	}
	var names []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasPrefix(e.Name(), fallbackPrefix) {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	out := make([][]byte, 0, len(names))
	for _, n := range names {
		if b := Get(n); b != nil {
			out = append(out, b)
		}
	}
	return out
}
