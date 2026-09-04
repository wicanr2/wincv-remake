// Package officedoc 把 Word、PowerPoint、Excel 收在同一個介面後面。
//
// 五種格式(.docx / .doc / .rtf / .pptx / .xlsx)的解析差異很大,但對
// 畫面來說它們是同一種東西:一份文件,可能分成幾段(章節、投影片、
// 工作表),每一段是一疊排版區塊,外加一些可以按名字取回的圖。
//
// 上層只認得這個介面。要加第六種格式時,internal/app 一行都不用改。
package officedoc

import (
	"fmt"
	"github.com/wicanr2/wincv-remake/internal/i18n"
	"os"
	"path/filepath"
	"strings"

	"github.com/wicanr2/wincv-remake/internal/doc97"
	"github.com/wicanr2/wincv-remake/internal/docx"
	"github.com/wicanr2/wincv-remake/internal/markdown"
	"github.com/wicanr2/wincv-remake/internal/pptx"
	"github.com/wicanr2/wincv-remake/internal/rtf"
	"github.com/wicanr2/wincv-remake/internal/xlsx"
)

// Kind 是文件的種類。用來決定分段叫什麼名字(章節 / 投影片 / 工作表)。
type Kind int

const (
	Word Kind = iota
	Presentation
	Spreadsheet
)

func (k Kind) String() string {
	switch k {
	case Presentation:
		return i18n.T("簡報")
	case Spreadsheet:
		return i18n.T("試算表")
	}
	return i18n.T("文件")
}

// PartWord 是分段在畫面上的稱呼。
func (k Kind) PartWord() string {
	switch k {
	case Presentation:
		return i18n.T("投影片")
	case Spreadsheet:
		return i18n.T("工作表")
	}
	return i18n.T("章節")
}

// Part 是文件裡可以分別打開的一段。
type Part struct {
	Title string
}

// Doc 是一份打開著的 Office 文件。
type Doc struct {
	Kind  Kind
	Title string
	Parts []Part

	impl impl
}

type impl interface {
	blocks(i int) []markdown.Block
	image(ref string) ([]byte, error)
	close() error
}

// Formats 是支援的副檔名與它們的種類。
//
// 做成一張表而不是一串 if:這張表同時是「支援哪些格式」的答案,
// 測試盯著它,不會悄悄過期。
var Formats = map[string]Kind{
	".docx": Word, ".docm": Word, ".dotx": Word, ".dotm": Word,
	".doc": Word, ".dot": Word, ".rtf": Word,
	".pptx": Presentation, ".pptm": Presentation, ".ppsx": Presentation,
	".ppsm": Presentation, ".potx": Presentation,
	".xlsx": Spreadsheet, ".xlsm": Spreadsheet, ".xltx": Spreadsheet,
}

// Is 判斷一個檔名是不是這一包處理的格式。
func Is(name string) bool {
	_, ok := Formats[strings.ToLower(filepath.Ext(name))]
	return ok
}

// Open 打開一份文件。
func Open(path string) (*Doc, error) {
	ext := strings.ToLower(filepath.Ext(path))
	kind, ok := Formats[ext]
	if !ok {
		return nil, fmt.Errorf(i18n.T("不認得的格式:%s"), ext)
	}
	switch ext {
	case ".pptx", ".pptm", ".ppsx", ".ppsm", ".potx":
		return openPPTX(path)
	case ".xlsx", ".xlsm", ".xltx":
		return openXLSX(path)
	case ".rtf":
		return openRTF(path)
	case ".doc", ".dot":
		// [雷] 副檔名不保證內容。用 .doc 存的 RTF 在真實世界很常見
		// (很多程式「輸出 Word 檔」就是輸出 RTF 再改副檔名),
		// 而那種檔案照複合文件去解會在第一個位元組就失敗。
		if looksLikeRTF(path) {
			return openRTF(path)
		}
		return openDOC(path)
	}
	_ = kind
	return openDOCX(path)
}

func looksLikeRTF(path string) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()
	var head [8]byte
	n, _ := f.Read(head[:])
	return strings.HasPrefix(string(head[:n]), "{\\rt")
}

// Blocks 排出第 i 段。
func (d *Doc) Blocks(i int) []markdown.Block {
	if i < 0 || i >= len(d.Parts) {
		return nil
	}
	return d.impl.blocks(i)
}

// Image 取一張圖。
func (d *Doc) Image(ref string) ([]byte, error) { return d.impl.image(ref) }

func (d *Doc) Close() error { return d.impl.close() }

// --- 各格式的接法 ---

type docxDoc struct{ d *docx.Doc }

func (w docxDoc) blocks(int) []markdown.Block      { return w.d.Blocks() }
func (w docxDoc) image(ref string) ([]byte, error) { return w.d.Image(ref) }
func (w docxDoc) close() error                     { return w.d.Close() }

func openDOCX(path string) (*Doc, error) {
	w, err := docx.Open(path)
	if err != nil {
		return nil, err
	}
	return &Doc{Kind: Word, Title: titleOr(w.Title, path),
		Parts: []Part{{Title: filepath.Base(path)}}, impl: docxDoc{w}}, nil
}

type doc97Doc struct{ d *doc97.Doc }

func (w doc97Doc) blocks(int) []markdown.Block { return w.d.Blocks() }
func (w doc97Doc) image(string) ([]byte, error) {
	return nil, fmt.Errorf(i18n.T("這個格式的圖片尚未支援"))
}
func (w doc97Doc) close() error { return w.d.Close() }

func openDOC(path string) (*Doc, error) {
	w, err := doc97.Open(path)
	if err != nil {
		return nil, err
	}
	return &Doc{Kind: Word, Title: filepath.Base(path),
		Parts: []Part{{Title: filepath.Base(path)}}, impl: doc97Doc{w}}, nil
}

type rtfDoc struct{ d *rtf.Doc }

func (w rtfDoc) blocks(int) []markdown.Block      { return w.d.Blocks() }
func (w rtfDoc) image(ref string) ([]byte, error) { return w.d.Image(ref) }
func (w rtfDoc) close() error                     { return w.d.Close() }

func openRTF(path string) (*Doc, error) {
	w, err := rtf.Open(path)
	if err != nil {
		return nil, err
	}
	return &Doc{Kind: Word, Title: filepath.Base(path),
		Parts: []Part{{Title: filepath.Base(path)}}, impl: rtfDoc{w}}, nil
}

type pptxDoc struct{ d *pptx.Deck }

func (w pptxDoc) blocks(i int) []markdown.Block {
	if i < 0 || i >= len(w.d.Slides) {
		return nil
	}
	return w.d.Slides[i].Blocks
}
func (w pptxDoc) image(ref string) ([]byte, error) { return w.d.Image(ref) }
func (w pptxDoc) close() error                     { return w.d.Close() }

func openPPTX(path string) (*Doc, error) {
	p, err := pptx.Open(path)
	if err != nil {
		return nil, err
	}
	d := &Doc{Kind: Presentation, Title: titleOr(p.Title, path), impl: pptxDoc{p}}
	for i, s := range p.Slides {
		t := s.Title
		if t == "" {
			t = fmt.Sprintf(i18n.T("第 %d 張"), i+1)
		}
		d.Parts = append(d.Parts, Part{Title: t})
	}
	return d, nil
}

type xlsxDoc struct{ d *xlsx.Book }

func (w xlsxDoc) blocks(i int) []markdown.Block { return w.d.Blocks(i) }
func (w xlsxDoc) image(string) ([]byte, error) {
	return nil, fmt.Errorf(i18n.T("試算表裡沒有圖"))
}
func (w xlsxDoc) close() error { return w.d.Close() }

func openXLSX(path string) (*Doc, error) {
	b, err := xlsx.Open(path)
	if err != nil {
		return nil, err
	}
	d := &Doc{Kind: Spreadsheet, Title: titleOr(b.Title, path), impl: xlsxDoc{b}}
	for i, s := range b.Sheets {
		t := s.Name
		if t == "" {
			t = fmt.Sprintf(i18n.T("工作表 %d"), i+1)
		}
		if s.Hidden {
			t += i18n.T("(隱藏)")
		}
		d.Parts = append(d.Parts, Part{Title: t})
	}
	return d, nil
}

func titleOr(title, path string) string {
	if strings.TrimSpace(title) != "" {
		return title
	}
	return filepath.Base(path)
}
