package app

import (
	"errors"
	"fmt"
	"image"
	"io"
	"path"
	"strings"

	"github.com/wicanr2/wincv-remake/internal/cell"
	"github.com/wicanr2/wincv-remake/internal/imgfmt"
	"github.com/wicanr2/wincv-remake/internal/imgview"
	"github.com/wicanr2/wincv-remake/internal/keys"
	"github.com/wicanr2/wincv-remake/internal/markdown"
	"github.com/wicanr2/wincv-remake/internal/render"
	"github.com/wicanr2/wincv-remake/internal/textenc"
)

// MaxMDImageBytes 是 markdown 內嵌圖片的大小上限。
//
// 文件裡順手引到一個 200 MB 的 TIFF 不是沒有的事,而看文件的人
// 要的是「這裡有張圖」。超過就只顯示 alt 與原因。
const MaxMDImageBytes = 32 << 20

// mdView 是 markdown 檢視模式的狀態。
type mdView struct {
	name   string
	blocks []markdown.Block
	lines  []markdown.Line
	pics   []*markdown.Pic
	top    int
	cols   int // 上次排版用的欄數,寬度變了要重排
	dir    string
}

// IsMarkdown 判斷一個檔名是不是 markdown。
func IsMarkdown(name string) bool {
	switch strings.ToLower(path.Ext(name)) {
	case ".md", ".markdown", ".mdown", ".mkd":
		return true
	}
	return false
}

// openMarkdown 用 markdown 模式開一個檔案。
func (a *App) openMarkdown(name string, data []byte) {
	enc := textenc.Detect(data)
	a.md = mdView{
		name:   name,
		blocks: markdown.Parse(textenc.Decode(data, enc)),
		dir:    a.Browser.Dir,
	}
	a.Mode = ModeMarkdown
	a.recallPos()
}

// mdImage 讀取 markdown 裡引到的圖。
//
// 只認相對於文件所在目錄的路徑,而且要求解出來的絕對路徑仍在該目錄之下 ——
// 一份來路不明的 .md 寫 `![](../../../etc/shadow)` 不該讓看文件的人
// 把那個檔案讀進記憶體。
func (a *App) mdImage(src string) (image.Image, error) {
	if src == "" {
		return nil, errors.New("沒有路徑")
	}
	// 遠端圖片不抓 —— 開一份 .md 不該變成連外行為,而那份 .md
	// 可能是從壓縮檔或別人給的目錄裡來的。瀏覽模式是另一回事:
	// 那是使用者自己輸入位址進去的,連外是他要的動作。
	for _, p := range []string{"http://", "https://", "//", "data:"} {
		if strings.HasPrefix(strings.ToLower(src), p) {
			return nil, errors.New("遠端圖片不下載")
		}
	}
	clean := path.Clean(strings.ReplaceAll(src, "\\", "/"))
	if path.IsAbs(clean) || strings.HasPrefix(clean, "../") || clean == ".." {
		return nil, errors.New("只能引用文件所在目錄底下的圖")
	}
	full := path.Join(a.md.dir, clean)
	rc, err := a.FS.Open(full)
	if err != nil {
		return nil, errors.New("找不到")
	}
	defer rc.Close()
	// 上限用讀的位元組數把關,不靠 Stat —— 壓縮檔成員的 vfs 沒有 Stat,
	// 而且宣告的大小與實際解出來的大小未必一致。
	data, err := io.ReadAll(io.LimitReader(rc, MaxMDImageBytes+1))
	if err != nil {
		return nil, err
	}
	if len(data) > MaxMDImageBytes {
		return nil, fmt.Errorf("超過 %s", comma(MaxMDImageBytes))
	}
	img, _, err := imgfmt.Decode(path.Base(clean), data)
	if err != nil {
		return nil, err
	}
	return img, nil
}

// mdLayout 需要時重新排版。欄數沒變就沿用上次的結果 ——
// 排版要解圖檔,每幀重做會讓捲動變成幻燈片。
func (a *App) mdLayout(cols int) {
	if cols == a.md.cols && a.md.lines != nil {
		return
	}
	a.md.cols = cols
	a.md.lines, a.md.pics = markdown.Layout(
		a.md.blocks, cols, a.CellW, a.CellH, a.mdImage, markdown.DefaultTheme())
}

func (a *App) drawMarkdown(s *cell.Screen) []*render.Overlay {
	t := markdown.DefaultTheme()
	s.Clear(t.Text, t.BG)
	rows := s.Rows - 1
	if rows < 1 {
		rows = 1
	}
	a.mdLayout(s.Cols)
	a.clampMD(rows)

	var ovs []*render.Overlay
	for i := 0; i < rows; i++ {
		idx := a.md.top + i
		if idx >= len(a.md.lines) {
			break
		}
		ln := a.md.lines[idx]
		if ln.Pic != nil {
			// 一張圖只在它的第一列排一次 overlay,而且要考慮
			// 圖被捲出畫面上緣的情況 —— 那時 overlay 的矩形要往上超出,
			// 由 rasterizer 自己裁掉。
			if ln.PicRow == 0 || idx == a.md.top {
				p := ln.Pic
				if p.Img != nil {
					top := i - ln.PicRow
					ovs = append(ovs, &render.Overlay{
						Img: p.Img,
						Rect: image.Rect(
							0, top*a.CellH,
							p.Cols*a.CellW, (top+p.Rows)*a.CellH),
						Fit: true,
					})
				}
			}
			continue
		}
		x := 0
		for _, pc := range ln.Pieces {
			if x >= s.Cols {
				break
			}
			bg := pc.BG
			n := s.Print(x, i, pc.Text, pc.FG, bg)
			if pc.Under {
				s.Underline(x, i, n, true)
			}
			x += n
		}
	}
	a.rows = rows
	a.drawMDStatus(s)
	return ovs
}

func (a *App) drawMDStatus(s *cell.Screen) {
	y := s.Rows - 1
	s.Fill(0, y, s.Cols, 1, ' ', cell.LtGray2, cell.Blue)
	pct := 100
	if n := len(a.md.lines); n > 0 {
		pct = (a.md.top + a.rows) * 100 / n
		if pct > 100 {
			pct = 100
		}
	}
	left := fmt.Sprintf("%s  %d 列  %d%%", a.md.name, len(a.md.lines), pct)
	s.Print(0, y, left, cell.LtGray2, cell.Blue)
	right := "Enter 看圖  Esc 離開"
	if x := s.Cols - len(right); x > len(left) {
		s.Print(x, y, right, cell.Gray, cell.Blue)
	}
}

func (a *App) clampMD(rows int) {
	max := len(a.md.lines) - rows
	if max < 0 {
		max = 0
	}
	if a.md.top > max {
		a.md.top = max
	}
	if a.md.top < 0 {
		a.md.top = 0
	}
}

func (a *App) markdownKey(k keys.Key) bool {
	rows := a.rows
	if rows < 1 {
		rows = 1
	}
	switch k.Code {
	case keys.Up:
		a.md.top--
	case keys.Down:
		a.md.top++
	case keys.PgUp:
		a.md.top -= rows
	case keys.PgDn:
		a.md.top += rows
	case keys.Home:
		a.md.top = 0
	case keys.End:
		a.md.top = len(a.md.lines)
	case keys.Enter:
		// 畫面上第一張圖用看圖模式全螢幕打開。
		return a.openFirstVisiblePic()
	case keys.Esc:
		a.Mode = ModeBrowser
		a.md = mdView{}
		return true
	default:
		return false
	}
	a.clampMD(rows)
	return true
}

// openFirstVisiblePic 把目前畫面上第一張圖丟給看圖模式。
func (a *App) openFirstVisiblePic() bool {
	for i := 0; i < a.rows; i++ {
		idx := a.md.top + i
		if idx >= len(a.md.lines) {
			break
		}
		if p := a.md.lines[idx].Pic; p != nil && p.Img != nil {
			a.Image = imgview.FromImage(path.Base(p.Src), "", p.Img, 0)
			a.Mode = ModeImage
			a.mdReturn = true
			return true
		}
	}
	a.Message = "這一頁沒有可以放大的圖"
	return true
}
