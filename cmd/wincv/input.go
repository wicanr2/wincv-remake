package main

import (
	"image"
	"time"

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
	// 按住不放要自動重複。Ebiten 只回報「剛按下」,作業系統的鍵盤重複
	// 不會透過它送進來 —— 所以按住 ↓ 只會動一格,而清單有幾百個檔案。
	// 只對導覽鍵做:F 鍵與 Enter 重複會連開好幾層。
	for _, ek := range repeating {
		if d := inpututil.KeyPressDuration(ek); d > repeatDelay && (d-repeatDelay)%repeatEvery == 0 {
			out = append(out, keys.Key{Code: named[ek], Alt: alt, Ctrl: ctrl, Shift: shift})
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

// repeating 是按住會自動重複的鍵。
var repeating = []ebiten.Key{
	ebiten.KeyArrowUp, ebiten.KeyArrowDown, ebiten.KeyArrowLeft, ebiten.KeyArrowRight,
	ebiten.KeyPageUp, ebiten.KeyPageDown, ebiten.KeyBackspace, ebiten.KeyDelete,
}

// 重複的節奏以 tick 計(Ebiten 預設 60 tick/秒):按住 0.4 秒後開始,
// 之後每 0.05 秒一次 —— 與 Windows 預設的鍵盤重複差不多。
const (
	repeatDelay = 24
	repeatEvery = 3
)

// mouse 是滑鼠的狀態:雙擊要記上一下點在哪、什麼時候。
type mouse struct {
	lastAt   time.Time
	lastCell image.Point
	lastZone int
	wheel    float64
}

const doubleClick = 400 * time.Millisecond

// 點到的區域。
const (
	zoneContent = iota
	zoneMenuBar
	zoneMenuDrop
)

// handleMouse 把這一幀的滑鼠事件送進 app。回傳畫面有沒有變。
//
// 像素 → 格子的換算在這裡做,因為只有外殼知道選單層與內容層各用
// 什麼倍率、選單列佔幾個像素、下拉框貼在哪。app 那一層只收格子座標。
func (g *game) handleMouse() bool {
	changed := false
	if inpututil.IsMouseButtonJustPressed(ebiten.MouseButtonLeft) {
		px, py := ebiten.CursorPosition()
		zone, col, row := g.hitTest(px, py)
		now := time.Now()
		cellPt := image.Pt(col, row)
		double := zone == zoneContent && zone == g.mouse.lastZone &&
			cellPt == g.mouse.lastCell && now.Sub(g.mouse.lastAt) < doubleClick
		g.mouse.lastAt, g.mouse.lastCell, g.mouse.lastZone = now, cellPt, zone
		if double {
			// 連點兩下之後歸零,第三下不算再一次雙擊。
			g.mouse.lastAt = time.Time{}
		}
		switch zone {
		case zoneMenuBar:
			changed = g.app.MenuBarClick(col)
		case zoneMenuDrop:
			changed = g.app.MenuDropClick(row)
		default:
			changed = g.app.Click(col, row, double)
		}
	}
	// 滾輪:累積到一格才送。觸控板會送很多小數。
	if _, dy := ebiten.Wheel(); dy != 0 {
		g.mouse.wheel += dy
		for g.mouse.wheel >= 1 {
			g.mouse.wheel--
			changed = g.app.Wheel(-wheelCells) || changed
		}
		for g.mouse.wheel <= -1 {
			g.mouse.wheel++
			changed = g.app.Wheel(wheelCells) || changed
		}
	}
	return changed
}

// wheelCells 是滾輪一格捲幾列。
const wheelCells = 3

// hitTest 把螢幕像素翻成「哪一層的第幾格」。
func (g *game) hitTest(px, py int) (zone, col, row int) {
	top := g.menuBarPx()
	if top > 0 {
		_, _, mcw, mch := g.menu()
		if r := g.dropRect; !r.Empty() && image.Pt(px, py).In(r) {
			return zoneMenuDrop, (px - r.Min.X) / mcw, (py - r.Min.Y) / mch
		}
		if py < top {
			return zoneMenuBar, px / mcw, 0
		}
	}
	cw, ch := g.cellPx()
	if cw <= 0 || ch <= 0 {
		return zoneContent, 0, 0
	}
	return zoneContent, px / cw, (py - top) / ch
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
