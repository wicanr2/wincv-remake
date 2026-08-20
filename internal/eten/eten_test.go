package eten

import (
	"os"
	"testing"
)

// 倚天字庫是外部素材,不進版控。路徑可用環境變數覆寫。
func load(t *testing.T) *Font {
	t.Helper()
	std := os.Getenv("WINCV_ETEN_STD")
	spc := os.Getenv("WINCV_ETEN_SPC")
	if std == "" {
		std = "../../original/eten/STDFONT.15"
	}
	if spc == "" {
		spc = "../../original/eten/SPCFONT.15"
	}
	if _, err := os.Stat(std); err != nil {
		t.Skipf("找不到倚天字庫 %s,跳過", std)
	}
	f, err := Load(std, spc, 16, 15)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	return f
}

func inkRows(f *Font, r rune) (rows []int, spans []int) {
	g := f.Glyph(r)
	if g == nil {
		return nil, nil
	}
	for y := 0; y < g.H; y++ {
		lo, hi, n := -1, -1, 0
		for x := 0; x < g.W; x++ {
			if g.At(x, y) {
				if lo < 0 {
					lo = x
				}
				hi = x
				n++
			}
		}
		if n > 0 {
			rows = append(rows, y)
			spans = append(spans, hi-lo+1)
		}
	}
	return rows, spans
}

// kb 明講:先過這一關再往下做,否則整批字會整體偏移
// (看起來像「有字但都不對」)。「一」必須是一條橫線。
func TestIndexOracleYi(t *testing.T) {
	f := load(t)
	rows, spans := inkRows(f, '一')
	if len(rows) == 0 {
		t.Fatal("「一」取不到字模")
	}
	if len(rows) > 3 {
		t.Errorf("「一」有 %d 列墨跡,應該只有 1-3 列(一條橫線)", len(rows))
	}
	if rows[len(rows)-1]-rows[0] != len(rows)-1 {
		t.Errorf("「一」的墨跡列不連續: %v", rows)
	}
	// 只要求最寬的那一列橫跨大半個字身。倚天的「一」在橫線上方
	// 還有一個單像素的起筆點,那一列的寬度是 1,不能一起要求。
	widest := 0
	for _, s := range spans {
		if s > widest {
			widest = s
		}
	}
	if widest < 10 {
		t.Errorf("「一」最寬的一列只有 %d 欄,應該橫跨大半個字身(spans=%v)", widest, spans)
	}
}

// 索引若偏移,這幾個字會取到別的字或空白。
func TestCommonCharsNotBlank(t *testing.T) {
	f := load(t)
	for _, r := range []rune{'中', '猴', '檔', '案', '目', '錄'} {
		g := f.Glyph(r)
		if g == nil {
			t.Errorf("%q 取不到字模", r)
			continue
		}
		n := 0
		for _, b := range g.Bits {
			if b {
				n++
			}
		}
		if n < 20 {
			t.Errorf("%q 只有 %d 個前景點,疑似取到空白或索引偏移", r, n)
		}
	}
}

// 這一條擋的是「漏帶 SPCFONT.15」:標點會整批變缺字。
func TestPunctuationFromSpc(t *testing.T) {
	f := load(t)
	if f.spc == nil {
		t.Skip("沒有 SPCFONT.15")
	}
	for _, r := range []rune{'，', '。', '！', '？', '「', '」', '（', '）'} {
		g := f.Glyph(r)
		if g == nil {
			t.Errorf("%q 取不到字模(SPCFONT 沒載到或索引錯)", r)
			continue
		}
		n := 0
		for _, b := range g.Bits {
			if b {
				n++
			}
		}
		if n == 0 {
			t.Errorf("%q 是全空白", r)
		}
	}
}

// 符號補充區(Big5 C6A1-C8FE)一律回缺字。
//
// 那一區有 408 個碼位但字庫只有 365 個字模,中間有洞;用線性索引
// 會取到**別的字**,而錯字看起來像「有解出來」,比缺字難發現得多。
func TestSupplementarySymbolAreaReturnsNil(t *testing.T) {
	f := load(t)
	for _, r := range []rune{'ゃ', 'や', 'ア', 'ヾ'} {
		if g := f.Glyph(r); g != nil {
			t.Errorf("%q 落在符號補充區,應回 nil(缺字)而不是取到別的字", r)
		}
	}
}

// 三個對得齊的區段要正常取得到,不可以被上面那條誤殺。
func TestAlignedRegionsStillWork(t *testing.T) {
	f := load(t)
	for _, tc := range []struct {
		r    rune
		what string
	}{
		{'，', "符號區"},
		{'一', "常用字區起點"},
		{'龜', "次常用字區"},
	} {
		g := f.Glyph(tc.r)
		if g == nil {
			t.Errorf("%q(%s)取不到字模", tc.r, tc.what)
			continue
		}
		n := 0
		for _, b := range g.Bits {
			if b {
				n++
			}
		}
		if n < 8 {
			t.Errorf("%q(%s)只有 %d 個前景點", tc.r, tc.what, n)
		}
	}
}
