package app

import (
	"embed"

	"github.com/wicanr2/wincv-remake/internal/i18n"
	"github.com/wicanr2/wincv-remake/internal/markdown"
)

// helpText 是 F1 的使用說明。
//
// 內嵌而不是讀檔:說明必須在任何情況下都叫得出來,包括沒有安裝目錄、
// 從壓縮檔裡跑、或是 Android 上根本沒有可讀的程式目錄的時候。
// 一份找不到檔案就打不開的說明,正好在最需要它的時候不在。
//
//go:embed help.md help.*.md
var helpFS embed.FS

// helpText 回傳目前語言的使用說明。
//
// 找不到那個語言的版本就用繁體中文的 —— 說明是這個程式最需要「一定
// 叫得出來」的東西,少一份翻譯不該讓 F1 變成空白畫面。翻譯進度因此
// 可以一份一份來,不必等四種語言到齊才上線。
func helpText() string {
	if l := i18n.Current(); l != i18n.ZhHant {
		if b, err := helpFS.ReadFile("help." + string(l) + ".md"); err == nil {
			return string(b)
		}
	}
	b, _ := helpFS.ReadFile("help.md")
	return string(b)
}

// openHelp 用 markdown 模式顯示使用說明。
//
// 借用既有的 markdown 檢視器而不是另寫一個畫面:說明書要的東西
// (標題階層、清單、表格、程式碼樣式)那邊都已經有了,而且使用者
// 在看 .md 檔時學到的按鍵在這裡照樣通。
// helpName 是說明在文件模式裡用的「檔名」。它不是檔案:session 還原與
// 逐檔位置記憶都要認得它,才不會拿它去 stat 或記進位置表。
//
// **不翻譯**:它同時是識別字。翻了的話換一次語言就對不上上一次存的
// session,而症狀是「說明莫名其妙被記進位置表」。要顯示給人看的時候
// 才過 i18n.T(見 mdview 的標題)。
const helpName = "使用說明"

func (a *App) openHelp() bool {
	a.md = mdView{name: helpName, blocks: markdown.Parse(helpText())}
	a.Mode = ModeMarkdown
	a.mdReturn = false
	return true
}
