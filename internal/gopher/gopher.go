// Package gopher 是 Gopher(RFC 1436)的客戶端。
//
// 為什麼是 gopher 而不是 HTTP:這支軟體要的是「文字與圖片,簡單就好」的瀏覽。
// gopher 的協定本身就是那個形狀 —— 一行一個項目、型別寫在第一個位元組、
// 沒有樣式表也沒有腳本,選單結構幾乎就是目錄列表。HTTP 的難處全在 HTML,
// 而 HTML 要的東西(排版、字體、浮動、JavaScript)這個畫面一樣都給不了。
//
// 這一包只做協定與文件模型,不碰畫面。排版借用 internal/markdown 的引擎:
// 選單變成一行一個 Para、文字檔變成 Pre(不重新斷行,gopher 的內容常常是
// 排好版的 ASCII art)、連結掛在 Span.Href 上。
package gopher

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"strings"
	"time"
)

// DefaultPort 是 gopher 的預設埠。
const DefaultPort = "70"

// 項目型別。第一個位元組,定義在 RFC 1436,另有幾個約定俗成的擴充。
const (
	TypeText   = '0' // 文字檔
	TypeMenu   = '1' // 選單(目錄)
	TypeError  = '3' // 錯誤
	TypeSearch = '7' // 查詢介面
	TypeBinary = '9' // 二進位
	TypeGIF    = 'g' // GIF
	TypeImage  = 'I' // 圖片(格式由內容決定)
	TypePNG    = 'p' // PNG(約定俗成)
	TypeHTML   = 'h' // HTML;selector 常是 "URL:https://..."
	TypeInfo   = 'i' // 純資訊,不是連結(約定俗成,但幾乎每個站都在用)
	TypeSound  = 's' // 音訊
	TypeDoc    = 'd' // 文件檔(約定俗成)
)

// URL 是一個 gopher 位址。
//
// 形式是 gopher://host[:port]/<型別><selector>。型別擠在路徑的第一個字元
// 是 gopher 特有的 —— 它讓客戶端在連線之前就知道對方會回什麼,
// 不必等 Content-Type。
type URL struct {
	Host string
	Port string // 空的表示 DefaultPort
	Type byte   // 0 表示未指定,當成 TypeMenu
	Sel  string // selector,原樣送出(不做 URL 解碼以外的加工)
	// Search 是型別 7 的查詢字串。送出時接在 selector 後面,中間一個 tab。
	Search string
}

// ParseURL 解析 gopher 位址。scheme 可以省略。
//
// 幾種都要吃:
//
//	gopher://host           → 選單,selector 空
//	gopher://host:7070/     → 同上,換埠
//	gopher://host/1/foo     → 型別 1,selector "/foo"
//	gopher://host/0/foo.txt → 型別 0
//	host/1/foo              → 沒有 scheme 也可以
//	gopher://host/7/find?貓  → 型別 7,查詢字串「貓」
func ParseURL(s string) (URL, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return URL{}, errors.New("位址是空的")
	}
	if i := strings.Index(s, "://"); i >= 0 {
		if !strings.EqualFold(s[:i], "gopher") {
			return URL{}, fmt.Errorf("只支援 gopher://,不是 %s://", s[:i])
		}
		s = s[i+3:]
	}

	var u URL
	// 查詢字串可以用 ? 或 tab 分隔。tab 是協定本身的寫法,
	// ? 是瀏覽器網址列的習慣 —— 兩種都收。
	if i := strings.IndexAny(s, "?\t"); i >= 0 {
		u.Search, s = s[i+1:], s[:i]
	}

	host, path, _ := strings.Cut(s, "/")
	if host == "" {
		return URL{}, errors.New("沒有主機名")
	}
	// IPv6 的字面位址寫成 [::1]:70,冒號不能當埠的分隔。
	if strings.HasPrefix(host, "[") {
		if j := strings.Index(host, "]"); j >= 0 {
			u.Host = host[:j+1]
			if rest := host[j+1:]; strings.HasPrefix(rest, ":") {
				u.Port = rest[1:]
			}
		} else {
			return URL{}, errors.New("IPv6 位址少了 ]")
		}
	} else if h, p, ok := strings.Cut(host, ":"); ok {
		u.Host, u.Port = h, p
	} else {
		u.Host = host
	}
	if u.Host == "" {
		return URL{}, errors.New("沒有主機名")
	}

	if path != "" {
		u.Type = path[0]
		u.Sel = path[1:]
	}
	return u, nil
}

// String 把位址還原成 gopher:// 形式。
func (u URL) String() string {
	b := &strings.Builder{}
	b.WriteString("gopher://")
	b.WriteString(u.Host)
	if u.Port != "" && u.Port != DefaultPort {
		b.WriteString(":")
		b.WriteString(u.Port)
	}
	b.WriteString("/")
	b.WriteByte(u.kind())
	b.WriteString(u.Sel)
	if u.Search != "" {
		b.WriteString("?")
		b.WriteString(u.Search)
	}
	return b.String()
}

// kind 回傳實際要用的型別。未指定時是選單 —— 那是 gopher 的預設進入點。
func (u URL) kind() byte {
	if u.Type == 0 {
		return TypeMenu
	}
	return u.Type
}

func (u URL) addr() string {
	p := u.Port
	if p == "" {
		p = DefaultPort
	}
	return net.JoinHostPort(u.Host, p)
}

// Item 是選單裡的一列。
type Item struct {
	Type    byte
	Display string
	Sel     string
	Host    string
	Port    string
}

// IsLink 說明這一列點得下去。
//
// 資訊列(型別 i)只是文字,沒有目標;錯誤列(型別 3)同理。
// 有些站台會用 host 為空或 "error.host" 的假項目來排版,那些也不算連結。
func (it Item) IsLink() bool {
	switch it.Type {
	case TypeInfo, TypeError, 0:
		return false
	}
	return it.Host != "" && !strings.EqualFold(it.Host, "error.host")
}

// WebURL 回傳這一列指向的 http(s) 位址。
//
// gopher 用型別 h 加上 "URL:https://..." 這個 selector 來連到 web。
// 這個客戶端不解 HTTP,所以只把位址交出去,由上層決定怎麼辦
// (交給系統瀏覽器,或只是顯示出來)。
func (it Item) WebURL() (string, bool) {
	if it.Type != TypeHTML {
		return "", false
	}
	if s, ok := cutPrefixFold(it.Sel, "URL:"); ok {
		return s, true
	}
	return "", false
}

// URL 回傳這一列的 gopher 位址。
func (it Item) URL() URL {
	return URL{Host: it.Host, Port: it.Port, Type: it.Type, Sel: it.Sel}
}

// ParseMenu 解析選單。
//
// 每一列是 <型別><顯示文字> TAB <selector> TAB <主機> TAB <埠>,
// 以只有一個 "." 的那一列結束。
//
// [雷] 欄位不足的列不能直接丟掉。實務上很多站台的資訊列只寫
// "i說明文字" 就換行,連 tab 都沒有 —— 丟掉的話整段說明會消失,
// 而畫面上看起來只是「這個站沒寫什麼」。
func ParseMenu(b []byte) []Item {
	var out []Item
	for _, ln := range strings.Split(string(b), "\n") {
		ln = strings.TrimSuffix(ln, "\r")
		if ln == "." {
			break
		}
		if ln == "" {
			out = append(out, Item{Type: TypeInfo})
			continue
		}
		f := strings.Split(ln[1:], "\t")
		it := Item{Type: ln[0], Display: f[0]}
		if len(f) > 1 {
			it.Sel = f[1]
		}
		if len(f) > 2 {
			it.Host = f[2]
		}
		if len(f) > 3 {
			it.Port = f[3]
		}
		out = append(out, it)
	}
	return out
}

// Client 取回 gopher 資源。
//
// 兩個上限都是必要的,不是保險:gopher 沒有 Content-Length,
// 伺服器可以一直送下去,而這支程式是檔案管理器不是下載器。
type Client struct {
	// Timeout 是連線與讀取的期限。零值用 DefaultTimeout。
	Timeout time.Duration
	// MaxBytes 是單次取回的上限。零值用 DefaultMaxBytes。
	MaxBytes int64
	// Dial 可以換掉,測試用。零值用 net.Dialer。
	Dial func(ctx context.Context, network, addr string) (net.Conn, error)
}

const (
	DefaultTimeout  = 20 * time.Second
	DefaultMaxBytes = 8 << 20
)

// ErrTooLarge 表示對方送的東西超過上限,回傳的內容是被截斷的。
var ErrTooLarge = errors.New("內容超過上限,已截斷")

// Fetch 取回一個資源。
//
// 回傳的位元組是原樣的 —— 文字的編碼判讀交給 internal/textenc,
// 圖片交給 internal/imgfmt。這一層不猜內容是什麼。
func (c *Client) Fetch(ctx context.Context, u URL) ([]byte, error) {
	if u.Host == "" {
		return nil, errors.New("沒有主機名")
	}
	to := c.Timeout
	if to <= 0 {
		to = DefaultTimeout
	}
	max := c.MaxBytes
	if max <= 0 {
		max = DefaultMaxBytes
	}

	ctx, cancel := context.WithTimeout(ctx, to)
	defer cancel()

	dial := c.Dial
	if dial == nil {
		dial = (&net.Dialer{}).DialContext
	}
	conn, err := dial(ctx, "tcp", u.addr())
	if err != nil {
		return nil, fmt.Errorf("連不上 %s: %w", u.addr(), err)
	}
	defer conn.Close()
	if dl, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(dl)
	}

	// 請求就是 selector 加 CRLF。型別 7 的查詢字串接在後面,中間一個 tab。
	req := u.Sel
	if u.kind() == TypeSearch && u.Search != "" {
		req += "\t" + u.Search
	}
	if _, err := io.WriteString(conn, req+"\r\n"); err != nil {
		return nil, fmt.Errorf("送出 selector 失敗: %w", err)
	}

	// 多讀一個位元組,才分得出「剛好等於上限」與「超過上限」。
	data, err := io.ReadAll(io.LimitReader(conn, max+1))
	if err != nil {
		return nil, fmt.Errorf("讀取失敗: %w", err)
	}
	if int64(len(data)) > max {
		return data[:max], ErrTooLarge
	}
	return data, nil
}

func cutPrefixFold(s, prefix string) (string, bool) {
	if len(s) >= len(prefix) && strings.EqualFold(s[:len(prefix)], prefix) {
		return s[len(prefix):], true
	}
	return "", false
}
