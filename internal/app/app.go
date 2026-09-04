// Package app 把各個模式接起來:檔案瀏覽器、文字檢視器,
// 以及它們之間的切換與按鍵分派。
//
// 這一層不依賴 Ebiten,所以整個互動流程可以 headless 測。
package app

import (
	"fmt"
	"github.com/wicanr2/wincv-remake/internal/i18n"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/wicanr2/wincv-remake/internal/archive"
	"github.com/wicanr2/wincv-remake/internal/browser"
	"github.com/wicanr2/wincv-remake/internal/cell"
	"github.com/wicanr2/wincv-remake/internal/dict"
	"github.com/wicanr2/wincv-remake/internal/editor"
	"github.com/wicanr2/wincv-remake/internal/epub"
	"github.com/wicanr2/wincv-remake/internal/fileop"
	"github.com/wicanr2/wincv-remake/internal/gopher"
	"github.com/wicanr2/wincv-remake/internal/hexview"
	"github.com/wicanr2/wincv-remake/internal/imgfmt"
	"github.com/wicanr2/wincv-remake/internal/imgview"
	"github.com/wicanr2/wincv-remake/internal/keys"
	"github.com/wicanr2/wincv-remake/internal/officedoc"
	"github.com/wicanr2/wincv-remake/internal/pdfdoc"
	"github.com/wicanr2/wincv-remake/internal/render"
	"github.com/wicanr2/wincv-remake/internal/session"
	"github.com/wicanr2/wincv-remake/internal/syntax"
	"github.com/wicanr2/wincv-remake/internal/textenc"
	"github.com/wicanr2/wincv-remake/internal/thumbs"
	"github.com/wicanr2/wincv-remake/internal/vfs"
	"github.com/wicanr2/wincv-remake/internal/viewer"
	"github.com/wicanr2/wincv-remake/internal/web"
)

// Mode 是目前在哪一個畫面。
type Mode int

const (
	ModeBrowser Mode = iota
	ModeViewer
	ModeHex
	ModeImage
	ModeEdit
	ModeThumbs
	ModeFind
	ModeMarkdown
	ModeBrowse
)

// MaxViewBytes 是檢視器一次讀進來的上限。
// 原版能順順地看大檔;先設一個保守上限,之後改成串流。
const MaxViewBytes = 64 << 20

// App 是整個程式的狀態。
type App struct {
	FS      vfs.FS
	Browser *browser.Model
	Viewer  *viewer.Model
	Hex     *hexview.Model
	Image   *imgview.Model
	Editor  *editor.Model
	Thumbs  *thumbs.Model
	Find    *FindResult
	Mode    Mode

	// Syntax 是語法上色設定,從原版的 keyword.cfg 載入。可為 nil。
	Syntax *syntax.Set

	// DictDir 是字典資料所在的目錄。ShowDict 打開時才會真的載入 ——
	// eng.txt.dat 有 5.6 MB,不該在啟動時就吃掉那個時間。
	DictDir  string
	ShowDict bool
	dict     *dict.Dict
	dictErr  string

	// CellW / CellH 是格子的像素尺寸。看圖模式要用它把格點座標
	// 換算成像素矩形;外層(cmd/wincv 或 celldump)要在建立後設好。
	CellW, CellH int

	// Message 是暫時顯示在狀態列的訊息(錯誤、提示)。
	Message string

	// Quit 被設成 true 時,外層應該結束程式。
	Quit bool

	// Fullscreen 是 F11 的狀態。app 這一層不會自己去改視窗,
	// 外層(cmd/wincv)每一幀比對這個旗標再呼叫 Ebiten。
	Fullscreen bool

	// EnglishOnly 是 F8 的狀態:關掉中文解讀,一個位元組畫一格。
	EnglishOnly bool

	// DriveFocus 是「焦點在左側磁碟窗格」。窗格關著時永遠是 false。
	DriveFocus bool

	// Touch 開啟底部的觸控功能列(Android 版的介面草案,見 touch.go)。
	Touch bool

	// MenuBar 開啟最上方那一列選單列(見 menu.go)。預設開著。
	MenuBar bool

	// MenuZoom 是選單那一層要用第幾個字級。-1 表示跟內容一樣。
	//
	// 與 Zoom 分開的理由:內容要的是「與原版逐像素對齊的點陣字」,
	// 那是這個 remake 存在的理由之一;選單只是介面,在高解析度螢幕上
	// 大一級反而好認。硬綁在一起就得二選一。
	MenuZoom int

	// ShowPreview 是 Alt-P 的狀態:畫面底部留一塊,顯示游標所在檔案的開頭。
	ShowPreview bool
	prev        preview

	// Zoom 是字級索引,MaxZoom 是最大值(由外層設定,因為有幾級取決於
	// 載到了幾種字型)。app 這一層只管索引,實際換字型是視窗層的事。
	Zoom, MaxZoom int

	// Scale 是放大倍率,以 0.1 為一階。與 Zoom 是兩件事:
	// Zoom 換的是點陣字本身,Scale 只是把同一份字模放大。
	Scale float64

	// WantCols / WantRows 是「請視窗層把視窗調成這麼多格」的請求。
	// 0 表示沒有請求。視窗層處理完要自己歸零。
	WantCols, WantRows int

	// rows 是最近一次繪製時內容區的列數,按鍵處理要用來算翻頁。
	rows int
	// thumbCols 是最近一次繪製時的欄數,縮圖列表算格位要用。
	thumbCols int
	// Lang 是目前的介面語言(BCP 47 標籤)。空的表示還沒決定過,
	// 由外殼在啟動時填 —— app 這一層不去讀環境變數,那是平台的事。
	Lang string
	// positions 是逐檔的位置記憶(見 session.DocPos):開檔時查、
	// 每次按鍵之後寫。存在 App 上而不是每次去讀檔,Snapshot 時一起帶出去。
	positions map[string]session.DocPos
	// now 是拿時間用的,測試可以換掉。
	now func() int64
	// screenCols / screenRows 是最近一次 Draw 拿到的整個畫面大小
	//(含觸控功能列)。Click 要靠它們知道點到的是不是功能列。
	screenCols, screenRows int
	// prompt 是畫面底部的輸入列,見 prompt.go。
	prompt prompt
	// menu 是選單列與它的下拉,見 menu.go。
	menu menu
	// about 是「關於」畫面,見 about.go。
	about bool
	// md 是 markdown 檢視模式,見 mdview.go。
	md mdView
	// Gopher / Web 是兩個協定的客戶端。nil 表示用預設設定。
	// 做成欄位是為了測試能換掉撥號函式,不必真的連外。
	Gopher *gopher.Client
	Web    *web.Client

	bv browseView
	// book 是瀏覽模式正開著的一本 EPUB,bookPath 是它的路徑。
	// 開著不關是為了翻頁 —— 換一節就重解一次 zip 會讓翻頁變成等待。
	book     *epub.Book
	bookPath string
	// pdf 是瀏覽模式正開著的一份 PDF。同上,開著不關是為了翻頁。
	pdf     *pdfdoc.Doc
	pdfPath string
	// office 是瀏覽模式正開著的一份 Word / PowerPoint / Excel 文件。
	office     *officedoc.Doc
	officePath string
	gpending   chan browseResult
	// gReturn 與 mdReturn 同理:看圖是從瀏覽模式進來的,Esc 要退回去。
	gReturn bool

	// mdReturn 記著「看圖是從 markdown 進來的」,Esc 要退回 markdown
	// 而不是退回檔案清單。
	mdReturn bool
	// editFind 是編輯器 F6 的尋找/取代狀態,見 editfind.go。
	editFind findState
	// findKindPending / convertPending 是「選單那一步」的暫存狀態:
	// 這兩個功能都要先問「做哪一種」再問細節,而輸入列一次只能問一件事。
	findKindPending bool
	convertPending  []string

	// viewRaw 留著原始解碼文字,切換 ANSI 時要重新解析。
	viewRaw string
	// viewData 是目前檢視中的原始位元組,文字與 16 進位之間切換要用。
	viewData []byte

	// stack 記錄「進入壓縮檔之前在哪」。進壓縮檔時 push,
	// 從壓縮檔最上層再往上時 pop —— 使用者感覺就只是進出目錄。
	stack []layer
}

type layer struct {
	fs     vfs.FS
	dir    string
	cursor int
}

func New(fsys vfs.FS, dir string) *App {
	a := &App{FS: fsys, Browser: browser.New(fsys, dir), CellW: 8, CellH: 15,
		Scale: 1, MenuBar: true, MenuZoom: -1}
	a.Browser.NoteLoader = a.loadNotes
	a.Browser.DiskStat = a.diskStat
	a.Browser.ColorOf = fileColor
	a.Browser.Reload() // New 時 hook 還沒接上,重讀一次才會有註解
	return a
}

// LoadSyntax 載入語法上色設定。找不到就不上色,不算錯。
func (a *App) LoadSyntax(dir string) {
	if s, err := syntax.LoadSet(dir); err == nil {
		a.Syntax = s
	}
}

// Draw 依目前模式畫出畫面。回傳的 overlay 要交給
// render.Rasterizer.DrawWith(s, ovs...)。
//
// 回傳的是一組而不是一個:markdown 一頁可以有好幾張圖。
func (a *App) Draw(s *cell.Screen) []*render.Overlay {
	a.screenCols, a.screenRows = s.Cols, s.Rows
	// 觸控功能列佔掉底部兩列。與其讓每個模式各自知道「底下被佔了幾列」,
	// 不如先畫進一個矮兩列的畫面再貼上去 —— 一次對所有模式成立,
	// 而且之後加新模式不必記得這件事。
	//
	// 選單列不在這裡:它可以用與內容不同的字型與大小,格點對不起來,
	// 所以由外殼用 MenuLayer 拿去自己畫(見 menu.go)。
	if a.Touch && s.Rows > TouchRows+2 {
		inner := cell.New(s.Cols, s.Rows-TouchRows)
		ov := a.drawModes(inner)
		s.CopyFrom(inner, 0, 0)
		a.drawTouchBar(s)
		return ov
	}
	return a.drawModes(s)
}

func (a *App) drawModes(s *cell.Screen) []*render.Overlay {
	// 每一幀收一次網路結果。放在這裡是因為 app 這一層沒有自己的迴圈,
	// 而外殼會在 Busy() 為真時持續重繪。
	a.browsePoll()
	if a.Mode == ModeBrowse {
		a.browseImages()
	}

	var ov []*render.Overlay
	switch a.Mode {
	case ModeBrowse:
		ov = a.drawBrowse(s)
	case ModeMarkdown:
		ov = a.drawMarkdown(s)
	case ModeViewer:
		a.rows = a.Viewer.Draw(s)
	case ModeHex:
		a.rows = a.Hex.Draw(s)
	case ModeImage:
		ov = []*render.Overlay{a.Image.Draw(s, a.CellW, a.CellH)}
		a.rows = s.Rows - 1
	case ModeEdit:
		a.rows = a.Editor.Draw(s)
		if a.ShowDict {
			a.drawDict(s)
		}
	case ModeThumbs:
		ov = []*render.Overlay{a.Thumbs.Draw(s, a.CellW, a.CellH)}
		a.rows = s.Rows - 1
		a.thumbCols = s.Cols
	case ModeFind:
		a.rows = a.drawFind(s)
	default:
		a.rows = a.Browser.Draw(s)
		if a.ShowPreview {
			a.drawPreview(s)
		}
	}
	if a.about {
		a.drawAbout(s)
	}
	if a.prompt.active {
		a.drawPrompt(s)
	} else if a.Message != "" {
		y := s.Rows - 1
		s.Fill(0, y, s.Cols, 1, ' ', cell.White, cell.Red)
		s.Print(0, y, a.Message, cell.White, cell.Red)
	}
	return ov
}

// HandleKey 分派一次按鍵。回傳是否有東西改變(需要重畫)。
func (a *App) HandleKey(k keys.Key) bool {
	changed := a.handleKey(k)
	a.rememberPos()
	return changed
}

// docKey 是現在開著的文件在位置表裡的鍵(完整路徑),沒開文件回空字串。
func (a *App) docKey() string {
	var name string
	switch a.Mode {
	case ModeViewer:
		if a.Viewer != nil {
			name = a.Viewer.Name
		}
	case ModeHex:
		if a.Hex != nil {
			name = a.Hex.Name
		}
	case ModeEdit:
		if a.Editor != nil {
			name = a.Editor.Name
		}
	case ModeMarkdown:
		name = a.md.name
	}
	if name == "" || name == helpName {
		return ""
	}
	return filepath.Join(a.Browser.Dir, name)
}

// rememberPos 把現在開著的文件的位置記進表裡。
//
// 每次按鍵之後都做:寫一個 map 很便宜,而「離開時才記」會漏掉被 kill
// 的那一次 —— 使用者失去的正是他讀到一半的那個位置。
func (a *App) rememberPos() {
	key := a.docKey()
	if key == "" {
		return
	}
	p := session.DocPos{At: a.unix()}
	switch a.Mode {
	case ModeViewer:
		p.Top, p.Line = a.Viewer.Top, a.Viewer.Cur
	case ModeHex:
		p.Top = a.Hex.Top
	case ModeEdit:
		p.Top, p.Line, p.Col = a.Editor.Top, a.Editor.Cur.Line, a.Editor.Cur.Col
	case ModeMarkdown:
		p.Top = a.md.top
	}
	if a.positions == nil {
		a.positions = map[string]session.DocPos{}
	}
	st := session.State{Positions: a.positions}
	st.Remember(key, p)
	a.positions = st.Positions
}

// recallPos 開檔之後把上次的位置套回去。檔案內容可能變了,一律夾邊界。
func (a *App) recallPos() {
	p, ok := a.positions[a.docKey()]
	if !ok {
		return
	}
	switch a.Mode {
	case ModeViewer:
		a.Viewer.Top = clampTop(p.Top, len(a.Viewer.Lines))
		a.Viewer.Cur = clampTop(p.Line, len(a.Viewer.Lines))
	case ModeHex:
		a.Hex.Top = clampTop(p.Top, len(a.Hex.Data)/16+1)
	case ModeEdit:
		a.Editor.Top = clampTop(p.Top, len(a.Editor.Lines))
		a.Editor.MoveTo(editor.Pos{Line: p.Line, Col: p.Col})
	case ModeMarkdown:
		a.md.top = clampTop(p.Top, len(a.md.blocks)*4)
	}
}

func (a *App) unix() int64 {
	if a.now != nil {
		return a.now()
	}
	return time.Now().Unix()
}

func (a *App) handleKey(k keys.Key) bool {
	if a.prompt.active {
		// 這兩個是「先選一種再問細節」的兩段式流程,
		// 第一段不走一般的輸入列處理。
		if a.findKindPending {
			return a.findKindKey(k)
		}
		if a.convertPending != nil {
			return a.convertKey(k)
		}
		return a.promptKey(k)
	}
	if a.about {
		return a.aboutKey(k)
	}
	if a.menu.active {
		return a.menuKey(k)
	}
	a.Message = ""
	// F1 說明、F2 網路瀏覽、F9 選單、F8 中英文、F11 全螢幕
	// 在每個模式下都通,所以在分派到各模式之前先攔下來。
	// 字級與縮放:Ctrl-+ / Ctrl-- / Ctrl-0。每個模式下都通。
	if k.Ctrl && k.Code == keys.Rune {
		switch k.R {
		case '+', '=':
			return a.setZoom(a.Zoom + 1)
		case '-', '_':
			return a.setZoom(a.Zoom - 1)
		case '0':
			return a.setZoom(0)
		}
	}
	// 放大倍率:Alt-+ / Alt-- 每次 0.1,Alt-0 回到 1.0。
	if k.Alt && k.Code == keys.Rune {
		switch k.R {
		case '+', '=':
			return a.setScale(a.Scale + ScaleStep)
		case '-', '_':
			return a.setScale(a.Scale - ScaleStep)
		case '0':
			return a.setScale(1)
		}
	}
	// Alt-X 離開。原版沒有離開鍵(Win32 程式關視窗就是離開),
	// 但重製版需要一條「程式自己知道要收尾」的路徑 —— 位置就是在那裡記下的。
	if k.Alt && k.Code == keys.Rune && (k.R == 'x' || k.R == 'X') {
		return a.quit()
	}
	switch k.Code {
	case keys.F1:
		return a.openHelp()
	case keys.F2:
		return a.browseAsk()
	case keys.F9:
		return a.openMenu()
	case keys.F8:
		if !k.Ctrl {
			return a.toggleCJK()
		}
	case keys.F11:
		return a.toggleFullscreen()
	}
	switch a.Mode {
	case ModeBrowse:
		return a.browseKey(k)
	case ModeMarkdown:
		return a.markdownKey(k)
	case ModeViewer:
		return a.viewerKey(k)
	case ModeHex:
		return a.hexKey(k)
	case ModeImage:
		return a.imageKey(k)
	case ModeEdit:
		return a.editKey(k)
	case ModeThumbs:
		return a.thumbsKey(k)
	case ModeFind:
		return a.findKey(k)
	default:
		return a.browserKey(k)
	}
}

// quit 收尾離開。編輯中還有沒存的東西就先問一句 ——
// 原版也問(image 裡有「檔案已修改,要先存檔才離開嗎?」)。
func (a *App) quit() bool {
	if a.Editor != nil && a.Editor.Dirty {
		a.ask(i18n.T("檔案已修改,要先存檔才離開嗎?(y/n)"), "y", func(ans string) {
			// 「是 / 否」是使用者打進來的字,不是畫面上的字:兩邊都要
			// 翻才對得起來,所以在 case 之前先取翻譯,不能寫在 case 裡
			// (那會變成「翻譯過的期望值」對上「沒翻的輸入」)。
			yes, no := i18n.T("是"), i18n.T("否")
			switch s := strings.ToLower(strings.TrimSpace(ans)); s {
			case "y", "yes", yes:
				a.SaveEditor()
				a.Quit = true
			case "n", "no", no:
				a.Quit = true
			}
			// 其他答案:當作取消,留在原地。
		})
		return true
	}
	a.Quit = true
	return true
}

// setZoom 換字級。回傳是否真的變了。
func (a *App) setZoom(n int) bool {
	if n < 0 {
		n = 0
	}
	if n > a.MaxZoom {
		n = a.MaxZoom
	}
	if n == a.Zoom {
		return false
	}
	a.Zoom = n
	return true
}

// --- 檔案瀏覽器 -----------------------------------------------------------

func (a *App) browserKey(k keys.Key) bool {
	b := a.Browser
	rows := a.rows
	if rows <= 0 {
		rows = 1
	}

	// 焦點在磁碟窗格時,方向鍵歸它。Tab 在兩邊之間切。
	if a.DriveFocus {
		if a.driveKey(k) {
			return true
		}
	} else if k.Code == keys.Tab && b.DrivePane > 0 {
		a.DriveFocus = true
		return true
	}

	// Ctrl-← / Ctrl-→ 調主檔名欄的寬度(滑鼠橫向拖曳也是)。
	if k.Ctrl && (k.Code == keys.Left || k.Code == keys.Right) {
		d := 1
		if k.Code == keys.Left {
			d = -1
		}
		return b.ResizeName(d)
	}

	switch k.Code {
	case keys.Up:
		b.MoveBy(-1, rows)
		return true
	case keys.Down:
		b.MoveBy(1, rows)
		return true
	case keys.PgUp:
		b.MoveBy(-rows, rows)
		return true
	case keys.PgDn:
		b.MoveBy(rows, rows)
		return true
	case keys.Home:
		b.Home(rows)
		return true
	case keys.End:
		b.End(rows)
		return true
	case keys.Enter:
		return a.enter()
	case keys.Backspace:
		return a.goParent()
	case keys.Delete:
		// 徹底刪除:先填 0 再刪(0.5 版新增的選項)。
		return a.startDelete(true)
	}

	if k.Code == keys.Rune && k.R == ' ' && !k.Alt && !k.Ctrl {
		b.ToggleMark(rows)
		return true
	}
	// 原版的 5 是「改變檔案列表的方式」(0.5 版 changelog),縮圖列表
	// 正是一種列表方式。實際有幾種模式還沒實測(keymap.md 待驗第 4 項)。
	if k.Code == keys.Rune && k.R == '5' && !k.Alt && !k.Ctrl {
		return a.openThumbs()
	}

	switch k.Letter() {
	case 'T':
		// T 只標檔案,Alt-T 連目錄一起標(0.5 版起的行為)。
		b.MarkAll(k.Alt)
		return true
	case 'U':
		if !k.Alt && !k.Ctrl {
			b.UnmarkAll()
			return true
		}
	case 'E':
		if k.Alt {
			return a.startNote()
		}
		if !k.Ctrl {
			return a.openEditor()
		}
	case 'C':
		if k.Alt {
			return a.compareMarked()
		}
		return a.startTransfer(false)
	case 'M':
		if !k.Alt && !k.Ctrl {
			return a.startTransfer(true)
		}
	case 'R':
		if !k.Alt && !k.Ctrl {
			return a.startRename()
		}
	case 'D':
		if k.Alt {
			return a.toggleDrivePane()
		}
		if !k.Ctrl {
			return a.startDelete(false)
		}
	case 'Z':
		if k.Alt {
			return a.startCreateArchive()
		}
		if !k.Ctrl {
			return a.startExtract()
		}
	case 'W':
		if !k.Alt && !k.Ctrl {
			return a.startFind()
		}
	case 'O':
		if k.Ctrl {
			return a.startConvert()
		}
		if !k.Alt {
			return a.startOpen()
		}
	case 'P':
		if k.Alt {
			return a.togglePreview()
		}
		if !k.Ctrl {
			return a.startChangeDir()
		}
	case 'G':
		if k.Alt {
			return a.browseAsk()
		}
		if !k.Ctrl {
			return a.startRun()
		}
	case 'S':
		if !k.Alt && !k.Ctrl {
			// 依序切換排序欄位。原版的 '5' 是「改變檔案列表的方式」,
			// 語意還沒實測清楚(見 docs/ui/keymap.md 待實測第 4 項),
			// 先給一個明確可用的排序切換。
			b.SortKey = (b.SortKey + 1) % 4
			b.Resort()
			a.Message = i18n.T("排序:") + sortName(b.SortKey)
			return true
		}
	}
	return false
}

func sortName(k vfs.SortKey) string {
	switch k {
	case vfs.ByExt:
		return i18n.T("副檔名")
	case vfs.BySize:
		return i18n.T("大小")
	case vfs.ByTime:
		return i18n.T("時間")
	}
	return i18n.T("檔名")
}

// enter 進入目錄或開啟檔案。
func (a *App) enter() bool {
	e := a.Browser.Current()
	if e == nil {
		return false
	}
	if e.IsDir {
		if e.Up {
			return a.goParent()
		}
		dir := a.joinDir(e.Name)
		if err := a.Browser.Load(dir); err != nil {
			a.Message = i18n.T("無法進入 ") + dir + ": " + err.Error()
		}
		return true
	}
	// [雷] .epub 要在壓縮檔判斷**之前**攔下來。EPUB 就是一個 zip,
	// 不先攔的話按 Enter 會走進去看到 META-INF 與一堆 xhtml,
	// 而那不是任何人想看一本書時要的東西。
	if IsBook(e.Name) && !a.readOnlyHere() {
		return a.openBook(e.Name)
	}
	if IsPDF(e.Name) && !a.readOnlyHere() {
		return a.openPDF(e.Name)
	}
	// [雷] .docx / .pptx / .xlsx 也是 zip,同樣要在壓縮檔判斷之前攔下來。
	// 不先攔的話按 Enter 會走進去看到 word/document.xml,而那不是
	// 任何人想看一份文件時要的東西。
	if IsOfficeDoc(e.Name) && !a.readOnlyHere() {
		return a.openOffice(e.Name)
	}
	if archive.IsArchive(e.Name) {
		return a.enterArchive(e.Name)
	}
	if imgfmt.IsImage(e.Name) {
		return a.openImage(e.Name)
	}
	return a.openViewer(e.Name)
}

// joinDir 接出子目錄的路徑。壓縮檔內用 / 分隔,不能用 filepath.Join
// (Windows 上會變成反斜線,而壓縮檔裡的路徑一律是斜線)。
func (a *App) joinDir(name string) string {
	if af, ok := a.Browser.FS.(*archive.FS); ok {
		_, inner := archiveSplit(a.Browser.Dir)
		if inner == "" {
			return af.Path(name)
		}
		return af.Path(inner + "/" + name)
	}
	return filepath.Join(a.Browser.Dir, name)
}

func archiveSplit(p string) (string, string) {
	if i := strings.Index(p, "!"); i >= 0 {
		return p[:i], strings.Trim(p[i+1:], "/")
	}
	return p, ""
}

// enterArchive 把壓縮檔當目錄進去。
func (a *App) enterArchive(name string) bool {
	full := filepath.Join(a.Browser.Dir, name)
	af, err := archive.Open(full)
	if err != nil {
		a.Message = err.Error()
		return true
	}
	a.stack = append(a.stack, layer{fs: a.Browser.FS, dir: a.Browser.Dir, cursor: a.Browser.Cursor})
	a.Browser.FS = af
	if err := a.Browser.Load(af.Root()); err != nil {
		a.Message = err.Error()
		a.popLayer()
	}
	return true
}

func (a *App) popLayer() bool {
	if len(a.stack) == 0 {
		return false
	}
	l := a.stack[len(a.stack)-1]
	a.stack = a.stack[:len(a.stack)-1]
	a.Browser.FS = l.fs
	if err := a.Browser.Load(l.dir); err != nil {
		a.Message = err.Error()
		return true
	}
	a.Browser.MoveTo(l.cursor, a.rows)
	return true
}

func (a *App) goParent() bool {
	// 在壓縮檔最上層再往上,就是離開這個壓縮檔。
	if af, ok := a.Browser.FS.(*archive.FS); ok {
		if af.IsRoot(a.Browser.Dir) {
			return a.popLayer()
		}
		_, inner := archiveSplit(a.Browser.Dir)
		up := path.Dir(inner)
		if up == "." {
			up = ""
		}
		prev := path.Base(inner)
		if err := a.Browser.Load(af.Path(up)); err != nil {
			a.Message = err.Error()
			return true
		}
		a.focusOn(prev)
		return true
	}

	p := vfs.Parent(a.Browser.Dir)
	if p == a.Browser.Dir {
		return false
	}
	prev := filepath.Base(a.Browser.Dir)
	if err := a.Browser.Load(p); err != nil {
		a.Message = i18n.T("無法回上一層: ") + err.Error()
		return true
	}
	a.focusOn(prev)
	return true
}

// focusOn 把游標移到指定名稱那一筆。回上一層時游標停在剛才離開的
// 目錄上 —— 一路往回走時每次都跳回第一筆會很難用。
func (a *App) focusOn(name string) {
	for i, e := range a.Browser.Entries {
		if e.Name == name {
			a.Browser.MoveTo(i, a.rows)
			return
		}
	}
}

// readCurrent 讀目前目錄下的一個檔案(壓縮檔內也適用)。
func (a *App) readCurrent(name string) ([]byte, error) {
	rc, err := a.Browser.FS.Open(a.joinDir(name))
	if err != nil {
		return nil, fmt.Errorf(i18n.T("開不了 %s: %w"), name, err)
	}
	defer rc.Close()
	data, err := io.ReadAll(io.LimitReader(rc, MaxViewBytes))
	if err != nil {
		return nil, fmt.Errorf(i18n.T("讀取失敗: %w"), err)
	}
	return data, nil
}

func (a *App) openViewer(name string) bool {
	data, err := a.readCurrent(name)
	if err != nil {
		a.Message = err.Error()
		return true
	}
	// markdown 走排版模式:標題、清單、表格照結構畫,圖片直接嵌在文件裡。
	// 要看原始碼按 E 進編輯器。
	if IsMarkdown(name) {
		a.openMarkdown(name, data)
		return true
	}
	// 判成二進位就直接開 16 進位檢視 —— 原版 0.5 版起就是這個行為
	// (「按 enter 看檔時自動將可能為執行檔的檔案以 16 進位方式看檔」)。
	m := viewer.Load(name, data, textenc.Unknown)
	if m.Enc == textenc.Binary {
		a.openHex(name, data)
		return true
	}
	a.viewData = data
	a.viewRaw = textenc.Decode(data, m.Enc)
	a.Viewer = m
	a.Mode = ModeViewer
	a.recallPos()
	return true
}

func (a *App) openImage(name string) bool {
	data, err := a.readCurrent(name)
	if err != nil {
		a.Message = err.Error()
		return true
	}
	m, err := imgview.Load(name, data)
	if err != nil {
		// 解不開就退回 16 進位 —— 至少看得到內容,而不是什麼都沒有。
		a.Message = err.Error()
		a.openHex(name, data)
		return true
	}
	a.Image = m
	a.Mode = ModeImage
	return true
}

// imageNeighbours 回傳目前目錄裡所有圖檔的索引,用於看上一張/下一張。
func (a *App) imageNeighbours() []int {
	var out []int
	for i, e := range a.Browser.Entries {
		if !e.IsDir && imgfmt.IsImage(e.Name) {
			out = append(out, i)
		}
	}
	return out
}

// stepImage 換到上一張或下一張圖(原版 Enter / BackSpace)。
func (a *App) stepImage(d int) bool {
	idx := a.imageNeighbours()
	if len(idx) == 0 {
		return false
	}
	cur := -1
	for i, v := range idx {
		if v == a.Browser.Cursor {
			cur = i
			break
		}
	}
	if cur < 0 {
		cur = 0
	}
	next := (cur + d + len(idx)) % len(idx)
	a.Browser.MoveTo(idx[next], a.rows)
	e := a.Browser.Current()
	if e == nil {
		return false
	}
	return a.openImage(e.Name)
}

func (a *App) imageKey(k keys.Key) bool {
	m := a.Image
	switch k.Code {
	case keys.Enter:
		return a.stepImage(1)
	case keys.Backspace, keys.PgUp:
		return a.stepImage(-1)
	case keys.PgDn:
		return a.stepImage(1)
	case keys.Esc:
		// 從瀏覽模式進來的就退回瀏覽模式。
		if a.gReturn {
			a.gReturn = false
			a.Mode = ModeBrowse
			return true
		}
		// 從 markdown 進來的就退回 markdown,不是退回檔案清單。
		if a.mdReturn {
			a.mdReturn = false
			a.Mode = ModeMarkdown
			return true
		}
		a.Mode = ModeBrowser
		return true
	case keys.Up:
		m.PanBy(0, -32)
		return true
	case keys.Down:
		m.PanBy(0, 32)
		return true
	case keys.Left:
		m.PanBy(-32, 0)
		return true
	case keys.Right:
		m.PanBy(32, 0)
		return true
	}
	// 放大 / 縮小 / 回到原尺寸。裸鍵沒有歧義 —— 看圖模式沒有文字輸入,
	// 而帶修飾鍵的 Ctrl-+ 與 Alt-+ 已經是字級與整個畫面的放大倍率。
	if k.Code == keys.Rune && !k.Ctrl && !k.Alt {
		switch k.R {
		case '+', '=':
			return m.ZoomBy(1)
		case '-', '_':
			return m.ZoomBy(-1)
		case '1':
			return m.SetZoom(1)
		}
	}
	if k.Code == keys.Rune && k.R == ' ' {
		// 標記此檔,換看下一張(原版行為)。
		if e := a.Browser.Current(); e != nil && !e.Up {
			e.Marked = !e.Marked
		}
		return a.stepImage(1)
	}
	// `;` 或 Alt-E 加註解(原版 0xd6908「; 或 Alt-E 註解」)。
	// 看圖模式沒有文字輸入,所以 `;` 直接當指令沒有歧義。
	if k.Code == keys.Rune && k.R == ';' && !k.Ctrl {
		return a.startNote()
	}
	switch k.Letter() {
	case 'I':
		m.Info = !m.Info
		return true
	case 'F':
		m.ToggleFit()
		return true
	case 'E':
		if k.Alt {
			return a.startNote()
		}
	}
	return false
}

func (a *App) openHex(name string, data []byte) {
	a.Hex = hexview.Load(name, data)
	a.viewData = data
	a.Mode = ModeHex
	a.recallPos()
}

// --- 檔案操作 -------------------------------------------------------------

// targets 回傳這次操作要處理哪些檔案:有標記就用標記的,
// 沒有就用游標所在的那一個。這是檔案管理程式的通則。
func (a *App) targets() []string {
	var out []string
	for _, e := range a.Browser.Entries {
		if e.Marked && !e.Up {
			out = append(out, e.Name)
		}
	}
	if len(out) == 0 {
		if e := a.Browser.Current(); e != nil && !e.Up {
			out = append(out, e.Name)
		}
	}
	return out
}

// readOnlyHere 回傳「現在在壓縮檔裡,不能改」。
func (a *App) readOnlyHere() bool {
	_, ok := a.Browser.FS.(*archive.FS)
	return ok
}

// startTransfer 開始拷貝(move=false)或移動(move=true)。
func (a *App) startTransfer(move bool) bool {
	if a.readOnlyHere() {
		a.Message = i18n.T("壓縮檔裡的檔案還不能拷貝或移動")
		return true
	}
	names := a.targets()
	if len(names) == 0 {
		return false
	}
	verb := i18n.T("拷貝")
	if move {
		verb = i18n.T("移動")
	}
	title := fmt.Sprintf(i18n.T("%s %d 個檔案到:"), verb, len(names))
	a.ask(title, a.Browser.Dir, func(dst string) {
		a.runTransfer(move, names, dst)
	})
	return true
}

func (a *App) runTransfer(move bool, names []string, dst string) {
	if dst == "" {
		return
	}
	if err := os.MkdirAll(dst, 0o755); err != nil {
		a.Message = i18n.T("目的目錄建不起來: ") + err.Error()
		return
	}
	// 覆蓋詢問走同一個輸入列。因為輸入列是非同步的(要等使用者按鍵),
	// 這裡先用「不覆蓋」跑一遍,把撞名的挑出來再一次問。
	opt := fileop.Options{Overwrite: fileop.Skip}
	var res *fileop.Result
	if move {
		res = fileop.Move(a.Browser.Dir, dst, names, opt)
	} else {
		res = fileop.Copy(a.Browser.Dir, dst, names, opt)
	}
	verb := i18n.T("已拷貝")
	if move {
		verb = i18n.T("已移動")
	}
	if len(res.Skipped) > 0 {
		skipped := res.Skipped
		done := res.Summary(verb)
		a.confirm(fmt.Sprintf(i18n.T("%d 個檔案已存在,要覆蓋嗎?"), len(skipped)), false,
			func(yes, _ bool) {
				if !yes {
					a.Message = done
					a.Browser.Reload()
					return
				}
				o := fileop.Options{Overwrite: fileop.All}
				var r2 *fileop.Result
				if move {
					r2 = fileop.Move(a.Browser.Dir, dst, skipped, o)
				} else {
					r2 = fileop.Copy(a.Browser.Dir, dst, skipped, o)
				}
				a.Message = done + i18n.T(";覆蓋 ") + r2.Summary(verb)
				a.Browser.Reload()
			})
		return
	}
	a.Message = res.Summary(verb)
	a.Browser.Reload()
}

func (a *App) startRename() bool {
	if a.readOnlyHere() {
		a.Message = i18n.T("壓縮檔裡的檔案還不能改名")
		return true
	}
	e := a.Browser.Current()
	if e == nil || e.Up {
		return false
	}
	old := e.Name
	a.ask(i18n.T("改名為:"), old, func(to string) {
		if to == "" || to == old {
			return
		}
		if err := fileop.Rename(a.Browser.Dir, old, to); err != nil {
			a.Message = i18n.T("改名失敗: ") + err.Error()
			return
		}
		a.Browser.Reload()
		a.focusOn(to)
		a.Message = old + " → " + to
	})
	return true
}

// startDelete 刪除。zero 為 true 時先把內容填 0 再刪。
func (a *App) startDelete(zero bool) bool {
	if a.readOnlyHere() {
		a.Message = i18n.T("壓縮檔裡的檔案還不能刪除")
		return true
	}
	names := a.targets()
	if len(names) == 0 {
		return false
	}
	what := fmt.Sprintf(i18n.T("刪除 %d 個檔案?"), len(names))
	if zero {
		what = fmt.Sprintf(i18n.T("徹底刪除 %d 個檔案(先填 0)?"), len(names))
	}
	if len(names) == 1 {
		what = i18n.T("刪除 ") + names[0] + "?"
		if zero {
			what = i18n.T("徹底刪除 ") + names[0] + i18n.T("(先填 0)?")
		}
	}
	a.confirm(what, false, func(yes, _ bool) {
		if !yes {
			return
		}
		res := fileop.Delete(a.Browser.Dir, names, fileop.Options{ZeroFill: zero})
		a.Message = res.Summary(i18n.T("已刪除"))
		a.Browser.Reload()
	})
	return true
}

// compareMarked 比對標記的兩個檔案(原版 Alt-C)。
func (a *App) compareMarked() bool {
	var names []string
	for _, e := range a.Browser.Entries {
		if e.Marked && !e.IsDir {
			names = append(names, e.Name)
		}
	}
	if len(names) != 2 {
		a.Message = i18n.T("請先標記剛好兩個檔案")
		return true
	}
	same, at, err := fileop.Compare(
		filepath.Join(a.Browser.Dir, names[0]),
		filepath.Join(a.Browser.Dir, names[1]))
	switch {
	case err != nil:
		a.Message = i18n.T("比對失敗: ") + err.Error()
	case same:
		a.Message = names[0] + i18n.T(" 與 ") + names[1] + i18n.T(" 內容相同")
	default:
		a.Message = fmt.Sprintf(i18n.T("內容不同,第一個相異位移 0x%X"), at)
	}
	return true
}

// --- 縮圖列表 -------------------------------------------------------------

// openThumbs 把目前目錄的圖檔排成縮圖列表。
func (a *App) openThumbs() bool {
	var names []string
	for _, e := range a.Browser.Entries {
		if !e.IsDir && imgfmt.IsImage(e.Name) {
			names = append(names, e.Name)
		}
	}
	if len(names) == 0 {
		a.Message = i18n.T("這個目錄沒有圖檔")
		return true
	}
	a.Thumbs = thumbs.New(names, a.readCurrent)
	a.Mode = ModeThumbs
	return true
}

// DecodeThumbs 解目前看得到的縮圖。呼叫端應該在背景做 ——
// 一個目錄幾十張 JPEG 解起來要好幾秒,卡著等會像當掉。
func (a *App) DecodeThumbs(cols, rows int) {
	if a.Thumbs != nil {
		a.Thumbs.DecodeVisible(cols, rows)
	}
}

func (a *App) thumbsKey(k keys.Key) bool {
	t := a.Thumbs
	cols, rows := a.thumbCols, a.rows
	if cols <= 0 {
		cols = 80
	}
	if rows <= 0 {
		rows = 24
	}
	switch k.Code {
	case keys.Left:
		t.MoveBy(-1, 0, cols, rows)
		return true
	case keys.Right:
		t.MoveBy(1, 0, cols, rows)
		return true
	case keys.Up:
		t.MoveBy(0, -1, cols, rows)
		return true
	case keys.Down:
		t.MoveBy(0, 1, cols, rows)
		return true
	case keys.Enter:
		if it := t.Current(); it != nil {
			// 游標也跟著移到那個檔案上,離開看圖時才接得回來。
			a.focusOn(it.Name)
			return a.openImage(it.Name)
		}
		return false
	case keys.Esc:
		a.Mode = ModeBrowser
		return true
	}
	return false
}

// --- 字典視窗 -------------------------------------------------------------

// DictPanelRows 是字典視窗佔幾列。
const DictPanelRows = 4

// wordUnderCursor 取游標所在的英文單字。
func (a *App) wordUnderCursor() string {
	e := a.Editor
	if e == nil || e.Cur.Line >= len(e.Lines) {
		return ""
	}
	l := e.Lines[e.Cur.Line]
	i := e.Cur.Col
	if i >= len(l) {
		i = len(l) - 1
	}
	if i < 0 || !isWordRune(l[i]) {
		return ""
	}
	from := i
	for from > 0 && isWordRune(l[from-1]) {
		from--
	}
	to := i
	for to < len(l) && isWordRune(l[to]) {
		to++
	}
	return string(l[from:to])
}

func isWordRune(r rune) bool {
	return r == '\'' || r == '-' ||
		(r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z')
}

// drawDict 在畫面底部畫字典視窗。
func (a *App) drawDict(s *cell.Screen) {
	if a.dict == nil && a.dictErr == "" {
		d, err := dict.Load(a.DictDir)
		if err != nil {
			a.dictErr = i18n.T("載不到字典: ") + err.Error()
		} else {
			a.dict = d
		}
	}
	top := s.Rows - 1 - DictPanelRows
	if top < 0 {
		return
	}
	s.Fill(0, top, s.Cols, DictPanelRows, ' ', cell.LtCyan, cell.Blue)
	if a.dictErr != "" {
		s.Print(1, top, a.dictErr, cell.LtYellow, cell.Blue)
		return
	}
	w := a.wordUnderCursor()
	if w == "" {
		s.Print(1, top, i18n.T("字典:把游標移到英文單字上"), cell.LtGray, cell.Blue)
		return
	}
	e, ok := a.dict.Lookup(w)
	if !ok {
		s.Print(1, top, w+i18n.T(" — 查無此字"), cell.LtGray, cell.Blue)
		return
	}
	head := e.Word
	if e.KK != "" {
		head += "  [" + e.KK + "]"
	}
	if e.Base != "" && e.Base != e.Word {
		head += i18n.T("  (原形 ") + e.Base + ")"
	}
	s.Print(1, top, head, cell.LtYellow, cell.Blue)
	// 解釋可能很長,換行塞進剩下的幾列。
	y := top + 1
	for _, line := range wrapCells(e.Trans, s.Cols-2) {
		if y >= s.Rows-1 {
			break
		}
		s.Print(1, y, line, cell.LtCyan, cell.Blue)
		y++
	}
}

// wrapCells 依顯示格數折行,不把全形字切一半。
func wrapCells(s string, width int) []string {
	if width <= 0 {
		return nil
	}
	var out []string
	var cur []rune
	n := 0
	for _, r := range s {
		w := 1
		if cell.IsWide(r) {
			w = 2
		}
		if n+w > width {
			out = append(out, string(cur))
			cur, n = nil, 0
		}
		cur = append(cur, r)
		n += w
	}
	if len(cur) > 0 {
		out = append(out, string(cur))
	}
	return out
}

// --- 文字編輯器 -----------------------------------------------------------

// openEditor 用編輯器開游標所在的檔案(原版主畫面的 E)。
func (a *App) openEditor() bool {
	e := a.Browser.Current()
	if e == nil || e.IsDir {
		return false
	}
	data, err := a.readCurrent(e.Name)
	if err != nil {
		a.Message = err.Error()
		return true
	}
	a.Editor = editor.Load(e.Name, data, textenc.Unknown, a.Syntax.For(e.Name))
	a.Mode = ModeEdit
	a.recallPos()
	return true
}

// SaveEditor 把編輯中的內容寫回檔案。壓縮檔內的檔案不能存 ——
// 那需要重建整個壓縮檔,是另一件事。
func (a *App) SaveEditor() bool {
	if a.Editor == nil {
		return false
	}
	if _, ok := a.Browser.FS.(*archive.FS); ok {
		a.Message = i18n.T("壓縮檔裡的檔案還不能直接存回去")
		return true
	}
	path := filepath.Join(a.Browser.Dir, a.Editor.Name)
	if err := os.WriteFile(path, a.Editor.Bytes(), 0o644); err != nil {
		a.Message = i18n.T("存檔失敗: ") + err.Error()
		return true
	}
	a.Editor.Dirty = false
	a.Message = i18n.T("已存檔 ") + a.Editor.Name
	return true
}

func (a *App) editKey(k keys.Key) bool {
	e := a.Editor
	rows := a.rows
	if rows <= 0 {
		rows = 1
	}

	switch k.Code {
	case keys.Up:
		e.MoveBy(-1, 0)
		return true
	case keys.Down:
		e.MoveBy(1, 0)
		return true
	case keys.Left:
		e.MoveBy(0, -1)
		return true
	case keys.Right:
		e.MoveBy(0, 1)
		return true
	case keys.PgUp:
		e.MoveBy(-rows, 0)
		return true
	case keys.PgDn:
		e.MoveBy(rows, 0)
		return true
	case keys.Home:
		e.Cur.Col = 0
		return true
	case keys.End:
		e.Cur.Col = len(e.Lines[e.Cur.Line])
		return true
	case keys.Enter:
		e.NewLine()
		return true
	case keys.Backspace:
		e.Backspace()
		return true
	case keys.Delete:
		e.Delete()
		return true
	case keys.Insert:
		e.Insert = !e.Insert
		return true
	case keys.Tab:
		e.InsertRune('\t')
		return true
	case keys.Esc:
		a.Mode = ModeBrowser
		return true
	case keys.F6:
		return a.startEditFind()
	case keys.F7:
		// 字典視窗。原版是設定項(「顯示字典視窗?」),沒找到對應的
		// 快捷鍵,先給 F7(證據等級 C,見 docs/ui/keymap.md)。
		a.ShowDict = !a.ShowDict
		return true
	}

	if k.Alt {
		switch k.Letter() {
		case 'B':
			e.MarkBlock(editor.BlockRect)
			return true
		case 'L':
			e.MarkBlock(editor.BlockLine)
			return true
		case 'U':
			e.UnmarkBlock()
			return true
		case 'Z':
			if e.CopyBlock() {
				e.UnmarkBlock()
				e.PasteBlock()
			}
			return true
		case 'M':
			e.MoveBlock()
			return true
		case 'D':
			e.DeleteBlock()
			return true
		case 'F':
			e.FillBlock(' ', true)
			return true
		case 'E':
			return a.editComment()
		}
		return false
	}

	if k.Ctrl {
		switch k.Letter() {
		case 'C':
			e.CopyBlock()
			return true
		case 'X':
			if e.CopyBlock() {
				e.DeleteBlock()
			}
			return true
		case 'V':
			e.PasteBlock()
			return true
		case 'U':
			e.Undo()
			return true
		case 'S':
			return a.SaveEditor()
		case 'E':
			// 原版 0x8838c「Ctrl-E ; 標記轉註解」。
			return a.editComment()
		case 'N':
			// 原版 0xe7e97「續找」。
			a.editFindNext(false)
			return true
		}
		return false
	}

	// 一般字元輸入。控制鍵在上面已經處理掉了。
	if k.Code == keys.Rune && k.R >= 0x20 {
		e.InsertRune(k.R)
		return true
	}
	return false
}

// --- 16 進位檢視 ----------------------------------------------------------

func (a *App) hexKey(k keys.Key) bool {
	h := a.Hex
	rows := a.rows
	if rows <= 0 {
		rows = 1
	}
	switch k.Code {
	case keys.Up:
		h.ScrollBy(-1, rows)
		return true
	case keys.Down:
		h.ScrollBy(1, rows)
		return true
	case keys.PgUp:
		h.ScrollBy(-rows, rows)
		return true
	case keys.PgDn:
		h.ScrollBy(rows, rows)
		return true
	case keys.Home:
		h.Home(rows)
		return true
	case keys.End:
		h.End(rows)
		return true
	case keys.Esc, keys.Enter, keys.Backspace:
		a.Mode = ModeBrowser
		return true
	}
	switch k.Letter() {
	case 'H':
		// 從 16 進位切回文字 —— 只有本來就是文字檔才切得回去。
		if a.Viewer != nil {
			a.Mode = ModeViewer
			return true
		}
	case 'F':
		if !k.Ctrl && !k.Alt {
			h.Big5 = !h.Big5
			return true
		}
	}
	return false
}

// --- 文字檢視器 -----------------------------------------------------------

func (a *App) viewerKey(k keys.Key) bool {
	v := a.Viewer
	rows := a.rows
	if rows <= 0 {
		rows = 1
	}

	switch k.Code {
	// 方向鍵移游標,畫面跟著游標走。原版的工具列本來就在報游標在第幾行
	// (「1 字 1 行/ 626」),只是它沒有把那一列畫出來。
	case keys.Up:
		v.MoveBy(-1, rows)
		return true
	case keys.Down:
		v.MoveBy(1, rows)
		return true
	case keys.PgUp:
		v.PageBy(-1, rows)
		return true
	case keys.PgDn:
		v.PageBy(1, rows)
		return true
	case keys.Home:
		v.Home(rows)
		return true
	case keys.End:
		v.End(rows)
		return true
	case keys.Left:
		if !v.Wrap && v.Left > 0 {
			v.Left -= 8
			if v.Left < 0 {
				v.Left = 0
			}
			return true
		}
	case keys.Right:
		if !v.Wrap {
			v.Left += 8
			return true
		}
	case keys.Esc, keys.Enter, keys.Backspace:
		// Esc 是回上一層,不是離開程式。
		// 見 knowledge-base/retro-cht/esc-cancel-f10-quit-autosave。
		a.Mode = ModeBrowser
		return true
	}

	switch k.Letter() {
	case 'W':
		if !k.Ctrl && !k.Alt {
			v.Wrap = !v.Wrap
			v.Left = 0
			return true
		}
	case 'A':
		if !k.Ctrl && !k.Alt {
			v.SetAnsi(!v.Ansi, a.viewRaw)
			return true
		}
	case 'N':
		if k.Ctrl {
			v.NextHit(rows)
			return true
		}
	case 'H':
		// H 切到 16 進位(image 內的標籤是「&Hex模式」)。
		if !k.Ctrl && !k.Alt {
			a.openHex(v.Name, a.viewData)
			return true
		}
	case 'L':
		// 光棒開關。ANSI 彩色簽名檔的底色本身有意義,蓋掉會失真,
		// 所以要留一個關得掉的辦法。
		if !k.Ctrl && !k.Alt {
			v.Bar = !v.Bar
			return true
		}
	}
	return false
}

// --- 小工具 ---------------------------------------------------------------

func comma(v int64) string {
	s := fmt.Sprintf("%d", v)
	var sb strings.Builder
	for i, c := range s {
		if i > 0 && (len(s)-i)%3 == 0 {
			sb.WriteByte(',')
		}
		sb.WriteRune(c)
	}
	return sb.String()
}
