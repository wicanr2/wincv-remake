// mdsum 把 markdown 檔 Parse 再 Layout,印出每個區塊、每一列、每個 Piece 與每張 Pic
// 的所有欄位,當 Rust 改寫(rust/crates/markdown)的對照真值。
//
//	tools/go.sh run ./tools/mdsum <cols,cols,...> <檔案|-synth>...
//
// 排版固定用 cellW=8、cellH=16、DefaultTheme。markdown 檔的圖一律不載
// (載入器回錯誤「不載圖」),所以只驗得到 alt 與錯誤訊息那一行。
// 「-synth」是一組手組的區塊(Pre、自訂清單記號、表格、各種尺寸的圖),
// 涵蓋 markdown 解析器不會產生的部分;它的載入器把 src「寬x高」當成圖的尺寸。
//
// 每一列以 TAB 分欄,文字裡的 \ TAB 換行 CR 寫成 \\ \t \n \r:
//
//	F 檔名                                      每個檔案一行
//	B 序號 種類 階層 語言 有序 序號 記號 src alt 對齊   每個區塊
//	S 樣式 文字 href                            區塊的 Span
//	X 文字                                      區塊的 Lines
//	R 格數 格...                                表格的一列
//	C 寬度                                      每種寬度一行,之後是排版結果
//	L 列號 圖序號(沒有是 -) 圖的第幾列
//	P 文字 前景 背景 底線 href
//	I 序號 src alt 有圖 錯誤 格寬 格高 起始列
//	E 訊息                                      讀不到或不是 UTF-8,略過
//
// Rust 端的 mdsum 輸出同樣格式。
package main

import (
	"bufio"
	"errors"
	"fmt"
	"image"
	"image/color"
	"os"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/wicanr2/wincv-remake/internal/markdown"
)

var esc = strings.NewReplacer(`\`, `\\`, "\t", `\t`, "\n", `\n`, "\r", `\r`)

func b01(b bool) int {
	if b {
		return 1
	}
	return 0
}

// sizeImg 只有尺寸的圖:排版只看 Bounds,不必真的配置像素。
type sizeImg struct{ w, h int }

func (s sizeImg) ColorModel() color.Model { return color.RGBAModel }
func (s sizeImg) Bounds() image.Rectangle { return image.Rect(0, 0, s.w, s.h) }
func (s sizeImg) At(int, int) color.Color { return color.RGBA{} }

func noLoad(string) (image.Image, error) { return nil, errors.New("不載圖") }

func sizeLoad(src string) (image.Image, error) {
	a, b, ok := strings.Cut(src, "x")
	w, e1 := strconv.Atoi(a)
	h, e2 := strconv.Atoi(b)
	if !ok || e1 != nil || e2 != nil {
		return nil, errors.New("壞的尺寸 " + src)
	}
	return sizeImg{w, h}, nil
}

func synth() []markdown.Block {
	sp := func(t string, st markdown.Style, href string) markdown.Span {
		return markdown.Span{Text: t, Style: st, Href: href}
	}
	long := strings.Repeat("全形與 half 混排的一段字,", 12)
	return []markdown.Block{
		{Kind: markdown.Heading, Level: 3, Spans: []markdown.Span{sp(long, 0, "")}},
		{Kind: markdown.Heading, Level: 6, Spans: []markdown.Span{sp("六級", markdown.Bold, "")}},
		{Kind: markdown.Pre, Lines: []string{"a\tb\tc", "中\t文\tx", strings.Repeat("x", 130), strings.Repeat("全形", 70), ""}},
		{Kind: markdown.Pre, Lines: []string{"緊接著的 Pre 不空行"}},
		{Kind: markdown.Code, Lines: []string{"\t縮排\t的碼", strings.Repeat("碼", 80)}},
		{Kind: markdown.List, Level: 1, Marker: "[目錄]", Spans: []markdown.Span{sp(long, 0, "")}},
		{Kind: markdown.List, Level: 0, Marker: "•", Spans: []markdown.Span{sp("沒有尾巴空白的記號", 0, "")}},
		{Kind: markdown.List, Ordered: true, Num: 0, Spans: []markdown.Span{sp("零號", 0, "")}},
		{Kind: markdown.List, Ordered: true, Num: -5, Spans: []markdown.Span{sp("負號", 0, "")}},
		{Kind: markdown.List, Ordered: true, Num: 1234567, Level: 3, Spans: []markdown.Span{sp(long, markdown.Italic, "")}},
		{Kind: markdown.Para, Spans: []markdown.Span{
			sp("粗連結 ", markdown.Bold|markdown.Link, "http://a/b"),
			sp("粗碼", markdown.Bold|markdown.Mono, ""),
			sp(" 刪除 ", markdown.Strike, ""),
			sp("斜刪", markdown.Italic|markdown.Strike, ""),
			sp("　全形空白　開頭", 0, ""),
			sp("   ", 0, ""),
			sp(strings.Repeat("https://example.com/very/long/path/", 6), markdown.Link, "https://x"),
		}},
		{Kind: markdown.Quote, Spans: []markdown.Span{sp(long, 0, "")}},
		{Kind: markdown.Table, Aligns: []byte{'c', 'r'}, Rows: [][]string{
			{"置中", "靠右", "預設"},
			{"中文字格", "1", "a\tb"},
			{"短"},
			{strings.Repeat("寬", 60), "", strings.Repeat("w", 90)},
		}},
		{Kind: markdown.Table, Rows: [][]string{{"x"}, {"", ""}}},
		{Kind: markdown.Table},
		{Kind: markdown.Rule},
		{Kind: markdown.Image, Src: "160x160", Alt: "方圖"},
		{Kind: markdown.Image, Src: "100x8000", Alt: "直圖"},
		{Kind: markdown.Image, Src: "5000x10", Alt: "橫圖"},
		{Kind: markdown.Image, Src: "1x1", Alt: "點"},
		{Kind: markdown.Image, Src: "0x10", Alt: "零寬"},
		{Kind: markdown.Image, Src: "壞", Alt: strings.Repeat("很長的替代文字", 20)},
		{Kind: markdown.Para},
	}
}

func main() {
	if len(os.Args) < 3 {
		fmt.Fprintln(os.Stderr, "用法: mdsum <cols,cols,...> <檔案|-synth>...")
		os.Exit(2)
	}
	var widths []int
	for _, s := range strings.Split(os.Args[1], ",") {
		n, err := strconv.Atoi(s)
		if err != nil {
			fmt.Fprintln(os.Stderr, "寬度不是數字:", s)
			os.Exit(2)
		}
		widths = append(widths, n)
	}
	w := bufio.NewWriter(os.Stdout)
	defer w.Flush()
	for _, name := range os.Args[2:] {
		fmt.Fprintf(w, "F\t%s\n", esc.Replace(name))
		var blocks []markdown.Block
		load := noLoad
		if name == "-synth" {
			blocks, load = synth(), sizeLoad
		} else {
			b, err := os.ReadFile(name)
			if err != nil {
				fmt.Fprintln(w, "E\t讀不到")
				continue
			}
			if !utf8.Valid(b) {
				fmt.Fprintln(w, "E\t不是 UTF-8")
				continue
			}
			blocks = markdown.Parse(string(b))
		}
		for i, b := range blocks {
			fmt.Fprintf(w, "B\t%d\t%d\t%d\t%s\t%d\t%d\t%s\t%s\t%s\t%s\n", i, b.Kind, b.Level, esc.Replace(b.Lang),
				b01(b.Ordered), b.Num, esc.Replace(b.Marker), esc.Replace(b.Src), esc.Replace(b.Alt), esc.Replace(string(b.Aligns)))
			for _, s := range b.Spans {
				fmt.Fprintf(w, "S\t%d\t%s\t%s\n", s.Style, esc.Replace(s.Text), esc.Replace(s.Href))
			}
			for _, l := range b.Lines {
				fmt.Fprintf(w, "X\t%s\n", esc.Replace(l))
			}
			for _, r := range b.Rows {
				fmt.Fprintf(w, "R\t%d", len(r))
				for _, c := range r {
					fmt.Fprintf(w, "\t%s", esc.Replace(c))
				}
				fmt.Fprintln(w)
			}
		}
		for _, cols := range widths {
			fmt.Fprintf(w, "C\t%d\n", cols)
			lines, pics := markdown.Layout(blocks, cols, 8, 16, load, markdown.DefaultTheme())
			for li, l := range lines {
				pi := "-"
				for j, p := range pics {
					if l.Pic == p {
						pi = strconv.Itoa(j)
					}
				}
				fmt.Fprintf(w, "L\t%d\t%s\t%d\n", li, pi, l.PicRow)
				for _, p := range l.Pieces {
					fmt.Fprintf(w, "P\t%s\t%d\t%d\t%d\t%s\n", esc.Replace(p.Text), p.FG, p.BG, b01(p.Under), esc.Replace(p.Href))
				}
			}
			for j, p := range pics {
				fmt.Fprintf(w, "I\t%d\t%s\t%s\t%d\t%s\t%d\t%d\t%d\n", j, esc.Replace(p.Src), esc.Replace(p.Alt),
					b01(p.Img != nil), esc.Replace(p.Err), p.Cols, p.Rows, p.Row)
			}
		}
	}
}
