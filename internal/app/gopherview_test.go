package app

import (
	"net"
	"strings"
	"testing"
	"time"

	"github.com/wicanr2/wincv-remake/internal/cell"
	"github.com/wicanr2/wincv-remake/internal/gopher"
	"github.com/wicanr2/wincv-remake/internal/keys"
	"github.com/wicanr2/wincv-remake/internal/vfs"
)

// gopherServer 起一個很小的 gopher 伺服器,依 selector 回不同的東西。
// 回傳位址。
func gopherServer(t *testing.T, routes map[string]string) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go func() {
				defer c.Close()
				buf := make([]byte, 1024)
				n, _ := c.Read(buf)
				sel := strings.TrimRight(string(buf[:n]), "\r\n")
				body, ok := routes[sel]
				if !ok {
					body = "3找不到\terror\terror.host\t1\r\n.\r\n"
				}
				_, _ = c.Write([]byte(body))
			}()
		}
	}()
	return ln.Addr().String()
}

// settle 一直畫到網路取回結束為止,回傳畫了幾幀。
//
// Draw 每一幀收一次結果 —— 這正是外殼的行為,測試照著做才驗得到
// 真實的路徑(而不是直接呼叫內部函式)。
func settle(t *testing.T, a *App, s *cell.Screen) {
	t.Helper()
	for i := 0; i < 200; i++ {
		a.Draw(s)
		if !a.Busy() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("等太久,取回沒有結束")
}

// screenText 把畫面轉成字串,一列一行。
func screenText(s *cell.Screen) string {
	var b strings.Builder
	for y := 0; y < s.Rows; y++ {
		for x := 0; x < s.Cols; x++ {
			c := s.At(x, y)
			if c == nil || c.Cont {
				continue
			}
			if c.Ch == 0 {
				b.WriteByte(' ')
				continue
			}
			b.WriteRune(c.Ch)
		}
		b.WriteByte('\n')
	}
	return b.String()
}

func newGopherApp(t *testing.T, addr string) (*App, *cell.Screen) {
	t.Helper()
	a := New(vfs.OS{}, t.TempDir())
	a.CellW, a.CellH = 8, 16
	a.Gopher = &gopher.Client{Timeout: 5 * time.Second}
	return a, cell.New(60, 20)
}

func TestGopherOpensMenu(t *testing.T) {
	routes := map[string]string{}
	addr := gopherServer(t, routes)
	host, port, _ := net.SplitHostPort(addr)
	menu := "i歡迎光臨\t\terror.host\t1\r\n" +
		"1文件目錄\t/docs\t" + host + "\t" + port + "\r\n" +
		"0讀我檔案\t/readme\t" + host + "\t" + port + "\r\n.\r\n"
	routes[""], routes["/"] = menu, menu

	a, s := newGopherApp(t, addr)
	a.OpenGopher("gopher://" + addr)
	settle(t, a, s)

	txt := screenText(s)
	for _, want := range []string{"歡迎光臨", "文件目錄", "讀我檔案", "[目錄]", "[文字]"} {
		if !strings.Contains(txt, want) {
			t.Fatalf("畫面上沒有 %q\n%s", want, txt)
		}
	}
	// 資訊列不該有型別標籤 —— 它不是可以點的東西。
	if strings.Contains(txt, "[?] 歡迎光臨") {
		t.Error("資訊列被標成連結了")
	}
}

func TestGopherFollowsLink(t *testing.T) {
	routes := map[string]string{}
	addr := gopherServer(t, routes)
	host, port, _ := net.SplitHostPort(addr)
	menu := "i歡迎光臨\t\terror.host\t1\r\n" +
		"0讀我檔案\t/readme\t" + host + "\t" + port + "\r\n.\r\n"
	routes[""] = menu
	routes["/"] = menu
	routes["/readme"] = "這是內文\r\n第二行\r\n.\r\n"

	a, s := newGopherApp(t, addr)
	a.OpenGopher("gopher://" + addr)
	settle(t, a, s)

	// 第一個(也是唯一一個)連結應該已經被選起來。
	a.HandleKey(keys.Named(keys.Enter))
	settle(t, a, s)

	txt := screenText(s)
	if !strings.Contains(txt, "這是內文") || !strings.Contains(txt, "第二行") {
		t.Fatalf("沒有跟著連結走\n%s", txt)
	}
	// 結束符不是內容。
	if strings.Contains(txt, "\n.\n") {
		t.Error("把結束符當成內容印出來了")
	}

	// Backspace 回上一頁。
	a.HandleKey(keys.Named(keys.Backspace))
	settle(t, a, s)
	if !strings.Contains(screenText(s), "讀我檔案") {
		t.Fatalf("回不去上一頁\n%s", screenText(s))
	}
}

// 連不上的時候要在狀態列說,不能靜靜地停在「連線中」。
func TestGopherShowsConnectionError(t *testing.T) {
	// 先起一個再關掉,拿一個確定沒人聽的埠。
	ln, _ := net.Listen("tcp", "127.0.0.1:0")
	dead := ln.Addr().String()
	ln.Close()

	a, s := newGopherApp(t, dead)
	a.OpenGopher("gopher://" + dead)
	settle(t, a, s)

	if !strings.Contains(screenText(s), "連不上") {
		t.Fatalf("沒有把錯誤說出來\n%s", screenText(s))
	}
}

// Busy 要在取回期間為真 —— 外殼靠它決定要不要繼續重繪。
func TestGopherBusyWhileFetching(t *testing.T) {
	block := make(chan struct{})
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	go func() {
		c, err := ln.Accept()
		if err != nil {
			return
		}
		defer c.Close()
		<-block // 拖著不回應
	}()

	a, s := newGopherApp(t, ln.Addr().String())
	a.OpenGopher("gopher://" + ln.Addr().String())
	a.Draw(s)
	if !a.Busy() {
		t.Fatal("取回期間 Busy 應該是真")
	}
	if !strings.Contains(screenText(s), "連線") {
		t.Errorf("狀態列沒有說在連線\n%s", screenText(s))
	}
	close(block)
}

// 位址解不開時不該進到瀏覽模式又卡在那裡。
func TestGopherRejectsBadURL(t *testing.T) {
	a, s := newGopherApp(t, "127.0.0.1:1")
	a.OpenGopher("http://example.org/")
	if a.Busy() {
		t.Fatal("不該開始取回")
	}
	if !strings.Contains(a.Message, "解不開") {
		t.Fatalf("訊息是 %q", a.Message)
	}
	_ = s
}
