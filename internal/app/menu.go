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

// MenuBarRows 是選單列佔掉的列數。
//
// 原版的選單是 Win32 的原生選單,掛在標題列底下、client area 之外,
// 不佔任何一個字元格。remake 全部自繪,所以它一定會吃掉一列 ——
// 預設視窗因此比原版高一列(93×22),內容區才維持原版的 21 列
// (docs/ui/main-screen.md)。
const MenuBarRows = 1

// menuItem 是下拉選單的一列。key 為零值時代表分隔線。
type menuItem struct {
	label string
	key   keys.Key
	sep   bool
	run   func() bool       // 沒有對應按鍵的功能(例如 MD5/SFV)用這個
	sub   func() []menuItem // 子選單。選中時換成它的內容,Esc 退回來
}

// menuCat 是選單列上的一個分類。
//
// items 是函式而不是切片:項目的標籤會隨狀態變(例如放大倍率),
// 而且每次展開才建構,不必在意快取失效。
type menuCat struct {
	name  string
	items func() []menuItem
}

// menu 是選單列與它展開的下拉。
//
// 選單列本身一直在畫面最上方那一列,active 說的是下拉有沒有展開。
// 下拉同時當說明書用:每一列都寫著對應的按鍵,按過幾次就記得了。
type menu struct {
	active bool
	cat    int // 選單列上的第幾個分類
	items  []menuItem
	cursor int
	top    int // 項目比畫面高時,第一列顯示的是第幾項
	rows   int // 最近一次繪製時裝得下幾項
	x      int // 下拉框的左緣,對齊分類標題

	// back 是子選單的返回點。只存一層 —— 選單是給人按的,
	// 深到要記路徑就表示分類方式錯了。
	back   []menuItem
	backAt int
	title  string
}

// Menuing 回傳現在下拉是不是展開著。
func (a *App) Menuing() bool { return a.menu.active }

// menuCats 是選單列上的分類。
//
// 分類的依據是「這個動作對誰做」:檔案是對選到的檔案做的事,
// 檢視是改變看的方式,工具是產生新東西(校驗碼、轉換、連出去),
// 設定是改變程式本身。
func (a *App) menuCats() []menuCat {
	return []menuCat{
		{"檔案", a.fileMenuItems},
		{"檢視", a.viewMenuItems},
		{"工具", a.toolMenuItems},
		{"設定", a.setupMenuItems},
		{"說明", a.helpMenuItems},
	}
}

func (a *App) fileMenuItems() []menuItem {
	k, alt := keys.Ch, keys.AltCh
	return []menuItem{
		{label: "檢視", key: keys.Named(keys.Enter)},
		{label: "編輯", key: k('E')},
		{label: "開啟(系統預設程式)", key: k('O')},
		{label: "執行", key: k('G')},
		{sep: true},
		{label: "拷貝", key: k('C')},
		{label: "移動", key: k('M')},
		{label: "更名", key: k('R')},
		{label: "刪除", key: k('D')},
		{sep: true},
		{label: "標記全部檔案", key: k('T')},
		{label: "標記全部(含目錄)", key: alt('T')},
		{label: "解除所有標記", key: k('U')},
		{label: "比對兩個標記的檔案", key: alt('C')},
		{sep: true},
		{label: "解壓縮", key: k('Z')},
		{label: "製作壓縮檔(.zip)", key: alt('Z')},
		{sep: true},
		{label: "離開", key: alt('X')},
	}
}

func (a *App) viewMenuItems() []menuItem {
	k, alt := keys.Ch, keys.AltCh
	return []menuItem{
		{label: "改變路徑", key: k('P')},
		{label: "排序方式", key: k('S')},
		{label: "縮圖列表", key: k('5')},
		{sep: true},
		{label: "預視窗格", key: alt('P')},
		{label: "磁碟窗格", key: alt('D')},
		{sep: true},
		{label: "尋找 檔名/字串/註解", key: k('W')},
		{label: "註解", key: alt('E')},
	}
}

func (a *App) toolMenuItems() []menuItem {
	return []menuItem{
		{label: "轉換(換行/編碼/去 HTML)", key: keys.CtrlCh('O')},
		{sep: true},
		{label: "算 MD5", run: a.runMD5},
		{label: "建 SFV(.sfv)", run: a.runMakeSFV},
		{label: "驗 SFV", run: a.runVerifySFV},
		{sep: true},
		{label: "網路瀏覽(gopher / http)", key: keys.Named(keys.F2)},
	}
}

func (a *App) setupMenuItems() []menuItem {
	return []menuItem{
		{label: "切換 中英文顯示", key: keys.Named(keys.F8)},
		{label: "全螢幕", key: keys.Named(keys.F11)},
		{sep: true},
		{label: "放大字體", key: keys.CtrlCh('+')},
		{label: "縮小字體", key: keys.CtrlCh('-')},
		{sep: true},
		{label: fmt.Sprintf("放大倍率 +%.1f(現在 %.1f×)", ScaleStep, a.Scale), key: keys.AltCh('+')},
		{label: fmt.Sprintf("放大倍率 -%.1f", ScaleStep), key: keys.AltCh('-')},
		{label: "視窗大小…", sub: a.sizeMenuItems},
	}
}

func (a *App) helpMenuItems() []menuItem {
	return []menuItem{
		{label: "使用說明", key: keys.Named(keys.F1)},
		{label: "關於", run: a.openAbout},
	}
}

// openMenu 展開下拉,已經展開的話收起來。
func (a *App) openMenu() bool {
	if a.menu.active {
		a.menu = menu{}
		return true
	}
	a.menu = menu{active: true}
	a.selectCat(0)
	return true
}

// selectCat 換到第 i 個分類並載入它的項目。左右兩端會繞回來。
func (a *App) selectCat(i int) {
	cats := a.menuCats()
	if i < 0 {
		i = len(cats) - 1
	}
	if i >= len(cats) {
		i = 0
	}
	m := &a.menu
	m.cat, m.items = i, cats[i].items()
	m.cursor, m.top = 0, 0
	m.back, m.backAt, m.title = nil, 0, ""
	// 游標不要停在分隔線上
	for m.cursor < len(m.items) && m.items[m.cursor].sep {
		m.cursor++
	}
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
		m.scrollToCursor()
		return true
	}
	// 子選單裡的左鍵是「退回上一層」而不是「換分類」——
	// 換掉分類會把使用者丟到別的地方,而他只是想退一步。
	inSub := m.back != nil
	switch k.Code {
	case keys.Up:
		return move(-1)
	case keys.Down:
		return move(1)
	case keys.Left:
		if inSub {
			return a.menuLeaveSub()
		}
		a.selectCat(m.cat - 1)
		return true
	case keys.Right:
		if inSub {
			return true
		}
		a.selectCat(m.cat + 1)
		return true
	case keys.Home:
		// 先退到範圍外再往前走一格,才會停在**第一個**可選項目上;
		// 直接設 0 再 move(1) 會跳過第一項。
		m.cursor = -1
		return move(1)
	case keys.End:
		m.cursor = len(m.items)
		return move(-1)
	case keys.PgUp:
		return move(-m.visible())
	case keys.PgDn:
		return move(m.visible())
	case keys.Esc, keys.F9:
		if inSub && k.Code == keys.Esc {
			return a.menuLeaveSub()
		}
		a.menu = menu{}
		return true
	case keys.Enter:
		if m.cursor < 0 || m.cursor >= len(m.items) {
			return false
		}
		it := m.items[m.cursor]
		if it.sub != nil {
			m.back, m.backAt, m.title = m.items, m.cursor, it.label
			m.items, m.cursor, m.top = it.sub(), 0, 0
			return true
		}
		a.menu = menu{}
		a.Message = ""
		if it.run != nil {
			return it.run()
		}
		return a.HandleKey(it.key)
	}
	// 直接按該功能的鍵也算數:下拉同時是說明書。
	for _, it := range m.items {
		if it.sep || it.run != nil || it.key != k {
			continue
		}
		a.menu = menu{}
		a.Message = ""
		return a.HandleKey(k)
	}
	return true // 下拉開著,其他鍵一律吃掉
}

// menuLeaveSub 從子選單退回上一層。
func (a *App) menuLeaveSub() bool {
	m := &a.menu
	m.items, m.cursor, m.back, m.title = m.back, m.backAt, nil, ""
	m.scrollToCursor()
	return true
}

// visible 回傳畫面裝得下幾項。還沒畫過時給一個保守值。
func (m *menu) visible() int {
	if m.rows > 0 {
		return m.rows
	}
	return 10
}

// scrollToCursor 讓游標所在的項目留在可見範圍內。
func (m *menu) scrollToCursor() {
	n := m.visible()
	if m.cursor < m.top {
		m.top = m.cursor
	}
	if m.cursor >= m.top+n {
		m.top = m.cursor - n + 1
	}
	if m.top < 0 {
		m.top = 0
	}
}

// drawMenuBar 畫最上方那一列。畫的是**主畫面**,不是各模式用的內層畫面。
func (a *App) drawMenuBar(s *cell.Screen) {
	s.Fill(0, 0, s.Cols, 1, ' ', cell.Black, cell.LtGray)
	x := 1
	for i, c := range a.menuCats() {
		label := " " + c.name + " "
		w := cellWidth(label)
		fg, bg := cell.Black, cell.LtGray
		if a.menu.active && i == a.menu.cat {
			fg, bg = cell.White, cell.Blue
			// 下拉要對齊自己的分類標題。位置在這裡才算得出來,
			// 因為標題寬度取決於字型(全形字兩格)。
			a.menu.x = x
		}
		if x+w > s.Cols {
			break
		}
		s.Fill(x, 0, w, 1, ' ', fg, bg)
		s.Print(x, 0, label, fg, bg)
		x += w
	}
	// 右側提示。擠不下就不畫 —— 半條提示比沒有提示更難懂。
	hint := "F9 選單  F1 說明  F2 網路"
	if px := s.Cols - cellWidth(hint) - 1; px > x+2 {
		s.Print(px, 0, hint, cell.Blue, cell.LtGray)
	}
}

// drawMenu 畫展開的下拉。座標系是各模式用的內層畫面,所以 y 從 0 開始
// 就已經緊貼在選單列底下了。
func (a *App) drawMenu(s *cell.Screen) {
	m := &a.menu

	// 寬度用**顯示格數**算,不是位元組數 —— 一個中文字是 3 個位元組
	// 但只佔 2 格,用 len() 會把選單撐成一倍半寬。
	w := 0
	for _, it := range m.items {
		l := cellWidth(it.label) + 4
		if it.run == nil {
			l += len(it.key.String()) + 4
		}
		if l > w {
			w = l
		}
	}
	if w > s.Cols-2 {
		w = s.Cols - 2
	}

	// 頂層的下拉不畫標題:分類名已經在選單列上反白著,再寫一次
	// 只是白佔一列。子選單要,不然不知道自己在哪一層。
	head := 0
	if m.title != "" {
		head = 1
	}
	// 底下留一列給狀態列,選單再高就自己捲。最後一列是捲動提示。
	maxH := s.Rows - 1
	if maxH < 3 {
		maxH = s.Rows
	}
	h := len(m.items) + head + 1
	if h > maxH {
		h = maxH
	}
	m.rows = h - head - 1
	m.scrollToCursor()

	x0, y0 := m.x, 0
	if x0+w > s.Cols {
		x0 = s.Cols - w
	}
	if x0 < 0 {
		x0 = 0
	}

	s.Fill(x0, y0, w, h, ' ', cell.Black, cell.LtGray)
	if head == 1 {
		s.Fill(x0, y0, w, 1, ' ', cell.White, cell.Blue)
		s.Print(x0+1, y0, " "+m.title+" ", cell.White, cell.Blue)
	}

	for row := 0; row < m.rows; row++ {
		i := m.top + row
		if i >= len(m.items) {
			break
		}
		it := m.items[i]
		y := y0 + head + row
		if it.sep {
			// 分隔線用 '-':cvga 是 Big5 的半形字型,0x80 以上是
			// 雙位元組字的前導位元組,沒有製表符號的字模,
			// 拿 U+2500 來畫會是一片空白。
			s.Fill(x0+2, y, w-4, 1, '-', cell.Gray, cell.LtGray)
			continue
		}
		fg, bg := cell.Black, cell.LtGray
		// [雷] 按鍵的顏色要跟著反白換。原本兩種狀態都用 Blue,
		// 於是游標那一列是藍字畫在藍底上 —— 按鍵安靜地消失,
		// 而畫面上看起來只是「這一項沒有快捷鍵」。
		kfg := cell.Blue
		if i == m.cursor {
			fg, bg, kfg = cell.White, cell.Blue, cell.LtCyan
			s.Fill(x0, y, w, 1, ' ', fg, bg)
		}
		s.Print(x0+2, y, it.label, fg, bg)
		if it.run == nil {
			ks := it.key.String()
			if px := x0 + w - 2 - len(ks); px > x0+2 {
				s.Print(px, y, ks, kfg, bg)
			}
		}
	}

	// 還有沒顯示到的項目時給個提示,不然看起來像選單就這麼多。
	if m.rows < len(m.items) {
		y := y0 + h - 1
		s.Fill(x0, y, w, 1, ' ', cell.Blue, cell.LtGray)
		pos := fmt.Sprintf("%d/%d", m.cursor+1, len(m.items))
		s.Print(x0+2, y, pos, cell.Blue, cell.LtGray)
		if m.top > 0 {
			s.Print(x0+w-4, y, "^", cell.Blue, cell.LtGray)
		}
		if m.top+m.rows < len(m.items) {
			s.Print(x0+w-2, y, "v", cell.Blue, cell.LtGray)
		}
	}
}

// cellWidth 算一串字佔幾格。全形字兩格。
func cellWidth(s string) int {
	n := 0
	for _, r := range s {
		if cell.IsWide(r) {
			n += 2
		} else {
			n++
		}
	}
	return n
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
