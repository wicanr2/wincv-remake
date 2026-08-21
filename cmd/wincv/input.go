package main

import (
	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/inpututil"

	"github.com/wicanr2/wincv-remake/internal/keys"
)

// named 是 Ebiten 具名鍵到 keys.Code 的對照。
var named = map[ebiten.Key]keys.Code{
	ebiten.KeyArrowUp:     keys.Up,
	ebiten.KeyArrowDown:   keys.Down,
	ebiten.KeyArrowLeft:   keys.Left,
	ebiten.KeyArrowRight:  keys.Right,
	ebiten.KeyPageUp:      keys.PgUp,
	ebiten.KeyPageDown:    keys.PgDn,
	ebiten.KeyHome:        keys.Home,
	ebiten.KeyEnd:         keys.End,
	ebiten.KeyEnter:       keys.Enter,
	ebiten.KeyNumpadEnter: keys.Enter,
	ebiten.KeyBackspace:   keys.Backspace,
	ebiten.KeyEscape:      keys.Esc,
	ebiten.KeyDelete:      keys.Delete,
	ebiten.KeyInsert:      keys.Insert,
	ebiten.KeyTab:         keys.Tab,
	ebiten.KeyF1:          keys.F1,
	ebiten.KeyF2:          keys.F2,
	ebiten.KeyF3:          keys.F3,
	ebiten.KeyF4:          keys.F4,
	ebiten.KeyF5:          keys.F5,
	ebiten.KeyF6:          keys.F6,
	ebiten.KeyF7:          keys.F7,
	ebiten.KeyF8:          keys.F8,
	ebiten.KeyF9:          keys.F9,
	ebiten.KeyF10:         keys.F10,
	ebiten.KeyF11:         keys.F11,
	ebiten.KeyF12:         keys.F12,
}

// translate 把這一幀剛按下的鍵翻成 keys.Key。
//
// 兩條路並用:
//   - 指令鍵(含 Alt-x / Ctrl-x)看**實體鍵** + 修飾鍵狀態。按著 Ctrl 或 Alt
//     時作業系統不會產生字元,只看字元事件就收不到那些指令。
//   - 打字看**字元事件**(AppendInputChars)。實體鍵給不出 shift 後的符號,
//     也給不出輸入法組出來的中文。
//
// textMode 為 true 時(編輯器)才收字元事件,否則主畫面的單鍵指令
// 會被同一個按鍵送出兩次。
func translate(textMode bool) []keys.Key {
	var out []keys.Key
	alt := ebiten.IsKeyPressed(ebiten.KeyAlt) ||
		ebiten.IsKeyPressed(ebiten.KeyAltLeft) ||
		ebiten.IsKeyPressed(ebiten.KeyAltRight)
	ctrl := ebiten.IsKeyPressed(ebiten.KeyControl) ||
		ebiten.IsKeyPressed(ebiten.KeyControlLeft) ||
		ebiten.IsKeyPressed(ebiten.KeyControlRight)
	shift := ebiten.IsKeyPressed(ebiten.KeyShift) ||
		ebiten.IsKeyPressed(ebiten.KeyShiftLeft) ||
		ebiten.IsKeyPressed(ebiten.KeyShiftRight)

	for _, ek := range inpututil.AppendJustPressedKeys(nil) {
		if c, ok := named[ek]; ok {
			out = append(out, keys.Key{Code: c, Alt: alt, Ctrl: ctrl, Shift: shift})
			continue
		}
		if textMode && !alt && !ctrl {
			continue // 這一顆交給下面的字元事件處理
		}
		if r, ok := runeOf(ek); ok {
			out = append(out, keys.Key{Code: keys.Rune, R: r, Alt: alt, Ctrl: ctrl, Shift: shift})
		}
	}
	if textMode && !alt && !ctrl {
		for _, r := range ebiten.AppendInputChars(nil) {
			if r >= 0x20 && r != 0x7F {
				out = append(out, keys.Key{Code: keys.Rune, R: r, Shift: shift})
			}
		}
	}
	return out
}

func runeOf(ek ebiten.Key) (rune, bool) {
	switch {
	case ek >= ebiten.KeyA && ek <= ebiten.KeyZ:
		return rune('A' + (ek - ebiten.KeyA)), true
	case ek >= ebiten.KeyDigit0 && ek <= ebiten.KeyDigit9:
		return rune('0' + (ek - ebiten.KeyDigit0)), true
	case ek == ebiten.KeySpace:
		return ' ', true
	case ek == ebiten.KeySemicolon:
		return ';', true
	case ek == ebiten.KeySlash:
		return '/', true
	case ek == ebiten.KeyBackslash:
		return '\\', true
	case ek == ebiten.KeyPeriod:
		return '.', true
	case ek == ebiten.KeyComma:
		return ',', true
	case ek == ebiten.KeyMinus:
		return '-', true
	case ek == ebiten.KeyEqual:
		return '=', true
	}
	return 0, false
}
