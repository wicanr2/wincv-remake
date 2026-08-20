package app

import (
	"fmt"
	"io"
	"path/filepath"
	"strings"

	"github.com/wicanr2/wincv-remake/internal/cell"
	"github.com/wicanr2/wincv-remake/internal/imgfmt"
	"github.com/wicanr2/wincv-remake/internal/textenc"
)

// PreviewRows 是預視窗格佔幾列。原版量到的是 8 列(docs/ui/main-screen.md)。
const PreviewRows = 8

// MaxPreviewBytes 是預視只讀進來多少。預視是「移到哪就看到哪」,
// 游標每動一格就要重讀,所以不能整個檔案讀進來。
const MaxPreviewBytes = 64 << 10

// preview 是預視窗格的內容快取。
//
// 快取的鍵是「哪個目錄的哪個檔案」:游標在列表上移動時每一格都要重畫,
// 每次都重讀檔案會讓上下鍵變鈍。
type preview struct {
	key   string
	lines []string
	enc   textenc.Enc
	err   string
}

// togglePreview 切換預視窗格(原版的 Alt-P)。
func (a *App) togglePreview() bool {
	a.ShowPreview = !a.ShowPreview
	if a.ShowPreview {
		a.Browser.ReserveBottom = PreviewRows
	} else {
		a.Browser.ReserveBottom = 0
	}
	return true
}

// previewFor 準備游標所在檔案的預視內容。
func (a *App) previewFor(cols int) *preview {
	e := a.Browser.Current()
	if e == nil {
		return &preview{}
	}
	key := a.Browser.Dir + "\x00" + e.Name
	if a.prev.key == key && a.prev.lines != nil {
		return &a.prev
	}
	p := preview{key: key}
	switch {
	case e.Up:
		p.err = "上一層"
	case e.IsDir:
		p.err = "<目錄>"
	default:
		p = a.readPreview(key, e.Name, cols)
	}
	a.prev = p
	return &a.prev
}

func (a *App) readPreview(key, name string, cols int) preview {
	p := preview{key: key}
	rc, err := a.Browser.FS.Open(filepath.Join(a.Browser.Dir, name))
	if err != nil {
		p.err = err.Error()
		return p
	}
	defer rc.Close()
	data, err := io.ReadAll(io.LimitReader(rc, MaxPreviewBytes))
	if err != nil {
		p.err = err.Error()
		return p
	}
	if len(data) == 0 {
		p.err = "(空檔案)"
		return p
	}
	if _, ok := imgfmt.DetectFormat(name); ok {
		// 圖檔不在這裡畫:預視窗格是格點,圖要走 overlay 那條路。
		// 先給尺寸,比塞一堆位元組有用。
		if img, kind, err := imgfmt.Decode(name, data); err == nil {
			b := img.Bounds()
			p.err = fmt.Sprintf("%s  %d x %d", kind, b.Dx(), b.Dy())
			return p
		}
	}
	p.enc = textenc.Detect(data)
	if p.enc == textenc.Binary {
		p.lines = hexLines(data, PreviewRows)
		return p
	}
	text := textenc.Decode(data, p.enc)
	for _, ln := range strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n") {
		p.lines = append(p.lines, printable(strings.TrimRight(ln, "\r"), cols))
		if len(p.lines) >= PreviewRows {
			break
		}
	}
	return p
}

// hexLines 把前面幾列排成 16 進位,一列 16 個位元組。
func hexLines(data []byte, rows int) []string {
	const per = 16
	var out []string
	for i := 0; i < len(data) && len(out) < rows; i += per {
		end := i + per
		if end > len(data) {
			end = len(data)
		}
		var b strings.Builder
		fmt.Fprintf(&b, "%08X  ", i)
		for j := i; j < i+per; j++ {
			if j < end {
				fmt.Fprintf(&b, "%02X ", data[j])
			} else {
				b.WriteString("   ")
			}
			if j-i == 7 {
				b.WriteByte(' ')
			}
		}
		b.WriteByte(' ')
		for j := i; j < end; j++ {
			c := data[j]
			if c < 0x20 || c == 0x7F {
				c = '.'
			}
			b.WriteByte(c)
		}
		out = append(out, b.String())
	}
	return out
}

// drawPreview 畫底部的預視窗格。
func (a *App) drawPreview(s *cell.Screen) {
	y0 := s.Rows - PreviewRows
	if y0 < 2 {
		return
	}
	s.Fill(0, y0, s.Cols, PreviewRows, ' ', cell.LtGray, cell.Black)
	p := a.previewFor(s.Cols)
	if p.err != "" {
		s.Print(1, y0, p.err, cell.Gray, cell.Black)
		return
	}
	for i, ln := range p.lines {
		if i >= PreviewRows {
			break
		}
		fg := cell.LtGray
		if p.enc == textenc.Binary {
			fg = cell.Gray
		}
		s.Print(0, y0+i, ln, fg, cell.Black)
	}
}
