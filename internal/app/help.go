package app

import (
	_ "embed"

	"github.com/wicanr2/wincv-remake/internal/markdown"
)

// helpText 是 F1 的使用說明。
//
// 內嵌而不是讀檔:說明必須在任何情況下都叫得出來,包括沒有安裝目錄、
// 從壓縮檔裡跑、或是 Android 上根本沒有可讀的程式目錄的時候。
// 一份找不到檔案就打不開的說明,正好在最需要它的時候不在。
//
//go:embed help.md
var helpText string

// openHelp 用 markdown 模式顯示使用說明。
//
// 借用既有的 markdown 檢視器而不是另寫一個畫面:說明書要的東西
// (標題階層、清單、表格、程式碼樣式)那邊都已經有了,而且使用者
// 在看 .md 檔時學到的按鍵在這裡照樣通。
// helpName 是說明在文件模式裡用的「檔名」。它不是檔案:session 還原與
// 逐檔位置記憶都要認得它,才不會拿它去 stat 或記進位置表。
const helpName = "使用說明"

func (a *App) openHelp() bool {
	a.md = mdView{name: helpName, blocks: markdown.Parse(helpText)}
	a.Mode = ModeMarkdown
	a.mdReturn = false
	return true
}
