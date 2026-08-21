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

	// Cols / Rows 是視窗的格數。
	Cols int `json:"cols,omitempty"`
	Rows int `json:"rows,omitempty"`
	// Zoom 是字級索引,Scale 是放大倍率。
	Zoom  int     `json:"zoom,omitempty"`
	Scale float64 `json:"scale,omitempty"`
	// MenuBar 是選單列開著沒有。指標型別才分得出「舊檔沒存這一項」
	// 與「使用者把它關掉了」—— 這一項的預設是開,零值會反過來。
	MenuBar *bool `json:"menubar,omitempty"`

	// URL 是關掉時瀏覽模式停在哪一頁。空的表示沒在瀏覽。
	URL string `json:"url,omitempty"`
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
		return errors.New("沒有可以寫的位置")
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

// Bool 把一個 bool 包成指標,給 MenuBar 那種「分得出沒設定」的欄位用。
func Bool(v bool) *bool { return &v }
