package app

import (
	"github.com/wicanr2/wincv-remake/internal/cell"
	"github.com/wicanr2/wincv-remake/internal/keys"
)

// prompt 是畫面底部的一行輸入或詢問。
//
// 原版用的是真正的對話框(Win32 視窗),remake 全部自繪,所以改成
// 佔一列的輸入列。功能等價:要嘛輸入一串字,要嘛回答是/否/全部。
type prompt struct {
	active bool
	title  string

	// 輸入模式
	input  []rune
	caret  int
	onDone func(string)

	// 詢問模式(是 / 否 / 全部)
	askAll  bool // 顯示「全部」這個選項
	onAnswer func(yes, all bool)
}

// Prompting 回傳現在是不是在等使用者輸入。
func (a *App) Prompting() bool { return a.prompt.active }

// ask 開一個輸入列。
func (a *App) ask(title, initial string, onDone func(string)) {
	a.prompt = prompt{
		active: true,
		title:  title,
		input:  []rune(initial),
		caret:  len([]rune(initial)),
		onDone: onDone,
	}
}

// confirm 開一個是/否詢問。all 為 true 時多一個「全部」選項。
func (a *App) confirm(title string, all bool, onAnswer func(yes, all bool)) {
	a.prompt = prompt{
		active:   true,
		title:    title,
		askAll:   all,
		onAnswer: onAnswer,
	}
}

func (a *App) closePrompt() { a.prompt = prompt{} }

// promptKey 處理輸入列的按鍵。回傳是否吃掉了這一顆。
func (a *App) promptKey(k keys.Key) bool {
	p := &a.prompt

	if p.onAnswer != nil {
		switch k.Letter() {
		case 'Y':
			fn := p.onAnswer
			a.closePrompt()
			fn(true, false)
			return true
		case 'N':
			fn := p.onAnswer
			a.closePrompt()
			fn(false, false)
			return true
		case 'A':
			if p.askAll {
				fn := p.onAnswer
				a.closePrompt()
				fn(true, true)
				return true
			}
		}
		if k.Code == keys.Esc {
			fn := p.onAnswer
			a.closePrompt()
			fn(false, true) // Esc = 全部都不要,不要一個一個問下去
			return true
		}
		return true // 詢問中,其他鍵一律吃掉
	}

	switch k.Code {
	case keys.Enter:
		fn, v := p.onDone, string(p.input)
		a.closePrompt()
		if fn != nil {
			fn(v)
		}
		return true
	case keys.Esc:
		a.closePrompt()
		return true
	case keys.Backspace:
		if p.caret > 0 {
			p.input = append(p.input[:p.caret-1], p.input[p.caret:]...)
			p.caret--
		}
		return true
	case keys.Delete:
		if p.caret < len(p.input) {
			p.input = append(p.input[:p.caret], p.input[p.caret+1:]...)
		}
		return true
	case keys.Left:
		if p.caret > 0 {
			p.caret--
		}
		return true
	case keys.Right:
		if p.caret < len(p.input) {
			p.caret++
		}
		return true
	case keys.Home:
		p.caret = 0
		return true
	case keys.End:
		p.caret = len(p.input)
		return true
	}

	if k.Code == keys.Rune && k.R >= 0x20 && !k.Ctrl && !k.Alt {
		p.input = append(p.input, 0)
		copy(p.input[p.caret+1:], p.input[p.caret:])
		p.input[p.caret] = k.R
		p.caret++
		return true
	}
	return true // 輸入中,其他鍵一律吃掉
}

// drawPrompt 畫輸入列。佔最後一列,蓋掉狀態列。
func (a *App) drawPrompt(s *cell.Screen) {
	p := &a.prompt
	y := s.Rows - 1
	s.Fill(0, y, s.Cols, 1, ' ', cell.Black, cell.LtCyan)

	x := s.Print(0, y, p.title, cell.Black, cell.LtCyan)
	if p.onAnswer != nil {
		opts := "  Y 是   N 否"
		if p.askAll {
			opts += "   A 全部"
		}
		s.Print(x, y, opts, cell.Blue, cell.LtCyan)
		return
	}

	x++
	s.Print(x, y, string(p.input), cell.Black, cell.LtCyan)
	// 游標
	cx := x
	for _, r := range p.input[:p.caret] {
		if cell.IsWide(r) {
			cx += 2
		} else {
			cx++
		}
	}
	if c := s.At(cx, y); c != nil {
		c.FG, c.BG = c.BG, c.FG
	}
}
