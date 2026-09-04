// i18nscan 掃出原始碼裡的中文字串,並分成「已經接上 i18n」與「還沒」。
//
// 掃描邏輯在 internal/i18n/srcscan(與 i18n 自己的測試共用同一份)。
// 這一支只負責輸出格式。
//
// 用法:
//
//	go run ./tools/i18nscan -mode todo   # 還沒接上的(檔案:行 + 內容)
//	go run ./tools/i18nscan -mode keys   # 已經接上的 key,JSON 陣列
//	go run ./tools/i18nscan -mode count  # 依套件統計
//	go run ./tools/i18nscan -mode plan   # 改寫計畫(TSV,餵給 tools/i18n-apply.py)
//
// plan 把每一條字串分成能不能自動包 i18n.T()。四種位置**不能**包:
//
//	const   常數運算式裡不能有函式呼叫。
//	case    switch 的 case 值包起來之後,比對的會是翻譯後的字串,
//	        而被比對的那一邊沒翻 —— 條件會安靜地永遠不成立。
//	var     套件層級的 var 在 init 時求值,那時語言還沒設定,
//	        會把啟動當下的語言凍進去。要搬進函式,或讓那個 var 存
//	        中文原文當 key、顯示的那一刻才過 i18n.T。
//	key     map 的 key 與 struct tag 同理:那是識別字不是介面文字。
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"

	"github.com/wicanr2/wincv-remake/internal/i18n/srcscan"
)

func main() {
	mode := flag.String("mode", "todo", "todo / keys / count / plan")
	root := flag.String("root", ".", "掃描起點")
	flag.Parse()

	hits, err := srcscan.Walk(*root)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	switch *mode {
	case "plan":
		for _, h := range hits {
			if h.Done {
				continue
			}
			fmt.Printf("%s\t%d\t%d\t%s\t%s\n", h.Pos.Filename, h.Pos.Offset,
				h.End-h.Pos.Offset, h.Kind, strconv.Quote(h.Text))
		}
	case "keys":
		seen := map[string]bool{}
		var out []string
		for _, h := range hits {
			if h.Done && !seen[h.Text] {
				seen[h.Text] = true
				out = append(out, h.Text)
			}
		}
		sort.Strings(out)
		// JSON 而不是一行一條:字串裡有換行(錯誤訊息常有 "\n"),
		// 一行一條會把一條 key 拆成兩條 —— 而拆出來的兩半看起來都像
		// 正常的句子,翻譯回來之後對不上任何一條原文,要等
		// TestNoOrphanKeys 才會抓到。
		enc := json.NewEncoder(os.Stdout)
		enc.SetEscapeHTML(false)
		enc.SetIndent("", "  ")
		if err := enc.Encode(out); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
	case "count":
		byPkg := map[string][2]int{}
		for _, h := range hits {
			d := filepath.Dir(h.Pos.Filename)
			c := byPkg[d]
			if h.Done {
				c[0]++
			} else {
				c[1]++
			}
			byPkg[d] = c
		}
		var dirs []string
		for d := range byPkg {
			dirs = append(dirs, d)
		}
		sort.Slice(dirs, func(i, j int) bool { return byPkg[dirs[i]][1] > byPkg[dirs[j]][1] })
		var td, tt int
		for _, d := range dirs {
			c := byPkg[d]
			td, tt = td+c[0], tt+c[1]
			fmt.Printf("%-28s 已接 %4d  待處理 %4d\n", d, c[0], c[1])
		}
		fmt.Printf("%-28s 已接 %4d  待處理 %4d\n", "總計", td, tt)
	default:
		for _, h := range hits {
			if !h.Done {
				fmt.Printf("%s:%d\t%s\n", h.Pos.Filename, h.Pos.Line, h.Text)
			}
		}
	}
}
