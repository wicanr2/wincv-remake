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
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

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
				return fmt.Errorf("重導向超過 %d 次", maxRedirects)
			}
			return nil
		},
	}
}

// Fetch 取一個網頁。
func (c *Client) Fetch(ctx context.Context, raw string) (*Doc, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return nil, fmt.Errorf("位址解不開: %w", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return nil, fmt.Errorf("只支援 http 與 https,不是 %s", u.Scheme)
	}
	if u.Host == "" {
		return nil, errors.New("沒有主機名")
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
		return nil, fmt.Errorf("不是 http 位址:%s", raw)
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
		return nil, "", u, fmt.Errorf("連不上 %s: %w", u.Host, err)
	}
	defer resp.Body.Close()

	final := resp.Request.URL
	if final == nil {
		final = u
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, "", final, fmt.Errorf("伺服器回 %s", resp.Status)
	}

	// 多讀一個位元組,才分得出「剛好等於上限」與「超過上限」。
	b, err := io.ReadAll(io.LimitReader(resp.Body, max+1))
	if err != nil {
		return nil, "", final, fmt.Errorf("讀取失敗: %w", err)
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
// 內容判讀。前兩者是網站自己宣告的,但**宣告會錯**,所以宣告的編碼
// 解出來全是替代字元時,還是要退回內容判讀。
func decode(b []byte, ctype string) string {
	if e := charsetOf(ctype); e != textenc.Unknown {
		if s := textenc.Decode(b, e); !mojibake(s) {
			return s
		}
	}
	head := b
	if len(head) > 4096 {
		head = head[:4096]
	}
	if e := charsetOf(string(head)); e != textenc.Unknown {
		if s := textenc.Decode(b, e); !mojibake(s) {
			return s
		}
	}
	return textenc.Decode(b, textenc.Detect(b))
}

// charsetOf 從一段文字裡找 charset= 的宣告。
func charsetOf(s string) textenc.Enc {
	m := charsetRe.FindStringSubmatch(s)
	if m == nil {
		return textenc.Unknown
	}
	switch strings.ToLower(m[1]) {
	case "utf-8", "utf8":
		return textenc.UTF8
	case "big5", "big-5", "big5-hkscs", "cp950", "ms950":
		return textenc.Big5
	case "gbk", "gb2312", "gb18030", "cp936":
		return textenc.GBK
	case "shift_jis", "shift-jis", "sjis", "cp932", "ms932", "windows-31j":
		return textenc.ShiftJIS
	case "euc-kr", "euckr", "cp949":
		return textenc.EUCKR
	case "us-ascii", "ascii", "iso-8859-1", "latin1", "windows-1252":
		return textenc.ASCII
	}
	return textenc.Unknown
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
