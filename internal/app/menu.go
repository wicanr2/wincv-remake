package app

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/wicanr2/wincv-remake/internal/cell"
	"github.com/wicanr2/wincv-remake/internal/checksum"
	"github.com/wicanr2/wincv-remake/internal/keys"
	"github.com/wicanr2/wincv-remake/internal/textenc"
	"github.com/wicanr2/wincv-remake/internal/viewer"
)

// menuItem 是選單的一列。key 為零值時代表分隔線。
type menuItem struct {
	label string
	key   keys.Key
	sep   bool
	run   func() bool // 沒有對應按鍵的功能(例如 MD5/SFV)用這個
}

// menu 是 F1 叫出來的指令選單。
//
// 原版是 Win32 的下拉選單,remake 全部自繪,所以做成一個浮在畫面上的
// 清單。它同時當說明書用:每一列都寫著對應的按鍵,按過幾次就記得了。
type menu struct {
	active bool
	items  []menuItem
	cursor int
}

// Menuing 回傳現在是不是開著選單。
func (a *App) Menuing() bool { return a.menu.active }

func (a *App) openMenu() bool {
	a.menu = menu{active: true, items: a.menuItems()}
	// 游標不要停在分隔線上
	for a.menu.cursor < len(a.menu.items) && a.menu.items[a.menu.cursor].sep {
		a.menu.cursor++
	}
	return true
}

func (a *App) menuItems() []menuItem {
	k := func(r rune) keys.Key { return keys.Ch(r) }
	alt := func(r rune) keys.Key { return keys.AltCh(r) }
	items := []menuItem{
		{label: "檢視", key: keys.Named(keys.Enter)},
		{label: "編輯", key: k('E')},
		{label: "開啟(系統預設程式)", key: k('O')},
		{label: "執行", key: k('G')},
		{sep: true},
		{label: "拷貝", key: k('C')},
		{label: "移動", key: k('M')},
		{label: "更名", key: k('R')},
		{label: "刪除", key: k('D')},
		{label: "比對兩個標記的檔案", key: alt('C')},
		{sep: true},
		{label: "標記全部檔案", key: k('T')},
		{label: "標記全部(含目錄)", key: alt('T')},
		{label: "解除所有標記", key: k('U')},
		{sep: true},
		{label: "解壓縮", key: k('Z')},
		{label: "製作壓縮檔(.zip)", key: alt('Z')},
		{label: "轉換(換行/編碼/去 HTML)", key: keys.CtrlCh('O')},
		{label: "尋找 檔名/字串/註解", key: k('W')},
		{label: "註解", key: alt('E')},
		{sep: true},
		{label: "算 MD5", run: a.runMD5},
		{label: "建 SFV(.sfv)", run: a.runMakeSFV},
		{label: "驗 SFV", run: a.runVerifySFV},
		{sep: true},
		{label: "改變路徑", key: k('P')},
		{label: "排序方式", key: k('S')},
		{label: "縮圖列表", key: k('5')},
		{label: "切換 中英文顯示", key: keys.Named(keys.F8)},
		{label: "全螢幕", key: keys.Named(keys.F11)},
	}
	return items
}

func (a *App) menuKey(k keys.Key) bool {
	m := &a.menu
	move := func(d int) bool {
		for i := 0; i < len(m.items); i++ {
			m.cursor += d
			if m.cursor < 0 {
				m.cursor = len(m.items) - 1
			}
			if m.cursor >= len(m.items) {
				m.cursor = 0
			}
			if !m.items[m.cursor].sep {
				break
			}
		}
		return true
	}
	switch k.Code {
	case keys.Up:
		return move(-1)
	case keys.Down:
		return move(1)
	case keys.Home:
		m.cursor = 0
		return move(1)
	case keys.End:
		m.cursor = len(m.items) - 1
		return move(-1)
	case keys.Esc, keys.F1:
		a.menu = menu{}
		return true
	case keys.Enter:
		if m.cursor < 0 || m.cursor >= len(m.items) {
			return false
		}
		it := m.items[m.cursor]
		a.menu = menu{}
		a.Message = ""
		if it.run != nil {
			return it.run()
		}
		return a.HandleKey(it.key)
	}
	// 直接按該功能的鍵也算數:選單同時是說明書。
	if k.Code == keys.Rune || k.Code == keys.F8 || k.Code == keys.F11 {
		for _, it := range m.items {
			if it.sep || it.run != nil {
				continue
			}
			if it.key == k {
				a.menu = menu{}
				a.Message = ""
				return a.HandleKey(k)
			}
		}
	}
	return true // 選單開著,其他鍵一律吃掉
}

func (a *App) drawMenu(s *cell.Screen) {
	m := &a.menu
	w := 0
	for _, it := range m.items {
		if l := len(it.label) + 12; l > w {
			w = l
		}
	}
	if w > s.Cols-2 {
		w = s.Cols - 2
	}
	h := len(m.items) + 2
	if h > s.Rows {
		h = s.Rows
	}
	x0, y0 := (s.Cols-w)/2, (s.Rows-h)/2
	if x0 < 0 {
		x0 = 0
	}
	if y0 < 0 {
		y0 = 0
	}

	s.Fill(x0, y0, w, h, ' ', cell.Black, cell.LtGray)
	s.Print(x0+1, y0, " 選單 (F1/Esc 關閉) ", cell.White, cell.Blue)

	for i, it := range m.items {
		y := y0 + 1 + i
		if y >= y0+h-1 {
			break
		}
		if it.sep {
			s.Fill(x0+1, y, w-2, 1, '─', cell.Gray, cell.LtGray)
			continue
		}
		fg, bg := cell.Black, cell.LtGray
		if i == m.cursor {
			fg, bg = cell.White, cell.Blue
			s.Fill(x0, y, w, 1, ' ', fg, bg)
		}
		s.Print(x0+2, y, it.label, fg, bg)
		if it.run == nil {
			ks := it.key.String()
			if px := x0 + w - 2 - len(ks); px > x0+2 {
				s.Print(px, y, ks, cell.Blue, bg)
			}
		}
	}
}

// --- MD5 / SFV ------------------------------------------------------------

func (a *App) runMD5() bool {
	if a.readOnlyHere() {
		a.Message = "壓縮檔裡的檔案要先解出來"
		return true
	}
	names := a.targets()
	if len(names) == 0 {
		a.Message = "沒有選到檔案"
		return true
	}
	var sb strings.Builder
	for _, n := range names {
		sum, err := checksum.MD5File(filepath.Join(a.Browser.Dir, n))
		if err != nil {
			sb.WriteString(fmt.Sprintf("%s  <%v>\n", n, err))
			continue
		}
		sb.WriteString(fmt.Sprintf("%s  %s\n", sum, n))
	}
	a.showText("MD5", sb.String())
	return true
}

func (a *App) runMakeSFV() bool {
	if a.readOnlyHere() {
		a.Message = "壓縮檔裡的檔案要先解出來"
		return true
	}
	names := a.targets()
	if len(names) == 0 {
		a.Message = "沒有選到檔案"
		return true
	}
	def := filepath.Base(a.Browser.Dir) + ".sfv"
	a.ask(fmt.Sprintf("把 %d 個檔案的 CRC 寫進:", len(names)), def, func(out string) {
		if out == "" {
			return
		}
		if !strings.HasSuffix(strings.ToLower(out), ".sfv") {
			out += ".sfv"
		}
		dst := out
		if !filepath.IsAbs(dst) {
			dst = filepath.Join(a.Browser.Dir, dst)
		}
		entries, err := checksum.MakeSFV(a.Browser.Dir, names)
		if err != nil {
			a.Message = "算 CRC 失敗: " + err.Error()
			return
		}
		f, err := create(dst)
		if err != nil {
			a.Message = err.Error()
			return
		}
		err = checksum.WriteSFV(f, entries)
		f.Close()
		if err != nil {
			a.Message = "寫不進去: " + err.Error()
			return
		}
		a.Message = fmt.Sprintf("已寫入 %d 筆到 %s", len(entries), filepath.Base(dst))
		a.Browser.Reload()
		a.focusOn(filepath.Base(dst))
	})
	return true
}

func (a *App) runVerifySFV() bool {
	if a.readOnlyHere() {
		a.Message = "壓縮檔裡的檔案要先解出來"
		return true
	}
	e := a.Browser.Current()
	if e == nil || e.IsDir || !strings.HasSuffix(strings.ToLower(e.Name), ".sfv") {
		a.Message = "游標要停在 .sfv 檔上"
		return true
	}
	rs, err := checksum.VerifySFV(filepath.Join(a.Browser.Dir, e.Name))
	if err != nil {
		a.Message = err.Error()
		return true
	}
	ok, bad, missing := checksum.Summary(rs)
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("%s:相符 %d,不符 %d,找不到 %d\n\n", e.Name, ok, bad, missing))
	for _, r := range rs {
		mark := "OK  "
		switch {
		case r.Missing:
			mark = "缺檔"
		case !r.OK:
			mark = "不符"
		}
		sb.WriteString(fmt.Sprintf("%s  %s  %s\n", mark, r.Want, r.Name))
	}
	a.showText("SFV 驗證", sb.String())
	return true
}

// showText 把一段產生出來的報告丟進檢視器。
//
// 它不是檔案,所以退出時要回瀏覽器而不是回上一層目錄;
// viewer 只看得到 Lines,對它來說沒有差別。
func (a *App) showText(title, body string) {
	a.viewData = []byte(body)
	a.viewRaw = body
	a.Viewer = viewer.Load(title, []byte(body), textenc.UTF8)
	a.Mode = ModeViewer
}

func create(p string) (*os.File, error) {
	f, err := os.Create(p)
	if err != nil {
		return nil, fmt.Errorf("建不了 %s: %w", filepath.Base(p), err)
	}
	return f, nil
}
