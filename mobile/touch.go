package mobile

import (
	"github.com/hajimehoshi/ebiten/v2"
	"github.com/wicanr2/wincv-remake/internal/app"
	"github.com/wicanr2/wincv-remake/internal/keys"
)

// touchState 把觸控事件翻成 keys.Key。
//
// 翻成同一組按鍵而不是另外做一套分派:app 那一層完全不知道自己是被
// 手指還是鍵盤驅動的。這是 internal/keys 當初做成「與後端無關的表示」
// 的回報 —— Android 版沒有動 app 的任何一行分派邏輯。
type touchState struct {
	active   bool
	id       ebiten.TouchID
	x0, y0   int // 按下的位置
	lastY    int
	moved    bool
	scrolled int // 這一次拖曳已經送出幾列捲動
}

// dragCells 是「拖曳幾格算捲一列」。
//
// 1 太靈敏(手指微抖就跑掉),3 以上感覺遲鈍。實際值要在真機上調,
// 這裡先取 1 格 = 1 列 —— 格子本身已經被 scale 放大過,
// 所以一格在螢幕上是好幾十個像素。
const dragCells = 1

func (t *touchState) keys(g *game) []keys.Key {
	var out []keys.Key
	ids := ebiten.AppendTouchIDs(nil)

	if !t.active {
		if len(ids) == 0 {
			return nil
		}
		x, y := ebiten.TouchPosition(ids[0])
		t.active, t.id = true, ids[0]
		t.x0, t.y0, t.lastY = x, y, y
		t.moved, t.scrolled = false, 0
		return nil
	}

	// 還按著:看有沒有拖出捲動
	for _, id := range ids {
		if id != t.id {
			continue
		}
		x, y := ebiten.TouchPosition(id)
		_ = x
		cell := g.rast.CellH * g.scale
		if cell <= 0 {
			return nil
		}
		for t.lastY-y >= cell*dragCells {
			t.lastY -= cell * dragCells
			t.moved = true
			out = append(out, keys.Named(keys.Down))
		}
		for y-t.lastY >= cell*dragCells {
			t.lastY += cell * dragCells
			t.moved = true
			out = append(out, keys.Named(keys.Up))
		}
		return out
	}

	// 放開了
	t.active = false
	if t.moved {
		return nil // 拖曳過就不算點擊
	}
	return t.tap(g, t.x0, t.y0)
}

// tap 把一次點擊翻成按鍵。
//
// 底部兩列是觸控功能列,點到哪一格就送那一格的按鍵;
// 其餘區域點一下把游標移過去(用上下鍵走,不直接設 —— app 那一層
// 沒有「跳到第 N 列」的公開介面,而為了觸控去開一個會讓兩邊的
// 捲動與夾邊界邏輯各走一套)。
func (t *touchState) tap(g *game, px, py int) []keys.Key {
	cw := g.rast.CellW * g.scale
	ch := g.rast.CellH * g.scale
	if cw <= 0 || ch <= 0 {
		return nil
	}
	col, row := px/cw, py/ch
	if row >= g.rows-app.TouchRows {
		if k, ok := g.a.TouchKeyAt(col, row-(g.rows-app.TouchRows), g.cols); ok {
			return []keys.Key{k}
		}
		return nil
	}
	// 內容區:第 0 列是路徑列,清單從第 1 列開始
	if row < 1 {
		return nil
	}
	want := row - 1
	cur := g.a.ListCursorRow()
	if cur < 0 {
		return nil
	}
	var out []keys.Key
	for i := cur; i < want; i++ {
		out = append(out, keys.Named(keys.Down))
	}
	for i := cur; i > want; i-- {
		out = append(out, keys.Named(keys.Up))
	}
	return out
}
