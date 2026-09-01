package pdf

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"testing"

	"github.com/pdfcpu/pdfcpu/pkg/api"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"
)

// 掃一批真實 PDF 的嵌入字型,數 seac(用兩個標準字形拼出重音字)用了幾次。
// 設 WINCV_PDF_CORPUS 指到一個放 PDF 的目錄才會跑。
func TestSeacUsageInCorpus(t *testing.T) {
	dir := os.Getenv("WINCV_PDF_CORPUS")
	if dir == "" {
		t.Skip("設 WINCV_PDF_CORPUS 指到一個放 PDF 的目錄才會跑")
	}
	files, _ := filepath.Glob(filepath.Join(dir, "*.pdf"))
	sort.Strings(files)

	var t1Fonts, t1Glyphs, t1Seac, t1Accent int
	accented := map[string]bool{}
	var cffFonts, cffGlyphs, cffSeac int
	for _, f := range files {
		conf := model.NewDefaultConfiguration()
		conf.ValidationMode = model.ValidationRelaxed
		ctx, err := api.ReadContextFile(f)
		if err != nil {
			continue
		}
		nums := make([]int, 0, len(ctx.XRefTable.Table))
		for n := range ctx.XRefTable.Table {
			nums = append(nums, n)
		}
		sort.Ints(nums)
		for _, n := range nums {
			o, err := ctx.XRefTable.Dereference(*types.NewIndirectRef(n, 0))
			if err != nil {
				continue
			}
			d, ok := o.(types.Dict)
			if !ok {
				continue
			}
			if ref, ok := d["FontFile"]; ok {
				if b := streamBytes(ctx.XRefTable, ref); len(b) > 0 {
					if ft, err := parseType1(b); err == nil {
						t1Fonts++
						for name, cs := range ft.chars {
							t1Glyphs++
							if scanT1Seac(cs) {
								t1Seac++
							}
							// 重音字的字形名一定含這幾個尾綴之一。
							for _, suf := range []string{"acute", "grave", "circumflex",
								"dieresis", "tilde", "cedilla", "ring", "caron", "breve"} {
								if len(name) > len(suf) && name[len(name)-len(suf):] == suf {
									t1Accent++
									accented[name] = true
								}
							}
						}
					}
				}
			}
			if ref, ok := d["FontFile3"]; ok {
				if b := streamBytes(ctx.XRefTable, ref); len(b) > 0 {
					if cf, err := parseCFF(b); err == nil {
						cffFonts++
						for _, cs := range cf.charStrings {
							cffGlyphs++
							if scanT2Seac(cs) {
								cffSeac++
							}
						}
					}
				}
			}
		}
	}
	fmt.Printf("Type1:%d 份字型 %d 個字形,用 seac 的 %d 個;"+
		"字形名看起來是重音字的 %d 個(%d 種)\n", t1Fonts, t1Glyphs, t1Seac, t1Accent, len(accented))
	names := make([]string, 0, len(accented))
	for n := range accented {
		names = append(names, n)
	}
	sort.Strings(names)
	if len(names) > 20 {
		names = names[:20]
	}
	fmt.Printf("  例:%v\n", names)
	fmt.Printf("CFF  :%d 份字型 %d 個字形,用 seac 式 endchar 的 %d 個\n", cffFonts, cffGlyphs, cffSeac)
}

// 正對照:直接掃系統上的 .pfb。零結果要先證明「掃得到的時候真的掃得到」,
// 不然「沒有」與「掃錯了」長得一模一樣。
func TestSeacUsageInSystemType1(t *testing.T) {
	dir := os.Getenv("WINCV_T1_DIR")
	if dir == "" {
		t.Skip("設 WINCV_T1_DIR 指到一個放 .pfb 的目錄才會跑,例如 /usr/share/fonts/X11/Type1")
	}
	files, _ := filepath.Glob(filepath.Join(dir, "*.pfb"))
	sort.Strings(files)
	total, seac, fonts := 0, 0, 0
	for _, f := range files {
		b, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		ft, err := parseType1(b)
		if err != nil {
			t.Logf("%s 解不開:%v", filepath.Base(f), err)
			continue
		}
		fonts++
		n := 0
		for _, cs := range ft.chars {
			total++
			if scanT1Seac(cs) {
				seac++
				n++
			}
		}
		if n > 0 {
			fmt.Printf("  %-32s %d / %d 個字形用 seac\n", filepath.Base(f), n, len(ft.chars))
		}
	}
	fmt.Printf("系統 Type1:%d 份字型 %d 個字形,用 seac 的 %d 個\n", fonts, total, seac)
}

// 掃描器本身的對照:手工組一條含 seac 的 charstring,以及一條沒有的。
func TestSeacScanners(t *testing.T) {
	// Type1:「139 139 139 139 139 12 6」= 五個 0 再加 seac。
	if !scanT1Seac([]byte{139, 139, 139, 139, 139, 12, 6}) {
		t.Error("Type1 掃描器漏掉 seac")
	}
	// 12 6 出現在運算元裡不算:247 之後那個位元組是數值的一部分。
	if scanT1Seac([]byte{247, 12, 6, 14}) {
		t.Error("Type1 掃描器把運算元當成 seac")
	}
	// Type2:四個運算元 + endchar。
	if !scanT2Seac([]byte{139, 139, 139, 139, 14}) {
		t.Error("Type2 掃描器漏掉四參數的 endchar")
	}
	// 一般的 endchar(沒有運算元,或只有一個寬度)不算。
	if scanT2Seac([]byte{14}) || scanT2Seac([]byte{139, 14}) {
		t.Error("Type2 掃描器把一般的 endchar 當成 seac")
	}
	// hintmask 後面的遮罩位元組不能被當成指令。這條的遮罩是 0x0E,
	// 沒跳過的話會被讀成 14(endchar)。
	if scanT2Seac([]byte{139, 139, 1, 19, 0x0E, 139, 139, 139, 139, 14}) != true {
		t.Error("Type2 掃描器沒有正確跳過 hintmask 的遮罩")
	}
}

// scanT1Seac 掃一條 Type1 charstring 有沒有 seac(12 6)。
// Type1 沒有 hintmask 那種吃後續位元組的運算子,所以逐一走過去就是精確的。
func scanT1Seac(cs []byte) bool {
	for i := 0; i < len(cs); {
		b := cs[i]
		switch {
		case b >= 32 && b <= 246:
			i++
		case b >= 247 && b <= 254:
			i += 2
		case b == 255:
			i += 5
		case b == 12:
			if i+1 < len(cs) && cs[i+1] == 6 {
				return true
			}
			i += 2
		default:
			i++
		}
	}
	return false
}

// scanT2Seac 掃一條 Type2 charstring 有沒有「帶四個運算元的 endchar」——
// CFF 沒有 seac 運算子,拼字是用 endchar 的舊式四參數形式。
//
// [雷] hintmask / cntrmask 後面跟著一段遮罩位元組,長度由前面宣告的
// stem 數決定。不數 stem 就會把遮罩當成指令解,而解出來仍然是一串
// 合法的指令,只是全錯。
func scanT2Seac(cs []byte) bool {
	stems, args := 0, 0
	for i := 0; i < len(cs); {
		b := cs[i]
		switch {
		case b >= 32 && b <= 246:
			i++
			args++
		case b >= 247 && b <= 254:
			i += 2
			args++
		case b == 255:
			i += 5
			args++
		case b == 28:
			i += 3
			args++
		case b == 12:
			i += 2
			args = 0
		case b == 1 || b == 3 || b == 18 || b == 23: // hstem vstem hstemhm vstemhm
			stems += args / 2
			args = 0
			i++
		case b == 19 || b == 20: // hintmask cntrmask
			stems += args / 2
			args = 0
			i++
			i += (stems + 7) / 8
		case b == 14: // endchar
			return args >= 4
		default:
			args = 0
			i++
		}
	}
	return false
}
