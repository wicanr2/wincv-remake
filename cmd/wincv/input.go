package main

import (
	"image"
	"time"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/inpututil"
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
