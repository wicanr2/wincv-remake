package dict

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func mk(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	// 資料是 Big5,測試裡用純 ASCII + 幾個中文(先轉 Big5 再寫入)。
	b5 := func(s string) []byte {
		out, _ := big5(s)
		return out
	}
	write := func(name string, lines ...string) {
		var sb strings.Builder
		for _, l := range lines {
			sb.WriteString(l)
			sb.WriteByte('\n')
		}
		if err := os.WriteFile(filepath.Join(dir, name), b5(sb.String()), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("eng.txt.dat",
		"go\x01vi. 去,走",
		"apple\x01n. 蘋果",
		"apple\x01n. 蘋果公司",
		"file\x01n. 檔案")
	write("kk.txt.dat", "go\x01go", "apple\x01`^pl")
	write("chi.txt.dat", "檔 ㄉㄤˋ 檔案")
	write("origin-verb.txt.dat", "went*go", "gone*go")
	return dir
}

func TestLookupBasic(t *testing.T) {
	d, err := Load(mk(t))
	if err != nil {
		t.Fatal(err)
	}
	e, ok := d.Lookup("go")
	if !ok {
		t.Fatal("查不到 go")
	}
	if !strings.Contains(e.Trans, "走") {
		t.Errorf("解釋 = %q", e.Trans)
	}
	if e.KK != "go" {
		t.Errorf("KK = %q", e.KK)
	}
}

// 同一個詞出現多次要接起來,不能後面的蓋掉前面的。
func TestDuplicateKeysMerge(t *testing.T) {
	d, _ := Load(mk(t))
	e, ok := d.Lookup("apple")
	if !ok {
		t.Fatal("查不到 apple")
	}
	if !strings.Contains(e.Trans, "蘋果") || !strings.Contains(e.Trans, "公司") {
		t.Errorf("兩筆解釋應該都在: %q", e.Trans)
	}
}

// 查 went 要找得到 go —— 沒有這一步,字典對真實文章沒什麼用。
func TestInflectedFormFallsBackToBase(t *testing.T) {
	d, _ := Load(mk(t))
	e, ok := d.Lookup("went")
	if !ok {
		t.Fatal("查不到 went")
	}
	if e.Base != "go" {
		t.Errorf("原形 = %q, 應為 go", e.Base)
	}
	if !strings.Contains(e.Trans, "走") {
		t.Errorf("應該給出 go 的解釋,得到 %q", e.Trans)
	}
}

func TestCaseInsensitive(t *testing.T) {
	d, _ := Load(mk(t))
	if _, ok := d.Lookup("GO"); !ok {
		t.Error("大寫查不到")
	}
	if _, ok := d.Lookup("Apple"); !ok {
		t.Error("首字大寫查不到")
	}
}

func TestChineseLookup(t *testing.T) {
	d, _ := Load(mk(t))
	e, ok := d.Lookup("檔")
	if !ok {
		t.Fatal("查不到「檔」")
	}
	if !strings.Contains(e.Trans, "檔案") {
		t.Errorf("解釋 = %q", e.Trans)
	}
}

func TestPrefix(t *testing.T) {
	d, _ := Load(mk(t))
	got := d.Prefix("f", 10)
	if len(got) != 1 || got[0] != "file" {
		t.Errorf("Prefix(\"f\") = %v", got)
	}
	if len(d.Prefix("zzz", 10)) != 0 {
		t.Error("不存在的前綴應回空")
	}
}

// 用原版真實資料跑一遍。
func TestRealDictionaries(t *testing.T) {
	dir := "../../original/app"
	if _, err := os.Stat(filepath.Join(dir, "eng.txt.dat")); err != nil {
		t.Skip("找不到原版字典")
	}
	d, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	eng, kk, chi, verb := d.Len()
	t.Logf("eng=%d kk=%d chi=%d verb=%d", eng, kk, chi, verb)
	if eng < 100000 {
		t.Errorf("英漢只載到 %d 筆,原檔有十五萬行以上", eng)
	}
	if kk < 50000 {
		t.Errorf("KK 只載到 %d 筆", kk)
	}
	if verb < 300 {
		t.Errorf("不規則動詞只載到 %d 筆", verb)
	}
	for _, w := range []string{"file", "computer", "dictionary"} {
		e, ok := d.Lookup(w)
		if !ok {
			t.Errorf("查不到 %q", w)
			continue
		}
		if e.Trans == "" {
			t.Errorf("%q 沒有中文解釋", w)
		}
	}
	// 直接收錄的變化形走 exact,不該被當成 fallback。
	if e, ok := d.Lookup("went"); !ok || e.Base != "" {
		t.Errorf("went 本身就在字典裡,應直接命中(ok=%v base=%q)", ok, e.Base)
	}
	// 沒被收錄的變化形才走 origin-verb。原版的 origin-verb 裡有 154 個
	// 這種詞,"abided" 是其中之一。
	e, ok := d.Lookup("abided")
	if !ok {
		t.Fatal("查不到 abided")
	}
	if e.Base != "abide" {
		t.Errorf("abided 的原形 = %q, 應為 abide", e.Base)
	}
	if e.Trans == "" {
		t.Error("應該給出 abide 的解釋")
	}
}
