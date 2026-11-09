// vecsum 列出一串字的向量外框,當 Rust 改寫(wincv-vecfont)的對照真值。
//
//	tools/go.sh run ./tools/vecsum [字型] < 字串
//
// 給了字型就只用那一份;沒給就用 vecfont.Default()(內嵌加系統字型)。每個字一列:
// 「U+XXXX<TAB>前進寬度<TAB>外框」,外框是以空白分隔的「op:x,y[,x,y…]」。
// 座標與寬度都乘 64 寫成整數(外框本來就是 26.6 定點,這樣寫不會有捨入差)。
// 字型沒有這個字時寫「U+XXXX<TAB>-」。Rust 端的 vecsum 輸出同樣格式。
package main

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/wicanr2/wincv-remake/internal/vecfont"
)

func main() {
	set := vecfont.Default()
	if len(os.Args) == 2 {
		b, err := os.ReadFile(os.Args[1])
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		sf := vecfont.Parse(b)
		if sf == nil {
			fmt.Fprintln(os.Stderr, os.Args[1]+": 讀不起來")
			os.Exit(1)
		}
		set = vecfont.New(sf)
	}
	in, _ := io.ReadAll(os.Stdin)
	w := bufio.NewWriter(os.Stdout)
	defer w.Flush()
	for _, r := range string(in) {
		if r == '\n' {
			continue
		}
		segs, adv, ok := set.Glyph(r)
		if !ok {
			fmt.Fprintf(w, "U+%04X\t-\n", r)
			continue
		}
		var b strings.Builder
		for i, s := range segs {
			if i > 0 {
				b.WriteByte(' ')
			}
			n := map[byte]int{'m': 1, 'l': 1, 'q': 2, 'c': 3}[s.Op]
			b.WriteByte(s.Op)
			b.WriteByte(':')
			for j := 0; j < n*2; j++ {
				if j > 0 {
					b.WriteByte(',')
				}
				fmt.Fprintf(&b, "%d", int64(s.Args[j]*64))
			}
		}
		fmt.Fprintf(w, "U+%04X\t%d\t%s\n", r, int64(adv*64), b.String())
	}
}
