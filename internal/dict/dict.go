// Package dict 是英漢字典與 KK 音標查詢。
//
// 原版隨附四份資料,格式各不相同(分隔字元不一樣):
//
//	eng.txt.dat         英文單字 → 中文解釋      分隔 0x01
//	kk.txt.dat          英文單字 → KK 音標       分隔 0x01
//	chi.txt.dat         國字     → 發音與解釋    分隔 空白
//	origin-verb.txt.dat 變化形   → 原形          分隔 '*'
//
// chi.txt 的分隔字元有 image 內的字串佐證:「國字及解釋間須以空白隔開」
// (docs/re/big5-strings.tsv 的 0x82156)。
//
// 原版另有 .idx 索引檔(兩層首字母表 + 位移表)。這裡不用它:
// 最大的一份 .dat 也只有 5.6 MB,載進來自己建索引更單純,
// 而且不必依賴一個還沒完全逆向的格式。.idx 的已知結構記在
// docs/formats/dict-idx.md。
package dict

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/wicanr2/wincv-remake/internal/textenc"
)

// Entry 是一筆查詢結果。
type Entry struct {
	Word  string
	Trans string // 中文解釋
	KK    string // KK 音標(已轉成 IPA,原始編碼見 kk.go)
	Base  string // 原形(查到的是變化形時才有)
}

// Dict 是載入好的字典。
type Dict struct {
	eng   map[string]string
	kk    map[string]string
	chi   map[string]string
	verb  map[string]string // 變化形 → 原形
	words []string          // 排序過的英文詞,供前綴查詢
}

// Load 從目錄載入四份資料。缺哪一份就少哪一種查詢,不會整個失敗 ——
// 使用者可能只留了其中幾份。
func Load(dir string) (*Dict, error) {
	d := &Dict{
		eng:  map[string]string{},
		kk:   map[string]string{},
		chi:  map[string]string{},
		verb: map[string]string{},
	}
	var loaded int
	type src struct {
		file string
		sep  byte
		into map[string]string
	}
	for _, s := range []src{
		{"eng.txt.dat", 0x01, d.eng},
		{"kk.txt.dat", 0x01, d.kk},
		{"chi.txt.dat", ' ', d.chi},
		{"origin-verb.txt.dat", '*', d.verb},
	} {
		raw, err := os.ReadFile(filepath.Join(dir, s.file))
		if err != nil {
			continue
		}
		parseInto(raw, s.sep, s.into)
		loaded++
	}
	if loaded == 0 {
		return nil, fmt.Errorf("%s 底下找不到任何字典資料", dir)
	}
	d.words = make([]string, 0, len(d.eng))
	for w := range d.eng {
		d.words = append(d.words, w)
	}
	sort.Strings(d.words)
	return d, nil
}

// parseInto 逐行拆 `<key><sep><value>`。資料是 Big5。
func parseInto(raw []byte, sep byte, into map[string]string) {
	for _, line := range bytes.Split(raw, []byte("\n")) {
		line = bytes.TrimRight(line, "\r")
		if len(line) == 0 {
			continue
		}
		i := bytes.IndexByte(line, sep)
		if i <= 0 {
			continue
		}
		key := textenc.Decode(line[:i], textenc.Big5)
		val := textenc.Decode(line[i+1:], textenc.Big5)
		if _, dup := into[key]; dup {
			// 同一個詞出現多次就接起來,不要後面的蓋掉前面的。
			into[key] += "; " + val
			continue
		}
		into[key] = val
	}
}

// Lookup 查一個詞。
//
// 查不到就試小寫,再試把變化形換成原形 —— 查 "went" 要找得到 "go",
// 否則字典對真實文章沒什麼用。
func (d *Dict) Lookup(word string) (Entry, bool) {
	w := strings.TrimSpace(word)
	if w == "" {
		return Entry{}, false
	}
	if e, ok := d.lookupExact(w); ok {
		return e, true
	}
	low := strings.ToLower(w)
	if low != w {
		if e, ok := d.lookupExact(low); ok {
			return e, true
		}
	}
	for _, k := range []string{w, low} {
		if base, ok := d.verb[k]; ok {
			if e, ok := d.lookupExact(base); ok {
				e.Base = base
				return e, true
			}
			return Entry{Word: k, Base: base}, true
		}
	}
	// 中文:逐字查國字解釋
	if e, ok := d.chi[w]; ok {
		return Entry{Word: w, Trans: e}, true
	}
	return Entry{}, false
}

func (d *Dict) lookupExact(w string) (Entry, bool) {
	t, okT := d.eng[w]
	k, okK := d.kk[w]
	if !okT && !okK {
		return Entry{}, false
	}
	return Entry{Word: w, Trans: t, KK: KKToIPA(k)}, true
}

// Prefix 回傳以 p 開頭的前 n 個詞,供輸入時的候選清單。
func (d *Dict) Prefix(p string, n int) []string {
	p = strings.ToLower(p)
	i := sort.SearchStrings(d.words, p)
	var out []string
	for ; i < len(d.words) && len(out) < n; i++ {
		if !strings.HasPrefix(d.words[i], p) {
			break
		}
		out = append(out, d.words[i])
	}
	return out
}

// Len 回傳各份資料的筆數,供狀態列顯示與測試。
func (d *Dict) Len() (eng, kk, chi, verb int) {
	return len(d.eng), len(d.kk), len(d.chi), len(d.verb)
}
