// Package cjk24 定義 WinCV 可持久化的 24 點 CJK 字形來源代號。
//
// JSON 只存短代號，不存使用者電腦的絕對路徑；實際檔案位置仍由
// datadir 與 WINCV_HOME 決定，設定才可隨資料目錄與裝置搬移。
package cjk24

// Source 是一種可選的 24×24 漢字與符號字模來源。
type Source struct {
	ID  string
	Std string
	Spc string
	// Alts 是同一套字形在其他版本裡的檔名,依序在 Std 找不到時才找。
	// 倚天 3.53 叫 STD.24M,倚天 2000 (5.0) 叫 STDFONT.24;內容一樣是
	// 24×24 字模,只是 2000 版沒有用 ETUNPACK 壓縮,載入端兩種都認。
	Alts []string
}

// StdNames 依尋找順序列出漢字區的候選檔名。
func (s Source) StdNames() []string {
	return append([]string{s.Std}, s.Alts...)
}

// Default 是舊版自動尋找的倚天明體，舊 session 沒有欄位時以此相容。
const Default = "eten-m"

var sources = []Source{
	{ID: "eten-m", Std: "STD.24M", Spc: "SPCFONT.24", Alts: []string{"STDFONT.24", "STDFONT.24M"}},
	{ID: "eten-k", Std: "STD.24K", Spc: "SPCFONT.24", Alts: []string{"STDFONT.24K"}},
	{ID: "eten-l", Std: "STD.24L", Spc: "SPCFONT.24", Alts: []string{"STDFONT.24L"}},
	{ID: "eten-r", Std: "STD.24R", Spc: "SPCFONT.24", Alts: []string{"STDFONT.24R"}},
	{ID: "eten-b", Std: "STD.24B", Spc: "SPCFONT.24", Alts: []string{"STDFONT.24B"}},
	{ID: "eten-s", Std: "STD.24S", Spc: "SPCFONT.24", Alts: []string{"STDFONT.24S"}},
	{ID: "guoqiao", Std: "stdfont.24f", Spc: "spcfont.24"},
}

// Sources 回傳可選來源的副本，呼叫端不可改動套件內的對照表。
func Sources() []Source { return append([]Source(nil), sources...) }

// Find 依持久化代號取來源。
func Find(id string) (Source, bool) {
	for _, s := range sources {
		if s.ID == id {
			return s, true
		}
	}
	return Source{}, false
}
