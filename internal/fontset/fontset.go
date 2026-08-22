// Package fontset 決定「一個字級長什麼樣」:半形點陣字型、配合它的
// 全形來源、以及補缺字的後備鏈。
//
// 為什麼獨立成一個套件而不是留在 cmd/wincv:桌面、celldump 與 Android
// 三個進入點都要做同一件事,而其中一項細節很容易做錯 —— 倚天字庫
// 載入時要給的是**字庫自己的**點陣尺寸(16×15),不是要畫多大。
// 傳錯的話那個字級整批沒有中文,而且沒有錯誤訊息。這裡有測試盯著。
//
// 這個套件不依賴 Ebiten,所以測得起來(Ebiten 的 package init 需要 DISPLAY)。
package fontset

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/wicanr2/wincv-remake/internal/bundled"
	"github.com/wicanr2/wincv-remake/internal/eten"
	"github.com/wicanr2/wincv-remake/internal/fnt"
	"github.com/wicanr2/wincv-remake/internal/render"
	"github.com/wicanr2/wincv-remake/internal/ttf"
)

// Level 是一個字級:一種半形點陣字型,加上配合它的全形來源。
//
// 原版隨附三種尺寸的 `.FON`(8x15 / 10x18 / 12x24),放大縮小字體就是
// 在這幾種之間換。全形字只有倚天的 15 點那一份,其餘尺寸由它縮放
// (24 點的漢字在倚天光碟裡是 ETUNPACK 壓縮的,還解不開)。
type Level struct {
	Name string
	Half render.HalfSource
	CJK  render.CJKSource
	FB   render.CJKSource
}

// FonNames 是原版隨附的三種半形點陣字型,由小到大。
var FonNames = []string{"cvga.fon", "CVGA1018.FON", "cvga1224.FON"}

// Load 把能載到的字級都準備好。載不到的就跳過,但要說出來 ——
// 少一個字級的症狀是「Ctrl-+ 沒反應」,那看起來像壞掉而不像缺檔案。
func Load(dir, stdPath, spcPath, fbPath string, noFB bool) []Level {
	var out []Level
	for _, name := range FonNames {
		// 磁碟優先,內嵌是後備 —— 使用者放在執行檔旁邊的字型永遠贏。
		d, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			if d = bundled.Get(name); d == nil {
				continue
			}
		}
		half, err := fnt.Parse(d)
		if err != nil {
			fmt.Fprintf(os.Stderr, "警告:%s 解不開 (%v)\n", name, err)
			continue
		}
		cw, ch := half.PixWidth, half.PixHeight+render.LineGap
		l := Level{Name: name, Half: half}
		if f, err := Eten(stdPath, spcPath, eten.NativeW, eten.NativeH); err == nil {
			l.CJK = render.ScaleCJK(f, f.W, f.H, cw*2, ch)
		} else {
			// 每一個字級都要報。只報第一個的話,「只有某幾個字級沒有中文」
			// 這種狀況會完全沒有訊息 —— 而使用者要按到那一級才看得到。
			fmt.Fprintf(os.Stderr, "提示:%s 這一級沒有倚天字庫,全形字改用後備字型 (%v)\n",
				name, err)
		}
		if !noFB {
			l.FB = Fallback(fbPath, cw, ch)
		}
		out = append(out, l)
	}
	return out
}

// Eten 讀倚天字庫:磁碟優先,內嵌是後備。
func Eten(stdPath, spcPath string, w, h int) (*eten.Font, error) {
	f, err := eten.Load(stdPath, spcPath, w, h)
	if err == nil {
		return f, nil
	}
	if std := bundled.Get("STDFONT.15"); std != nil {
		return eten.LoadBytes(std, bundled.Get("SPCFONT.15"),
			bundled.Get("SPCFSUPP.15"), w, h)
	}
	return nil, err
}

// Sizes 是原版隨附的三種半形點陣字型的尺寸。
//
// 沒有那三個 .FON 時照同樣的尺寸從系統字型現場產一份 —— 尺寸一樣,
// 版面、欄位對齊、按鍵行為就完全一樣,只有字形不同。
var Sizes = []struct {
	name string
	w, h int
}{{"8x15", 8, 15}, {"10x18", 10, 18}, {"12x24", 12, 24}}

// FromTTF 用系統字型現場產出三個字級,當作沒有原版 .FON 時的退路。
//
// 為什麼需要:原版的 cvga.fon 是第三方版權物,不能隨產物散布,所以
// 對外的版本解開之後那三個檔一定不在。以前這種情況直接結束程式,
// 而「少一個字型檔」與「這個程式跑不起來」是完全不同的嚴重程度 ——
// Android 版早就是這樣做的(那邊根本沒有可讀的程式目錄),桌面版
// 只是沒接上。
func FromTTF(stdPath, spcPath, fbPath string, noFB bool) []Level {
	var out []Level
	var lastErrs []error
	for _, s := range Sizes {
		// 半形優先用等寬字型,而且字級要縮到塞得進格子(見 ttf.FindMono
		// 與 ttf.build)。全形用同一份字型但不縮 —— 那邊格子有兩倍寬。
		hf, path, errs := ttf.FindMonoHalf(s.w, s.h)
		if hf == nil {
			lastErrs = errs
			continue
		}
		cw, ch := s.w, s.h+render.LineGap
		l := Level{Name: "系統字型 " + s.name, Half: hf}
		if f, err := ttf.Load(path, s.w, s.w*2, s.h); err == nil {
			l.CJK = f
		}
		// 倚天字庫有的話仍然優先:那才是與原版對齊的全形字形。
		if e, err := Eten(stdPath, spcPath, eten.NativeW, eten.NativeH); err == nil {
			l.CJK = render.ScaleCJK(e, e.W, e.H, cw*2, ch)
		}
		if !noFB {
			if fb := Fallback(fbPath, cw, ch); fb != nil {
				l.FB = fb
			}
		}
		out = append(out, l)
	}
	for _, e := range lastErrs {
		fmt.Fprintf(os.Stderr, "警告:字型載不起來 %v\n", e)
	}
	return out
}

// Fallback 掛上後備字型,補倚天(Big5 索引)沒有的字:
// 简体字、韓文、希臘文、多數符號。找不到就沒有,缺字會畫成空框。
func Fallback(path string, cw, ch int) render.CJKSource {
	if path != "" {
		f, err := ttf.Load(path, cw, cw*2, ch)
		if err != nil {
			fmt.Fprintf(os.Stderr, "警告:載不到後備字型 %s (%v)\n", path, err)
			return nil
		}
		return f
	}
	chain, _, errs := ttf.LoadChain(cw, cw*2, ch)
	// 內嵌的後備接在系統字型後面,不是取代它 —— 系統上裝的字型
	// 通常比較新、比較齊,而內嵌的那份是「這台機器什麼都沒裝」時的保險。
	for _, b := range bundled.Fallbacks() {
		f, err := ttf.LoadBytes(b, cw, cw*2, ch)
		if err != nil {
			continue
		}
		chain = append(chain, f)
	}
	if len(chain) > 0 {
		// 個別字型載不起來(x/image 讀不了某些 TTC)在有其他字型
		// 頂上時只是雜訊。一條都沒載到才是使用者需要知道的事。
		return chain
	}
	for _, e := range errs {
		fmt.Fprintf(os.Stderr, "警告:字型載不起來 %v\n", e)
	}
	fmt.Fprintln(os.Stderr, "警告:"+ttf.MissingHint())
	return nil
}
