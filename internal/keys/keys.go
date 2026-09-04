// Package keys 是與後端無關的按鍵表示。
//
// app 層依賴這個而不是 Ebiten,所以按鍵行為可以完全用測試驅動,
// 不需要開視窗、不需要合成 X11 事件。
package keys

import (
	"fmt"
	"github.com/wicanr2/wincv-remake/internal/i18n"
	"strings"
)

// Code 是具名按鍵。一般字元用 Rune 帶。
type Code int

const (
	Rune Code = iota
	Up
	Down
	Left
	Right
	PgUp
	PgDn
	Home
	End
	Enter
	Backspace
	Esc
	Delete
	Insert
	Tab
	F1
	F2
	F3
	F4
	F5
	F6
	F7
	F8
	F9
	F10
	F11
	F12
)

var codeNames = map[Code]string{
	Up: "Up", Down: "Down", Left: "Left", Right: "Right",
	PgUp: "PgUp", PgDn: "PgDn", Home: "Home", End: "End",
	Enter: "Enter", Backspace: "BackSpace", Esc: "Esc",
	Delete: "Del", Insert: "Ins", Tab: "Tab",
	F1: "F1", F2: "F2", F3: "F3", F4: "F4", F5: "F5", F6: "F6",
	F7: "F7", F8: "F8", F9: "F9", F10: "F10", F11: "F11", F12: "F12",
}

// Key 是一次按鍵事件。
type Key struct {
	Code  Code
	R     rune // Code == Rune 時有意義
	Alt   bool
	Ctrl  bool
	Shift bool
}

// Ch 造一個純字元按鍵。
func Ch(r rune) Key { return Key{Code: Rune, R: r} }

// Alt / Ctrl 造帶修飾鍵的字元按鍵。
func AltCh(r rune) Key  { return Key{Code: Rune, R: r, Alt: true} }
func CtrlCh(r rune) Key { return Key{Code: Rune, R: r, Ctrl: true} }

// Named 造一個具名按鍵。
func Named(c Code) Key { return Key{Code: c} }

// String 給出人看得懂的表示,格式與 docs/ui/keymap.md 一致。
func (k Key) String() string {
	var sb strings.Builder
	if k.Ctrl {
		sb.WriteString("Ctrl-")
	}
	if k.Alt {
		sb.WriteString("Alt-")
	}
	if k.Shift && k.Code != Rune {
		sb.WriteString("Shift-")
	}
	if k.Code == Rune {
		sb.WriteRune(k.R)
	} else if n, ok := codeNames[k.Code]; ok {
		sb.WriteString(n)
	} else {
		sb.WriteString("?")
	}
	return sb.String()
}

// Letter 回傳這個按鍵對應的英文字母(大寫),不是字母就回 0。
//
// 原版的單鍵指令(C 拷貝、M 移動…)不分大小寫,所以比對時一律轉大寫,
// 不要在每個 case 裡寫兩份。
func (k Key) Letter() rune {
	if k.Code != Rune {
		return 0
	}
	r := k.R
	if r >= 'a' && r <= 'z' {
		r -= 'a' - 'A'
	}
	if r >= 'A' && r <= 'Z' {
		return r
	}
	return 0
}

// Parse 把 "F1"、"C-o"、"A-e"、"Down"、"a" 這種寫法變成 Key。
//
// 給 cmd/celldump 用:要在沒有視窗的情況下把畫面推到某個狀態再截圖,
// 就得有辦法用文字描述按鍵。順帶讓 keymap 文件裡的寫法可以直接餵進來。
func Parse(s string) (Key, bool) {
	k := Key{}
	// 兩種寫法都吃:短的("C-o")打起來快,長的("Ctrl-O")是 String()
	// 的輸出,也是 docs/ui/keymap.md 裡的寫法,要能直接貼回來。
	for {
		switch {
		case strings.HasPrefix(s, "Ctrl-"):
			k.Ctrl, s = true, s[5:]
		case strings.HasPrefix(s, "Alt-"):
			k.Alt, s = true, s[4:]
		case strings.HasPrefix(s, "Shift-"):
			k.Shift, s = true, s[6:]
		case strings.HasPrefix(s, "C-"):
			k.Ctrl, s = true, s[2:]
		case strings.HasPrefix(s, "A-"):
			k.Alt, s = true, s[2:]
		case strings.HasPrefix(s, "S-"):
			k.Shift, s = true, s[2:]
		default:
			goto done
		}
	}
done:
	if s == "" {
		return k, false
	}
	if s == "Space" {
		k.Code, k.R = Rune, ' '
		return k, true
	}
	for c, n := range codeNames {
		if n == s {
			k.Code = c
			return k, true
		}
	}
	r := []rune(s)
	if len(r) != 1 {
		return k, false
	}
	k.Code, k.R = Rune, r[0]
	return k, true
}

// ParseAll 解析以逗號分隔的一串按鍵。
func ParseAll(s string) ([]Key, error) {
	var out []Key
	if s == "" {
		return nil, nil
	}
	for _, part := range strings.Split(s, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		k, ok := Parse(part)
		if !ok {
			return nil, fmt.Errorf(i18n.T("看不懂的按鍵: %q"), part)
		}
		out = append(out, k)
	}
	return out, nil
}
