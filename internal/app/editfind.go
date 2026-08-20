package app

import (
	"fmt"

	"github.com/wicanr2/wincv-remake/internal/editor"
)

// findState 是編輯器 F6 尋找/取代進行到哪裡。
//
// 原版是一個對話框一次問完(尋找什麼、換成什麼、要不要逐一確認),
// remake 只有一列輸入列,所以拆成兩段問,再用 Y/N/A 逐一確認。
type findState struct {
	pattern string
	with    string
	replace bool // 有輸入取代字串
	at      editor.Pos
}

func (a *App) startEditFind() bool {
	a.ask("尋找:", a.editFind.pattern, func(pat string) {
		if pat == "" {
			return
		}
		a.ask("取代為(直接按 Enter = 只尋找):", "", func(with string) {
			a.editFind = findState{
				pattern: pat,
				with:    with,
				replace: with != "",
				at:      a.Editor.Cur,
			}
			a.editFindNext(false)
		})
	})
	return true
}

// editFindNext 找下一處。fromCursor 為 true 時從游標往下找,
// 否則從上次停的地方接下去。
func (a *App) editFindNext(fromCursor bool) {
	f := &a.editFind
	if f.pattern == "" {
		a.Message = "還沒設定要找什麼(F6)"
		return
	}
	from := f.at
	if fromCursor {
		from = a.Editor.Cur
	}
	at, ok := a.Editor.Find(f.pattern, from, false)
	if !ok {
		a.Message = fmt.Sprintf("找不到 %q(已到檔尾)", f.pattern)
		f.at = editor.Pos{}
		return
	}
	a.Editor.MoveTo(at)
	f.at = editor.Pos{Line: at.Line, Col: at.Col + len([]rune(f.pattern))}

	if !f.replace {
		return
	}
	a.confirm(fmt.Sprintf("取代成 %q?", f.with), true, func(yes, all bool) {
		switch {
		case all && yes:
			// 從這一處開始,剩下的全換掉
			n := a.Editor.ReplaceAll(f.pattern, f.with, false)
			a.Message = fmt.Sprintf("已取代 %d 處", n)
			f.at = editor.Pos{}
		case all: // Esc:停在這裡,不再問
			a.Message = "已停止取代"
		case yes:
			a.Editor.Replace(at, len([]rune(f.pattern)), f.with)
			f.at = editor.Pos{Line: at.Line, Col: at.Col + len([]rune(f.with))}
			a.editFindNext(false)
		default:
			a.editFindNext(false)
		}
	})
}

// editComment 註解/取消註解目前行,或整個行區塊。
func (a *App) editComment() bool {
	e := a.Editor
	top, bot := e.Cur.Line, e.Cur.Line
	if e.Block.Kind != editor.BlockNone {
		t, b, _, _ := e.Block.Norm()
		top, bot = t, b
	}
	if !e.CommentLines(top, bot) {
		if e.Syntax == nil {
			a.Message = "這個副檔名沒有語法設定,不知道註解記號是什麼"
		} else {
			a.Message = e.Syntax.Name + " 沒有定義行註解記號"
		}
	}
	return true
}
