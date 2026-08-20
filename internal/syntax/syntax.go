// Package syntax 讀 WinCV 的 keyword_*.cfg 語法上色設定。
//
// 檔案結構:
//
//	<設定鍵><空白><值>      ← 檔頭,一行一個
//	=========[任意說明]      ← 分隔線
//	<關鍵字><空白><顏色名>   ← 之後每行一個
//
// 顏色名是 WinCV 自己的 29 個具名顏色(見 internal/cell)。
// `keyword.cfg` 是總表:`<副檔名><空白><設定檔名>`,值為 `.` 表示沿用上一個。
package syntax

import (
	"bufio"
	"bytes"
	"os"
	"path/filepath"
	"strings"

	"github.com/wicanr2/wincv-remake/internal/cell"
	"github.com/wicanr2/wincv-remake/internal/textenc"
)

// Config 是一種語言的上色設定。
type Config struct {
	Name string

	QuoteColor  cell.Color
	NumberColor cell.Color
	HasQuote    bool
	HasNumber   bool

	LineComment      string
	OnlyBeginLine    bool // 行註解只有在行首才算
	KeywordDelimiter string

	// BlockStart / BlockEnd 是跨行註解的起訖標記。
	//
	// **設定檔裡沒有這一項**,但 image 的符號表有 COMMENTSTATE、
	// END-COMMENT$、"CHECK-END-COMMENT,表示原版有跨行註解的狀態機。
	// 既然沒有設定可讀,推測它是依語言寫死的;這裡在 LineComment 為
	// "//" 時預設用 C 家族的 /* */。**這一條還沒查證**(CLAUDE.md 假設 A12)。
	BlockStart, BlockEnd string

	// Keywords 是關鍵字 → 顏色。原版的比對不分大小寫的語言(如 Pascal)
	// 與分大小寫的(如 C)都用同一個檔,所以兩種鍵都放進來。
	Keywords map[string]cell.Color
	// Symbols 是單一符號 → 顏色(keyword_txt.cfg 那種全是符號的設定)。
	Symbols map[rune]cell.Color
}

// defaultDelimiter 是沒指定 KeywordDelimiter 時的斷詞字元。
const defaultDelimiter = " \t,;:?'({[]})+-*/%&~<>=!|.\"#^@\\`$"

// ParseConfig 讀一個 keyword_*.cfg。內容是 Big5。
func ParseConfig(name string, data []byte) *Config {
	c := &Config{
		Name:             name,
		Keywords:         map[string]cell.Color{},
		Symbols:          map[rune]cell.Color{},
		KeywordDelimiter: defaultDelimiter,
	}
	inHeader := true
	sc := bufio.NewScanner(bytes.NewReader(data))
	sc.Buffer(make([]byte, 0, 64*1024), 1<<20)
	for sc.Scan() {
		line := strings.TrimRight(sc.Text(), " \t\r")
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "====") {
			inHeader = false
			continue
		}
		if inHeader {
			c.applyHeader(line)
			continue
		}
		k, v, ok := splitKV(line)
		if !ok {
			continue
		}
		col, ok := cell.ByName(v)
		if !ok {
			continue
		}
		key := textenc.Decode([]byte(k), textenc.Big5)
		r := []rune(key)
		if len(r) == 1 && !isWordChar(r[0]) {
			c.Symbols[r[0]] = col
			continue
		}
		c.Keywords[key] = col
	}
	if c.LineComment == "//" && c.BlockStart == "" {
		c.BlockStart, c.BlockEnd = "/*", "*/"
	}
	return c
}

func (c *Config) applyHeader(line string) {
	k, v, ok := splitKV(line)
	if !ok {
		// LineCommentStart 可以是空值(keyword_txt.cfg 就是),
		// 那表示這個語言沒有行註解。
		if strings.EqualFold(strings.TrimSpace(line), "LineCommentStart") {
			c.LineComment = ""
		}
		return
	}
	switch strings.ToLower(k) {
	case "quotecolor":
		if col, ok := cell.ByName(v); ok {
			c.QuoteColor, c.HasQuote = col, true
		}
	case "numbercolor":
		if col, ok := cell.ByName(v); ok {
			c.NumberColor, c.HasNumber = col, true
		}
	case "linecommentstart":
		c.LineComment = v
	case "onlybeginlinecomment":
		c.OnlyBeginLine = strings.EqualFold(v, "true")
	case "keyworddelimiter":
		// 值用單引號或雙引號包住,裡面就是那些字元。
		c.KeywordDelimiter = strings.Trim(v, `'"`) + " \t"
	}
}

// splitKV 以第一段連續空白切成鍵與值。
func splitKV(line string) (string, string, bool) {
	i := strings.IndexAny(line, " \t")
	if i < 0 {
		return "", "", false
	}
	k := line[:i]
	v := strings.TrimSpace(line[i:])
	if v == "" {
		return "", "", false
	}
	return k, v, true
}

func isWordChar(r rune) bool {
	return r == '_' || r == '#' ||
		(r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9')
}

// Set 是一整組語法設定,含副檔名對照。
type Set struct {
	byExt map[string]*Config
}

// LoadSet 從一個目錄讀 keyword.cfg 與它指到的所有設定檔。
func LoadSet(dir string) (*Set, error) {
	index, err := os.ReadFile(filepath.Join(dir, "keyword.cfg"))
	if err != nil {
		return nil, err
	}
	s := &Set{byExt: map[string]*Config{}}
	cache := map[string]*Config{}

	var last *Config
	sc := bufio.NewScanner(bytes.NewReader(index))
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		ext, file, ok := splitKV(line)
		if !ok {
			continue
		}
		ext = strings.ToLower(ext)
		// "." 表示沿用上一行指到的設定檔 —— keyword.cfg 用這招讓
		// .c .h .rc 共用 keyword_c.cfg。
		if file == "." {
			if last != nil {
				s.byExt[ext] = last
			}
			continue
		}
		cfg, ok := cache[file]
		if !ok {
			data, err := os.ReadFile(filepath.Join(dir, file))
			if err != nil {
				continue
			}
			cfg = ParseConfig(file, data)
			cache[file] = cfg
		}
		s.byExt[ext] = cfg
		last = cfg
	}
	return s, nil
}

// For 依副檔名取設定。沒有對應的回 nil。
func (s *Set) For(filename string) *Config {
	if s == nil {
		return nil
	}
	ext := strings.ToLower(strings.TrimPrefix(filepath.Ext(filename), "."))
	return s.byExt[ext]
}

// Exts 回傳有設定的副檔名,供測試與狀態列顯示。
func (s *Set) Exts() []string {
	var out []string
	for e := range s.byExt {
		out = append(out, e)
	}
	return out
}

// Token 是一行裡上色相同的一段。
type Token struct {
	Start, End int // rune 索引
	Color      cell.Color
	Colored    bool // false 表示用預設前景色
}

// CommentColor 是註解的顏色。設定檔沒有這一項,用灰色。
const CommentColor = cell.Gray

// Highlight 把一行切成上色片段(不在跨行註解裡)。
func (c *Config) Highlight(line string) []Token {
	toks, _ := c.HighlightState(line, false)
	return toks
}

// HighlightState 把一行切成上色片段,並回傳這一行結束時是不是還在
// 跨行註解裡。呼叫端要把上一行的回傳值餵進來。
//
// 順序有意義:跨行註解 → 行註解 → 字串 → 數字 → 關鍵字 → 符號。
// 先判註解,否則註解裡的關鍵字會被上色;先判字串,否則字串裡的符號會被上色。
func (c *Config) HighlightState(line string, inComment bool) ([]Token, bool) {
	r := []rune(line)
	var out []Token
	if c == nil {
		return out, false
	}

	pos := 0
	for pos < len(r) {
		if inComment {
			e := indexFrom(r, c.BlockEnd, pos)
			if e < 0 {
				out = append(out, Token{Start: pos, End: len(r), Color: CommentColor, Colored: true})
				return out, true
			}
			end := e + len([]rune(c.BlockEnd))
			out = append(out, Token{Start: pos, End: end, Color: CommentColor, Colored: true})
			pos = end
			inComment = false
			continue
		}

		lineAt := -1
		if c.LineComment != "" {
			if i := commentAt(r[pos:], c.LineComment, c.OnlyBeginLine && pos == 0); i >= 0 {
				lineAt = pos + i
			}
		}
		blockAt := -1
		if c.BlockStart != "" {
			blockAt = indexFrom(r, c.BlockStart, pos)
		}

		switch {
		case lineAt >= 0 && (blockAt < 0 || lineAt < blockAt):
			if lineAt > pos {
				out = append(out, c.highlightRange(r, pos, lineAt)...)
			}
			out = append(out, Token{Start: lineAt, End: len(r), Color: CommentColor, Colored: true})
			return out, false
		case blockAt >= 0:
			if blockAt > pos {
				out = append(out, c.highlightRange(r, pos, blockAt)...)
			}
			pos = blockAt + len([]rune(c.BlockStart))
			out = append(out, Token{Start: blockAt, End: pos, Color: CommentColor, Colored: true})
			inComment = true
		default:
			out = append(out, c.highlightRange(r, pos, len(r))...)
			return out, false
		}
	}
	return out, inComment
}

// indexFrom 從 from 開始找 sub 在 r 裡的 rune 位置。
func indexFrom(r []rune, sub string, from int) int {
	m := []rune(sub)
	if len(m) == 0 {
		return -1
	}
	for i := from; i+len(m) <= len(r); i++ {
		if string(r[i:i+len(m)]) == sub {
			return i
		}
	}
	return -1
}

func commentAt(r []rune, marker string, onlyBegin bool) int {
	m := []rune(marker)
	if len(m) == 0 || len(m) > len(r) {
		return -1
	}
	limit := len(r) - len(m)
	if onlyBegin {
		limit = 0
		// 行首可以有空白
		for limit < len(r) && (r[limit] == ' ' || r[limit] == '\t') {
			limit++
		}
		if limit+len(m) > len(r) {
			return -1
		}
		if string(r[limit:limit+len(m)]) == marker {
			return limit
		}
		return -1
	}
	for i := 0; i <= limit; i++ {
		if string(r[i:i+len(m)]) == marker {
			return i
		}
	}
	return -1
}

func (c *Config) highlightRange(r []rune, from, to int) []Token {
	var out []Token
	i := from
	for i < to {
		ch := r[i]

		// 字串
		if c.HasQuote && (ch == '"' || ch == '\'') {
			j := i + 1
			for j < to && r[j] != ch {
				if r[j] == '\\' && j+1 < to {
					j++
				}
				j++
			}
			if j < to {
				j++
			}
			out = append(out, Token{Start: i, End: j, Color: c.QuoteColor, Colored: true})
			i = j
			continue
		}

		// 數字
		if c.HasNumber && ch >= '0' && ch <= '9' && (i == from || !isWordChar(r[i-1])) {
			j := i
			for j < to && (isHexDigit(r[j]) || r[j] == '.' || r[j] == 'x' || r[j] == 'X') {
				j++
			}
			out = append(out, Token{Start: i, End: j, Color: c.NumberColor, Colored: true})
			i = j
			continue
		}

		// 關鍵字
		if isWordChar(ch) {
			j := i
			for j < to && isWordChar(r[j]) {
				j++
			}
			w := string(r[i:j])
			if col, ok := c.lookupKeyword(w); ok {
				out = append(out, Token{Start: i, End: j, Color: col, Colored: true})
			} else {
				out = append(out, Token{Start: i, End: j})
			}
			i = j
			continue
		}

		// 單一符號
		if col, ok := c.Symbols[ch]; ok {
			out = append(out, Token{Start: i, End: i + 1, Color: col, Colored: true})
			i++
			continue
		}

		j := i
		for j < to && !isWordChar(r[j]) && r[j] != '"' && r[j] != '\'' {
			if _, ok := c.Symbols[r[j]]; ok {
				break
			}
			j++
		}
		if j == i {
			j++
		}
		out = append(out, Token{Start: i, End: j})
		i = j
	}
	return out
}

func (c *Config) lookupKeyword(w string) (cell.Color, bool) {
	if col, ok := c.Keywords[w]; ok {
		return col, true
	}
	// 不分大小寫的語言(Pascal、BASIC…)在設定檔裡通常只寫一種寫法。
	if col, ok := c.Keywords[strings.ToLower(w)]; ok {
		return col, true
	}
	return 0, false
}

func isHexDigit(r rune) bool {
	return (r >= '0' && r <= '9') || (r >= 'a' && r <= 'f') || (r >= 'A' && r <= 'F')
}
