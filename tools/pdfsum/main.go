// pdfsum 印出一份 PDF 經 internal/pdfdoc 取出的內容,當 Rust 改寫(wincv-pdfdoc)的對照。
//
//	tools/go.sh run ./tools/pdfsum [-pages N] [-glyphs] [-shot 目錄] [-dpi 96] 檔案.pdf
//
// 每列以 TAB 分隔,第一欄是種類:
//
//	file      路徑
//	pages     頁數
//	size      頁 寬 高                 (point,取兩位小數)
//	bookmark  層 頁 標題
//	line      頁 Y 縮排 文字
//	glyph     頁 X Y W 字級 字        (只在 -glyphs 時印)
//	image     頁 名稱 位元組數 sha256 寬x高 像素sha256 平均RGBA
//	shot      頁 寬x高 dpi 檔名       (只在 -shot 時印)
//	error     段落 [頁] 訊息
//
// 文字欄位裡的反斜線、TAB、換行跳脫成 \\、\t、\n、\r。Rust 端的
// `cargo run -p wincv-pdfdoc --example pdfsum` 印同樣的格式,兩邊直接 diff。
// 單一段落 panic 時印 error 列並繼續,不讓一個壞掉的段落蓋掉其餘的結果。
package main

import (
	"crypto/sha256"
	"encoding/hex"
	"flag"
	"fmt"
	"image"
	"image/draw"
	"image/png"
	"os"
	"path/filepath"
	"strings"

	"github.com/wicanr2/wincv-remake/internal/imgfmt"
	"github.com/wicanr2/wincv-remake/internal/pdf"
	"github.com/wicanr2/wincv-remake/internal/pdfdoc"
)

var (
	maxPages = flag.Int("pages", 0, "只看前 N 頁(0 表示全部)")
	glyphs   = flag.Bool("glyphs", false, "另外印出每個字的座標")
	shotDir  = flag.String("shot", "", "把頁面畫成 PNG 放在這個目錄")
	shotN    = flag.Int("shotpages", 1, "-shot 畫前幾頁")
	dpi      = flag.Float64("dpi", 96, "-shot 的解析度")
)

func esc(s string) string {
	return strings.NewReplacer(`\`, `\\`, "\t", `\t`, "\n", `\n`, "\r", `\r`).Replace(s)
}

func f2(v float64) string { return fmt.Sprintf("%.2f", v) }

// guard 執行一段,panic 時印成 error 列。
func guard(section string, page int, fn func()) {
	defer func() {
		if r := recover(); r != nil {
			if page > 0 {
				fmt.Printf("error\t%s\t%d\tpanic: %s\n", section, page, esc(fmt.Sprint(r)))
			} else {
				fmt.Printf("error\t%s\tpanic: %s\n", section, esc(fmt.Sprint(r)))
			}
		}
	}()
	fn()
}

func main() {
	flag.Parse()
	if flag.NArg() != 1 {
		fmt.Fprintln(os.Stderr, "用法: pdfsum [-pages N] [-glyphs] [-shot 目錄] 檔案.pdf")
		os.Exit(2)
	}
	path := flag.Arg(0)
	fmt.Printf("file\t%s\n", esc(path))
	var d *pdfdoc.Doc
	var err error
	guard("open", 0, func() { d, err = pdfdoc.Open(path) })
	if d == nil {
		if err != nil {
			fmt.Printf("error\topen\t%s\n", esc(err.Error()))
		}
		return
	}
	defer d.Close()
	fmt.Printf("pages\t%d\n", d.Pages)
	n := d.Pages
	if *maxPages > 0 && n > *maxPages {
		n = *maxPages
	}
	for p := 1; p <= n; p++ {
		guard("size", p, func() {
			w, h, err := d.PageSize(p)
			if err != nil {
				fmt.Printf("error\tsize\t%d\t%s\n", p, esc(err.Error()))
				return
			}
			fmt.Printf("size\t%d\t%s\t%s\n", p, f2(w), f2(h))
		})
	}
	guard("outline", 0, func() {
		for _, b := range d.Outline() {
			fmt.Printf("bookmark\t%d\t%d\t%s\n", b.Level, b.Page, esc(b.Title))
		}
	})
	for p := 1; p <= n; p++ {
		if *glyphs {
			guard("glyphs", p, func() { printGlyphs(path, p) })
		}
		guard("text", p, func() {
			lines, err := d.Text(p)
			if err != nil {
				fmt.Printf("error\ttext\t%d\t%s\n", p, esc(err.Error()))
				return
			}
			for _, l := range lines {
				fmt.Printf("line\t%d\t%s\t%d\t%s\n", p, f2(l.Y), l.Indent, esc(l.Text))
			}
		})
		guard("images", p, func() {
			names, err := d.ImageNames(p)
			if err != nil {
				fmt.Printf("error\timages\t%d\t%s\n", p, esc(err.Error()))
				return
			}
			for _, name := range names {
				b, err := d.Image(p, name)
				if err != nil {
					fmt.Printf("error\timage\t%d\t%s\n", p, esc(err.Error()))
					continue
				}
				sum := sha256.Sum256(b)
				fmt.Printf("image\t%d\t%s\t%d\t%s\t%s\n", p, esc(name), len(b), hex.EncodeToString(sum[:]), pixelSum(name, b))
			}
		})
	}
	if *shotDir != "" {
		for p := 1; p <= n && p <= *shotN; p++ {
			guard("shot", p, func() {
				r, err := d.Render(p, *dpi)
				if err != nil {
					fmt.Printf("error\tshot\t%d\t%s\n", p, esc(err.Error()))
					return
				}
				name := fmt.Sprintf("%d.png", p)
				f, err := os.Create(filepath.Join(*shotDir, name))
				if err != nil {
					fmt.Printf("error\tshot\t%d\t%s\n", p, esc(err.Error()))
					return
				}
				defer f.Close()
				if err := png.Encode(f, r.Img); err != nil {
					fmt.Printf("error\tshot\t%d\t%s\n", p, esc(err.Error()))
					return
				}
				fmt.Printf("shot\t%d\t%dx%d\t%s\t%s\n", p, r.Img.Bounds().Dx(), r.Img.Bounds().Dy(), f2(r.DPI), name)
			})
		}
	}
}

// printGlyphs 印出排列之前的每個字。pdfdoc 不對外交出這一層,直接用 internal/pdf。
func printGlyphs(path string, p int) {
	pd, err := pdf.Open(path)
	if err != nil {
		fmt.Printf("error\tglyphs\t%d\t%s\n", p, esc(err.Error()))
		return
	}
	pg, err := pd.Page(p)
	if err != nil {
		fmt.Printf("error\tglyphs\t%d\t%s\n", p, esc(err.Error()))
		return
	}
	for _, t := range pg.Texts() {
		fmt.Printf("glyph\t%d\t%s\t%s\t%s\t%s\t%s\n", p, f2(t.X), f2(t.Y), f2(t.W), f2(t.Size), esc(t.S))
	}
}

// pixelSum 用 app 走的同一個解碼器(imgfmt)解圖,回「寬x高 像素sha256 平均RGBA」。
// 像素是非預乘的 RGBA8。解不開回「- - -」。
func pixelSum(name string, b []byte) string {
	m, _, err := imgfmt.Decode(name, b)
	if err != nil || m == nil {
		return "-\t-\t-"
	}
	r := m.Bounds()
	n := image.NewNRGBA(image.Rect(0, 0, r.Dx(), r.Dy()))
	draw.Draw(n, n.Bounds(), m, r.Min, draw.Src)
	sum := sha256.Sum256(n.Pix)
	var acc [4]float64
	for i := 0; i+3 < len(n.Pix); i += 4 {
		for c := 0; c < 4; c++ {
			acc[c] += float64(n.Pix[i+c])
		}
	}
	px := float64(len(n.Pix) / 4)
	if px == 0 {
		px = 1
	}
	return fmt.Sprintf("%dx%d\t%s\t%.1f,%.1f,%.1f,%.1f", r.Dx(), r.Dy(), hex.EncodeToString(sum[:]),
		acc[0]/px, acc[1]/px, acc[2]/px, acc[3]/px)
}
