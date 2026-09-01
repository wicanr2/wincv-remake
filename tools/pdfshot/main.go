// pdfshot 把 PDF 的一頁畫成 PNG。
//
// 給驗收用:同一份檔案讓 LibreOffice 也畫一張,再用 tools/inkdiff 比對
// 兩張的墨水分布。app 裡的頁面圖走的是同一個 internal/pdf.Render,
// 所以這裡畫得對,app 裡就畫得對。
//
//	tools/go.sh run ./tools/pdfshot -o out.png -page 1 -dpi 96 in.pdf
package main

import (
	"flag"
	"fmt"
	"image/png"
	"os"
	"strings"

	"github.com/wicanr2/wincv-remake/internal/pdf"
)

func main() {
	out := flag.String("o", "page.png", "輸出 PNG")
	page := flag.Int("page", 1, "第幾頁")
	dpi := flag.Float64("dpi", 96, "解析度")
	flag.Parse()
	if flag.NArg() != 1 {
		fmt.Fprintln(os.Stderr, "用法:pdfshot [旗標] 檔案.pdf")
		os.Exit(2)
	}
	d, err := pdf.Open(flag.Arg(0))
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	defer d.Close()
	p, err := d.Page(*page)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	r, err := p.Render(pdf.RenderOptions{DPI: *dpi})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	f, err := os.Create(*out)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	defer f.Close()
	if err := png.Encode(f, r.Img); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	b := r.Img.Bounds()
	fmt.Printf("%s  %d×%d px  %.0f dpi  共 %d 頁\n", *out, b.Dx(), b.Dy(), r.DPI, d.Pages)
	if len(r.Substituted) > 0 {
		fmt.Printf("用系統字型補畫:%s\n", strings.Join(r.Substituted, ", "))
	}
	if len(r.Missing) > 0 {
		fmt.Printf("畫不出來的字型:%s\n", strings.Join(r.Missing, ", "))
	}
}
