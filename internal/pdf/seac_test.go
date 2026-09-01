package pdf

import (
	"math"
	"testing"

	"os"
	"path/filepath"

	"golang.org/x/image/font/sfnt"

	"github.com/wicanr2/wincv-remake/internal/ttf"
)

// seac 是「用兩個標準字形拼出重音字」:charstring 只帶基底字與重音字在
// StandardEncoding 上的字碼,外框要自己合成。
//
// testdata/seac.pdf 裡的 Type1 字型是自己寫的,三個字形都是方塊:
//
//	A       50..450 × 0..600
//	acute   50..250 × 700..800
//	Aacute  seac 拼出來的,重音再往右 300、往上 100
//
// 用方塊而不是真的字母,是因為「重音有沒有偏一點」在真字母上看不出來,
// 在方塊上差一格都看得到。對照組是 poppler(相關係數 0.9997)。
func TestRenderType1Seac(t *testing.T) {
	r := renderPage(t, "testdata/seac.pdf", 1, RenderOptions{DPI: 96})
	const pageH = 792

	black := func(name string, x, y float64) {
		t.Helper()
		c := pixelAt(t, r, x, y, pageH)
		if c.R > 60 || c.G > 60 || c.B > 60 {
			t.Errorf("%s(%.0f, %.0f)應該有墨水,拿到 %+v", name, x, y, c)
		}
	}
	white := func(name string, x, y float64) {
		t.Helper()
		c := pixelAt(t, r, x, y, pageH)
		if c.R < 200 || c.G < 200 || c.B < 200 {
			t.Errorf("%s(%.0f, %.0f)應該是白的,拿到 %+v", name, x, y, c)
		}
	}

	// 單獨的兩個字形先確認畫得對,不然合成對不對無從談起。
	black("A", 95, 530)
	black("acute", 147, 575)

	// 合成字:基底在原處,重音往右上位移。
	black("Aacute 的基底", 185, 530)
	black("Aacute 的重音", 207, 585)

	// [雷] 這一格是關鍵:位移沒套上去的話,重音會落在這裡。
	// 少了這條檢查,「畫了重音但位置錯」與「畫對了」在測試上一模一樣。
	white("重音沒位移時會落的位置", 177, 575)
	white("基底與重音之間", 185, 570)
}

// CFF 沒有 seac 運算子,拼字是用「帶四個運算元的 endchar」。
// 手工組一條那樣的 charstring,丟進系統上真實的 OpenType/CFF 字型裡跑,
// 結果應該等於「基底字形」加上「位移過的重音字形」。
func TestCFFSeacComposition(t *testing.T) {
	path, _, f := findNonCIDCFF(t)
	if f == nil {
		t.Skip("這台機器上沒有非 CID 的 OpenType/CFF 字型")
	}
	if f.matrix != [6]float64{0.001, 0, 0, 0.001, 0, 0} {
		t.Skipf("%s 的 FontMatrix 不是預設值,座標尺度不同", path)
	}
	baseGID, ok1 := f.gidForStandardCode('A')
	accGID, ok2 := f.gidForStandardCode(0302) // acute
	if !ok1 || !ok2 {
		t.Skipf("%s 裡沒有 A 或 acute", path)
	}
	base, ok1 := f.glyph(int(baseGID))
	acc, ok2 := f.glyph(int(accGID))
	if !ok1 || !ok2 {
		t.Skipf("%s 的 A 或 acute 畫不出來", path)
	}

	// 「300 100 65 194 endchar」。Type2 的數字編碼:-107…107 加 139,
	// 108…1131 用兩個位元組。
	two := func(v int) []byte { v -= 108; return []byte{byte(v>>8) + 247, byte(v)} }
	cs := append(two(300), 100+139)
	cs = append(cs, 'A'+139)
	cs = append(cs, two(0302)...)
	cs = append(cs, 14)

	tr := &t2{f: f, subrs: f.subrs}
	tr.run(cs)
	if len(tr.out) == 0 {
		t.Fatal("合成出來是空的")
	}

	gx0, gy0, gx1, gy1 := bboxOf(tr.out)
	bx0, by0, bx1, by1 := bboxOf(base)
	ax0, ay0, ax1, ay1 := bboxOf(acc)
	// 位移之後的重音,與基底取聯集。
	wx0 := math.Min(bx0, ax0+300)
	wy0 := math.Min(by0, ay0+100)
	wx1 := math.Max(bx1, ax1+300)
	wy1 := math.Max(by1, ay1+100)
	const tol = 0.01
	if math.Abs(gx0-wx0) > tol || math.Abs(gy0-wy0) > tol ||
		math.Abs(gx1-wx1) > tol || math.Abs(gy1-wy1) > tol {
		t.Errorf("%s:合成的外接矩形 %.1f %.1f %.1f %.1f,期待 %.1f %.1f %.1f %.1f",
			path, gx0, gy0, gx1, gy1, wx0, wy0, wx1, wy1)
	}
	// 位移真的有套上去:重音在基底右上方,聯集一定比基底本身寬。
	if wx1 <= bx1 && wy1 <= by1 {
		t.Skipf("%s 的 acute 位移後仍被基底包住,這個字型驗不到位移", path)
	}
}

// standardSID 靠的是「CFF 的前 149 個標準字串就是 StandardEncoding 的順序」
// 這個規律,不是抄來的表。拿真實字型驗一次:由字碼查到的字形,
// 要跟 sfnt 用 cmap 查同一個字元查到的是同一個。
func TestStandardSIDAgainstRealFont(t *testing.T) {
	path, data, f := findNonCIDCFF(t)
	if f == nil {
		t.Skip("這台機器上沒有非 CID 的 OpenType/CFF 字型")
	}
	sf, err := sfnt.Parse(data)
	if err != nil {
		if col, err2 := sfnt.ParseCollection(data); err2 == nil && col.NumFonts() > 0 {
			sf, err = col.Font(0)
		}
		if err != nil {
			t.Skipf("%s 這份 sfnt 解不開:%v", path, err)
		}
	}
	var buf sfnt.Buffer
	checked := 0
	// 下半部(字碼就是 ASCII)與上半部各挑幾個。
	for _, c := range []struct {
		code int
		r    rune
	}{
		{'A', 'A'}, {'Z', 'Z'}, {'a', 'a'}, {'z', 'z'},
		{'0' + 0, '0'}, {' ', ' '}, {'?', '?'},
		{0302, '´'}, // acute
		{0310, '¨'}, // dieresis
		{0341, 'Æ'}, // AE
	} {
		gid, ok := f.gidForStandardCode(c.code)
		if !ok {
			continue
		}
		want, err := sf.GlyphIndex(&buf, c.r)
		if err != nil || want == 0 {
			continue
		}
		checked++
		if sfnt.GlyphIndex(gid) != want {
			t.Errorf("%s:字碼 %d(%q)由 SID 查到第 %d 號字形,cmap 查到第 %d 號",
				path, c.code, c.r, gid, want)
		}
	}
	if checked < 4 {
		t.Skipf("%s 只驗到 %d 個字碼,樣本太少", path, checked)
	}
	t.Logf("%s:驗了 %d 個字碼", path, checked)
}

// findNonCIDCFF 找一份非 CID 的 OpenType/CFF 字型。
// CID 字型的 charset 存的是 CID 不是 SID,拿來驗 SID 的對應會得到
// 一個看起來合理但無意義的結果。
//
// 不能只看 internal/ttf 的候選清單:那份是為了補中日韓的字挑的,
// 上面全是 CID 字型。西文的 .otf(URW 那一套)才是非 CID 的。
func findNonCIDCFF(t *testing.T) (string, []byte, *cffFont) {
	t.Helper()
	var paths []string
	for _, dir := range []string{
		"/usr/share/fonts/opentype/urw-base35",
		"/usr/share/fonts/opentype",
		"/usr/share/fonts",
	} {
		more, _ := filepath.Glob(filepath.Join(dir, "*.otf"))
		paths = append(paths, more...)
		if len(paths) > 0 {
			break
		}
	}
	paths = append(paths, ttf.Candidates()...)
	for _, path := range paths {
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		cff := sfntTable(data, "CFF ")
		if cff == nil {
			continue
		}
		f, err := parseCFF(cff)
		if err != nil || f.isCID {
			continue
		}
		return path, data, f
	}
	return "", nil, nil
}
