package ttf

import (
	"github.com/wicanr2/wincv-remake/internal/cell"
	"github.com/wicanr2/wincv-remake/internal/fnt"
)

// HalfFont 用一份 TrueType 現場產出半形字模,實作 render.HalfSource。
//
// 什麼時候需要:原版的 cvga.fon 是隨安裝檔散布的第三方檔案,
// Android 版不能打包進 APK,而沒有半形字模的話整個畫面一個字都畫不出來。
// 拿系統字型頂上,使用者之後再匯入原版字型換成點陣像素對齊的版本。
//
// 字碼走 CP437(與原版的 .FON 相同),所以上層完全不知道換過來源。
type HalfFont struct {
	f    *Font
	w, h int
}

// NewHalf 包一份已經載好的 TrueType。
//
// 傳進來的 Font 的 halfW 必須等於 w —— 半形字型是定寬的,
// 寬度不一致的話格點會歪掉,而歪掉的症狀是「字距怪怪的」,
// 不像設定錯誤。
func NewHalf(f *Font, w, h int) *HalfFont {
	return &HalfFont{f: f, w: w, h: h}
}

// LoadHalf 從候選字型裡挑一份能用的產半形字模。
//
// 回傳的第三個值是「檔案在、但載不起來」的原因,呼叫端要印出來 ——
// 沒裝字型與裝了卻載不動,症狀都只是「畫面一片空白」。
func LoadHalf(w, h int) (*HalfFont, string, []error) {
	f, path, errs := FindAndLoad(w, w*2, h)
	if f == nil {
		return nil, "", errs
	}
	return NewHalf(f, w, h), path, errs
}

func (h *HalfFont) Size() (int, int) { return h.w, h.h }

func (h *HalfFont) Glyph(code byte) *fnt.Glyph {
	r := cell.CP437[code]
	if r == 0 {
		return nil
	}
	return h.f.Glyph(r)
}
