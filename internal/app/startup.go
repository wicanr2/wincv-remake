package app

import (
	"os"
	"path/filepath"

	"github.com/wicanr2/wincv-remake/internal/i18n"
)

// StartPath 套用原版命令列指定的起始目標。
//
// 目錄只切換瀏覽器；檔案則沿用 Enter 的既有分派，讓圖片、壓縮檔與
// 文件格式和使用者從清單按 Enter 時走完全相同的正常路徑。edit 為真時
// 則直接進編輯器，對應原版的 /e。
func (a *App) StartPath(full string, edit bool) error {
	info, err := os.Stat(full)
	if err != nil {
		return err
	}
	if info.IsDir() {
		if edit {
			return i18n.Errorf("/e 只能指定檔案，不可指定目錄: %q", full)
		}
		return a.Browser.Load(full)
	}

	dir, name := filepath.Dir(full), filepath.Base(full)
	if err := a.Browser.Load(dir); err != nil {
		return err
	}
	a.focusOn(name)
	e := a.Browser.Current()
	if e == nil || e.Name != name {
		return i18n.Errorf("起始檔案 %q 不在目錄 %q 內", name, dir)
	}
	if edit {
		if !a.openEditor() {
			return i18n.Errorf("無法編輯 %q", full)
		}
		return nil
	}
	a.enter()
	if a.Message != "" {
		return i18n.Errorf("無法開啟 %q: %s", full, a.Message)
	}
	return nil
}
