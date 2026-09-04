// Package session 記住上次關掉程式時人在哪裡。
//
// 存的是「回到原處要知道的事」,不是完整的程式狀態:目錄、游標停在
// 哪一個檔、開著哪一份文件與捲到第幾列,加上畫面的尺寸與字級。
// 不存的東西同樣重要 —— 標記、剪貼、搜尋結果、瀏覽歷史都不存:
// 那些是「這一次」的工作,下次開起來還在反而礙事。
package session

import (
	"encoding/json"
	"errors"
	"github.com/wicanr2/wincv-remake/internal/i18n"
	"os"
	"path/filepath"
)

// State 是一份工作階段快照。
//
// 欄位全部是可選的:舊版存的檔少了新欄位、新版讀到舊檔,兩邊都要能用。
// 所以每一個欄位的零值都必須是「安全的預設」,而不是「無效」。
type State struct {
	// Dir 是最後所在的目錄。
	Dir string `json:"dir,omitempty"`
	// Cursor 是游標停在哪一個檔名(不存索引 —— 目錄的內容會變)。
	Cursor string `json:"cursor,omitempty"`

	// Mode 是關掉時開著的畫面:"" / "viewer" / "hex" / "markdown" / "edit" / "image"。
	Mode string `json:"mode,omitempty"`
	// File 是那份文件的檔名(相對於 Dir)。
	File string `json:"file,omitempty"`
	// Top 是那份文件捲到第幾列。
	Top int `json:"top,omitempty"`
	// Cur 是文字檢視器的游標停在第幾行(光棒那一列)。
	// 與 Top 分開存:捲動與游標是兩件事,只存 Top 的話下次開起來
	// 光棒會跳到畫面最上面,而使用者記得的是光棒的位置。
	Cur int `json:"cur,omitempty"`

	// Cols / Rows 是視窗的格數。
	Cols int `json:"cols,omitempty"`
	Rows int `json:"rows,omitempty"`
	// Zoom 是字級索引,Scale 是放大倍率。
	Zoom  int     `json:"zoom,omitempty"`
	Scale float64 `json:"scale,omitempty"`
	// MenuZoom 是選單那一層用第幾級字。-1(跟著內容)是預設,
	// 而零值 0 是「第一級」—— 兩者不同,所以用指標分辨「沒存過」。
	MenuZoom *int `json:"menuzoom,omitempty"`
	// MenuBar 是選單列開著沒有。指標型別才分得出「舊檔沒存這一項」
	// 與「使用者把它關掉了」—— 這一項的預設是開,零值會反過來。
	MenuBar *bool `json:"menubar,omitempty"`

	// Positions 是「每個檔案上次看到哪」:鍵是完整路徑。原版 0.5x 起
	// 有這個功能(「自動記錄上次看檔、編輯位置,下次看或編同一檔時自動
	// 回到上次的位置」)。上限見 MaxPositions,滿了丟最久沒用的。
	Positions map[string]DocPos `json:"positions,omitempty"`
	// Lang 是介面語言(BCP 47 標籤)。空的表示沒選過 —— 那時要看系統
	// 語系,而不是套一個預設值:「沒選過」與「選了繁中」是兩件事,
	// 前者會跟著系統走,後者不會。
	Lang string `json:"lang,omitempty"`
	// NameW 是檔案清單主檔名欄的寬度,0 表示預設。
	NameW int `json:"namew,omitempty"`
	// URL 是關掉時瀏覽模式停在哪一頁。空的表示沒在瀏覽。
	URL string `json:"url,omitempty"`
}

// DocPos 是一份文件的位置。Top 是捲到第幾列;Line / Col 只有編輯器用
// (游標所在),看檔模式留 0。At 是最後一次用的時間(Unix 秒),淘汰用。
type DocPos struct {
	Top  int   `json:"top"`
	Line int   `json:"line,omitempty"`
	Col  int   `json:"col,omitempty"`
	At   int64 `json:"at"`
}

// MaxPositions 是逐檔位置表的上限。500 份文件夠用好幾年,而 JSON 仍然
// 只有幾十 KB;無上限的話這個檔會跟著使用者看過的每一個檔案一直長。
const MaxPositions = 500

// Remember 記下一份文件的位置,滿了就丟最久沒用的。
func (s *State) Remember(key string, p DocPos) {
	if key == "" {
		return
	}
	if s.Positions == nil {
		s.Positions = map[string]DocPos{}
	}
	s.Positions[key] = p
	for len(s.Positions) > MaxPositions {
		oldest, oldAt := "", int64(0)
		for k, v := range s.Positions {
			if oldest == "" || v.At < oldAt {
				oldest, oldAt = k, v.At
			}
		}
		delete(s.Positions, oldest)
	}
}

// Path 是狀態檔的位置。
//
// 走 os.UserConfigDir(Linux 上是 $XDG_CONFIG_HOME 或 ~/.config)。
// 取不到就退回工作目錄底下的隱藏檔 —— Android 與某些精簡環境沒有
// 這些變數,而「存不了」不該讓程式少一個功能。
func Path() string {
	if dir, err := os.UserConfigDir(); err == nil && dir != "" {
		return filepath.Join(dir, "wincv", "session.json")
	}
	if dir, err := os.UserHomeDir(); err == nil && dir != "" {
		return filepath.Join(dir, ".wincv-session.json")
	}
	return ".wincv-session.json"
}

// Load 讀回上次的狀態。
//
// 檔案不在、壞掉、或是空的都回零值加 nil 錯誤:這是「還原上次的位置」
// 這種錦上添花的功能,任何一種失敗都只該讓程式從頭開始,
// 不該變成一句使用者看不懂的錯誤訊息。
func Load() State {
	return LoadFrom(Path())
}

// LoadFrom 從指定路徑讀。測試用。
func LoadFrom(path string) State {
	b, err := os.ReadFile(path)
	if err != nil {
		return State{}
	}
	var s State
	if err := json.Unmarshal(b, &s); err != nil {
		return State{}
	}
	return s
}

// Save 把狀態寫回去。
func (s State) Save() error { return s.SaveTo(Path()) }

// SaveTo 寫到指定路徑。測試用。
//
// 先寫暫存檔再改名:程式關掉的那一刻正是最容易被中斷的時候,
// 而寫到一半的 JSON 讀回來是壞的 —— 那會讓「上次的位置」永久消失,
// 而不是這一次沒存到。
func (s State) SaveTo(path string) error {
	if path == "" {
		return errors.New(i18n.T("沒有可以寫的位置"))
	}
	if dir := filepath.Dir(path); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}
	b, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, append(b, '\n'), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// Int 把一個 int 包成指標,給 MenuZoom 那種「零值有意義」的欄位用。
func Int(v int) *int { return &v }

// Bool 把一個 bool 包成指標,給 MenuBar 那種「分得出沒設定」的欄位用。
func Bool(v bool) *bool { return &v }
