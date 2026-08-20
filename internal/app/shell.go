package app

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/wicanr2/wincv-remake/internal/launch"
	"github.com/wicanr2/wincv-remake/internal/note"
	"github.com/wicanr2/wincv-remake/internal/textenc"
)

// --- P 改變路徑 -----------------------------------------------------------

func (a *App) startChangeDir() bool {
	a.ask("改變路徑:", a.Browser.Dir, func(p string) {
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
		a.Message = "壓縮檔裡的檔案要先解出來才能開啟"
		return true
	}
	e := a.Browser.Current()
	if e == nil || e.Up {
		return false
	}
	p := filepath.Join(a.Browser.Dir, e.Name)
	if err := launch.Open(p); err != nil {
		a.Message = "開不起來: " + err.Error()
		return true
	}
	a.Message = "已交給系統開啟: " + e.Name
	return true
}

// startRun 執行一行命令,預設帶入游標上的檔案。
func (a *App) startRun() bool {
	if a.readOnlyHere() {
		a.Message = "壓縮檔裡的檔案不能直接執行"
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
	a.ask("執行:", def, func(line string) {
		if strings.TrimSpace(line) == "" {
			return
		}
		if err := launch.Run(a.Browser.Dir, line); err != nil {
			a.Message = "執行失敗: " + err.Error()
			return
		}
		a.Message = "已執行: " + line
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

func (a *App) startNote() bool {
	if a.readOnlyHere() {
		a.Message = "壓縮檔裡不能加註解"
		return true
	}
	e := a.Browser.Current()
	if e == nil || e.Up {
		return false
	}
	dir, name := a.Browser.Dir, e.Name
	a.ask("註解 "+name+":", a.Browser.Notes[name], func(text string) {
		s, err := note.Load(dir)
		if err != nil {
			a.Message = "讀不到註解檔: " + err.Error()
			return
		}
		s.Set(name, text)
		if err := s.Save(dir); err != nil {
			a.Message = "寫不進註解檔: " + err.Error()
			return
		}
		a.Browser.Reload()
		if strings.TrimSpace(text) == "" {
			a.Message = "已清掉 " + name + " 的註解"
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
		a.Message = "英文顯示(一個位元組一格)"
	} else {
		a.Message = "中文顯示"
	}
	return true
}

// decodeView 依目前的中英文顯示設定把位元組轉成要畫的文字。
func (a *App) decodeView(data []byte, enc textenc.Enc) string {
	if !a.EnglishOnly {
		return textenc.Decode(data, enc)
	}
	// 每個位元組畫一格。0x00-0xFF 直接對到半形字型的字碼
	// (見 render.toCP950Byte),所以 rune(b) 就是要的東西。
	rs := make([]rune, len(data))
	for i, b := range data {
		rs[i] = rune(b)
	}
	return string(rs)
}

// --- F11 全螢幕 -----------------------------------------------------------

func (a *App) toggleFullscreen() bool {
	a.Fullscreen = !a.Fullscreen
	return true
}
