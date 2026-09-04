package app

import (
	"github.com/wicanr2/wincv-remake/internal/i18n"
	"github.com/wicanr2/wincv-remake/internal/keys"
	"github.com/wicanr2/wincv-remake/internal/vfs"
)

// DrivePaneCols 是磁碟窗格的寬度。
//
// 原版的磁碟窗格寬約 34 px(≈4 格),顯示 "C:\" 這種短標籤。
// 這裡放寬到 10 格,因為 Linux / macOS 沒有磁碟機代號,
// 掛載點的名字("usb-stick"、"Macintosh HD")比一個字母長得多。
const DrivePaneCols = 10

// toggleDrivePane 開關左側磁碟窗格。
//
// 原版把它做成一個窗格(設定裡有「調整 磁碟視窗、預視視窗 的大小」),
// 開關在設定對話框裡(「檔案列表要列出磁碟機?」)。重製版沒有那個
// 對話框,改綁一個鍵 —— 跟 Alt-P 預視窗格同一個作法。
func (a *App) toggleDrivePane() bool {
	if a.Browser.DrivePane > 0 {
		a.Browser.DrivePane = 0
		a.Browser.Drives = nil
		a.DriveFocus = false
		return true
	}
	a.Browser.Drives = vfs.Drives()
	if len(a.Browser.Drives) == 0 {
		a.Message = i18n.T("找不到可切換的磁碟")
		return true
	}
	a.Browser.DrivePane = DrivePaneCols
	a.Browser.DriveCursor = a.currentDriveIndex()
	return true
}

// currentDriveIndex 找出目前目錄落在哪一個磁碟上,讓游標一開就停在對的那列。
// 比對用最長前綴 —— "/" 是每個路徑的前綴,先比到它就永遠選不到別的。
func (a *App) currentDriveIndex() int {
	best, bestLen := 0, -1
	for i, d := range a.Browser.Drives {
		if hasPathPrefix(a.Browser.Dir, d.Path) && len(d.Path) > bestLen {
			best, bestLen = i, len(d.Path)
		}
	}
	return best
}

func hasPathPrefix(p, prefix string) bool {
	if p == prefix {
		return true
	}
	if len(p) <= len(prefix) || p[:len(prefix)] != prefix {
		return false
	}
	// 前綴要停在路徑分隔上,否則 "/media" 會是 "/mediafoo" 的前綴。
	return prefix[len(prefix)-1] == '/' || p[len(prefix)] == '/' || p[len(prefix)] == '\\'
}

// driveKey 處理焦點在磁碟窗格時的按鍵。
func (a *App) driveKey(k keys.Key) bool {
	ds := a.Browser.Drives
	switch k.Code {
	case keys.Up:
		if a.Browser.DriveCursor > 0 {
			a.Browser.DriveCursor--
		}
		return true
	case keys.Down:
		if a.Browser.DriveCursor < len(ds)-1 {
			a.Browser.DriveCursor++
		}
		return true
	case keys.Home:
		a.Browser.DriveCursor = 0
		return true
	case keys.End:
		a.Browser.DriveCursor = len(ds) - 1
		return true
	case keys.Enter, keys.Right:
		if i := a.Browser.DriveCursor; i >= 0 && i < len(ds) {
			a.DriveFocus = false
			if err := a.Browser.Load(ds[i].Path); err != nil {
				a.Message = err.Error()
			}
		}
		return true
	case keys.Esc, keys.Left, keys.Tab:
		a.DriveFocus = false
		return true
	}
	return false
}
