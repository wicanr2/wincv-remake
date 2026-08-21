//go:build !fonts

// Package bundled 是「把字型打包進執行檔」的開關。
//
// 預設(沒有 `fonts` 這個 build tag)是空的。對外散布的產物**不含**原版的
// `.FON` 與倚天字庫 —— 那些是第三方版權物,由使用者自備。
//
// 帶 `-tags fonts` 建出來的版本會把 `internal/bundled/assets/` 底下的字型
// 嵌進執行檔,不必另外放檔案就是點陣像素對齊的畫面。那種產物**只留在本機**,
// 不進 release。建置方式見 `tools/build-full.sh`。
//
// 兩種建法走同一條載入路徑:呼叫端先看磁碟上有沒有檔案,沒有才問這裡。
// 所以使用者放在執行檔旁邊的字型永遠優先 —— 內嵌的是後備,不是覆蓋。
package bundled

// Available 說明這個執行檔有沒有內嵌字型。
//
// 呼叫端用它決定「找不到字型」時要不要報錯:沒有內嵌的版本找不到就是錯,
// 有內嵌的版本找不到磁碟檔案是正常的。
const Available = false

// Get 取一份內嵌字型。沒有內嵌時一律回 nil。
func Get(name string) []byte { return nil }

// Fallbacks 回傳內嵌的 Unicode 後備字型,依偏好排序。沒有就是空的。
func Fallbacks() [][]byte { return nil }
