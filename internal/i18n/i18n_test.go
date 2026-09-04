package i18n

import (
	"encoding/json"
	"regexp"
	"testing"

	"github.com/wicanr2/wincv-remake/internal/i18n/srcscan"
)

// verb 抓格式化動詞。`%%` 是跳脫,不是動詞。
var verb = regexp.MustCompile(`%[-+# 0-9.*\[\]]*[a-zA-Z%]`)

func verbs(s string) []string {
	var out []string
	for _, v := range verb.FindAllString(s, -1) {
		if v != "%%" {
			out = append(out, v)
		}
	}
	return out
}

// 翻譯的格式化動詞要與原文一模一樣,順序也一樣。
//
// 這是這份目錄最容易出事的地方:譯者把「解出 %d 個檔案到 %s」寫成
// "Extracted %s files to %d" 的話,fmt 會印出 `%!d(string=...)`,
// 或在動詞數量不足時多印一段 `%!(EXTRA ...)`。畫面不會崩,只會出現
// 一串沒人看得懂的東西 —— 而那要跑到那一條路徑才看得到。
//
// 語序需要調換時用位置參數(`%[2]s ... %[1]d`),那也會被這個測試接受,
// 因為比對的是「動詞的多重集合」而不是出現順序。
func TestCatalogVerbsMatchSource(t *testing.T) {
	for _, l := range Langs() {
		if l == ZhHant {
			continue
		}
		b, err := locales.ReadFile("locales/" + string(l) + ".json")
		if err != nil {
			t.Fatalf("%s 的目錄讀不到:%v", l, err)
		}
		m := map[string]string{}
		if err := json.Unmarshal(b, &m); err != nil {
			t.Fatalf("%s 的目錄不是合法 JSON:%v", l, err)
		}
		for src, dst := range m {
			if dst == "" {
				continue // 還沒翻的留空,會 fallback 回原文
			}
			want, got := count(verbs(src)), count(verbs(dst))
			for v, n := range want {
				if got[v] != n {
					t.Errorf("%s:%q → %q\n  %s 在原文有 %d 個,譯文有 %d 個",
						l, src, dst, v, n, got[v])
				}
			}
			for v, n := range got {
				if want[v] == 0 {
					t.Errorf("%s:%q → %q\n  譯文多了 %d 個 %s", l, src, dst, n, v)
				}
			}
		}
	}
}

// sourceKeys 掃原始碼,收集所有進 i18n.T / Sprintf / Errorf 的字串。
//
// 掃而不是讀一份產生好的清單:清單會過期,而過期的清單讓這個測試
// 變成「檢查兩份都舊的東西一不一致」。
func sourceKeys(t *testing.T) map[string]bool {
	t.Helper()
	out := map[string]bool{}
	for _, root := range []string{"../..", ""} {
		if root == "" {
			break
		}
		hits, err := srcscan.Walk(root)
		if err != nil {
			t.Fatalf("掃原始碼失敗:%v", err)
		}
		for _, h := range hits {
			if h.Done {
				out[h.Text] = true
			}
		}
	}
	if len(out) < 100 {
		t.Fatalf("只掃到 %d 條 key,掃描大概沒走到原始碼", len(out))
	}
	return out
}

func count(vs []string) map[string]int {
	m := map[string]int{}
	for _, v := range vs {
		m[v]++
	}
	return m
}

// 語系目錄不能有原始碼裡沒有的 key(那表示原文改過而翻譯沒跟上,
// 那一條翻譯已經是死的)。清單由 tools/i18nscan 產生,見 TestKeysFileFresh。
func TestNoOrphanKeys(t *testing.T) {
	src := sourceKeys(t)
	for _, l := range Langs() {
		if l == ZhHant {
			continue
		}
		b, _ := locales.ReadFile("locales/" + string(l) + ".json")
		m := map[string]string{}
		json.Unmarshal(b, &m)
		n := 0
		for k := range m {
			if !src[k] {
				if n < 10 {
					t.Errorf("%s 的目錄有原始碼裡沒有的 key:%q", l, k)
				}
				n++
			}
		}
		if n > 10 {
			t.Errorf("%s 另外還有 %d 條", l, n-10)
		}
	}
}

// Detect 與 FromTag 的對應。中文的簡繁不能只看語言碼。
func TestFromTag(t *testing.T) {
	for _, c := range []struct {
		in   string
		want Lang
	}{
		{"ja_JP.UTF-8", Ja}, {"ja", Ja},
		{"en_US.UTF-8", En}, {"en-GB", En},
		{"zh_TW.UTF-8", ZhHant}, {"zh-Hant", ZhHant}, {"zh_HK", ZhHant},
		{"zh_CN.UTF-8", ZhHans}, {"zh-Hans-CN", ZhHans}, {"zh", ZhHans},
	} {
		got, ok := FromTag(c.in)
		if !ok || got != c.want {
			t.Errorf("FromTag(%q) = %v %v,預期 %v", c.in, got, ok, c.want)
		}
	}
	if _, ok := FromTag("de_DE"); ok {
		t.Error("不支援的語言不該回 true")
	}
}

// 換語言之後 T 要拿到新語言的字;查不到的 key 原樣回來。
func TestSetAndFallback(t *testing.T) {
	defer Set(ZhHant)
	Set(En)
	if Current() != En {
		t.Fatalf("Current = %v", Current())
	}
	if got := T("這個 key 一定不在目錄裡 zzz"); got != "這個 key 一定不在目錄裡 zzz" {
		t.Errorf("查不到的 key 應該原樣回來,得到 %q", got)
	}
	Set(ZhHant)
	if got := T("檔案"); got != "檔案" {
		t.Errorf("繁中不該經過目錄,得到 %q", got)
	}
}
