// Package note 讀寫檔案註解。
//
// 原版把註解放在每個目錄底下的 dir.doc,一行一筆,格式是
// `<檔名><空白><註解>`。這個格式沒有跳脫,所以檔名裡有空白時
// 只能靠「第一段空白之前」切,和原版一樣。
package note

import (
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/wicanr2/wincv-remake/internal/convert"
	"github.com/wicanr2/wincv-remake/internal/textenc"
)

// File 是註解檔的檔名。原版可在設定裡改。
var File = "dir.doc"

// Set 是一個目錄的註解表。
type Set struct {
	m   map[string]string
	enc textenc.Enc // 原檔的編碼,寫回去時要維持,否則原版讀到亂碼
}

// Get 回傳某個檔案的註解,沒有就回空字串。
func (s *Set) Get(name string) string {
	if s == nil || s.m == nil {
		return ""
	}
	return s.m[name]
}

// Set 設定註解。text 為空表示刪掉這一筆。
func (s *Set) Set(name, text string) {
	if s.m == nil {
		s.m = map[string]string{}
	}
	if strings.TrimSpace(text) == "" {
		delete(s.m, name)
		return
	}
	s.m[name] = text
}

// Raw 回傳底下的表。呼叫端不應該改它。
func (s *Set) Raw() map[string]string {
	if s == nil {
		return nil
	}
	return s.m
}

// Len 回傳有幾筆註解。
func (s *Set) Len() int {
	if s == nil {
		return 0
	}
	return len(s.m)
}

// Load 讀一個目錄的註解。檔案不存在不算錯,回一個空的 Set。
func Load(dir string) (*Set, error) {
	s := &Set{m: map[string]string{}, enc: textenc.UTF8}
	data, err := os.ReadFile(filepath.Join(dir, File))
	if err != nil {
		if os.IsNotExist(err) {
			return s, nil
		}
		return s, err
	}
	enc := textenc.Detect(data)
	if enc != textenc.Binary {
		s.enc = enc
	}
	text := textenc.Decode(data, s.enc)
	for _, line := range strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n") {
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		i := strings.IndexAny(line, " \t")
		if i <= 0 {
			continue
		}
		if v := strings.TrimSpace(line[i:]); v != "" {
			s.m[line[:i]] = v
		}
	}
	return s, nil
}

// Save 把註解寫回目錄。沒有任何一筆時就把檔案刪掉,不要留一個空檔。
func (s *Set) Save(dir string) error {
	p := filepath.Join(dir, File)
	if len(s.m) == 0 {
		if err := os.Remove(p); err != nil && !os.IsNotExist(err) {
			return err
		}
		return nil
	}
	names := make([]string, 0, len(s.m))
	for n := range s.m {
		names = append(names, n)
	}
	sort.Strings(names)

	var sb strings.Builder
	for _, n := range names {
		sb.WriteString(n)
		sb.WriteByte(' ')
		sb.WriteString(s.m[n])
		sb.WriteByte('\n')
	}
	// 維持原檔的編碼:原版是 Big5,寫成 UTF-8 會讓原版讀到亂碼。
	out := convert.RecodeLossy([]byte(sb.String()), textenc.UTF8, s.enc, "?")
	return os.WriteFile(p, out, 0o644)
}
