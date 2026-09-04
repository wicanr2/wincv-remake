package app

import (
	"fmt"
	"github.com/wicanr2/wincv-remake/internal/i18n"
	"os"
	"path/filepath"
	"strings"

	"github.com/wicanr2/wincv-remake/internal/archive"
	"github.com/wicanr2/wincv-remake/internal/cell"
	"github.com/wicanr2/wincv-remake/internal/convert"
	"github.com/wicanr2/wincv-remake/internal/keys"
	"github.com/wicanr2/wincv-remake/internal/search"
	"github.com/wicanr2/wincv-remake/internal/textenc"
)

// --- Z 解壓縮 -------------------------------------------------------------

// startExtract 解壓縮。
//
// 兩種情境:在瀏覽器裡游標停在壓縮檔上,或者人已經在壓縮檔裡面。
// 原版的 Z 兩種都吃,所以這裡也是。
func (a *App) startExtract() bool {
	if af, ok := a.Browser.FS.(*archive.FS); ok {
		names := a.targets()
		inner := make([]string, 0, len(names))
		_, base := archiveSplit(a.Browser.Dir)
		for _, n := range names {
			if base == "" {
				inner = append(inner, n)
			} else {
				inner = append(inner, base+"/"+n)
			}
		}
		outer := filepath.Dir(af.Name())
		a.ask(fmt.Sprintf(i18n.T("解 %d 個項目到:"), len(inner)), outer, func(dst string) {
			a.runExtract(af, dst, inner)
		})
		return true
	}

	e := a.Browser.Current()
	if e == nil || e.IsDir || !archive.IsArchive(e.Name) {
		a.Message = i18n.T("游標要停在壓縮檔上")
		return true
	}
	full := filepath.Join(a.Browser.Dir, e.Name)
	af, err := archive.Open(full)
	if err != nil {
		a.Message = err.Error()
		return true
	}
	base := strings.TrimSuffix(e.Name, filepath.Ext(e.Name))
	a.ask(i18n.T("解壓縮到:"), filepath.Join(a.Browser.Dir, base), func(dst string) {
		a.runExtract(af, dst, nil)
	})
	return true
}

func (a *App) runExtract(af *archive.FS, dst string, names []string) {
	if dst == "" {
		return
	}
	if err := os.MkdirAll(dst, 0o755); err != nil {
		a.Message = i18n.T("目的目錄建不起來: ") + err.Error()
		return
	}
	n, err := af.Extract(dst, names, nil)
	if err != nil {
		a.Message = fmt.Sprintf(i18n.T("解出 %d 個後失敗: %v"), n, err)
	} else {
		a.Message = fmt.Sprintf(i18n.T("已解出 %d 個檔案到 %s"), n, dst)
	}
	a.Browser.Reload()
}

// --- Alt-Z 製作壓縮檔 -----------------------------------------------------

func (a *App) startCreateArchive() bool {
	if a.readOnlyHere() {
		a.Message = i18n.T("壓縮檔裡不能再建壓縮檔")
		return true
	}
	names := a.targets()
	if len(names) == 0 {
		return false
	}
	def := filepath.Base(a.Browser.Dir) + ".zip"
	if len(names) == 1 {
		def = strings.TrimSuffix(names[0], filepath.Ext(names[0])) + ".zip"
	}
	a.ask(fmt.Sprintf(i18n.T("把 %d 個項目打包成(只支援 .zip):"), len(names)), def, func(out string) {
		if out == "" {
			return
		}
		if !strings.HasSuffix(strings.ToLower(out), ".zip") {
			out += ".zip"
		}
		dst := out
		if !filepath.IsAbs(dst) {
			dst = filepath.Join(a.Browser.Dir, dst)
		}
		if err := archive.CreateZip(dst, a.Browser.Dir, names, nil); err != nil {
			a.Message = i18n.T("打包失敗: ") + err.Error()
			return
		}
		a.Message = fmt.Sprintf(i18n.T("已打包 %d 個項目到 %s"), len(names), filepath.Base(dst))
		a.Browser.Reload()
		a.focusOn(filepath.Base(dst))
	})
	return true
}

// --- W 尋找 ---------------------------------------------------------------

// FindResult 是搜尋結果清單的狀態。
type FindResult struct {
	Pattern string
	Kind    search.Kind
	Hits    []search.Hit
	Cursor  int
	Top     int
}

func (a *App) startFind() bool {
	a.confirm(i18n.T("尋找:N 檔名  S 字串  C 註解"), false, func(bool, bool) {})
	// confirm 只吃 Y/N/A,這裡要的是三選一,所以自己接一個小狀態機。
	a.prompt.onAnswer = nil
	a.prompt.title = i18n.T("尋找  N)檔名  S)字串  C)註解")
	a.prompt.onDone = nil
	a.findKindPending = true
	return true
}

// findKindKey 處理「尋找哪一種」那一步。
func (a *App) findKindKey(k keys.Key) bool {
	var kind search.Kind
	switch k.Letter() {
	case 'N':
		kind = search.ByName
	case 'S':
		kind = search.ByContent
	case 'C':
		kind = search.ByComment
	default:
		if k.Code == keys.Esc {
			a.findKindPending = false
			a.closePrompt()
			return true
		}
		return true
	}
	a.findKindPending = false
	a.closePrompt()
	label := map[search.Kind]string{
		search.ByName: i18n.T("檔名"), search.ByContent: i18n.T("字串"), search.ByComment: i18n.T("註解"),
	}[kind]
	a.ask(i18n.T("尋找 ")+label+":", "", func(pattern string) {
		a.runFind(kind, pattern)
	})
	return true
}

func (a *App) runFind(kind search.Kind, pattern string) {
	if pattern == "" {
		return
	}
	if a.readOnlyHere() {
		a.Message = i18n.T("壓縮檔裡還不能搜尋")
		return
	}
	hits, err := search.Run(a.Browser.Dir, pattern, search.Options{
		Kind: kind, Recursive: true,
	})
	if err != nil {
		a.Message = i18n.T("搜尋失敗: ") + err.Error()
		return
	}
	if len(hits) == 0 {
		a.Message = fmt.Sprintf(i18n.T("找不到 %q"), pattern)
		return
	}
	a.Find = &FindResult{Pattern: pattern, Kind: kind, Hits: hits}
	a.Mode = ModeFind
}

func (a *App) findKey(k keys.Key) bool {
	f := a.Find
	rows := a.rows
	if rows <= 0 {
		rows = 1
	}
	move := func(d int) bool {
		f.Cursor += d
		if f.Cursor < 0 {
			f.Cursor = 0
		}
		if f.Cursor >= len(f.Hits) {
			f.Cursor = len(f.Hits) - 1
		}
		if f.Cursor < f.Top {
			f.Top = f.Cursor
		}
		if f.Cursor >= f.Top+rows {
			f.Top = f.Cursor - rows + 1
		}
		return true
	}
	switch k.Code {
	case keys.Up:
		return move(-1)
	case keys.Down:
		return move(1)
	case keys.PgUp:
		return move(-rows)
	case keys.PgDn:
		return move(rows)
	case keys.Home:
		f.Cursor, f.Top = 0, 0
		return true
	case keys.End:
		return move(len(f.Hits))
	case keys.Esc:
		a.Mode = ModeBrowser
		return true
	case keys.Enter:
		if f.Cursor < 0 || f.Cursor >= len(f.Hits) {
			return false
		}
		h := f.Hits[f.Cursor]
		if err := a.Browser.Load(h.Dir); err != nil {
			a.Message = err.Error()
			return true
		}
		a.Mode = ModeBrowser
		a.focusOn(h.Name)
		return true
	}
	return false
}

func (a *App) drawFind(s *cell.Screen) int {
	f := a.Find
	s.Clear(cell.LtGray, cell.Black)
	rows := s.Rows - 2
	if rows < 0 {
		rows = 0
	}

	kindName := map[search.Kind]string{
		search.ByName: i18n.T("檔名"), search.ByContent: i18n.T("字串"), search.ByComment: i18n.T("註解"),
	}[f.Kind]
	s.Fill(0, 0, s.Cols, 1, ' ', cell.LtCyan, cell.Blue)
	s.Print(0, 0, fmt.Sprintf(i18n.T("尋找 %s: %q"), kindName, f.Pattern), cell.LtCyan, cell.Blue)
	right := fmt.Sprintf("%d / %d", f.Cursor+1, len(f.Hits))
	if x := s.Cols - len(right); x >= 0 {
		s.Print(x, 0, right, cell.LtCyan, cell.Blue)
	}

	for i := 0; i < rows; i++ {
		idx := f.Top + i
		if idx >= len(f.Hits) {
			break
		}
		h := f.Hits[idx]
		y := 1 + i
		loc := h.Name
		if h.Line > 0 {
			loc = fmt.Sprintf("%s:%d", h.Name, h.Line)
		}
		x := s.Print(1, y, loc, cell.LtGreen, cell.Black)
		if h.Text != "" {
			s.Print(x+2, y, printable(h.Text, s.Cols-x-2), cell.LtGray, cell.Black)
		} else {
			s.Print(x+2, y, relDir(a.Browser.Dir, h.Dir), cell.DkGray, cell.Black)
		}
		if idx == f.Cursor {
			s.SetAttr(0, y, s.Cols, cell.Black, cell.LtGray)
		}
	}

	y := s.Rows - 1
	s.Fill(0, y, s.Cols, 1, ' ', cell.Black, cell.LtGray)
	if f.Cursor >= 0 && f.Cursor < len(f.Hits) {
		s.Print(0, y, f.Hits[f.Cursor].Path(), cell.Black, cell.LtGray)
	}
	return rows
}

// printable 把一行內容整理成可以直接畫的樣子。
//
// 編碼判讀不是二分法:像 store.cab 這種「大部分是純文字、夾著標頭」的
// 檔案不會被判成二進位,於是控制碼會原樣畫到畫面上。列表這一層自己
// 擋掉比去調判讀門檻安全 —— 門檻調嚴會讓真的文字檔搜不到。
func printable(s string, max int) string {
	if max < 1 {
		return ""
	}
	out := make([]rune, 0, max)
	for _, r := range s {
		if len(out) >= max {
			break
		}
		switch {
		case r == '\t':
			out = append(out, ' ')
		case r < 0x20 || r == 0x7F:
			out = append(out, '.')
		case r == 0xFFFD:
			out = append(out, '.')
		default:
			out = append(out, r)
		}
	}
	return string(out)
}

func relDir(base, dir string) string {
	if r, err := filepath.Rel(base, dir); err == nil && !strings.HasPrefix(r, "..") {
		if r == "." {
			return ""
		}
		return r
	}
	return dir
}

// --- Ctrl-O 轉換 ----------------------------------------------------------

func (a *App) startConvert() bool {
	if a.readOnlyHere() {
		a.Message = i18n.T("壓縮檔裡的檔案還不能轉換")
		return true
	}
	// 目錄轉不了(讀進來就是失敗),先濾掉再問要轉什麼,
	// 不然選單開了才發現一個都做不了。
	var names []string
	for _, n := range a.targets() {
		if fi, err := os.Stat(filepath.Join(a.Browser.Dir, n)); err == nil && !fi.IsDir() {
			names = append(names, n)
		}
	}
	if len(names) == 0 {
		a.Message = i18n.T("沒有可以轉換的檔案(目錄不算)")
		return true
	}
	a.convertPending = names
	a.prompt = prompt{
		active: true,
		title: fmt.Sprintf(i18n.T("轉換 %d 個檔案  U)UNIX換行  P)PC換行  B)轉Big5  ")+
			i18n.T("G)轉GBK  H)去HTML  A)去ANSI"), len(names)),
	}
	return true
}

// convertKey 處理轉換的選單那一步。
func (a *App) convertKey(k keys.Key) bool {
	if k.Code == keys.Esc {
		a.convertPending = nil
		a.closePrompt()
		return true
	}
	letter := k.Letter()
	fn, label := convertOp(letter)
	if fn == nil {
		return true
	}
	names := a.convertPending
	a.convertPending = nil
	a.closePrompt()

	ok, failed := 0, 0
	for _, n := range names {
		p := filepath.Join(a.Browser.Dir, n)
		data, err := os.ReadFile(p)
		if err != nil {
			failed++
			continue
		}
		out := fn(data)
		if err := os.WriteFile(p, out, 0o644); err != nil {
			failed++
			continue
		}
		ok++
	}
	a.Message = fmt.Sprintf(i18n.T("%s:成功 %d 個"), label, ok)
	if failed > 0 {
		a.Message += fmt.Sprintf(i18n.T(",失敗 %d 個"), failed)
	}
	a.Browser.Reload()
	return true
}

// convertOp 把選單字母對到轉換函式。
func convertOp(letter rune) (func([]byte) []byte, string) {
	switch letter {
	case 'U':
		return func(b []byte) []byte { return convert.ToEOL(b, convert.EOLUnix) }, i18n.T("轉 UNIX 換行")
	case 'P':
		return func(b []byte) []byte { return convert.ToEOL(b, convert.EOLPC) }, i18n.T("轉 PC 換行")
	case 'B':
		return func(b []byte) []byte {
			return convert.RecodeLossy(b, textenc.Detect(b), textenc.Big5, "?")
		}, i18n.T("轉 Big5")
	case 'G':
		return func(b []byte) []byte {
			return convert.RecodeLossy(b, textenc.Detect(b), textenc.GBK, "?")
		}, i18n.T("轉 GBK")
	case 'H':
		return func(b []byte) []byte {
			e := textenc.Detect(b)
			s := convert.StripHTML(textenc.Decode(b, e))
			return convert.RecodeLossy([]byte(s), textenc.UTF8, e, "?")
		}, i18n.T("去 HTML")
	case 'A':
		return func(b []byte) []byte {
			e := textenc.Detect(b)
			s := convert.StripANSI(textenc.Decode(b, e))
			return convert.RecodeLossy([]byte(s), textenc.UTF8, e, "?")
		}, i18n.T("去 ANSI 色碼")
	}
	return nil, ""
}
