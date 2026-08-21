package gopher

import (
	"strings"

	"github.com/wicanr2/wincv-remake/internal/markdown"
	"github.com/wicanr2/wincv-remake/internal/textenc"
)

// Label 是項目型別給人看的名稱。
//
// 用中文而不是原始的型別字元:畫面上一整排 "0"、"1"、"I" 對使用者
// 沒有意義,而這些字元本來就是給程式看的。
func Label(t byte) string {
	switch t {
	case TypeText:
		return "文字"
	case TypeMenu:
		return "目錄"
	case TypeError:
		return "錯誤"
	case TypeSearch:
		return "查詢"
	case TypeBinary, TypeDoc:
		return "檔案"
	case TypeGIF, TypeImage, TypePNG:
		return "圖片"
	case TypeHTML:
		return "網頁"
	case TypeSound:
		return "音訊"
	case '2':
		return "電話簿"
	case '4', '5', '6':
		return "封裝檔"
	case '8', 'T':
		return "終端機"
	case '+':
		return "備援"
	}
	return "?"
}

// IsImage 說明這個型別該用看圖模式開。
func IsImage(t byte) bool {
	switch t {
	case TypeGIF, TypeImage, TypePNG:
		return true
	}
	return false
}

// MenuBlocks 把選單排成可以交給 markdown.Layout 的區塊。
//
// 每一列一個 List 區塊 —— 選 List 是因為 markdown 的排版引擎在連續的
// List 之間不插空行,而選單要單倍行距。型別標籤放在 Marker,
// 顯示文字放在 Spans,連結掛在 Span.Href。
//
// 資訊列(型別 i)沒有目標,所以不給 Href,也不給標籤 —— 它就是一行說明,
// 不該長得像可以點的東西。
func MenuBlocks(items []Item) []markdown.Block {
	out := make([]markdown.Block, 0, len(items))
	for _, it := range items {
		if !it.IsLink() {
			// 資訊列用 Pre:原樣輸出,不加項目符號也不縮排。
			// 那是站台作者自己排的版面(橫線、置中的標題、ASCII banner),
			// 前面多任何一個字元都會把它推歪。
			out = append(out, markdown.Block{
				Kind: markdown.Pre, Lines: []string{it.Display},
			})
			continue
		}
		sp := markdown.Span{Text: it.Display, Style: markdown.Link}
		if web, ok := it.WebURL(); ok {
			// 網頁連結交出原始位址。這個客戶端不解 HTTP,
			// 上層決定要不要丟給系統瀏覽器。
			sp.Href = web
		} else {
			sp.Href = it.URL().String()
		}
		if sp.Text == "" {
			sp.Text = sp.Href
		}
		out = append(out, markdown.Block{
			Kind: markdown.List, Marker: "[" + Label(it.Type) + "] ",
			Spans: []markdown.Span{sp},
		})
	}
	return out
}

// TextBlocks 把文字檔排成區塊。
//
// 用 Pre 而不是 Para:gopher 的文字內容幾乎都是已經排好版的
// (70 欄的說明、ASCII art、表格),重新斷行會把它們弄壞。
//
// 編碼交給 textenc 判讀 —— gopher 沒有 charset 欄位,而中文站台
// 那個年代多半是 Big5。
func TextBlocks(b []byte) []markdown.Block {
	s := textenc.Decode(b, textenc.Detect(b))
	s = strings.ReplaceAll(s, "\r\n", "\n")
	lines := strings.Split(s, "\n")
	// 協定用單獨一列的 "." 結束,那不是內容。
	for i, ln := range lines {
		if ln == "." {
			lines = lines[:i]
			break
		}
	}
	// 尾端的空行沒有意義,但中間的有(段落分隔),只砍尾端。
	for len(lines) > 0 && strings.TrimSpace(lines[len(lines)-1]) == "" {
		lines = lines[:len(lines)-1]
	}
	if len(lines) == 0 {
		return nil
	}
	return []markdown.Block{{Kind: markdown.Pre, Lines: lines}}
}
