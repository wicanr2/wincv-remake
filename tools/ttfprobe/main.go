package main

import (
	"fmt"
	"os"

	"github.com/wicanr2/wincv-remake/internal/ttf"
)

func shortName(p string) string {
	if len(p) > 56 {
		return "..." + p[len(p)-53:]
	}
	return p
}

func main() {
	paths := ttf.Candidates()
	if len(os.Args) > 1 {
		paths = os.Args[1:]
	}
	for _, p := range paths {
		if _, err := os.Stat(p); err != nil {
			continue
		}
		f, err := ttf.Load(p, 8, 16, 16)
		if err != nil {
			fmt.Printf("%-58s 載入失敗: %v\n", shortName(p), err)
			continue
		}
		ok, bad := 0, []rune{}
		for _, r := range []rune("繁體简软计汉한글Ελληνικά→★☆●中文abc") {
			if f.Glyph(r) != nil {
				ok++
			} else {
				bad = append(bad, r)
			}
		}
		fmt.Printf("%-58s 有 %d 個,缺 %q\n", shortName(p), ok, string(bad))
	}
}
