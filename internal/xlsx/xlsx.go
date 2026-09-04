// Package xlsx 讀 Excel 的 .xlsx(SpreadsheetML)。
//
// 一張工作表在字元格點上就是一個表格。要處理的不是版面而是**值**:
// 試算表裡存的是數字與參照,人看到的字是「數字加上格式」算出來的。
// 兩件事因此必須做:
//
//   - 共用字串表:儲存格寫的是編號,字在另一個組件裡。
//   - 日期:Excel 的日期是一個天數,靠格式碼才知道它是日期不是數字。
//     不還原的話一整欄日期會變成 45000 這種數字,而那是合法的顯示結果,
//     不會有任何錯誤。
//
// 沒有做的是完整的數字格式引擎(千分位、會計格式、條件式格式碼)。
// 那要把 Excel 的格式碼語言整套實作,而在字元格點上看一份試算表時,
// 「數字本身是對的」比「逗號點在正確的位置」重要。
package xlsx

import (
	"encoding/xml"
	"fmt"
	"github.com/wicanr2/wincv-remake/internal/i18n"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/wicanr2/wincv-remake/internal/markdown"
	"github.com/wicanr2/wincv-remake/internal/ooxml"
)

// 一張表最多取這麼多列與欄。
//
// 上限存在的理由是畫面而不是記憶體:一份三萬列的表格排進格點要花很久,
// 而看的人最多往下捲幾百列。被截掉時會明講。
const (
	MaxRows = 4000
	MaxCols = 128
)

// Book 是一份打開著的活頁簿。用完要 Close。
type Book struct {
	Title  string
	Sheets []Sheet

	pkg     *ooxml.Package
	shared  []string
	styles  []cellStyle
	epoch   time.Time
	loaded  map[int][]markdown.Block
	wbPart  string
	numFmts map[int]string
}

// Sheet 是一張工作表。
type Sheet struct {
	Name   string
	Part   string
	Hidden bool
}

type cellStyle struct {
	numFmtID int
	code     string
}

// Open 打開一份 .xlsx。
func Open(name string) (*Book, error) {
	p, err := ooxml.Open(name)
	if err != nil {
		return nil, err
	}
	b, err := New(p)
	if err != nil {
		p.Close()
		return nil, err
	}
	return b, nil
}

// New 從一個已經打開的 OPC 包建 Book。
func New(p *ooxml.Package) (*Book, error) {
	wb := wbPart(p)
	if wb == "" {
		return nil, fmt.Errorf(i18n.T("這不是 Excel 活頁簿(找不到 workbook.xml)"))
	}
	b := &Book{pkg: p, wbPart: wb, loaded: map[int][]markdown.Block{},
		numFmts: map[int]string{}, epoch: time.Date(1899, 12, 30, 0, 0, 0, 0, time.UTC)}
	b.readWorkbook()
	b.readShared()
	b.readStyles()
	if len(b.Sheets) == 0 {
		return nil, fmt.Errorf(i18n.T("這份活頁簿沒有工作表"))
	}
	return b, nil
}

func (b *Book) Close() error { return b.pkg.Close() }

func wbPart(p *ooxml.Package) string {
	for _, t := range p.RelsByType("", "/officeDocument") {
		if p.Has(t) {
			return t
		}
	}
	if p.Has("xl/workbook.xml") {
		return "xl/workbook.xml"
	}
	return ""
}

func (b *Book) readWorkbook() {
	raw, err := b.pkg.Bytes(b.wbPart)
	if err != nil {
		return
	}
	dec := ooxml.NewDecoder(raw)
	root, err := ooxml.Root(dec)
	if err != nil || root.Name.Local != "workbook" {
		return
	}
	_ = ooxml.Each(dec, func(se xml.StartElement) (bool, error) {
		switch se.Name.Local {
		case "workbookPr":
			// [雷] 1904 制的活頁簿(Mac 上建的)日期基準差 1462 天。
			// 照 1900 制解會整批早四年,而每一個日期看起來都是合法日期。
			if ooxml.Attr(se, "date1904") == "1" || strings.EqualFold(ooxml.Attr(se, "date1904"), "true") {
				b.epoch = time.Date(1904, 1, 1, 0, 0, 0, 0, time.UTC)
			}
		case "sheets":
			return true, ooxml.Each(dec, func(s xml.StartElement) (bool, error) {
				if s.Name.Local != "sheet" {
					return false, nil
				}
				t := b.pkg.RelTarget(b.wbPart, ooxml.RelID(s))
				if t == "" || !b.pkg.Has(t) {
					return false, nil
				}
				b.Sheets = append(b.Sheets, Sheet{
					Name:   ooxml.Attr(s, "name"),
					Part:   t,
					Hidden: ooxml.Attr(s, "state") != "" && ooxml.Attr(s, "state") != "visible",
				})
				return false, nil
			})
		}
		return false, nil
	})
}

func (b *Book) readShared() {
	part := b.relPart("/sharedStrings")
	if part == "" {
		return
	}
	raw, err := b.pkg.Bytes(part)
	if err != nil {
		return
	}
	dec := ooxml.NewDecoder(raw)
	if _, err := ooxml.Root(dec); err != nil {
		return
	}
	_ = ooxml.Each(dec, func(se xml.StartElement) (bool, error) {
		if se.Name.Local != "si" {
			return false, nil
		}
		// 一個項目可以是單純的 <t>,也可以是好幾段帶樣式的 <r><t>。
		// 兩種都只要文字,接起來就是。
		b.shared = append(b.shared, ooxml.Text(dec))
		return true, nil
	})
}

func (b *Book) readStyles() {
	part := b.relPart("/styles")
	if part == "" {
		return
	}
	raw, err := b.pkg.Bytes(part)
	if err != nil {
		return
	}
	dec := ooxml.NewDecoder(raw)
	if _, err := ooxml.Root(dec); err != nil {
		return
	}
	_ = ooxml.Each(dec, func(se xml.StartElement) (bool, error) {
		switch se.Name.Local {
		case "numFmts":
			return true, ooxml.Each(dec, func(n xml.StartElement) (bool, error) {
				if n.Name.Local == "numFmt" {
					b.numFmts[atoiDef(ooxml.Attr(n, "numFmtId"), -1)] = ooxml.Attr(n, "formatCode")
				}
				return false, nil
			})
		case "cellXfs":
			return true, ooxml.Each(dec, func(x xml.StartElement) (bool, error) {
				if x.Name.Local == "xf" {
					b.styles = append(b.styles, cellStyle{numFmtID: atoiDef(ooxml.Attr(x, "numFmtId"), 0)})
				}
				return false, nil
			})
		}
		return false, nil
	})
	for i := range b.styles {
		b.styles[i].code = b.numFmts[b.styles[i].numFmtID]
	}
}

func (b *Book) relPart(typ string) string {
	for _, t := range b.pkg.RelsByType(b.wbPart, typ) {
		if b.pkg.Has(t) {
			return t
		}
	}
	return ""
}

// Blocks 排出第 i 張工作表。
func (b *Book) Blocks(i int) []markdown.Block {
	if i < 0 || i >= len(b.Sheets) {
		return nil
	}
	if got, ok := b.loaded[i]; ok {
		return got
	}
	out := b.sheetBlocks(b.Sheets[i])
	b.loaded[i] = out
	return out
}

func (b *Book) sheetBlocks(sh Sheet) []markdown.Block {
	rows, more := b.readSheet(sh.Part)
	out := []markdown.Block{{Kind: markdown.Heading, Level: 1,
		Spans: []markdown.Span{{Text: sh.Name}}}}
	if len(rows) == 0 {
		return append(out, markdown.Block{Kind: markdown.Para,
			Spans: []markdown.Span{{Text: i18n.T("(這張工作表是空的)")}}})
	}
	out = append(out, markdown.Block{Kind: markdown.Table, Rows: rows})
	if more {
		out = append(out, markdown.Block{Kind: markdown.Para,
			Spans: []markdown.Span{{Text: fmt.Sprintf(i18n.T("(只顯示前 %d 列 × %d 欄)"), MaxRows, MaxCols),
				Style: markdown.Italic}}})
	}
	return out
}

// readSheet 讀一張工作表的儲存格,回傳補齊成矩形的內容。
func (b *Book) readSheet(part string) ([][]string, bool) {
	raw, err := b.pkg.Bytes(part)
	if err != nil {
		return nil, false
	}
	dec := ooxml.NewDecoder(raw)
	root, err := ooxml.Root(dec)
	if err != nil || root.Name.Local != "worksheet" {
		return nil, false
	}
	cells := map[int]map[int]string{}
	maxRow, maxCol := 0, 0
	truncated := false

	_ = ooxml.Each(dec, func(se xml.StartElement) (bool, error) {
		if se.Name.Local != "sheetData" {
			return false, nil
		}
		rowN := 0
		return true, ooxml.Each(dec, func(r xml.StartElement) (bool, error) {
			if r.Name.Local != "row" {
				return false, nil
			}
			rowN = atoiDef(ooxml.Attr(r, "r"), rowN+1)
			if rowN > MaxRows {
				truncated = true
				return false, nil
			}
			colN := 0
			err := ooxml.Each(dec, func(c xml.StartElement) (bool, error) {
				if c.Name.Local != "c" {
					return false, nil
				}
				if ref := ooxml.Attr(c, "r"); ref != "" {
					colN = colOf(ref)
				} else {
					colN++
				}
				if colN > MaxCols {
					truncated = true
					return false, nil
				}
				v := b.cellValue(dec, c)
				if v == "" {
					return true, nil
				}
				if cells[rowN] == nil {
					cells[rowN] = map[int]string{}
				}
				cells[rowN][colN] = v
				if rowN > maxRow {
					maxRow = rowN
				}
				if colN > maxCol {
					maxCol = colN
				}
				return true, nil
			})
			return true, err
		})
	})

	if maxRow == 0 || maxCol == 0 {
		return nil, truncated
	}
	out := make([][]string, 0, maxRow)
	for r := 1; r <= maxRow; r++ {
		row := make([]string, maxCol)
		for c := 1; c <= maxCol; c++ {
			row[c-1] = cells[r][c]
		}
		out = append(out, row)
	}
	return out, truncated
}

// cellValue 算出一個儲存格顯示出來是什麼。
func (b *Book) cellValue(dec *xml.Decoder, c xml.StartElement) string {
	typ := ooxml.Attr(c, "t")
	styleIdx := atoiDef(ooxml.Attr(c, "s"), -1)
	var v, inline string
	_ = ooxml.Each(dec, func(in xml.StartElement) (bool, error) {
		switch in.Name.Local {
		case "v":
			v = ooxml.Text(dec)
			return true, nil
		case "is":
			inline = ooxml.Text(dec)
			return true, nil
		}
		return false, nil
	})
	switch typ {
	case "s":
		n, err := strconv.Atoi(strings.TrimSpace(v))
		if err != nil || n < 0 || n >= len(b.shared) {
			return ""
		}
		return b.shared[n]
	case "inlineStr":
		return inline
	case "str":
		return v
	case "b":
		if strings.TrimSpace(v) == "1" {
			return "TRUE"
		}
		return "FALSE"
	case "e":
		return v
	case "d":
		return v
	}
	return b.formatNumber(v, styleIdx)
}

// formatNumber 把數值換成看得懂的字。
func (b *Book) formatNumber(v string, styleIdx int) string {
	s := strings.TrimSpace(v)
	if s == "" {
		return ""
	}
	f, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return s
	}
	code := ""
	fmtID := 0
	if styleIdx >= 0 && styleIdx < len(b.styles) {
		code, fmtID = b.styles[styleIdx].code, b.styles[styleIdx].numFmtID
	}
	if isDateFormat(fmtID, code) {
		return b.serialToDate(f, fmtID, code)
	}
	if strings.Contains(stripQuoted(code), "%") {
		return trimFloat(f*100) + "%"
	}
	return trimFloat(f)
}

// isDateFormat 判斷一個格式碼是不是日期。
//
// 內建編號要寫死:14–22 與 45–47 是 Excel 保留給日期時間的,
// 檔案裡不會附格式碼。自訂格式則看碼裡有沒有日期欄位字母,
// 但要先把引號裡的文字拿掉 —— "May" 裡的 y 不是年份。
func isDateFormat(id int, code string) bool {
	switch {
	case id >= 14 && id <= 22, id >= 45 && id <= 47:
		return true
	}
	c := stripQuoted(code)
	return strings.ContainsAny(c, "yYdDhHsS")
}

// stripQuoted 去掉格式碼裡不是欄位的部分:引號括起來的字面文字、
// 反斜線跳脫的單一字元,以及中括號裡的顏色與地區設定。
//
// 少了這一步會誤判:`"May"` 裡的 y 不是年份,`[$-409]` 裡的 4 不是欄位。
func stripQuoted(code string) string {
	var sb strings.Builder
	inQuote, inBracket := false, false
	for i := 0; i < len(code); i++ {
		switch {
		case code[i] == '"':
			inQuote = !inQuote
		case inQuote:
		case code[i] == '[':
			inBracket = true
		case code[i] == ']':
			inBracket = false
		case inBracket:
		case code[i] == '\\':
			i++
		default:
			sb.WriteByte(code[i])
		}
	}
	return sb.String()
}

// serialToDate 把 Excel 的日期序號換成日期。
func (b *Book) serialToDate(f float64, id int, code string) string {
	// [雷] 1900 制把 1900 年當成閏年(那是 Lotus 1-2-3 留下來的錯,
	// Excel 為了相容照抄)。基準取 1899-12-30 就把那一天的偏移吸收掉,
	// 序號 60 之後全部對得上。
	// 超出 Excel 日期範圍的序號當成一般數字。硬換算會得到
	// 一個看起來合法但毫無意義的年份。
	if f < 0 || f > 2958465 {
		return trimFloat(f)
	}
	days := math.Trunc(f)
	frac := f - days
	t := b.epoch.AddDate(0, 0, int(days)).Add(time.Duration(frac * 24 * float64(time.Hour)))
	c := stripQuoted(code)
	timeOnly := (id >= 18 && id <= 21) || id == 45 || id == 46 || id == 47
	hasTime := strings.ContainsAny(c, "hHsS") || timeOnly
	hasDate := strings.ContainsAny(c, "yYdD") || (id >= 14 && id <= 17) || id == 22
	switch {
	case timeOnly && !hasDate:
		return t.Format("15:04:05")
	case hasTime && hasDate, id == 22:
		return t.Format("2006-01-02 15:04:05")
	default:
		return t.Format("2006-01-02")
	}
}

func trimFloat(f float64) string {
	return strconv.FormatFloat(f, 'f', -1, 64)
}

// colOf 從 A1 式參照算出欄號,從 1 起算。
func colOf(ref string) int {
	n := 0
	for i := 0; i < len(ref); i++ {
		ch := ref[i]
		switch {
		case ch >= 'A' && ch <= 'Z':
			n = n*26 + int(ch-'A') + 1
		case ch >= 'a' && ch <= 'z':
			n = n*26 + int(ch-'a') + 1
		default:
			return n
		}
	}
	return n
}

func atoiDef(s string, def int) int {
	if n, err := strconv.Atoi(strings.TrimSpace(s)); err == nil {
		return n
	}
	return def
}
