// textencsum 列出每個檔案的編碼判讀結果,與強制用各種編碼解碼後的 sha256,
// 當 Rust 改寫的對照真值。
//
//	tools/go.sh run ./tools/textencsum 檔案...
//	tools/go.sh run ./tools/textencsum -dump GBK 檔案   # 直接輸出解碼後的文字
//
// 每列「路徑<TAB>判讀結果<TAB>編碼=sha256:字元數:U+FFFD 個數 ...」。解碼結果先轉成 []rune 再
// 編回 UTF-8:Go 的字串可以裝不合法的 UTF-8,這樣做等於把每個壞位元組
// 換成一個 U+FFFD,與畫面上看到的一致。Rust 端的 textencsum 輸出同樣格式。
package main

import (
	"crypto/sha256"
	"fmt"
	"os"
	"strings"

	"github.com/wicanr2/wincv-remake/internal/textenc"
)

var all = []textenc.Enc{textenc.UTF8, textenc.UTF16LE, textenc.UTF16BE,
	textenc.Big5, textenc.GBK, textenc.ShiftJIS, textenc.EUCKR, textenc.Binary}

func main() {
	if len(os.Args) == 4 && os.Args[1] == "-dump" {
		b, err := os.ReadFile(os.Args[3])
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		for _, e := range all {
			if e.String() == os.Args[2] {
				os.Stdout.WriteString(string([]rune(textenc.Decode(b, e))))
				return
			}
		}
		fmt.Fprintln(os.Stderr, "不認得的編碼", os.Args[2])
		os.Exit(1)
	}
	for _, p := range os.Args[1:] {
		b, err := os.ReadFile(p)
		if err != nil {
			fmt.Fprintln(os.Stderr, p, err)
			continue
		}
		var parts []string
		for _, e := range all {
			rs := []rune(textenc.Decode(b, e))
			bad := 0
			for _, r := range rs {
				if r == 0xFFFD {
					bad++
				}
			}
			parts = append(parts, fmt.Sprintf("%s=%x:%d:%d", e, sha256.Sum256([]byte(string(rs))), len(rs), bad))
		}
		fmt.Printf("%s\t%s\t%s\n", p, textenc.Detect(b), strings.Join(parts, " "))
	}
}
