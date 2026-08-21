// Package ttf 把 TrueType / OpenType 字型光柵化成 1-bit 點陣。
//
// 為什麼需要:倚天字庫是用 Big5 索引的,Big5 沒有的字就沒有字模 ——
// 简体字、韓文、希臘文、大部分符號都畫不出來,而且是安靜地空白。
// UTF-8 檔案很容易碰到這些字,所以補一條後備:任何一個系統上的
// CJK 字型都能拿來補,渲染成與點陣字同尺寸的 1-bit 圖。
//
// 門檻取二值而不是灰階:整個 UI 是點陣風格,混一個帶反鋸齒的字進去
// 會比缺字更突兀。
package ttf

import (
	"fmt"
	"image"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"

	"golang.org/x/image/font"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/math/fixed"

	"github.com/wicanr2/wincv-remake/internal/cell"
	"github.com/wicanr2/wincv-remake/internal/fnt"
)

// Font 是一個後備字型,依 cell.IsWide 決定每個字佔一格或兩格。
type Font struct {
	face   font.Face
	halfW  int
	fullW  int
	h      int
	ascent int
	mu     sync.Mutex
	cache  map[rune]*fnt.Glyph
}

// Load 讀入字型並建立指定尺寸的 face。
//
// size 用格高當像素大小:點陣字的「字身高」就是它的視覺大小,
// 用同一個數字兩邊才對得起來。
func Load(path string, halfW, fullW, h int) (*Font, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	col, err := opentype.ParseCollection(data)
	if err != nil {
		return nil, fmt.Errorf("%s: 認不得的字型: %w", path, err)
	}
	sf, err := col.Font(0)
	if err != nil {
		return nil, err
	}
	// Size 用格高、DPI 72,em 方框就剛好等於一格高。
	//
	// [雷] 基線**不能**用 face.Metrics() 的 ascent。TrueType 的 ascent
	// 含行距,CJK 字型尤其誇張(Noto CJK 在 16 px 時 ascent 約 18),
	// 直接拿來當基線會把字推出格子下緣。照 ascent+descent 等比縮回來
	// 也不對 —— 那會把字縮得比周圍的點陣字小一大截。
	// 這裡用 CJK 慣用的基線比例:字身頂端對齊格頂,基線在 0.88 em 處。
	face, err := opentype.NewFace(sf, &opentype.FaceOptions{
		Size: float64(h), DPI: 72, Hinting: font.HintingFull,
	})
	if err != nil {
		return nil, err
	}
	asc := (h*88 + 50) / 100
	return &Font{
		face: face, halfW: halfW, fullW: fullW, h: h,
		ascent: asc,
		cache:  map[rune]*fnt.Glyph{},
	}, nil
}

// Glyph 取一個字的點陣圖。字型裡沒有就回 nil。
func (f *Font) Glyph(r rune) *fnt.Glyph {
	f.mu.Lock()
	defer f.mu.Unlock()
	if g, ok := f.cache[r]; ok {
		return g
	}
	g := f.render(r)
	f.cache[r] = g
	return g
}

func (f *Font) render(r rune) *fnt.Glyph {
	if _, ok := f.face.GlyphAdvance(r); !ok {
		return nil
	}
	w := f.halfW
	if cell.IsWide(r) {
		w = f.fullW
	}
	dr, mask, maskp, _, ok := f.face.Glyph(fixed.P(0, f.ascent), r)
	if !ok || mask == nil {
		return nil
	}
	g := &fnt.Glyph{W: w, H: f.h, Bits: make([]bool, w*f.h)}
	alpha, _ := mask.(*image.Alpha)
	if alpha == nil {
		return nil
	}
	ink := false
	for y := 0; y < f.h; y++ {
		sy := y - dr.Min.Y + maskp.Y
		if sy < alpha.Rect.Min.Y || sy >= alpha.Rect.Max.Y {
			continue
		}
		for x := 0; x < w; x++ {
			sx := x - dr.Min.X + maskp.X
			if sx < alpha.Rect.Min.X || sx >= alpha.Rect.Max.X {
				continue
			}
			// 一半以上不透明才算有筆畫。門檻放低會讓細筆畫糊掉,
			// 放高會讓整個字變瘦。
			if alpha.AlphaAt(sx, sy).A >= 0x80 {
				g.Bits[y*w+x] = true
				ink = true
			}
		}
	}
	if !ink {
		return nil
	}
	return g
}

// Candidates 是各平台常見的 CJK 字型位置。
// 第一個讀得起來的就用,都找不到就沒有後備字型。
func Candidates() []string {
	var list []string
	switch runtime.GOOS {
	case "android":
		// Android 的系統字型都在 /system/fonts,讀得到但不能改。
		// Noto CJK 從 Android 7 起是標配;DroidSansMono 老機器上還在,
		// 而且它**是等寬的** —— 半形字模拿它產出來的格點最整齊。
		list = []string{
			"/system/fonts/DroidSansMono.ttf",
			"/system/fonts/CutiveMono.ttf",
			"/system/fonts/NotoSansCJK-Regular.ttc",
			"/system/fonts/NotoSansMonoCJKtc-Regular.otf",
			"/system/fonts/NotoSansMonoCJKsc-Regular.otf",
			"/system/fonts/NotoSansCJKtc-Regular.otf",
			"/system/fonts/NotoSansKR-Regular.otf",
			"/system/fonts/NotoSansJP-Regular.otf",
			"/system/fonts/NotoSansSymbols-Regular-Subsetted.ttf",
			"/system/fonts/DroidSansFallback.ttf",
			"/system/fonts/Roboto-Regular.ttf",
		}
	case "darwin":
		list = []string{
			"/System/Library/Fonts/Menlo.ttc",
			"/System/Library/Fonts/PingFang.ttc",
			"/System/Library/Fonts/Hiragino Sans GB.ttc",
			"/System/Library/Fonts/AppleSDGothicNeo.ttc",
			"/System/Library/Fonts/STHeiti Medium.ttc",
			"/System/Library/Fonts/Apple Symbols.ttf",
			"/Library/Fonts/Arial Unicode.ttf",
		}
	case "windows":
		list = []string{
			`C:\Windows\Fonts\consola.ttf`,
			`C:\Windows\Fonts\msjh.ttc`,
			`C:\Windows\Fonts\mingliu.ttc`,
			`C:\Windows\Fonts\simsun.ttc`,
			`C:\Windows\Fonts\msgothic.ttc`,
			`C:\Windows\Fonts\malgun.ttf`,
			`C:\Windows\Fonts\segoeui.ttf`,
			`C:\Windows\Fonts\arialuni.ttf`,
		}
	default:
		// 路徑因發行版而異,列的是幾個常見的位置。真正管用的是
		// 底下的目錄掃描 —— 寫死清單永遠追不上所有發行版。
		list = []string{
			"/usr/share/fonts/opentype/noto/NotoSansCJK-Regular.ttc",
			"/usr/share/fonts/truetype/noto/NotoSansCJK-Regular.ttc",
			"/usr/share/fonts/noto-cjk/NotoSansCJK-Regular.ttc",
			"/usr/share/fonts/opentype/noto/NotoSerifCJK-Regular.ttc",
			"/usr/share/fonts/truetype/arphic/uming.ttc",
			"/usr/share/fonts/truetype/arphic/ukai.ttc",
			"/usr/share/fonts/truetype/wqy/wqy-zenhei.ttc",
			"/usr/share/fonts/truetype/wqy/wqy-microhei.ttc",
			"/usr/share/fonts/wqy-zenhei/wqy-zenhei.ttc",
			"/usr/share/fonts/truetype/dejavu/DejaVuSansMono.ttf",
			"/usr/share/fonts/truetype/dejavu/DejaVuSans.ttf",
			"/usr/share/fonts/dejavu/DejaVuSans.ttf",
			"/usr/share/fonts/truetype/liberation/LiberationMono-Regular.ttf",
			"/usr/share/fonts/liberation/LiberationMono-Regular.ttf",
		}
	}
	return append(list, scanned()...)
}

// fontDirs 是要掃的目錄。
func fontDirs() []string {
	switch runtime.GOOS {
	case "android":
		return []string{"/system/fonts"}
	case "darwin":
		dirs := []string{"/System/Library/Fonts", "/Library/Fonts"}
		if h, err := os.UserHomeDir(); err == nil {
			dirs = append(dirs, filepath.Join(h, "Library", "Fonts"))
		}
		return dirs
	case "windows":
		return []string{`C:\Windows\Fonts`}
	}
	dirs := []string{"/usr/share/fonts", "/usr/local/share/fonts"}
	if h, err := os.UserHomeDir(); err == nil {
		dirs = append(dirs,
			filepath.Join(h, ".local", "share", "fonts"),
			filepath.Join(h, ".fonts"))
	}
	return dirs
}

// wanted 是掃描時認得的字型檔名關鍵字,依偏好排序。
//
// 為什麼要挑而不是全收:一台桌機的字型目錄裡有好幾百個檔,
// 每一個都載進來要花數秒、吃掉數十 MB,而其中絕大多數
// (裝飾字、單一語系的花體)補不到任何一個缺字。
var wanted = []string{
	"notosansmonocjk", "notosanscjk", "notoserifcjk", "notosansmono",
	"sourcehansans", "sourcehanserif", "wqy-zenhei", "wqy-microhei",
	"uming", "ukai", "notosanskr", "notosansjp", "notosanssc", "notosanstc",
	"notosanssymbols", "notosansmath", "notosansthai", "notosansdevanagari",
	"notosansarabic", "notosanshebrew", "unifont",
	"dejavusansmono", "dejavusans", "liberationmono", "freemono", "freeserif",
}

// scanCap 是掃描結果的數量上限。
//
// 有上限是必要的:每一個字型都會被載進記憶體並建 face,而「多一個
// 補得到的字」的邊際效益掉得非常快。二十個已經涵蓋到罕用符號了。
const scanCap = 20

var (
	scanOnce sync.Once
	scanList []string
)

// scanned 掃常見的字型目錄,找出名字像「涵蓋面廣」的那幾個。
//
// 寫死路徑清單永遠追不上所有發行版 —— 同一個 Noto CJK 在 Debian、
// Fedora、Arch、NixOS 底下是四個不同的路徑。掃描一次就都找得到。
//
// 只掃一次(sync.Once):結果在一次執行裡不會變,而每次換字級都重掃
// 會讓放大縮小卡住。
func scanned() []string {
	scanOnce.Do(func() {
		type hit struct {
			path string
			rank int
		}
		var hits []hit
		seen := map[string]bool{}
		for _, dir := range fontDirs() {
			// 深度有限:字型目錄底下通常只有一兩層,而遞迴進一個
			// 意外的符號連結會走很久。
			_ = filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
				if err != nil {
					return nil // 讀不到就跳過,不是錯
				}
				if d.IsDir() {
					if strings.Count(p[len(dir):], string(os.PathSeparator)) > 3 {
						return fs.SkipDir
					}
					return nil
				}
				switch strings.ToLower(filepath.Ext(p)) {
				case ".ttf", ".ttc", ".otf", ".otc":
				default:
					return nil
				}
				base := strings.ToLower(filepath.Base(p))
				base = strings.NewReplacer("-", "", "_", "", " ", "").Replace(base)
				// 同一家族的不同字重涵蓋的字**完全一樣**,補不到任何新字,
				// 卻各吃一份記憶體與建 face 的時間。實測某台機器上
				// 21 個載進來的字型有 15 個是這種。
				if otherWeight(base) {
					return nil
				}
				for i, w := range wanted {
					if strings.Contains(base, strings.ReplaceAll(w, "-", "")) {
						if !seen[p] {
							seen[p] = true
							hits = append(hits, hit{p, i})
						}
						break
					}
				}
				return nil
			})
		}
		sort.SliceStable(hits, func(i, j int) bool { return hits[i].rank < hits[j].rank })
		for _, h := range hits {
			if len(scanList) >= scanCap {
				break
			}
			scanList = append(scanList, h.path)
		}
	})
	return scanList
}

// otherWeight 認出「同一家族但不是 Regular」的檔名。
//
// 只看檔名而不是讀字型的 OS/2 表:讀了就得先載進來,而載進來
// 正是這裡要避免的事。檔名慣例夠一致,判錯的代價也只是多載一個。
func otherWeight(base string) bool {
	for _, w := range []string{
		"black", "bold", "thin", "light", "medium", "semibold", "demi",
		"extra", "ultra", "heavy", "italic", "oblique", "condensed",
		// unifont 附的 *_sample 是展示用的節錄,補不到正式字型沒有的字。
		"sample",
	} {
		if strings.Contains(base, w) {
			return true
		}
	}
	return false
}

// MissingHint 是缺字時給使用者的建議:裝哪個套件就有。
//
// 存在的理由是「缺字」與「字型載不起來」的症狀一模一樣(都是空框),
// 而使用者能做的事只有一件 —— 裝一個涵蓋廣的字型。與其讓他自己猜,
// 不如直接說出這個平台上該裝什麼。
func MissingHint() string {
	switch runtime.GOOS {
	case "linux":
		return "缺字:裝一個涵蓋廣的字型就有(Debian/Ubuntu: fonts-noto-cjk、" +
			"Fedora: google-noto-sans-cjk-fonts、Arch: noto-fonts-cjk)"
	case "windows":
		return "缺字:控制台 → 地區 → 系統管理 → 安裝東亞語系字型"
	case "darwin":
		return "缺字:系統內建的 PingFang 應該就夠,若仍缺字請用 -fallback 指定字型檔"
	}
	return "缺字:用 -fallback 指定一個涵蓋廣的字型檔"
}

// Chain 依序試多個字型,誰有這個字就用誰。
//
// 一個字型很難涵蓋全部:實測 Noto CJK 有漢字與韓文但缺帶聲調的希臘字母,
// DejaVu 剛好相反。串起來比選一個好。
type Chain []*Font

func (c Chain) Glyph(r rune) *fnt.Glyph {
	for _, f := range c {
		if f == nil {
			continue
		}
		if g := f.Glyph(r); g != nil {
			return g
		}
	}
	return nil
}

// LoadChain 把所有讀得起來的候選字型串成一條。
//
// 回傳的第二個值是實際用到的路徑,第三個是「檔案在、但載不起來」的原因。
// **不要把第三個丟掉** —— 字型在那裡卻用不了,和根本沒裝是完全不同的
// 兩件事,而兩者的症狀都只是「中文變成空框」。
func LoadChain(halfW, fullW, h int) (Chain, []string, []error) {
	var chain Chain
	var used []string
	var errs []error
	// 寫死清單與目錄掃描一定會撞在一起(掃描找得到的正是清單上那幾個)。
	// 不去重的話同一個字型會被載兩次,而第二次一個新字都補不到。
	seen := map[string]bool{}
	for _, p := range Candidates() {
		if seen[p] {
			continue
		}
		seen[p] = true
		if _, err := os.Stat(p); err != nil {
			continue // 沒裝,不算錯
		}
		f, err := Load(p, halfW, fullW, h)
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", p, err))
			continue
		}
		chain = append(chain, f)
		used = append(used, p)
	}
	return chain, used, errs
}

// FindAndLoad 依序試 Candidates,回第一個成功的。
//
// 第三個回傳值是「檔案在、但載不起來」的原因清單。**不要把它丟掉** ——
// 字型在那裡卻用不了,和根本沒裝是完全不同的兩件事,而兩者的症狀
// 都只是「中文變成空框」。
func FindAndLoad(halfW, fullW, h int) (*Font, string, []error) {
	var errs []error
	seen := map[string]bool{}
	for _, p := range Candidates() {
		if seen[p] {
			continue
		}
		seen[p] = true
		if _, err := os.Stat(p); err != nil {
			continue // 沒裝,不算錯
		}
		f, err := Load(p, halfW, fullW, h)
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", p, err))
			continue
		}
		return f, p, errs
	}
	return nil, "", errs
}
