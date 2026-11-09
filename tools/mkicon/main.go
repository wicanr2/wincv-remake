// mkicon 由 internal/appicon 產生打包用的圖示檔。
//
//	tools/go.sh run ./tools/mkicon -out <目錄>
//
// 產出:
//
//	<目錄>/wincv.ico            Windows 執行檔資源用(16 到 256,PNG 壓縮)
//	<目錄>/wincv-<n>.png        各尺寸預覽
//	-android <res 目錄> 時另寫 mipmap-*/ic_launcher.png
//
// .ico 的每一張都存 PNG:Windows Vista 起支援,而 256 那張存 BMP 會多出
// 兩百多 KB,其他尺寸也沒有理由走兩套格式。
package main

import (
	"bytes"
	"encoding/binary"
	"flag"
	"fmt"
	"image"
	"image/png"
	"os"
	"path/filepath"

	"github.com/wicanr2/wincv-remake/internal/appicon"
)

func main() {
	out := flag.String("out", "", "輸出目錄")
	android := flag.String("android", "", "Android 的 res 目錄(留空不寫)")
	flag.Parse()
	if *out == "" {
		fmt.Fprintln(os.Stderr, "用法: mkicon -out <目錄> [-android <res 目錄>]")
		os.Exit(2)
	}
	must(os.MkdirAll(*out, 0o755))
	var pngs [][]byte
	for _, n := range appicon.Sizes {
		b := encode(appicon.Image(n))
		pngs = append(pngs, b)
		must(os.WriteFile(filepath.Join(*out, fmt.Sprintf("wincv-%d.png", n)), b, 0o644))
	}
	must(os.WriteFile(filepath.Join(*out, "wincv.ico"), ico(appicon.Sizes, pngs), 0o644))

	if *android != "" {
		// Android 的啟動器圖示:mdpi 48 為基準,依密度放大。
		for dir, n := range map[string]int{
			"mipmap-mdpi": 48, "mipmap-hdpi": 72, "mipmap-xhdpi": 96,
			"mipmap-xxhdpi": 144, "mipmap-xxxhdpi": 192,
		} {
			d := filepath.Join(*android, dir)
			must(os.MkdirAll(d, 0o755))
			must(os.WriteFile(filepath.Join(d, "ic_launcher.png"), encode(appicon.Image(n)), 0o644))
		}
	}
}

func encode(img image.Image) []byte {
	var b bytes.Buffer
	enc := png.Encoder{CompressionLevel: png.BestCompression}
	must(enc.Encode(&b, img))
	return b.Bytes()
}

// ico 組一個 ICONDIR:6 位元組檔頭、每張 16 位元組的目錄項,後面接 PNG。
// 寬高欄位是一個位元組,256 要寫成 0。
func ico(sizes []int, pngs [][]byte) []byte {
	var b bytes.Buffer
	le := binary.LittleEndian
	binary.Write(&b, le, [3]uint16{0, 1, uint16(len(sizes))})
	off := 6 + 16*len(sizes)
	for i, n := range sizes {
		dim := byte(n)
		if n >= 256 {
			dim = 0
		}
		b.Write([]byte{dim, dim, 0, 0})
		binary.Write(&b, le, [2]uint16{1, 32})
		binary.Write(&b, le, [2]uint32{uint32(len(pngs[i])), uint32(off)})
		off += len(pngs[i])
	}
	for _, p := range pngs {
		b.Write(p)
	}
	return b.Bytes()
}

func must(err error) {
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
