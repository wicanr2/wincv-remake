package main

import (
	"fmt"
	"strings"

	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"
)

// dumpPage 印出一頁的內容資料流與資源字典,用來看真實檔案到底怎麼畫。
func dumpPage(path string, page int, grep string) {
	ctx, err := read(path)
	if err != nil {
		fmt.Println("讀不到:", err)
		return
	}
	if err := ctx.EnsurePageCount(); err != nil {
		fmt.Println(err)
		return
	}
	d, _, _, err := ctx.PageDict(page, false)
	if err != nil || d == nil {
		fmt.Println("拿不到頁面:", err)
		return
	}
	res, _ := ctx.XRefTable.Dereference(d["Resources"])
	fmt.Println("--- Resources ---")
	printDict(ctx.XRefTable, res, 0)

	b, err := ctx.PageContent(d)
	if err != nil {
		fmt.Println("拿不到內容:", err)
		return
	}
	fmt.Printf("--- 內容 %d 位元組 ---\n", len(b))
	s := string(b)
	if grep == "" {
		if len(s) > 4000 {
			s = s[:4000] + "\n…(截斷)"
		}
		fmt.Println(s)
		return
	}
	for i, line := range strings.Split(s, "\n") {
		if strings.Contains(line, grep) {
			fmt.Printf("%6d: %s\n", i+1, line)
		}
	}
}

func printDict(x *model.XRefTable, o types.Object, depth int) {
	if depth > 3 {
		return
	}
	pad := strings.Repeat("  ", depth)
	switch v := o.(type) {
	case types.Dict:
		for k, e := range v {
			de, _ := x.Dereference(e)
			switch dv := de.(type) {
			case types.Dict:
				fmt.Printf("%s%s:\n", pad, k)
				printDict(x, dv, depth+1)
			case types.StreamDict:
				fmt.Printf("%s%s: (stream) %v\n", pad, k, dv.Dict)
			default:
				fmt.Printf("%s%s: %v\n", pad, k, de)
			}
		}
	}
}

// dumpLines 印出內容資料流的某個行區間。
func dumpLines(path string, page, from, to int) {
	ctx, err := read(path)
	if err != nil {
		fmt.Println(err)
		return
	}
	ctx.EnsurePageCount()
	d, _, _, err := ctx.PageDict(page, false)
	if err != nil || d == nil {
		fmt.Println("拿不到頁面")
		return
	}
	b, err := ctx.PageContent(d)
	if err != nil {
		fmt.Println(err)
		return
	}
	lines := strings.Split(string(b), "\n")
	for i := from; i <= to && i <= len(lines); i++ {
		if i >= 1 {
			fmt.Printf("%6d: %s\n", i, lines[i-1])
		}
	}
}

// dumpObj 印出一個物件編號的內容。
func dumpObj(path string, num int) {
	ctx, err := read(path)
	if err != nil {
		fmt.Println(err)
		return
	}
	o, err := ctx.XRefTable.Dereference(*types.NewIndirectRef(num, 0))
	if err != nil {
		fmt.Println(err)
		return
	}
	fmt.Printf("%d 0 obj  型別 %T\n%v\n", num, o, o)
	if d, ok := o.(types.Dict); ok {
		printDict(ctx.XRefTable, d, 1)
	}
	if sd, ok := o.(types.StreamDict); ok {
		printDict(ctx.XRefTable, sd.Dict, 1)
	}
}
