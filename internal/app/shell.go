package app

import (
	"github.com/wicanr2/wincv-remake/internal/i18n"
	"os"
	"path/filepath"
	"strings"

	"github.com/wicanr2/wincv-remake/internal/cell"
	"github.com/wicanr2/wincv-remake/internal/launch"
	"github.com/wicanr2/wincv-remake/internal/note"
	"github.com/wicanr2/wincv-remake/internal/textenc"
	"github.com/wicanr2/wincv-remake/internal/vfs"
)

// --- P 改變路徑 -----------------------------------------------------------

func (a *App) startChangeDir() bool {
	a.ask(i18n.T("改變路徑:"), a.Browser.Dir, func(p string) {
		if p == "" {
			return
		}
		p = expandHome(p)
		if !filepath.IsAbs(p) {
			p = filepath.Join(a.Browser.Dir, p)
		}
		// 路徑指到檔案時,進它所在的目錄並把游標停上去 ——
		// 貼一整條檔案路徑進來是很自然的用法。
		want := ""
		if fi, err := os.Stat(p); err == nil && !fi.IsDir() {
			want = filepath.Base(p)
			p = filepath.Dir(p)
		}
		if err := a.Browser.Load(p); err != nil {
			a.Message = err.Error()
			return
		}
		if want != "" {
			a.focusOn(want)
		}
	})
	return true
}

func expandHome(p string) string {
	if p == "~" || strings.HasPrefix(p, "~/") {
		if h, err := os.UserHomeDir(); err == nil {
			return filepath.Join(h, strings.TrimPrefix(strings.TrimPrefix(p, "~"), "/"))
		}
	}
	return p
}

// --- O 開啟 / G 執行 ------------------------------------------------------

// startOpen 用系統預設的程式開啟游標上的項目。
func (a *App) startOpen() bool {
	if a.readOnlyHere() {
		a.Message = i18n.T("壓縮檔裡的檔案要先解出來才能開啟")
		return true
	}
	e := a.Browser.Current()
	if e == nil || e.Up {
		return false
	}
	p := filepath.Join(a.Browser.Dir, e.Name)
	if err := launch.Open(p); err != nil {
		a.Message = i18n.T("開不起來: ") + err.Error()
		return true
	}
	a.Message = i18n.T("已交給系統開啟: ") + e.Name
	return true
}

// startRun 執行一行命令,預設帶入游標上的檔案。
func (a *App) startRun() bool {
	if a.readOnlyHere() {
		a.Message = i18n.T("壓縮檔裡的檔案不能直接執行")
		return true
	}
	def := ""
	if e := a.Browser.Current(); e != nil && !e.Up && !e.IsDir {
		p := filepath.Join(a.Browser.Dir, e.Name)
		if launch.Executable(p) {
			def = shellQuote("./" + e.Name)
		} else {
			def = " " + shellQuote(e.Name)
		}
	}
	a.ask(i18n.T("執行:"), def, func(line string) {
		if strings.TrimSpace(line) == "" {
			return
		}
		if err := launch.Run(a.Browser.Dir, line); err != nil {
			a.Message = i18n.T("執行失敗: ") + err.Error()
			return
		}
		a.Message = i18n.T("已執行: ") + line
	})
	return true
}

// shellQuote 把檔名包成 shell 安全的字面值。
// 檔名裡有空白或引號很常見,不包起來會被切成好幾個參數。
func shellQuote(s string) string {
	if s == "" {
		return "''"
	}
	safe := true
	for _, r := range s {
		if !(r == '.' || r == '/' || r == '-' || r == '_' || r == '+' ||
			(r >= '0' && r <= '9') || (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z')) {
			safe = false
			break
		}
	}
	if safe {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// --- Alt-E 註解 -----------------------------------------------------------

// loadNotes 是給 browser 的註解讀取 hook。壓縮檔裡沒有真的目錄可讀,
// 回 nil 讓它不顯示。
func (a *App) loadNotes(dir string) map[string]string {
	if a.readOnlyHere() {
		return nil
	}
	s, err := note.Load(dir)
	if err != nil || s.Len() == 0 {
		return nil
	}
	return s.Raw()
}

// diskStat 是給 browser 的磁碟容量 hook。壓縮檔內部沒有對應的磁碟,
// 回 0 讓狀態列不顯示 —— 顯示宿主磁碟的剩餘空間會誤導。
func (a *App) diskStat(dir string) (free, total int64) {
	if a.readOnlyHere() {
		return 0, 0
	}
	f, t, err := vfs.DiskUsage(dir)
	if err != nil {
		return 0, 0
	}
	return int64(f), int64(t)
}

func (a *App) startNote() bool {
	if a.readOnlyHere() {
		a.Message = i18n.T("壓縮檔裡不能加註解")
		return true
	}
	e := a.Browser.Current()
	if e == nil || e.Up {
		return false
	}
	dir, name := a.Browser.Dir, e.Name
	a.ask(i18n.T("註解 ")+name+":", a.Browser.Notes[name], func(text string) {
		s, err := note.Load(dir)
		if err != nil {
			a.Message = i18n.T("讀不到註解檔: ") + err.Error()
			return
		}
		s.Set(name, text)
		if err := s.Save(dir); err != nil {
			a.Message = i18n.T("寫不進註解檔: ") + err.Error()
			return
		}
		a.Browser.Reload()
		if strings.TrimSpace(text) == "" {
			a.Message = i18n.T("已清掉 ") + name + i18n.T(" 的註解")
		}
	})
	return true
}

// --- F8 切換中英文顯示 ----------------------------------------------------

// toggleCJK 在「解讀成中文」與「一個位元組一個字」之間切換。
//
// 原版的 F8 叫「切換 中英文顯示」:關掉之後雙位元組不再被併成一個
// 中文字,而是照 .FON 的 0x00-0xFF 各畫一格。用來看混了控制碼的檔案,
// 或確認某兩個位元組到底是什麼。
func (a *App) toggleCJK() bool {
	a.EnglishOnly = !a.EnglishOnly
	if a.Mode == ModeViewer && a.Viewer != nil {
		a.viewRaw = a.decodeView(a.viewData, a.Viewer.Enc)
		a.Viewer.SetAnsi(a.Viewer.Ansi, a.viewRaw)
	}
	if a.EnglishOnly {
		a.Message = i18n.T("英文顯示(一個位元組一格)")
	} else {
		a.Message = i18n.T("中文顯示")
	}
	return true
}

// decodeView 依目前的中英文顯示設定把位元組轉成要畫的文字。
func (a *App) decodeView(data []byte, enc textenc.Enc) string {
	if !a.EnglishOnly {
		return textenc.Decode(data, enc)
	}
	// 每個位元組畫一格,走字型自己的字碼表(CP437)。
	//
	// 不能用 rune(b):那是把位元組當 Latin-1,0x80 以上會對到別的字模
	// —— 而 DOS 時代的 ANSI art 用的正好就是 0xB0-0xDF 那一段方框與網點。
	rs := make([]rune, len(data))
	for i, b := range data {
		rs[i] = rawRune(b)
	}
	return string(rs)
}

// rawRune 把一個位元組轉成它在字型裡的字形。
//
// 換行與 tab 例外:它們決定的是**版面**,不是字形。CP437 把 0x0A 畫成 ◙、
// 0x0D 畫成 ♪ —— 那是 DOS 時代拿它們當圖案用的情況,但這裡的位元組來自
// 一個正在被斷行的文字檔,交給斷行邏輯處理才對。0x1A 是 DOS 的檔尾記號,
// 同理。
func rawRune(b byte) rune {
	switch b {
	case '\t', '\n', '\r', 0x1A:
		return rune(b)
	}
	if r := cell.CP437[b]; r != 0 {
		return r
	}
	return ' '
}

// --- F11 全螢幕 -----------------------------------------------------------

func (a *App) toggleFullscreen() bool {
	a.Fullscreen = !a.Fullscreen
	return true
}
