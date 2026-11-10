// Package web 用 gopher 的方式看網頁:只留文字、連結與圖片。
//
// 為什麼是「gopher 的方式」而不是一個瀏覽器:這個畫面是字元格點,
// 畫不出 HTML 要的排版、字體、浮動與腳本。與其做一個什麼都做不好的
// 瀏覽器,不如明確地只做一件事——把網頁壓成「一段一段的文字、
// 一個一個的連結、幾張圖」,那正好是 gopher 選單的形狀,
// 而這個程式已經有一整套畫那個形狀的東西了。
//
// 這一包只做「取回」與「壓成區塊」,不碰畫面,也不決定要不要載圖。
package web

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"github.com/wicanr2/wincv-remake/internal/i18n"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/wicanr2/wincv-remake/internal/markdown"
	"github.com/wicanr2/wincv-remake/internal/textenc"
)

const (
	DefaultTimeout = 20 * time.Second
	// DefaultMaxBytes 是一頁 HTML 的上限。
	DefaultMaxBytes = 4 << 20
	// DefaultMaxImageBytes 是單張圖的上限。比 HTML 寬,但仍然有限 ——
	// 這是檔案管理器裡的一個檢視模式,不是下載器。
	DefaultMaxImageBytes = 8 << 20
	// maxRedirects 是重導向的層數上限。
	maxRedirects = 5
)

// ErrTooLarge 表示對方送的東西超過上限,回傳的內容是被截斷的。
var ErrTooLarge = errors.New("內容超過上限,已截斷")

// Doc 是取回來的一頁。
type Doc struct {
	// URL 是**最終**位址(重導向之後)。後面的相對連結要以它為基準。
	URL   string
	Title string
	// Blocks 是排版用的區塊。位址指向圖片時是空的。
	Blocks []markdown.Block
	// Image 不為空表示這個位址本身就是一張圖,交給看圖模式處理。
	Image []byte
	// Truncated 表示內容被上限截斷了。不是失敗,但要說出來。
	Truncated bool
}

// Client 取回網頁。
//
// 兩個上限都是必要的,不是保險:HTTP 的 Content-Length 可以不寫也可以說謊,
// 而這個程式是檔案管理器,不該因為點到一個大檔就把記憶體吃光。
type Client struct {
	// Timeout 是整次取回的期限。零值用 DefaultTimeout。
	Timeout time.Duration
	// MaxBytes 是一頁的上限。零值用 DefaultMaxBytes。
	MaxBytes int64
	// MaxImageBytes 是單張圖的上限。零值用 DefaultMaxImageBytes。
	MaxImageBytes int64
	// UserAgent 空的話用 DefaultUserAgent。
	UserAgent string
	// HTTP 可以換掉,測試用。零值自己建一個。
	HTTP *http.Client
}

// DefaultUserAgent 照實說自己是什麼。
//
// 不假裝成別的瀏覽器:伺服器依 UA 送不同版面時,誠實的 UA 拿到的
// 通常是比較簡單的那一份,而簡單的那一份正是這個畫面畫得出來的。
const DefaultUserAgent = "WinCV-Remake/0.52 (text-mode; +https://github.com/wicanr2/wincv-remake)"

// IsHTTP 說明這個位址是不是這一包處理的範圍。
func IsHTTP(raw string) bool {
	s := strings.ToLower(strings.TrimSpace(raw))
	return strings.HasPrefix(s, "http://") || strings.HasPrefix(s, "https://")
}

func (c *Client) client() *http.Client {
	if c.HTTP != nil {
		return c.HTTP
	}
	to := c.Timeout
	if to <= 0 {
		to = DefaultTimeout
	}
	return &http.Client{
		Timeout: to,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= maxRedirects {
				return fmt.Errorf(i18n.T("重導向超過 %d 次"), maxRedirects)
			}
			return nil
		},
	}
}

// Fetch 取一個網頁。
func (c *Client) Fetch(ctx context.Context, raw string) (*Doc, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return nil, fmt.Errorf(i18n.T("位址解不開: %w"), err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return nil, fmt.Errorf(i18n.T("只支援 http 與 https,不是 %s"), u.Scheme)
	}
	if u.Host == "" {
		return nil, errors.New(i18n.T("沒有主機名"))
	}

	max := c.MaxBytes
	if max <= 0 {
		max = DefaultMaxBytes
	}
	body, ctype, final, err := c.get(ctx, u, max)
	// [雷] 截斷要在「是不是錯誤」之前判斷。先 return 再問 err 是什麼,
	// truncated 就永遠是 false —— 大頁面會被靜靜切掉一半而沒有任何提示,
	// 而小頁面測不出來。
	truncated := errors.Is(err, ErrTooLarge)
	if err != nil && !truncated {
		return nil, err
	}

	d := &Doc{URL: final.String(), Truncated: truncated}

	switch {
	case strings.HasPrefix(ctype, "image/"):
		d.Image = body
		d.Title = path(final)
		return d, nil
	case isHTML(ctype, body):
		text := decode(body, ctype)
		d.Title, d.Blocks = ParseHTML(final, strings.NewReader(text))
		if d.Title == "" {
			d.Title = final.Host
		}
		d.Title = strings.TrimSpace(d.Title)
		return d, nil
	default:
		// 純文字與認不得的型別都當文字看:認不得時倒出可讀的部分,
		// 比一句「不支援」有用 —— 很多站台的 Content-Type 是錯的。
		d.Title = path(final)
		d.Blocks = TextBlocks(body)
		return d, nil
	}
}

// FetchImage 取一張圖的位元組。給排版引擎的圖片載入器用。
func (c *Client) FetchImage(ctx context.Context, raw string) ([]byte, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return nil, err
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return nil, fmt.Errorf(i18n.T("不是 http 位址:%s"), raw)
	}
	max := c.MaxImageBytes
	if max <= 0 {
		max = DefaultMaxImageBytes
	}
	b, _, _, err := c.get(ctx, u, max)
	if err != nil && !errors.Is(err, ErrTooLarge) {
		return nil, err
	}
	return b, nil
}

// get 做一次 GET,回傳內容、Content-Type 與最終位址。
//
// 超過上限時仍然回傳讀到的部分,錯誤是 ErrTooLarge —— 截斷的 HTML
// 通常還是看得懂前半,直接丟掉對使用者沒有好處。
func (c *Client) get(ctx context.Context, u *url.URL, max int64) ([]byte, string, *url.URL, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, "", u, err
	}
	ua := c.UserAgent
	if ua == "" {
		ua = DefaultUserAgent
	}
	req.Header.Set("User-Agent", ua)
	req.Header.Set("Accept", "text/html,text/plain,image/*;q=0.8,*/*;q=0.5")
	// 不要壓縮過的內容:Go 會自己處理 gzip,但只在我們沒有自己設
	// Accept-Encoding 的時候。設了就要自己解,那是白做的工。

	resp, err := c.client().Do(req)
	if err != nil {
		return nil, "", u, fmt.Errorf(i18n.T("連不上 %s: %w"), u.Host, err)
	}
	defer resp.Body.Close()

	final := resp.Request.URL
	if final == nil {
		final = u
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, "", final, fmt.Errorf(i18n.T("伺服器回 %s"), resp.Status)
	}

	// 多讀一個位元組,才分得出「剛好等於上限」與「超過上限」。
	b, err := io.ReadAll(io.LimitReader(resp.Body, max+1))
	if err != nil {
		return nil, "", final, fmt.Errorf(i18n.T("讀取失敗: %w"), err)
	}
	ctype := strings.ToLower(strings.TrimSpace(resp.Header.Get("Content-Type")))
	if int64(len(b)) > max {
		return b[:max], ctype, final, ErrTooLarge
	}
	return b, ctype, final, nil
}

// TextBlocks 把純文字排成區塊。
//
// 與 gopher 那邊同一個判斷:網路上的文字多半是排好版的,重新斷行
// 會把表格與 ASCII art 弄壞。
func TextBlocks(b []byte) []markdown.Block {
	s := decode(b, "")
	s = strings.ReplaceAll(s, "\r\n", "\n")
	lines := strings.Split(s, "\n")
	for len(lines) > 0 && strings.TrimSpace(lines[len(lines)-1]) == "" {
		lines = lines[:len(lines)-1]
	}
	if len(lines) == 0 {
		return nil
	}
	return []markdown.Block{{Kind: markdown.Pre, Lines: lines}}
}

// isHTML 判斷內容是不是 HTML。
//
// 不能只信 Content-Type:很多站台送 text/plain 卻給 HTML,
// 也有送 application/octet-stream 的。型別沒說清楚時看內容開頭。
func isHTML(ctype string, body []byte) bool {
	if strings.Contains(ctype, "html") || strings.Contains(ctype, "xhtml") {
		return true
	}
	if strings.Contains(ctype, "text/plain") {
		return false
	}
	head := body
	if len(head) > 1024 {
		head = head[:1024]
	}
	s := strings.ToLower(string(head))
	return strings.Contains(s, "<!doctype html") || strings.Contains(s, "<html")
}

var charsetRe = regexp.MustCompile(`(?i)charset\s*=\s*"?([a-z0-9_\-]+)`)

// decode 把位元組變成字串。
//
// 優先順序:Content-Type 的 charset → HTML 裡的 <meta charset> →
// 內容判讀。前兩者是網站自己宣告的,但**宣告會錯**,所以宣告看起來
// 是錯的時候(見 declared),還是要退回內容判讀。
func decode(b []byte, ctype string) string {
	if s, ok := declared(b, charsetOf(ctype)); ok {
		return s
	}
	head := b
	if len(head) > 4096 {
		head = head[:4096]
	}
	if s, ok := declared(b, charsetOf(string(head))); ok {
		return s
	}
	return textenc.Decode(b, textenc.Detect(b))
}

// DecodeCharset 用一個編碼名稱(例如 XML 宣告裡的 encoding)解碼。
// 名稱認不得,或解出來看得出宣告是錯的,就改用內容判讀。
func DecodeCharset(b []byte, name string) string {
	if s, ok := declared(b, label(strings.ToLower(name))); ok {
		return s
	}
	return textenc.Decode(b, textenc.Detect(b))
}

// DecodeMarkup 把一份沒有 Content-Type 可依的 HTML 或 XHTML(EPUB 的章節)
// 變成字串。
//
// 優先順序:BOM → XML 宣告的 encoding → <meta charset> → 內容判讀。
// BOM 最確定,有就照它;XML 宣告與 meta 是作者寫的,一樣會錯,
// 錯的時候往下一個依據退。
func DecodeMarkup(b []byte) string {
	switch {
	case bytes.HasPrefix(b, []byte{0xEF, 0xBB, 0xBF}):
		return textenc.Decode(b, textenc.UTF8)
	case bytes.HasPrefix(b, []byte{0xFF, 0xFE}):
		return textenc.Decode(b, textenc.UTF16LE)
	case bytes.HasPrefix(b, []byte{0xFE, 0xFF}):
		return textenc.Decode(b, textenc.UTF16BE)
	}
	if name := xmlEncoding(b); name != "" {
		if s, ok := declared(b, label(strings.ToLower(name))); ok {
			return s
		}
	}
	return decode(b, "")
}

// xmlEncoding 取開頭那個 XML 宣告(<?xml ... ?>)裡的 encoding。沒有就回空字串。
func xmlEncoding(b []byte) string {
	b = bytes.TrimLeft(b, " \t\r\n")
	if !bytes.HasPrefix(b, []byte("<?xml")) || len(b) < 6 || !isXMLSpace(b[5]) {
		return ""
	}
	end := bytes.Index(b, []byte("?>"))
	if end < 0 {
		return ""
	}
	d := b[5:end]
	i := bytes.Index(d, []byte("encoding"))
	if i < 0 {
		return ""
	}
	d = bytes.TrimLeft(d[i+len("encoding"):], " \t\r\n")
	if len(d) == 0 || d[0] != '=' {
		return ""
	}
	d = bytes.TrimLeft(d[1:], " \t\r\n")
	if len(d) == 0 || (d[0] != '"' && d[0] != '\'') {
		return ""
	}
	j := bytes.IndexByte(d[1:], d[0])
	if j < 0 {
		return ""
	}
	return string(d[1 : 1+j])
}

func isXMLSpace(c byte) bool {
	return c == ' ' || c == '\t' || c == '\r' || c == '\n'
}

// charset 是一個編碼宣告對到的解碼方式。
//
// windows-1252 不在 textenc 裡:內容判讀不會猜它(西歐文字的位元組
// 落在 Big5 的範圍裡,判讀分不出來),它只會從宣告來,所以在這裡另外標。
type charset struct {
	enc    textenc.Enc
	cp1252 bool
}

// charsetOf 從一段文字裡找 charset= 的宣告。
func charsetOf(s string) charset {
	m := charsetRe.FindStringSubmatch(s)
	if m == nil {
		return charset{}
	}
	return label(strings.ToLower(m[1]))
}

// label 把(小寫的)編碼名稱對到解碼方式。認不得的回零值。
func label(name string) charset {
	switch name {
	case "utf-8", "utf8":
		return charset{enc: textenc.UTF8}
	case "big5", "big-5", "big5-hkscs", "cp950", "ms950":
		return charset{enc: textenc.Big5}
	case "gbk", "gb2312", "gb18030", "cp936":
		return charset{enc: textenc.GBK}
	case "shift_jis", "shift-jis", "sjis", "cp932", "ms932", "windows-31j":
		return charset{enc: textenc.ShiftJIS}
	case "euc-kr", "euckr", "cp949":
		return charset{enc: textenc.EUCKR}
	case "us-ascii", "ascii":
		return charset{enc: textenc.ASCII}
	case "iso-8859-1", "iso8859-1", "iso_8859-1", "latin1", "l1", "cp819", "ibm819",
		"windows-1252", "cp1252", "x-cp1252":
		// WHATWG 的 Encoding 規格把 ISO-8859-1 這一族都對到 windows-1252:
		// 自稱 Latin-1 的頁面實際上幾乎都用到 0x80–0x9F 的彎引號與破折號。
		return charset{cp1252: true}
	}
	return charset{}
}

// declared 照宣告的編碼解碼。宣告看起來是錯的就回 false,由呼叫端退回下一個依據。
//
// 「看起來是錯的」有兩種:
//   - 宣告了 UTF-8 以外的編碼,內容卻是含非 ASCII 字元的合法 UTF-8。
//     真正的 Big5 或 windows-1252 文字幾乎不可能整份剛好是合法的 UTF-8;
//     反過來,伺服器預設送 ISO-8859-1、內容其實是 UTF-8 的頁面很常見。
//   - 解出來滿是替代字元(見 mojibake)。宣告 UTF-8 或 ASCII 時沒有解碼器,
//     字串裡留著原始位元組,所以先把每個不合法的位元組算成一個替代字元再數。
func declared(b []byte, cs charset) (string, bool) {
	if cs == (charset{}) {
		return "", false
	}
	if cs.enc != textenc.UTF8 && cs.enc != textenc.ASCII && utf8.Valid(b) && hasMultibyte(b) {
		return textenc.Decode(b, textenc.UTF8), true
	}
	if cs.cp1252 {
		return decodeCP1252(b), true
	}
	s := textenc.Decode(b, cs.enc)
	if cs.enc == textenc.UTF8 || cs.enc == textenc.ASCII {
		return s, !mojibake(string([]rune(s)))
	}
	return s, !mojibake(s)
}

func hasMultibyte(b []byte) bool {
	for _, c := range b {
		if c >= 0x80 {
			return true
		}
	}
	return false
}

// cp1252High 是 windows-1252 的 0x80–0x9F。其餘位元組與 Unicode 的
// U+0000–U+00FF 相同。
//
// 表照 WHATWG 的 index-windows-1252:沒有定義的 0x81、0x8D、0x8F、0x90、0x9D
// 對到同值的 C1 控制字元。x/text 的 charmap 把這五個換成 U+FFFD,
// encoding_rs 照 WHATWG,所以兩邊都用這張表,不用各自的函式庫。
var cp1252High = [32]rune{
	0x20AC, 0x0081, 0x201A, 0x0192, 0x201E, 0x2026, 0x2020, 0x2021,
	0x02C6, 0x2030, 0x0160, 0x2039, 0x0152, 0x008D, 0x017D, 0x008F,
	0x0090, 0x2018, 0x2019, 0x201C, 0x201D, 0x2022, 0x2013, 0x2014,
	0x02DC, 0x2122, 0x0161, 0x203A, 0x0153, 0x009D, 0x017E, 0x0178,
}

func decodeCP1252(b []byte) string {
	var sb strings.Builder
	sb.Grow(len(b))
	for _, c := range b {
		switch {
		case c < 0x80:
			sb.WriteByte(c)
		case c < 0xA0:
			sb.WriteRune(cp1252High[c-0x80])
		default:
			sb.WriteRune(rune(c))
		}
	}
	return sb.String()
}

// mojibake 說明一段解出來的字是不是壞的。
//
// 判準是替代字元(U+FFFD)的比例:宣告錯編碼的頁面解出來會滿是這個,
// 而正常的頁面幾乎不會有。用比例而不是「有沒有」——一個壞位元組
// 不該讓整份判讀翻盤。
func mojibake(s string) bool {
	if s == "" {
		return false
	}
	n := strings.Count(s, "�")
	return n > 8 && n*20 > len([]rune(s))
}

// path 從位址取一個像檔名的東西,給標題用。
func path(u *url.URL) string {
	s := u.Path
	if i := strings.LastIndex(s, "/"); i >= 0 {
		s = s[i+1:]
	}
	if s == "" {
		return u.Host
	}
	return s
}
