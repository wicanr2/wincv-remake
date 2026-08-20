// Package editor 是 PE2 式的區塊文字編輯器。
//
// PE2 的特徵是**矩形區塊**:標記的是一個矩形範圍,不是「從某個位置到
// 另一個位置」的連續文字。原版 changelog 講得很清楚:「標記寬度 5 的區塊,
// 然後以插入模式填充空白」——那是矩形才做得到的事。
//
// 區塊有兩種:
//
//	Alt-B 矩形區塊 —— 每一列取相同的欄位範圍
//	Alt-L 整列區塊 —— 整列,不分欄
//
// 兩種都能拷貝、移動、刪除、填充,行為不同,所以 Block 帶著自己的種類。
package editor

import (
	"strings"

	"github.com/wicanr2/wincv-remake/internal/cell"
	"github.com/wicanr2/wincv-remake/internal/syntax"
	"github.com/wicanr2/wincv-remake/internal/textenc"
)

// BlockKind 是區塊的種類。
type BlockKind int

const (
	BlockNone BlockKind = iota
	BlockRect
	BlockLine
)

// Pos 是緩衝區裡的一個位置。Col 以 rune 計,不是位元組也不是顯示格。
type Pos struct {
	Line, Col int
}

// Block 是目前標記的區塊。
type Block struct {
	Kind         BlockKind
	Anchor, Head Pos
}

// Norm 回傳左上與右下(含端點的列、不含端點的欄)。
func (b Block) Norm() (top, bot, left, right int) {
	top, bot = b.Anchor.Line, b.Head.Line
	if top > bot {
		top, bot = bot, top
	}
	left, right = b.Anchor.Col, b.Head.Col
	if left > right {
		left, right = right, left
	}
	return
}

// Theme 是編輯器的配色。
type Theme struct {
	FG, BG             cell.Color
	BlockFG, BlockBG   cell.Color
	StatusFG, StatusBG cell.Color
	LineNoFG           cell.Color
}

func DefaultTheme() Theme {
	return Theme{
		FG: cell.LtGray, BG: cell.Black,
		BlockFG: cell.Black, BlockBG: cell.LtGray,
		StatusFG: cell.Black, StatusBG: cell.LtGray,
		LineNoFG: cell.DkGray,
	}
}

// Model 是編輯器的狀態。
type Model struct {
	Name  string
	Enc   textenc.Enc
	EOL   string // 存檔時用的換行,載入時記下來
	Lines [][]rune

	Cur      Pos
	Top      int // 畫面第一列對應的行號
	Left     int // 水平捲動量(顯示格)
	Insert   bool
	TabWidth int
	Dirty    bool

	Block     Block
	Clipboard []string
	ClipKind  BlockKind

	Syntax *syntax.Config
	Theme  Theme

	undo []snapshot

	// 跨行註解的狀態快取,見 draw.go 的 commentStateAt。
	cmtState []bool
	cmtDirty bool
}

type snapshot struct {
	lines [][]rune
	cur   Pos
}

// Load 從位元組建立編輯緩衝區。
func Load(name string, data []byte, enc textenc.Enc, cfg *syntax.Config) *Model {
	if enc == textenc.Unknown {
		enc = textenc.Detect(data)
	}
	eol := "\n"
	if strings.Contains(string(data), "\r\n") {
		eol = "\r\n"
	}
	s := textenc.Decode(data, enc)
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "\n")
	raw := strings.Split(s, "\n")
	if n := len(raw); n > 1 && raw[n-1] == "" {
		raw = raw[:n-1]
	}
	lines := make([][]rune, len(raw))
	for i, l := range raw {
		lines[i] = []rune(l)
	}
	if len(lines) == 0 {
		lines = [][]rune{{}}
	}
	return &Model{
		Name: name, Enc: enc, EOL: eol, Lines: lines,
		Insert: true, TabWidth: 8, Syntax: cfg, Theme: DefaultTheme(),
	}
}

// Bytes 把緩衝區寫回位元組,用載入時的編碼與換行。
func (m *Model) Bytes() []byte {
	parts := make([]string, len(m.Lines))
	for i, l := range m.Lines {
		parts[i] = string(l)
	}
	s := strings.Join(parts, m.EOL) + m.EOL
	out, err := encode(s, m.Enc)
	if err != nil {
		return []byte(s)
	}
	return out
}

// --- 復原 -----------------------------------------------------------------

// MaxUndo 是保留幾步。原版有 Ctrl-U 復原,層數未知,先給一個夠用的值。
const MaxUndo = 200

func (m *Model) push() {
	cp := make([][]rune, len(m.Lines))
	for i, l := range m.Lines {
		cp[i] = append([]rune(nil), l...)
	}
	m.undo = append(m.undo, snapshot{lines: cp, cur: m.Cur})
	m.cmtDirty = true
	if len(m.undo) > MaxUndo {
		m.undo = m.undo[len(m.undo)-MaxUndo:]
	}
	m.Dirty = true
}

// Undo 回到上一步。
func (m *Model) Undo() bool {
	if len(m.undo) == 0 {
		return false
	}
	s := m.undo[len(m.undo)-1]
	m.undo = m.undo[:len(m.undo)-1]
	m.Lines, m.Cur = s.lines, s.cur
	m.cmtDirty = true
	m.clampCur()
	return true
}

// --- 游標與編輯 -----------------------------------------------------------

// MaxCol 是游標能到的最遠欄位。只是個防呆上限,不是排版限制。
const MaxCol = 4096

// clampCur 夾住游標位置。
//
// **欄位不夾到行尾**:PE2 這類 DOS 編輯器允許游標停在行尾之後的
// 「虛擬空白」上。矩形區塊要能在 3 個字的行上標出 5 格寬,靠的就是這件事;
// 「在每一行前面加上 5 個空白」那個用法也是。真的在虛擬空白處打字時,
// 才把中間補成空白(見 InsertRune)。
func (m *Model) clampCur() {
	if m.Cur.Line < 0 {
		m.Cur.Line = 0
	}
	if m.Cur.Line >= len(m.Lines) {
		m.Cur.Line = len(m.Lines) - 1
	}
	if m.Cur.Col < 0 {
		m.Cur.Col = 0
	}
	if m.Cur.Col > MaxCol {
		m.Cur.Col = MaxCol
	}
}

func (m *Model) MoveTo(p Pos) { m.Cur = p; m.clampCur() }

func (m *Model) MoveBy(dl, dc int) {
	m.Cur.Line += dl
	m.Cur.Col += dc
	m.clampCur()
}

// Insert 在游標處插入一個字。覆蓋模式下取代原有的字。
func (m *Model) InsertRune(r rune) {
	m.push()
	l := m.Lines[m.Cur.Line]
	// 游標在虛擬空白處:先把中間補成空白,才有地方放這個字。
	for len(l) < m.Cur.Col {
		l = append(l, ' ')
	}
	m.Lines[m.Cur.Line] = l
	if !m.Insert && m.Cur.Col < len(l) {
		l[m.Cur.Col] = r
		m.Cur.Col++
		return
	}
	nl := make([]rune, 0, len(l)+1)
	nl = append(nl, l[:m.Cur.Col]...)
	nl = append(nl, r)
	nl = append(nl, l[m.Cur.Col:]...)
	m.Lines[m.Cur.Line] = nl
	m.Cur.Col++
}

// NewLine 斷行。
func (m *Model) NewLine() {
	m.push()
	l := m.Lines[m.Cur.Line]
	col := m.Cur.Col
	if col > len(l) {
		col = len(l)
	}
	head := append([]rune(nil), l[:col]...)
	tail := append([]rune(nil), l[col:]...)
	m.Lines[m.Cur.Line] = head
	m.Lines = append(m.Lines, nil)
	copy(m.Lines[m.Cur.Line+2:], m.Lines[m.Cur.Line+1:])
	m.Lines[m.Cur.Line+1] = tail
	m.Cur.Line++
	m.Cur.Col = 0
}

// Backspace 刪除游標左邊一個字;在行首則與上一行合併。
func (m *Model) Backspace() {
	if m.Cur.Col == 0 && m.Cur.Line == 0 {
		return
	}
	m.push()
	if m.Cur.Col > 0 {
		l := m.Lines[m.Cur.Line]
		if m.Cur.Col > len(l) {
			// 在虛擬空白處往左退,只動游標,不動內容。
			m.Cur.Col--
			return
		}
		m.Lines[m.Cur.Line] = append(l[:m.Cur.Col-1], l[m.Cur.Col:]...)
		m.Cur.Col--
		return
	}
	prev := m.Lines[m.Cur.Line-1]
	m.Cur.Col = len(prev)
	m.Lines[m.Cur.Line-1] = append(prev, m.Lines[m.Cur.Line]...)
	m.Lines = append(m.Lines[:m.Cur.Line], m.Lines[m.Cur.Line+1:]...)
	m.Cur.Line--
}

// Delete 刪除游標所在的字;在行尾(含虛擬空白)則與下一行合併。
func (m *Model) Delete() {
	l := m.Lines[m.Cur.Line]
	if m.Cur.Col >= len(l) {
		if m.Cur.Line+1 >= len(m.Lines) {
			return
		}
		m.push()
		m.Lines[m.Cur.Line] = append(l, m.Lines[m.Cur.Line+1]...)
		m.Lines = append(m.Lines[:m.Cur.Line+1], m.Lines[m.Cur.Line+2:]...)
		return
	}
	m.push()
	m.Lines[m.Cur.Line] = append(l[:m.Cur.Col], l[m.Cur.Col+1:]...)
}

// --- 區塊 -----------------------------------------------------------------

// MarkBlock 開始或延伸一個區塊。同一種類再按一次就把終點移到游標。
func (m *Model) MarkBlock(k BlockKind) {
	if m.Block.Kind != k {
		m.Block = Block{Kind: k, Anchor: m.Cur, Head: m.Cur}
		return
	}
	m.Block.Head = m.Cur
}

// UnmarkBlock 取消區塊(原版 Alt-U)。
func (m *Model) UnmarkBlock() { m.Block = Block{} }

// blockText 取出區塊內容。矩形取每列的同一段欄位,整列取整行。
func (m *Model) blockText() []string {
	if m.Block.Kind == BlockNone {
		return nil
	}
	top, bot, left, right := m.Block.Norm()
	var out []string
	for ln := top; ln <= bot && ln < len(m.Lines); ln++ {
		l := m.Lines[ln]
		if m.Block.Kind == BlockLine {
			out = append(out, string(l))
			continue
		}
		a, b := left, right
		if a > len(l) {
			a = len(l)
		}
		if b > len(l) {
			b = len(l)
		}
		out = append(out, string(l[a:b]))
	}
	return out
}

// CopyBlock 把區塊放進剪貼區(原版 Ctrl-C / Alt-Z 的來源)。
func (m *Model) CopyBlock() bool {
	t := m.blockText()
	if t == nil {
		return false
	}
	m.Clipboard, m.ClipKind = t, m.Block.Kind
	return true
}

// DeleteBlock 刪掉區塊(原版 Alt-D)。
func (m *Model) DeleteBlock() bool {
	if m.Block.Kind == BlockNone {
		return false
	}
	m.push()
	top, bot, left, right := m.Block.Norm()
	if m.Block.Kind == BlockLine {
		if bot >= len(m.Lines) {
			bot = len(m.Lines) - 1
		}
		m.Lines = append(m.Lines[:top], m.Lines[bot+1:]...)
		if len(m.Lines) == 0 {
			m.Lines = [][]rune{{}}
		}
		m.Cur = Pos{Line: top}
	} else {
		for ln := top; ln <= bot && ln < len(m.Lines); ln++ {
			l := m.Lines[ln]
			a, b := left, right
			if a > len(l) {
				a = len(l)
			}
			if b > len(l) {
				b = len(l)
			}
			m.Lines[ln] = append(append([]rune(nil), l[:a]...), l[b:]...)
		}
		m.Cur = Pos{Line: top, Col: left}
	}
	m.UnmarkBlock()
	m.clampCur()
	return true
}

// PasteBlock 在游標處貼上(原版 Ctrl-V / Alt-Z 的插入)。
//
// 矩形區塊是**插入**而不是覆蓋:每一列在游標欄插進去,右邊的字往後推。
// 這是 PE2 的行為,也是「在每一行前面加上 5 個空白」這種用法成立的原因。
func (m *Model) PasteBlock() bool {
	if len(m.Clipboard) == 0 {
		return false
	}
	m.push()
	if m.ClipKind == BlockLine {
		ins := make([][]rune, len(m.Clipboard))
		for i, s := range m.Clipboard {
			ins[i] = []rune(s)
		}
		tail := append([][]rune(nil), m.Lines[m.Cur.Line:]...)
		m.Lines = append(m.Lines[:m.Cur.Line], append(ins, tail...)...)
		return true
	}
	col := m.Cur.Col
	for i, s := range m.Clipboard {
		ln := m.Cur.Line + i
		for ln >= len(m.Lines) {
			m.Lines = append(m.Lines, []rune{})
		}
		l := m.Lines[ln]
		for len(l) < col {
			l = append(l, ' ')
		}
		nl := append([]rune(nil), l[:col]...)
		nl = append(nl, []rune(s)...)
		nl = append(nl, l[col:]...)
		m.Lines[ln] = nl
	}
	return true
}

// FillBlock 用同一個字填滿矩形區塊(原版 Alt-F)。
// insert 為 true 時是插入而不是覆蓋 —— changelog 明講這是 0.5 版新增的選項。
func (m *Model) FillBlock(r rune, insert bool) bool {
	if m.Block.Kind != BlockRect {
		return false
	}
	m.push()
	top, bot, left, right := m.Block.Norm()
	w := right - left
	if w <= 0 {
		return false
	}
	pad := make([]rune, w)
	for i := range pad {
		pad[i] = r
	}
	for ln := top; ln <= bot && ln < len(m.Lines); ln++ {
		l := m.Lines[ln]
		for len(l) < left {
			l = append(l, ' ')
		}
		if insert {
			nl := append([]rune(nil), l[:left]...)
			nl = append(nl, pad...)
			nl = append(nl, l[left:]...)
			m.Lines[ln] = nl
			continue
		}
		for len(l) < right {
			l = append(l, ' ')
		}
		copy(l[left:right], pad)
		m.Lines[ln] = l
	}
	return true
}

// MoveBlock 把區塊搬到游標處(原版 Alt-M)。
func (m *Model) MoveBlock() bool {
	if !m.CopyBlock() {
		return false
	}
	top, _, left, _ := m.Block.Norm()
	dst := m.Cur
	m.DeleteBlock()
	// 刪掉之後,如果目標在被刪區塊的後面,位置要往前挪。
	if m.ClipKind == BlockLine && dst.Line > top {
		dst.Line -= len(m.Clipboard)
		if dst.Line < top {
			dst.Line = top
		}
	} else if m.ClipKind == BlockRect && dst.Line >= top && dst.Col > left {
		dst.Col -= len([]rune(m.Clipboard[0]))
		if dst.Col < 0 {
			dst.Col = 0
		}
	}
	m.MoveTo(dst)
	return m.PasteBlock()
}

// --- 搜尋與取代 -----------------------------------------------------------

// Find 從 from 之後找下一個 pattern,回傳位置。
func (m *Model) Find(pattern string, from Pos, caseSensitive bool) (Pos, bool) {
	if pattern == "" {
		return Pos{}, false
	}
	pat := pattern
	if !caseSensitive {
		pat = strings.ToLower(pat)
	}
	for ln := from.Line; ln < len(m.Lines); ln++ {
		s := string(m.Lines[ln])
		if !caseSensitive {
			s = strings.ToLower(s)
		}
		start := 0
		if ln == from.Line {
			start = from.Col
			if start > len(m.Lines[ln]) {
				continue
			}
			// 位元組與 rune 的位置要換算,先切 rune 再轉字串。
			s = string([]rune(s)[start:])
		}
		if i := strings.Index(s, pat); i >= 0 {
			col := len([]rune(s[:i])) + start
			return Pos{Line: ln, Col: col}, true
		}
	}
	return Pos{}, false
}

// Replace 把 at 開始的 n 個 rune 換成 with。
func (m *Model) Replace(at Pos, n int, with string) {
	if at.Line < 0 || at.Line >= len(m.Lines) {
		return
	}
	m.push()
	l := m.Lines[at.Line]
	if at.Col > len(l) {
		return
	}
	if at.Col+n > len(l) {
		n = len(l) - at.Col
	}
	nl := append([]rune(nil), l[:at.Col]...)
	nl = append(nl, []rune(with)...)
	nl = append(nl, l[at.Col+n:]...)
	m.Lines[at.Line] = nl
}

// ReplaceAll 全部取代,回傳換了幾處。
func (m *Model) ReplaceAll(pattern, with string, caseSensitive bool) int {
	if pattern == "" {
		return 0
	}
	m.push()
	n := 0
	plen := len([]rune(pattern))
	pos := Pos{}
	for {
		at, ok := m.Find(pattern, pos, caseSensitive)
		if !ok {
			break
		}
		l := m.Lines[at.Line]
		nl := append([]rune(nil), l[:at.Col]...)
		nl = append(nl, []rune(with)...)
		nl = append(nl, l[at.Col+plen:]...)
		m.Lines[at.Line] = nl
		n++
		pos = Pos{Line: at.Line, Col: at.Col + len([]rune(with))}
	}
	return n
}

// CommentLines 對 [top,bot] 這幾行加上或拿掉行註解。
//
// 註解記號取自語法設定(keyword_*.cfg 的 LineComment),沒有設定就
// 什麼都不做 —— 猜一個 "//" 上去,在 .ini 或 .asm 裡是錯的。
//
// 只要**還有一行沒被註解**就整段加註解;全部都已註解才是拿掉。
// 這樣對半註解的區塊按一次,結果是「全部註解」,語意明確。
func (m *Model) CommentLines(top, bot int) bool {
	if m.Syntax == nil || m.Syntax.LineComment == "" {
		return false
	}
	mark := m.Syntax.LineComment
	if top < 0 {
		top = 0
	}
	if bot >= len(m.Lines) {
		bot = len(m.Lines) - 1
	}
	if top > bot {
		return false
	}

	allCommented := true
	for ln := top; ln <= bot; ln++ {
		s := strings.TrimLeft(string(m.Lines[ln]), " \t")
		if s == "" {
			continue // 空行不算數
		}
		if !strings.HasPrefix(s, mark) {
			allCommented = false
			break
		}
	}

	m.push()
	for ln := top; ln <= bot; ln++ {
		l := m.Lines[ln]
		s := string(l)
		if strings.TrimSpace(s) == "" {
			continue
		}
		if allCommented {
			i := 0
			for i < len(l) && (l[i] == ' ' || l[i] == '\t') {
				i++
			}
			rest := string(l[i:])
			rest = strings.TrimPrefix(rest, mark)
			// 加註解時補的那一個空白,拿掉時也要收回來
			rest = strings.TrimPrefix(rest, " ")
			m.Lines[ln] = append(append([]rune(nil), l[:i]...), []rune(rest)...)
		} else {
			// 註解記號放在行首,不跟著縮排走 —— 縮排不同的幾行
			// 一起註解時,記號對齊比較看得出範圍。
			m.Lines[ln] = append([]rune(mark+" "), l...)
		}
	}
	m.Dirty = true
	m.cmtDirty = true
	m.clampCur()
	return true
}
