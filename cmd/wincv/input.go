package main

import (
	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/inpututil"

	"github.com/wicanr2/wincv-remake/internal/keys"
)

// named 是 Ebiten 具名鍵到 keys.Code 的對照。
var named = map[ebiten.Key]keys.Code{
	ebiten.KeyArrowUp:    keys.Up,
	ebiten.KeyArrowDown:  keys.Down,
	ebiten.KeyArrowLeft:  keys.Left,
	ebiten.KeyArrowRight: keys.Right,
	ebiten.KeyPageUp:     keys.PgUp,
	ebiten.KeyPageDown:   keys.PgDn,
	ebiten.KeyHome:       keys.Home,
	ebiten.KeyEnd:        keys.End,
	ebiten.KeyEnter:      keys.Enter,
	ebiten.KeyNumpadEnter: keys.Enter,
	ebiten.KeyBackspace:  keys.Backspace,
	ebiten.KeyEscape:     keys.Esc,
	ebiten.KeyDelete:     keys.Delete,
	ebiten.KeyInsert:     keys.Insert,
	ebiten.KeyTab:        keys.Tab,
	ebiten.KeyF1:         keys.F1,
	ebiten.KeyF2:         keys.F2,
	ebiten.KeyF3:         keys.F3,
	ebiten.KeyF4:         keys.F4,
	ebiten.KeyF5:         keys.F5,
	ebiten.KeyF6:         keys.F6,
	ebiten.KeyF7:         keys.F7,
	ebiten.KeyF8:         keys.F8,
	ebiten.KeyF9:         keys.F9,
	ebiten.KeyF10:        keys.F10,
	ebiten.KeyF11:        keys.F11,
	ebiten.KeyF12:        keys.F12,
}

// translate 把這一幀剛按下的鍵翻成 keys.Key。
//
// 不用 AppendInputChars 取字元:那條路在按著 Ctrl 或 Alt 時不會產生字元,
// 而原版大量使用 Alt-x / Ctrl-x 指令。改成直接看實體鍵 + 修飾鍵狀態。
func translate() []keys.Key {
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
		if r, ok := runeOf(ek); ok {
			out = append(out, keys.Key{Code: keys.Rune, R: r, Alt: alt, Ctrl: ctrl, Shift: shift})
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
