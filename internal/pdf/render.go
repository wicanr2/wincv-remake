package pdf

import (
	"fmt"
	"github.com/wicanr2/wincv-remake/internal/i18n"
	"image"
	"math"
	"sort"
)

// RenderOptions 是畫一頁的設定。
type RenderOptions struct {
	// DPI 決定解析度。0 表示用 DefaultDPI。
	DPI float64
	// MaxPixels 是輸出圖的像素數上限,0 表示用 DefaultMaxPixels。
	// 超過的話自動降解析度 —— 一份海報尺寸的 PDF 在 300 dpi 下
	// 會是好幾億個像素。
	MaxPixels int
}

const (
	// DefaultDPI 取 96:那是螢幕的慣例值,一頁 A4 大約 794×1123。
	DefaultDPI = 96.0
	// DefaultMaxPixels 是預設的像素上限(約 4000×4000)。
	DefaultMaxPixels = 16 << 20
)

// Rendered 是畫好的一頁。
type Rendered struct {
	Img *image.RGBA
	// DPI 是實際用的解析度(可能因為像素上限而低於要求的值)。
	DPI float64
	// Substituted 是改用系統字型補畫的字型。畫面看起來正常,
	// 但字形不是原檔的那一套 —— 這件事要讓上層有機會講出來。
	Substituted []string
	// Missing 是連補都補不出來的字型,那些字沒有畫上去。
	Missing []string
}

// Render 把一頁畫成點陣圖。
func (p *Page) Render(opt RenderOptions) (r *Rendered, err error) {
	defer func() {
		if e := recover(); e != nil {
			r, err = nil, fmt.Errorf(i18n.T("這一頁畫不出來(%v)"), e)
		}
	}()

	dpi := opt.DPI
	if dpi <= 0 {
		dpi = DefaultDPI
	}
	maxPx := opt.MaxPixels
	if maxPx <= 0 {
		maxPx = DefaultMaxPixels
	}
	pw, ph := p.Width(), p.Height()
	if pw <= 0 || ph <= 0 {
		return nil, fmt.Errorf(i18n.T("這一頁沒有尺寸"))
	}
	s := dpi / 72
	capped := false
	if px := pw * ph * s * s; px > float64(maxPx) {
		s = math.Sqrt(float64(maxPx) / (pw * ph))
		dpi = s * 72
		capped = true
	}

	// 沒有壓上限時往上取整,免得最右邊那一列內容被切掉;壓上限時往下取整,
	// 不然兩個維度各進位一格就會超過上限。
	round := math.Ceil
	if capped {
		round = math.Floor
	}
	w := int(round(pw * s))
	h := int(round(ph * s))
	if w < 1 || h < 1 {
		return nil, fmt.Errorf(i18n.T("這一頁太小"))
	}

	// 基底變換:PDF 的 Y 軸由下往上,圖的 Y 軸由上往下,所以縱向要翻,
	// 順便把 MediaBox 的原點移到 (0,0) —— 有些頁面的原點不是零。
	base := matrix{s, 0, 0, -s, -p.X0 * s, p.Y1 * s}
	if rot := ((p.Rotate % 360) + 360) % 360; rot != 0 {
		base = mul(base, rotation(rot, w, h))
		if rot == 90 || rot == 270 {
			w, h = h, w
		}
	}

	dev := newRasterDevice(p.doc, w, h)
	p.interpret(dev, base)
	return &Rendered{
		Img: dev.img, DPI: dpi,
		Substituted: sortedKeys(dev.substituted),
		Missing:     sortedKeys(dev.missing),
	}, nil
}

// rotation 是 /Rotate 對應的旋轉。w、h 是還沒旋轉的尺寸。
//
// [雷] 旋轉之後畫布的長寬要對調,而且原點會跑掉 —— 只轉不平移的話
// 內容會整個轉到畫布外面,結果是一張全白的圖,看起來像「這一頁是空的」。
func rotation(deg, w, h int) matrix {
	switch deg {
	case 90:
		return matrix{0, 1, -1, 0, float64(h), 0}
	case 180:
		return matrix{-1, 0, 0, -1, float64(w), float64(h)}
	case 270:
		return matrix{0, -1, 1, 0, 0, float64(w)}
	}
	return identity
}

func sortedKeys(m map[string]bool) []string {
	if len(m) == 0 {
		return nil
	}
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
