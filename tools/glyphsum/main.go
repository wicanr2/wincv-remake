// glyphsum 用 ttf 把一份字型的一串字畫成字模,當 Rust 改寫的對照真值。
//
//	tools/go.sh run ./tools/glyphsum <字型> <半形寬> <全形寬> <高> <fit 0|1> <aa 0|1> < 字串
//
// fit=1 時用 LoadHalfFit(字級縮到半形塞得下)當半形來源,不讀字串,改畫 CP437 的
// 256 個字碼(列首寫「C+XX」),aa 不適用。其餘每個字一列:
// 「U+XXXX<TAB>寬<TAB>高<TAB>位元的 hex<TAB>覆蓋率的 hex」,字型沒有這個字時寫「U+XXXX<TAB>-」。
// Rust 端的 glyphsum 輸出同樣格式。
package main

import (
	"bufio"
	"encoding/hex"
	"fmt"
	"os"
	"strconv"

	"github.com/wicanr2/wincv-remake/internal/fnt"
	"github.com/wicanr2/wincv-remake/internal/ttf"
)

func main() {
	if len(os.Args) != 7 {
		fmt.Fprintln(os.Stderr, "用法: glyphsum <字型> <半形寬> <全形寬> <高> <fit 0|1> <aa 0|1> < 字串")
		os.Exit(2)
	}
	n := func(i int) int { v, _ := strconv.Atoi(os.Args[i]); return v }
	hw, fw, h := n(2), n(3), n(4)
	w := bufio.NewWriter(os.Stdout)
	defer w.Flush()
	if os.Args[5] == "1" {
		hf, err := ttf.LoadHalfFit(os.Args[1], hw, h)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		for c := 0; c < 256; c++ {
			put(w, fmt.Sprintf("C+%02X", c), hf.Glyph(byte(c)))
		}
		return
	}
	f, err := ttf.Load(os.Args[1], hw, fw, h)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	f.AA = os.Args[6] == "1"
	in, _ := bufio.NewReader(os.Stdin).ReadString(0)
	for _, r := range in {
		if r != '\n' {
			put(w, fmt.Sprintf("U+%04X", r), f.Glyph(r))
		}
	}
}

func put(w *bufio.Writer, key string, g *fnt.Glyph) {
	if g == nil {
		fmt.Fprintf(w, "%s\t-\n", key)
		return
	}
	bits := make([]byte, (len(g.Bits)+7)/8)
	for i, b := range g.Bits {
		if b {
			bits[i/8] |= 0x80 >> (i % 8)
		}
	}
	fmt.Fprintf(w, "%s\t%d\t%d\t%s\t%s\n", key, g.W, g.H, hex.EncodeToString(bits), hex.EncodeToString(g.Alpha))
}
