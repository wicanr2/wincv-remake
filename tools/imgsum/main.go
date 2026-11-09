// imgsum 用 imgfmt 解一批圖檔,每張的像素(非預乘 RGBA8)寫成原始檔,
// 當 Rust 改寫的對照真值。
//
//	tools/go.sh run ./tools/imgsum <輸出目錄> < 清單
//
// 清單每行一個圖檔路徑。從 stdin 讀而不收參數:清單很長時 xargs 會分批執行,
// 每批的序號都從 0 起算,序號就對不上檔名了。
// stdout 每列「序號<TAB>格式<TAB>寬<TAB>高<TAB>sha256」,解不開的寫「序號<TAB>ERR<TAB>訊息」。
// 第 i 張的像素在 <輸出目錄>/<i>.rgba。Rust 端的 imgsum 輸出同樣格式。
package main

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"image"
	"bufio"
	"image/draw"
	"os"
	"path/filepath"

	"github.com/wicanr2/wincv-remake/internal/imgfmt"
)

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintln(os.Stderr, "用法: imgsum <輸出目錄> < 清單")
		os.Exit(2)
	}
	out := os.Args[1]
	sc := bufio.NewScanner(os.Stdin)
	sc.Buffer(make([]byte, 0, 64*1024), 1<<20)
	for i := 0; sc.Scan(); i++ {
		name := sc.Text()
		data, err := os.ReadFile(name)
		if err != nil {
			fmt.Printf("%d\tERR\t%v\n", i, err)
			continue
		}
		img, kind, err := decode(name, data)
		if err != nil {
			fmt.Printf("%d\tERR\t%v\n", i, err)
			continue
		}
		b := img.Bounds()
		n := image.NewNRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
		draw.Draw(n, n.Bounds(), img, b.Min, draw.Src)
		sum := sha256.Sum256(n.Pix)
		os.WriteFile(filepath.Join(out, fmt.Sprintf("%d.rgba", i)), n.Pix, 0o644)
		fmt.Printf("%d\t%s\t%d\t%d\t%s\n", i, kind, b.Dx(), b.Dy(), hex.EncodeToString(sum[:]))
	}
}

// decode 把解碼器的 panic 轉成錯誤,一張壞圖不影響其餘的。
func decode(name string, data []byte) (img image.Image, kind string, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("panic: %v", r)
		}
	}()
	return imgfmt.Decode(name, data)
}
