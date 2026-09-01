package main

import (
	"fmt"
	"image"
	_ "image/png"
	"os"
)

// dumpPixels 沿一條水平線取樣,用來看漸層到底有沒有在變。
//
// 墨水密度那種整頁的指標對「顏色對不對」是瞎的:一片平塗與一道漸層
// 的墨水量可以一樣。要驗顏色就得逐點看。
func dumpPixels(path string, y int, xs []int) {
	f, err := os.Open(path)
	if err != nil {
		fmt.Println(err)
		return
	}
	defer f.Close()
	m, _, err := image.Decode(f)
	if err != nil {
		fmt.Println(err)
		return
	}
	fmt.Printf("%s  %v\n", path, m.Bounds())
	for _, x := range xs {
		r, g, b, _ := m.At(x, y).RGBA()
		fmt.Printf("  (%4d,%4d) = %3d %3d %3d\n", x, y, r>>8, g>>8, b>>8)
	}
}
