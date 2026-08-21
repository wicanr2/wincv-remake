// Package render 把 cell.Screen 畫成像素。
//
// 這一整包是純 CPU 光柵器,不 import Ebiten:同一份程式碼可以在沒有顯示器的
// 環境下產生 PNG,驗收(與原版截圖做格點比對)因此不需要開視窗。
// Ebiten 那一層在 cmd/wincv(桌面)與 mobile(Android),
// 兩邊都只負責把這裡產出的像素貼上去。
package render

import (
	"image"
	"image/color"

	"github.com/wicanr2/wincv-remake/internal/cell"
	"github.com/wicanr2/wincv-remake/internal/fnt"
)

// DefaultPalette 是 29 個具名顏色的 RGB,取自 WINCV.IMG 本體。
//
// 名字與順序在 0x5692d 的斜線分隔清單;RGB 值在每個顏色 word 的 body ——
// 每個 body 是 0x24 個位元組,第 8-10 個位元組就是 R、G、B
// (Win32 的 COLORREF 是 0x00BBGGRR,小端存放後記憶體順序正好是 R G B)。
// 顏色 word 的 xt 在它的 Forth 標頭裡,位置是「名字結尾 + 9」的那個 dword。
//
// 前 29 個是 keyword_*.cfg 用的具名顏色,後 14 個是 image 裡另外定義的
// (檔案清單的副檔名配色用 DIR-* 那幾個)。
//
// [雷] `LTGRAY` 在 image 裡**定義了兩次**,而且兩個都活著:
// 0x1dc40 是 #C0C0C0,0x50210 是 #C5C5C5。走「編譯期綁定」的那一路
// (檔案清單)拿到舊的 #C0C0C0,走「名稱查詢」的那一路(keyword_*.cfg
// 與狀態列)拿到後定義的 #C5C5C5。這個槽位放 #C0C0C0,名稱查詢的那一路
// 由 cell.ByName 對到 cell.LtGray2 —— 兩者的實測證據都在那邊。
//
// 用 tools/palette.py 可以重新抽一次(需要 original/app/WINCV.IMG)。
var DefaultPalette = [cell.NumColors]color.RGBA{
	{0x00, 0x00, 0x00, 0xFF}, // black
	{0x40, 0x40, 0x40, 0xFF}, // dkgray
	{0x80, 0x00, 0x00, 0xFF}, // red
	{0xFF, 0x00, 0x00, 0xFF}, // ltred
	{0x00, 0x80, 0x00, 0xFF}, // green
	{0x00, 0xFF, 0x00, 0xFF}, // ltgreen
	{0x00, 0x00, 0x80, 0xFF}, // blue
	{0x00, 0x00, 0xFF, 0xFF}, // ltblue
	{0x80, 0x80, 0x00, 0xFF}, // yellow
	{0xA0, 0xA0, 0x00, 0xFF}, // mildyellow
	{0xFF, 0xFF, 0x00, 0xFF}, // ltyellow
	{0x80, 0x00, 0x80, 0xFF}, // magenta
	{0xFF, 0x00, 0xFF, 0xFF}, // ltmagenta
	{0x00, 0x80, 0x80, 0xFF}, // cyan
	{0x00, 0xFF, 0xFF, 0xFF}, // ltcyan
	{0x80, 0x80, 0x80, 0xFF}, // gray
	{0xFF, 0xFF, 0xFF, 0xFF}, // white
	{0xC0, 0xC0, 0xC0, 0xFF}, // ltgray
	{0x80, 0x80, 0xFF, 0xFF}, // purple
	{0xC0, 0xC0, 0xFF, 0xFF}, // ltpurple
	{0xFF, 0x80, 0x40, 0xFF}, // orange
	{0xFF, 0xC0, 0x80, 0xFF}, // ltorange
	{0xFF, 0xFF, 0xD7, 0xFF}, // gooseyellow
	{0xAA, 0xDC, 0xBE, 0xFF}, // bluegreen
	{0x00, 0x66, 0x00, 0xFF}, // inkgreen
	{0xE1, 0xE1, 0xE1, 0xFF}, // mildwhite
	{0x00, 0xC0, 0x00, 0xFF}, // mildgreen
	{0x00, 0xD6, 0xD6, 0xFF}, // mildcyan
	{0xC0, 0x00, 0xC0, 0xFF}, // mildmagenta
	{0x14, 0xBE, 0x00, 0xFF}, // dir-green
	{0x00, 0xF0, 0x00, 0xFF}, // dir-ltgreen
	{0x1E, 0xBE, 0xBE, 0xFF}, // dir-cyan
	{0x14, 0xDC, 0xDC, 0xFF}, // dir-ltcyan
	{0xAF, 0x46, 0x00, 0xFF}, // dir-yellow
	{0xB9, 0xB9, 0xB9, 0xFF}, // ltgray1
	{0xC8, 0xC8, 0xC8, 0xFF}, // newwhite
	{0xAA, 0xAA, 0xAA, 0xFF}, // toolgray
	{0xAA, 0xAA, 0xAA, 0xFF}, // histogram-ltgray
	{0xAA, 0xFF, 0xE6, 0xFF}, // ltbluegreen
	{0x00, 0xB4, 0xB4, 0xFF}, // ef-cyan
	{0x1E, 0xC8, 0x00, 0xFF}, // origin-green
	{0x00, 0xB4, 0x00, 0xFF}, // removeable-disk-green
	{0x96, 0x96, 0x96, 0xFF}, // c-lccw-ltgray
	{0xC5, 0xC5, 0xC5, 0xFF}, // ltgray2(LTGRAY 的第二個定義,0x50210)
}

// HalfSource 提供半形字模。
//
// 做成介面的理由與 CJKSource 相同:光柵器不該在意字模從哪來。
// 桌面版拿原版的 .FON,Android 上沒有那個檔(是版權物,不能打包),
// 就從系統的 TrueType 現場產一份 —— 兩者對光柵器是同一件事。
type HalfSource interface {
	// Glyph 回傳一個字碼的點陣圖。字碼是 CP437(見 cell.CP437)。
	Glyph(code byte) *fnt.Glyph
	// Size 回傳字身的寬與高(不含 LineGap)。
	Size() (w, h int)
}

// CJKSource 提供全形字的點陣圖,寬度必須是半形的兩倍、高度相同。
//
// 原版的全形中文是 Windows GDI 用系統字型(image 裡指名「新細明體」)畫的,
// 不在隨附的 .FON 內。也就是說「原版的中文字形」本來就隨使用者的 Windows
// 而異,不是單一固定答案 —— 所以這裡做成可抽換的來源。
type CJKSource interface {
	Glyph(r rune) *fnt.Glyph
}

// Rasterizer 把 Screen 畫進一張 RGBA。
type Rasterizer struct {
	Half    HalfSource // 半形字模來源(原版的 cvga / cvga1018 / cvga1224,或系統字型現場產的)
	CJK     CJKSource  // 全形字型來源,可為 nil
	Palette [cell.NumColors]color.RGBA

	// Fallback 補點陣字庫沒有的字。倚天是用 Big5 索引的,Big5 以外的
	// (简体字、韓文、希臘文、多數符號)沒有字模;UTF-8 檔案很容易碰到。
	// 可為 nil,那樣缺字就畫成缺字記號。
	Fallback CJKSource

	// MissingMark 決定缺字要不要畫一個記號。畫出來比留白好 ——
	// 留白會讓人以為檔案裡本來就是空的。
	MissingMark bool

	CellW, CellH int

	// RuleHi / RuleShadow 是 cell.Rule 那條 2 px 橫線的兩條掃描線。
	// 原版量到的是 #FFFFFF + #C5C5C5(立體感的凹線)。
	//
	// 顏色不取自格子的 FG:那條線是**框線**不是字,而它所在的那一列
	// 同時要用自己的顏色印字(狀態列是黃字)。兩者綁在一起就得二選一。
	RuleHi, RuleShadow cell.Color
	buf                *image.RGBA
}

// LineGap 是格子高度比半形字身多出來的像素列。
//
// 原版的格點量測(792x506 的視窗,docs/ui/main-screen.md)顯示列距是 **16**,
// 而 cvga 的字身是 15、`dfExternalLeading` 是 0。多的那一列不是字型要的,
// 是程式自己的:Wine 的 font trace 顯示 app 同時要了
// `pix_h 15 charset 255`(OEM,半形)與 `pix_h 16 charset 136`(Big5,全形),
// 也就是**用全形字的高度當列高**。字模靠上對齊,多的一列留在下面。
const LineGap = 1

func New(half HalfSource, cjk CJKSource) *Rasterizer {
	hw, hh := half.Size()
	return &Rasterizer{
		Half:    half,
		CJK:     cjk,
		Palette: DefaultPalette,
		CellW:   hw,
		CellH:   hh + LineGap,

		RuleHi:     cell.White,
		RuleShadow: cell.LtGray2,
	}
}

// clampColor 把超出範圍的索引夾回黑色,避免越界。
func clampColor(c cell.Color) cell.Color {
	if c >= cell.NumColors {
		return cell.Black
	}
	return c
}

// Size 回傳畫一個 cols x rows 的畫面需要多少像素。
func (r *Rasterizer) Size(cols, rows int) (int, int) {
	return cols * r.CellW, rows * r.CellH
}

// Overlay 是畫在格點之上的像素層。
//
// 看圖模式需要它:圖片不是字元,硬塞進格點會失去解析度。
// 格點仍然負責周邊的狀態列與訊息,只有圖片本身跳出格點。
type Overlay struct {
	Img image.Image
	// 目標矩形(像素座標)。Fit 為 true 時,圖片等比例縮放後置中放進來;
	// 為 false 時以 1:1 貼上,超出的部分裁掉。
	Rect image.Rectangle
	Fit  bool
	// Pan 是 1:1 模式下的平移量(來源座標)。
	Pan image.Point
}

// Draw 把 s 畫成一張 RGBA。回傳的緩衝區會被下一次呼叫重用。
func (r *Rasterizer) Draw(s *cell.Screen) *image.RGBA {
	return r.DrawWith(s)
}

// DrawWith 畫格點,再把 overlay 依序疊上去。
//
// 收多個而不是一個:markdown 一頁可以有好幾張圖,每張各佔自己的格位。
// nil 的項目直接跳過,呼叫端不必先過濾。
func (r *Rasterizer) DrawWith(s *cell.Screen, ovs ...*Overlay) *image.RGBA {
	w, h := r.Size(s.Cols, s.Rows)
	if r.buf == nil || r.buf.Rect.Dx() != w || r.buf.Rect.Dy() != h {
		r.buf = image.NewRGBA(image.Rect(0, 0, w, h))
	}
	// 兩趟:先把所有底色鋪完,再畫所有字模。
	//
	// 不能一格畫完再畫下一格 —— 全形字的字模橫跨兩格,
	// 右半格(Cont)接著鋪自己的底色時會把字的右半抹掉。
	for y := 0; y < s.Rows; y++ {
		for x := 0; x < s.Cols; x++ {
			r.fillBG(s, x, y)
		}
	}
	for y := 0; y < s.Rows; y++ {
		for x := 0; x < s.Cols; x++ {
			r.drawGlyph(s, x, y)
		}
	}
	for _, ov := range ovs {
		if ov != nil && ov.Img != nil {
			r.blit(ov)
		}
	}
	return r.buf
}

// blit 把 overlay 的圖片畫進緩衝區。
func (r *Rasterizer) blit(ov *Overlay) {
	dst := ov.Rect.Intersect(r.buf.Rect)
	if dst.Empty() {
		return
	}
	src := ov.Img.Bounds()
	if ov.Fit {
		// 等比例縮放後置中。用最近鄰取樣 —— 這是點陣風格的介面,
		// 平滑縮放會跟旁邊的點陣字打架。
		sw, sh := src.Dx(), src.Dy()
		if sw == 0 || sh == 0 {
			return
		}
		scaleNum, scaleDen := ov.Rect.Dx(), sw
		if ov.Rect.Dy()*sw < ov.Rect.Dx()*sh {
			scaleNum, scaleDen = ov.Rect.Dy(), sh
		}
		outW := sw * scaleNum / scaleDen
		outH := sh * scaleNum / scaleDen
		offX := ov.Rect.Min.X + (ov.Rect.Dx()-outW)/2
		offY := ov.Rect.Min.Y + (ov.Rect.Dy()-outH)/2
		for y := 0; y < outH; y++ {
			dy := offY + y
			if dy < dst.Min.Y || dy >= dst.Max.Y {
				continue
			}
			sy := src.Min.Y + y*sh/outH
			for x := 0; x < outW; x++ {
				dx := offX + x
				if dx < dst.Min.X || dx >= dst.Max.X {
					continue
				}
				sx := src.Min.X + x*sw/outW
				r.setPix(dx, dy, ov.Img.At(sx, sy))
			}
		}
		return
	}
	for dy := dst.Min.Y; dy < dst.Max.Y; dy++ {
		sy := src.Min.Y + (dy - ov.Rect.Min.Y) + ov.Pan.Y
		if sy < src.Min.Y || sy >= src.Max.Y {
			continue
		}
		for dx := dst.Min.X; dx < dst.Max.X; dx++ {
			sx := src.Min.X + (dx - ov.Rect.Min.X) + ov.Pan.X
			if sx < src.Min.X || sx >= src.Max.X {
				continue
			}
			r.setPix(dx, dy, ov.Img.At(sx, sy))
		}
	}
}

// setPix 把一個像素疊上去,尊重 alpha。
//
// 透明度不能忽略:.ICO 的 AND 遮罩、PNG 與 GIF 的透明色都會產生
// alpha = 0 的像素,直接寫 RGB 會把「應該看不見」的顏色畫出來
// (圖示的透明區會變成一片雜色)。
func (r *Rasterizer) setPix(x, y int, c color.Color) {
	o := r.buf.PixOffset(x, y)
	if o < 0 || o+3 >= len(r.buf.Pix) {
		return
	}
	cr, cg, cb, ca := c.RGBA()
	switch {
	case ca == 0:
		return // 全透明,保留底下的格點畫面
	case ca == 0xFFFF:
		r.buf.Pix[o+0] = uint8(cr >> 8)
		r.buf.Pix[o+1] = uint8(cg >> 8)
		r.buf.Pix[o+2] = uint8(cb >> 8)
	default:
		// c.RGBA() 回的是 premultiplied,直接加上背景的 (1-alpha) 部分。
		inv := 0xFFFF - ca
		r.buf.Pix[o+0] = uint8((cr + uint32(r.buf.Pix[o+0])*257*inv/0xFFFF) >> 8)
		r.buf.Pix[o+1] = uint8((cg + uint32(r.buf.Pix[o+1])*257*inv/0xFFFF) >> 8)
		r.buf.Pix[o+2] = uint8((cb + uint32(r.buf.Pix[o+2])*257*inv/0xFFFF) >> 8)
	}
	r.buf.Pix[o+3] = 0xFF
}

func (r *Rasterizer) fillBG(s *cell.Screen, cx, cy int) {
	c := s.At(cx, cy)
	if c == nil {
		return
	}
	px, py := cx*r.CellW, cy*r.CellH
	bg := r.Palette[clampColor(c.BG)]
	for y := 0; y < r.CellH; y++ {
		row := r.buf.PixOffset(px, py+y)
		for x := 0; x < r.CellW; x++ {
			o := row + x*4
			r.buf.Pix[o+0] = bg.R
			r.buf.Pix[o+1] = bg.G
			r.buf.Pix[o+2] = bg.B
			r.buf.Pix[o+3] = 0xFF
		}
	}
}

func (r *Rasterizer) drawGlyph(s *cell.Screen, cx, cy int) {
	c := s.At(cx, cy)
	if c == nil || c.Cont {
		return // 全形字在左半格一次畫完,右半格不再畫
	}
	// 格子上緣的 2 px 橫線(原版用它隔開檔案清單與狀態列)。
	if c.Rule {
		w := r.CellW
		if c.Wide {
			w *= 2
		}
		px, py := cx*r.CellW, cy*r.CellH
		r.hline(px, py, w, r.Palette[clampColor(r.RuleHi)])
		r.hline(px, py+1, w, r.Palette[clampColor(r.RuleShadow)])
	}
	// 底線畫在最後一條掃描線,而且空白格也要畫 —— 原版的長檔名欄
	// 底線是連續的一整條,不會在空格處斷掉。所以放在字模查詢之前。
	//
	// [雷] 全形格不畫。倚天 16×15 的字模**十五列全是字身**,底線那一列
	// 正好壓在筆畫上,而且同色 —— 畫出來是「這個字下半截糊掉了」,
	// 不是「這個字底下有一條線」。半形字有下伸部的空間,不會撞到。
	//
	// 續格(Cont)要一起排除:全形字佔兩格,而 Wide 只標在第一格,
	// 只看 Wide 的話右半邊照樣被劃一條。
	if c.Under && !c.Wide && !c.Cont {
		r.hline(cx*r.CellW, cy*r.CellH+r.CellH-1, r.CellW, r.Palette[clampColor(c.FG)])
	}
	var g *fnt.Glyph
	if c.Wide {
		if r.CJK != nil {
			g = r.CJK.Glyph(c.Ch)
		}
	} else if b, ok := toFontByte(c.Ch); ok {
		g = r.Half.Glyph(b)
	}
	if g == nil && r.Fallback != nil {
		g = r.Fallback.Glyph(c.Ch)
	}
	if g == nil {
		if r.MissingMark && c.Ch != ' ' && c.Ch != 0 {
			r.drawMissing(s, cx, cy, c)
		}
		return
	}

	px, py := cx*r.CellW, cy*r.CellH
	fg := r.Palette[clampColor(c.FG)]
	for y := 0; y < g.H && y < r.CellH; y++ {
		row := r.buf.PixOffset(px, py+y)
		for x := 0; x < g.W; x++ {
			if !g.At(x, y) {
				continue
			}
			o := row + x*4
			if o < 0 || o+3 >= len(r.buf.Pix) {
				continue
			}
			r.buf.Pix[o+0] = fg.R
			r.buf.Pix[o+1] = fg.G
			r.buf.Pix[o+2] = fg.B
			r.buf.Pix[o+3] = 0xFF
		}
	}
}

// hline 畫一條水平線。
func (r *Rasterizer) hline(px, py, w int, col color.RGBA) {
	if py < 0 || py >= r.buf.Rect.Dy() {
		return
	}
	row := r.buf.PixOffset(px, py)
	for x := 0; x < w; x++ {
		o := row + x*4
		if o < 0 || o+3 >= len(r.buf.Pix) {
			continue
		}
		r.buf.Pix[o+0] = col.R
		r.buf.Pix[o+1] = col.G
		r.buf.Pix[o+2] = col.B
		r.buf.Pix[o+3] = 0xFF
	}
}

// toFontByte 把一個半形 rune 對回 .FON 的字碼。
//
// 隨附的三個 .FON 是 **CP437**(Wine 的 font trace 是 charset 255 = OEM),
// 不是 Latin-1。兩者只有 0x20-0x7E 相同,對照表在 cell.CP437。
func toFontByte(r rune) (byte, bool) {
	return cell.FromCP437(r)
}

// drawMissing 畫缺字記號:一個空心框,佔該字應有的寬度。
//
// 缺字要看得見。留白的話,使用者看到的是「這個檔案這裡沒東西」,
// 而事實是「這裡有字但畫不出來」—— 那是兩件完全不同的事。
func (r *Rasterizer) drawMissing(s *cell.Screen, cx, cy int, c *cell.Cell) {
	w := r.CellW
	if c.Wide {
		w *= 2
	}
	px, py := cx*r.CellW, cy*r.CellH
	fg := r.Palette[clampColor(c.FG)]
	set := func(x, y int) {
		if x < 0 || y < 0 {
			return
		}
		o := r.buf.PixOffset(px+x, py+y)
		if o < 0 || o+3 >= len(r.buf.Pix) {
			return
		}
		r.buf.Pix[o+0], r.buf.Pix[o+1] = fg.R, fg.G
		r.buf.Pix[o+2], r.buf.Pix[o+3] = fg.B, 0xFF
	}
	top, bot := 2, r.CellH-3
	left, right := 1, w-2
	for x := left; x <= right; x++ {
		set(x, top)
		set(x, bot)
	}
	for y := top; y <= bot; y++ {
		set(left, y)
		set(right, y)
	}
}
