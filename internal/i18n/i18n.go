// Package i18n 是介面文字的語系目錄。
//
// **key 就是繁體中文原文**,不另外發明訊息 ID。這樣做的理由有兩個:
// 讀原始碼的人看到 T("檔案") 就知道畫面上會出現什麼,不必去查一張表;
// 而且漏翻的字串會原樣顯示成中文 —— 那是這個專案的母語,比
// "menu.file.label" 這種東西有用得多,也不會讓畫面破一個洞。
//
// 代價是原文改動時對應的翻譯會失效(key 變了)。TestCatalogsCoverSource
// 盯著這件事:掃過原始碼裡所有進 T() 的字串,對不上目錄就報。
package i18n

import (
	"embed"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"sync"
)

// Lang 是 BCP 47 的語言標籤子集。
type Lang string

const (
	ZhHant Lang = "zh-Hant" // 繁體中文,原生語言
	ZhHans Lang = "zh-Hans" // 簡體中文
	En     Lang = "en"
	Ja     Lang = "ja"
)

// Langs 是支援的語言,依選單顯示的順序。
func Langs() []Lang { return []Lang{ZhHant, ZhHans, En, Ja} }

// Name 是語言自己的名字(選單上用),不是翻譯過的名字 ——
// 找自己語言的人認得的是「日本語」而不是「日文」。
func Name(l Lang) string {
	switch l {
	case ZhHant:
		return "繁體中文"
	case ZhHans:
		return "简体中文"
	case En:
		return "English"
	case Ja:
		return "日本語"
	}
	return string(l)
}

// Valid 說一個字串是不是支援的語言。
func Valid(s string) (Lang, bool) {
	for _, l := range Langs() {
		if string(l) == s {
			return l, true
		}
	}
	return "", false
}

//go:embed locales/*.json
var locales embed.FS

var (
	mu   sync.RWMutex
	cur  = ZhHant
	dict map[string]string
)

// Set 換語言。繁體中文不需要目錄(key 就是它),其餘語言載入對應的目錄;
// 載不到就留在原地 —— 換語言失敗不該讓畫面變成一堆缺字。
func Set(l Lang) {
	mu.Lock()
	defer mu.Unlock()
	if l == ZhHant {
		cur, dict = ZhHant, nil
		return
	}
	b, err := locales.ReadFile("locales/" + string(l) + ".json")
	if err != nil {
		return
	}
	m := map[string]string{}
	if err := json.Unmarshal(b, &m); err != nil {
		return
	}
	cur, dict = l, m
}

// Current 是目前的語言。
func Current() Lang {
	mu.RLock()
	defer mu.RUnlock()
	return cur
}

// T 查一個字串的翻譯。查不到就回原文。
//
// 查不到是正常狀態,不是錯誤:新加的字串在翻譯跟上之前會顯示中文,
// 那比顯示一個空字串或 key 本身好。
func T(s string) string {
	mu.RLock()
	defer mu.RUnlock()
	if dict == nil {
		return s
	}
	if v, ok := dict[s]; ok && v != "" {
		return v
	}
	return s
}

// Sprintf 先翻格式字串再套用參數。
//
// 翻的是**格式字串本身**(含 %d %s),不是翻好之後再拼 —— 語序在不同
// 語言裡會變,「第 %d 頁」在英文是 "page %d",動詞和數字的前後關係
// 只有整句一起翻才對得起來。
func Sprintf(format string, a ...any) string {
	return fmt.Sprintf(T(format), a...)
}

// Errorf 與 Sprintf 相同,但回傳 error。
func Errorf(format string, a ...any) error {
	return fmt.Errorf(T(format), a...)
}

// Detect 從環境猜使用者的語言。猜不出來就是繁體中文。
//
// 只看語言與文字(zh-Hant / zh-Hans),不看地區:同一個語言在不同地區
// 的介面用詞差異遠小於「介面是中文還是英文」這件事。
//
// 中文的簡繁判斷不能只看語言碼:`zh_TW` 與 `zh_HK` 是繁體,`zh_CN`、
// `zh_SG` 是簡體,而單純的 `zh` 兩邊都有可能 —— 這裡把裸 `zh` 當簡體,
// 因為 zh 沒有標註時最常見的來源是中國大陸的環境。
func Detect() Lang {
	for _, k := range []string{"LC_ALL", "LC_MESSAGES", "LANG"} {
		if v := os.Getenv(k); v != "" {
			if l, ok := fromTag(v); ok {
				return l
			}
		}
	}
	return ZhHant
}

// FromTag 把一個 locale 字串(zh_TW.UTF-8、ja-JP、en_US)對到語言。
func FromTag(s string) (Lang, bool) { return fromTag(s) }

func fromTag(s string) (Lang, bool) {
	s = strings.ToLower(s)
	if i := strings.IndexAny(s, ".@"); i >= 0 {
		s = s[:i]
	}
	s = strings.ReplaceAll(s, "_", "-")
	switch {
	case strings.HasPrefix(s, "ja"):
		return Ja, true
	case strings.HasPrefix(s, "en"):
		return En, true
	case strings.HasPrefix(s, "zh"):
		// hant / hans 明寫的優先,其次看地區。
		if strings.Contains(s, "hant") {
			return ZhHant, true
		}
		if strings.Contains(s, "hans") {
			return ZhHans, true
		}
		for _, r := range []string{"-tw", "-hk", "-mo"} {
			if strings.Contains(s, r) {
				return ZhHant, true
			}
		}
		return ZhHans, true
	}
	return "", false
}
