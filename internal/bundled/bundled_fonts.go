//go:build fonts

package bundled

import (
	"embed"
	"path"
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
