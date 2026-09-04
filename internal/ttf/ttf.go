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
	"github.com/wicanr2/wincv-remake/internal/i18n"
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
	// AA 開著時字模帶覆蓋率(fnt.Glyph.Alpha)而不是用門檻切成 1-bit。
	// 要在第一次取字之前設好 —— 字模有快取。
	AA bool
}

// SetAA 設定整條鏈的反鋸齒。
func (c Chain) SetAA(on bool) {
	for _, f := range c {
		if f != nil {
			f.AA = on
		}
	}
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
	f, err := loadFrom(data, halfW, fullW, h)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return f, nil
}

// loadFrom 是 Load 與 LoadBytes 共用的那一段。
func loadFrom(data []byte, halfW, fullW, h int) (*Font, error) {
	return build(data, halfW, fullW, h, false)
}

// build 建一份字型。fitHalf 為真時把字級縮到「半形字塞得進 halfW」。
//
// 為什麼需要縮:字級是用**格高**設的(em = h),而一個字佔多寬是字型
// 自己決定的。DejaVu Sans Mono 在 15 px em 下一格是 9 px,塞進 8 px 的
// 格子右邊就被切掉 1 px —— m 少一豎會看成 n,W 少一撇會看成 V。
// 那不是「有點醜」,是**讀到錯的字**。
//
// 只有拿來當半形來源時才縮。同一份字型當全形來源時格子有兩倍寬,
// 漢字的 advance 剛好是 1 em,本來就塞得下;跟著縮只會讓中文平白變小。
func build(data []byte, halfW, fullW, h int, fitHalf bool) (*Font, error) {
	col, err := opentype.ParseCollection(data)
	if err != nil {
		return nil, fmt.Errorf(i18n.T("認不得的字型: %w"), err)
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
	size := float64(h)
	face, err := opentype.NewFace(sf, &opentype.FaceOptions{
		Size: size, DPI: 72, Hinting: font.HintingFull,
	})
	if err != nil {
		return nil, err
	}
	if fitHalf {
		// 量 M 的 advance:等寬字型每個字一樣寬,比例字型的 M 是最寬的
		// 那一批,兩種情況拿它當基準都對。收斂得很快,三次夠了。
		for i := 0; i < 3; i++ {
			adv, ok := face.GlyphAdvance('M')
			if !ok || adv.Ceil() <= halfW || adv.Ceil() <= 0 {
				break
			}
			size = size * float64(halfW) / float64(adv.Ceil())
			face.Close()
			face, err = opentype.NewFace(sf, &opentype.FaceOptions{
				Size: size, DPI: 72, Hinting: font.HintingFull,
			})
			if err != nil {
				return nil, err
			}
		}
	}
	// 基線。縮過字級的話字身比格子矮,把它垂直置中 ——
	// 照格高算基線會把縮小後的字壓在格子底部,與旁邊沒縮的字對不齊。
	asc := int(float64(h)-size)/2 + int(size*0.88+0.5)
	if asc > h {
		asc = h
	}
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
	if f.AA {
		g.Alpha = make([]uint8, w*f.h)
	}
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
			a := alpha.AlphaAt(sx, sy).A
			if g.Alpha != nil {
				g.Alpha[y*w+x] = a
			}
			if a >= 0x80 {
				g.Bits[y*w+x] = true
			}
			if a > 0 {
				ink = true
			}
		}
	}
	if !ink {
		return nil
	}
	return g
}

// host 是「這台機器長什麼樣」。抽出來是為了測試:字型放在哪裡完全由
// 平台決定,而四個平台裡任何一台機器上都只驗得到一個。把 GOOS、環境變數
// 與家目錄變成參數之後,Windows 與 macOS 的路徑組法在 Linux 上也驗得動。
type host struct {
	goos string
	env  func(string) string
	home string
}

func realHost() host {
	h, _ := os.UserHomeDir()
	return host{goos: runtime.GOOS, env: os.Getenv, home: h}
}

// join 把家目錄底下的路徑接起來。沒有家目錄就回空字串,由呼叫端濾掉。
func (h host) join(parts ...string) string {
	if h.home == "" {
		return ""
	}
	return filepath.Join(append([]string{h.home}, parts...)...)
}

// winFontsDir 是 Windows 的系統字型目錄。
//
// [雷] 不能寫死 `C:\Windows\Fonts`。Windows 不一定裝在 C:(企業的
// 重新導向、多重開機、WinPE 都會變),而症狀是「整台機器一個中文都沒有」
// —— 看起來像沒裝字型,不像路徑猜錯。
func (h host) winFontsDir() string {
	for _, k := range []string{"SystemRoot", "windir"} {
		if v := h.env(k); v != "" {
			return filepath.Join(v, "Fonts")
		}
	}
	return `C:\Windows\Fonts`
}

// Candidates 是各平台常見的 CJK 字型位置,後面接上目錄掃描的結果。
// 第一個讀得起來的就用,都找不到就沒有後備字型。
func Candidates() []string { return candidatesFor(realHost()) }

func candidatesFor(h host) []string {
	var list []string
	switch h.goos {
	case "android":
		// Android 的系統字型讀得到但不能改。Noto CJK 從 Android 7 起是
		// 標配;DroidSansMono 老機器上還在,而且它**是等寬的** ——
		// 半形字模拿它產出來的格點最整齊。
		for _, dir := range h.fontDirs() {
			for _, n := range []string{
				"DroidSansMono.ttf", "CutiveMono.ttf",
				"NotoSansCJK-Regular.ttc", "NotoSansMonoCJKtc-Regular.otf",
				"NotoSansMonoCJKsc-Regular.otf", "NotoSansCJKtc-Regular.otf",
				"NotoSansKR-Regular.otf", "NotoSansJP-Regular.otf",
				"NotoSansSymbols-Regular-Subsetted.ttf",
				"DroidSansFallback.ttf", "Roboto-Regular.ttf",
			} {
				list = append(list, filepath.Join(dir, n))
			}
		}
	case "darwin":
		list = []string{
			"/System/Library/Fonts/Menlo.ttc",
			"/System/Library/Fonts/PingFang.ttc",
			"/System/Library/Fonts/Hiragino Sans GB.ttc",
			"/System/Library/Fonts/AppleSDGothicNeo.ttc",
			"/System/Library/Fonts/STHeiti Medium.ttc",
			"/System/Library/Fonts/Apple Symbols.ttf",
			// Catalina(10.15)把附帶的字型搬進 Supplemental,舊路徑
			// 兩個都留著 —— 舊系統上在前面那個位置。
			"/System/Library/Fonts/Supplemental/Songti.ttc",
			"/System/Library/Fonts/Supplemental/Arial Unicode.ttf",
			"/Library/Fonts/Arial Unicode.ttf",
		}
	case "windows":
		d := h.winFontsDir()
		for _, n := range []string{
			"consola.ttf", "msjh.ttc", "mingliu.ttc", "simsun.ttc",
			"msyh.ttc", "simhei.ttf", "msgothic.ttc", "malgun.ttf",
			"segoeui.ttf", "seguisym.ttf", "arialuni.ttf",
		} {
			list = append(list, filepath.Join(d, n))
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
func fontDirs() []string { return realHost().fontDirs() }

func (h host) fontDirs() []string {
	var dirs []string
	add := func(p string) {
		if p != "" {
			dirs = append(dirs, p)
		}
	}
	switch h.goos {
	case "android":
		// Android 10 之後系統被拆成好幾個分割區,字型不再只在
		// /system/fonts —— GSI 與部分機型放在 /product/fonts。
		return []string{"/system/fonts", "/product/fonts", "/system_ext/fonts"}
	case "darwin":
		add("/System/Library/Fonts")
		// Catalina 起附帶的字型都在這裡(Arial Unicode、Songti…)。
		add("/System/Library/Fonts/Supplemental")
		add("/Library/Fonts")
		add(h.join("Library", "Fonts"))
		return dirs
	case "windows":
		add(h.winFontsDir())
		// Windows 10 1803 起,「只為我安裝」的字型放在使用者自己的
		// 目錄底下,系統字型目錄裡看不到。右鍵安裝預設就是這一種。
		if v := h.env("LOCALAPPDATA"); v != "" {
			add(filepath.Join(v, "Microsoft", "Windows", "Fonts"))
		}
		return dirs
	}
	add("/usr/share/fonts")
	add("/usr/local/share/fonts")
	// XDG:使用者自己裝的字型。$XDG_DATA_HOME 沒設才是 ~/.local/share。
	if v := h.env("XDG_DATA_HOME"); v != "" {
		add(filepath.Join(v, "fonts"))
	} else {
		add(h.join(".local", "share", "fonts"))
	}
	add(h.join(".fonts"))
	for _, d := range filepath.SplitList(h.env("XDG_DATA_DIRS")) {
		if d != "" {
			add(filepath.Join(d, "fonts"))
		}
	}
	// 容器裡跑的時候,主機的字型掛在這裡(Flatpak),
	// 而 NixOS 的系統字型不在 /usr 底下。
	add("/run/host/fonts")
	add("/run/host/user-fonts")
	add("/run/current-system/sw/share/X11/fonts")
	return dirs
}

// wanted 是掃描時認得的字型檔名關鍵字,依偏好排序。
//
// 為什麼要挑而不是全收:一台桌機的字型目錄裡有好幾百個檔,
// 每一個都載進來要花數秒、吃掉數十 MB,而其中絕大多數
// (裝飾字、單一語系的花體)補不到任何一個缺字。
// [雷] 這份清單原本只有 Noto 與幾個 Linux 發行版的字型名。掃描於是在
// Windows 與 macOS 上**一個字型都找不到** —— 那兩個平台只剩上面那份寫死的
// 清單能用,而寫死清單追不上系統版本的變動,正是掃描存在的理由。
// 症狀是「在我的 Linux 上好好的,同一份程式在 Windows 上缺一堆字」。
var wanted = []string{
	// 跨平台都可能裝到的,涵蓋面最廣的排前面。
	"notosansmonocjk", "notosanscjk", "notoserifcjk", "notosansmono",
	"sourcehansans", "sourcehanserif",
	// Windows
	"consola", "msgothic", "msjh", "msyh", "mingliu", "simsun", "simhei",
	"malgun", "gulim", "batang", "yugoth", "segoeui", "seguisym", "arialuni",
	// macOS
	"menlo", "pingfang", "hiraginosans", "stheiti", "songti", "heiti",
	"applesdgothicneo", "applesymbols", "arialunicode",
	// Linux
	"wqy-zenhei", "wqy-microhei", "uming", "ukai",
	"notosanskr", "notosansjp", "notosanssc", "notosanstc",
	"notosanssymbols", "notosansmath", "notosansthai", "notosansdevanagari",
	"notosansarabic", "notosanshebrew", "unifont",
	"dejavusansmono", "dejavusans", "liberationmono", "freemono", "freeserif",
	// Android
	"droidsansmono", "droidsansfallback", "cutivemono", "roboto",
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
					// 比開頭而不是「包含」。包含會把 Inconsolata 當成
					// Windows 的 consola —— 而那是一份沒有中日韓的拉丁
					// 字型,被當成等寬來源之後半形字會整個換一套字形,
					// 畫面看起來仍然整齊,只是不再與原版對齊。
					if strings.HasPrefix(base, strings.ReplaceAll(w, "-", "")) {
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
		return i18n.T("缺字:裝一個涵蓋廣的字型就有(Debian/Ubuntu: fonts-noto-cjk、") +
			"Fedora: google-noto-sans-cjk-fonts、Arch: noto-fonts-cjk)"
	case "windows":
		return i18n.T("缺字:控制台 → 地區 → 系統管理 → 安裝東亞語系字型")
	case "darwin":
		return i18n.T("缺字:系統內建的 PingFang 應該就夠,若仍缺字請用 -fallback 指定字型檔")
	}
	return i18n.T("缺字:用 -fallback 指定一個涵蓋廣的字型檔")
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

// FindMono 先在等寬字型裡找,找不到才退回一般的候選。
//
// 半形字模一格只有 8 像素寬。比例字型的 m 與 i 差三倍寬,硬塞進同一格
// 會把 m 擠成一團、i 旁邊空一大片 —— 整頁看起來像沒對齊。等寬字型
// 本來就是照固定寬度設計的,同樣的格子塞進去該有的筆畫都還在。
func FindMono(halfW, fullW, h int) (*Font, string, []error) {
	var errs []error
	seen := map[string]bool{}
	for _, p := range Candidates() {
		if seen[p] {
			continue
		}
		seen[p] = true
		base := strings.ToLower(filepath.Base(p))
		if !strings.Contains(strings.ReplaceAll(base, "-", ""), "mono") &&
			!strings.Contains(base, "consola") && !strings.Contains(base, "menlo") &&
			!strings.Contains(base, "courier") && !strings.Contains(base, "cutive") {
			continue
		}
		if _, err := os.Stat(p); err != nil {
			continue
		}
		f, err := Load(p, halfW, fullW, h)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		return f, p, errs
	}
	f, path, more := FindAndLoad(halfW, fullW, h)
	return f, path, append(errs, more...)
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

// LoadBytes 從記憶體裡的一份字型建 face。
//
// 給打包進執行檔的字型用:那時候沒有檔案路徑可以給 Load。
func LoadBytes(data []byte, halfW, fullW, h int) (*Font, error) {
	return loadFrom(data, halfW, fullW, h)
}

// LoadHalfFit 讀一份字型當半形來源,字級縮到一個字塞得進 w 像素寬。
//
// 與 Load 分開的理由見 build:同一份字型當全形來源時不該跟著縮。
func LoadHalfFit(path string, w, h int) (*HalfFont, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	f, err := build(data, w, w*2, h, true)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return NewHalf(f, w, h), nil
}

// LoadHalfFitBytes 同 LoadHalfFit,但字型來自記憶體。
func LoadHalfFitBytes(data []byte, w, h int) (*HalfFont, error) {
	f, err := build(data, w, w*2, h, true)
	if err != nil {
		return nil, err
	}
	return NewHalf(f, w, h), nil
}

// FindMonoHalf 找一份等寬字型當半形來源,字級縮到塞得下。
func FindMonoHalf(w, h int) (*HalfFont, string, []error) {
	_, path, errs := FindMono(w, w*2, h)
	if path == "" {
		return nil, "", errs
	}
	hf, err := LoadHalfFit(path, w, h)
	if err != nil {
		return nil, "", append(errs, err)
	}
	return hf, path, errs
}
