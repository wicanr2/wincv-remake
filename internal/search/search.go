// Package search 是「尋找 檔名/字串/註解」(原版主畫面的 W)。
//
// 三種目標:
//
//	檔名  遞迴比對路徑上的檔名
//	字串  比對文字檔的內容(要先判編碼,否則 Big5 檔搜不到中文)
//	註解  比對 dir.doc 這類註解檔裡的說明
//
// 內容搜尋一定要經過編碼判讀:直接拿 UTF-8 的關鍵字去比對 Big5 檔案的
// 位元組,中文永遠搜不到,而且**不會報錯**,只是安靜地找不到。
package search

import (
	"bufio"
	"bytes"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/wicanr2/wincv-remake/internal/textenc"
)

// Kind 是搜尋目標。
type Kind int

const (
	ByName Kind = iota
	ByContent
	ByComment
)

// Hit 是一筆結果。
type Hit struct {
	Dir  string // 檔案所在的目錄
	Name string
	Line int    // 內容搜尋時的行號(1 起算),其餘為 0
	Text string // 內容搜尋時命中的那一行(已轉成 UTF-8)
}

// Path 回傳完整路徑。
func (h Hit) Path() string { return filepath.Join(h.Dir, h.Name) }

// Options 是搜尋設定。
type Options struct {
	Kind      Kind
	Recursive bool
	// MaxHits 是上限,達到就停。0 表示用 DefaultMaxHits。
	MaxHits int
	// MaxFileSize 是內容搜尋時單檔的大小上限,超過就跳過。
	MaxFileSize int64
}

const (
	DefaultMaxHits     = 2000
	DefaultMaxFileSize = 8 << 20
)

// Run 執行搜尋。pattern 不分大小寫。
func Run(root, pattern string, o Options) ([]Hit, error) {
	if pattern == "" {
		return nil, nil
	}
	if o.MaxHits == 0 {
		o.MaxHits = DefaultMaxHits
	}
	if o.MaxFileSize == 0 {
		o.MaxFileSize = DefaultMaxFileSize
	}
	low := strings.ToLower(pattern)

	var hits []Hit
	walk := func(dir string, entries []os.DirEntry) {
		for _, e := range entries {
			if len(hits) >= o.MaxHits {
				return
			}
			if e.IsDir() {
				continue
			}
			switch o.Kind {
			case ByName:
				if strings.Contains(strings.ToLower(e.Name()), low) {
					hits = append(hits, Hit{Dir: dir, Name: e.Name()})
				}
			case ByContent:
				hits = appendContentHits(hits, dir, e, low, o)
			}
		}
	}

	if o.Kind == ByComment {
		return searchComments(root, low, o)
	}

	if !o.Recursive {
		entries, err := os.ReadDir(root)
		if err != nil {
			return nil, err
		}
		walk(root, entries)
		return hits, nil
	}

	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil // 讀不到的目錄跳過,不要讓整個搜尋失敗
		}
		if len(hits) >= o.MaxHits {
			return filepath.SkipAll
		}
		if d.IsDir() {
			return nil
		}
		switch o.Kind {
		case ByName:
			if strings.Contains(strings.ToLower(d.Name()), low) {
				hits = append(hits, Hit{Dir: filepath.Dir(p), Name: d.Name()})
			}
		case ByContent:
			hits = appendContentHits(hits, filepath.Dir(p), d, low, o)
		}
		return nil
	})
	return hits, err
}

func appendContentHits(hits []Hit, dir string, d fs.DirEntry, low string, o Options) []Hit {
	info, err := d.Info()
	if err != nil || info.Size() > o.MaxFileSize || info.Size() == 0 {
		return hits
	}
	p := filepath.Join(dir, d.Name())
	data, err := os.ReadFile(p)
	if err != nil {
		return hits
	}
	enc := textenc.Detect(data)
	if enc == textenc.Binary {
		return hits
	}
	// 先整份轉成 UTF-8 再比對。少了這一步,Big5 檔案裡的中文永遠搜不到。
	text := textenc.Decode(data, enc)
	if !strings.Contains(strings.ToLower(text), low) {
		return hits
	}
	sc := bufio.NewScanner(strings.NewReader(text))
	sc.Buffer(make([]byte, 0, 64*1024), 4<<20)
	n := 0
	for sc.Scan() {
		n++
		line := sc.Text()
		if strings.Contains(strings.ToLower(line), low) {
			hits = append(hits, Hit{Dir: dir, Name: d.Name(), Line: n, Text: strings.TrimSpace(line)})
			if len(hits) >= o.MaxHits {
				break
			}
		}
	}
	return hits
}

// CommentFile 是註解檔的檔名。原版可以在設定裡改,預設是 dir.doc。
var CommentFile = "dir.doc"

// searchComments 在註解檔裡找。格式是 `<檔名><空白><註解>`。
func searchComments(root, low string, o Options) ([]Hit, error) {
	var hits []Hit
	add := func(dir string) {
		data, err := os.ReadFile(filepath.Join(dir, CommentFile))
		if err != nil {
			return
		}
		text := textenc.Decode(data, textenc.Detect(data))
		for _, line := range strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n") {
			if line == "" {
				continue
			}
			i := strings.IndexAny(line, " \t")
			if i <= 0 {
				continue
			}
			name, note := line[:i], strings.TrimSpace(line[i:])
			if strings.Contains(strings.ToLower(note), low) {
				hits = append(hits, Hit{Dir: dir, Name: name, Text: note})
			}
		}
	}
	if !o.Recursive {
		add(root)
		return hits, nil
	}
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err == nil && d.IsDir() {
			add(p)
		}
		return nil
	})
	return hits, err
}

var _ = bytes.Contains
