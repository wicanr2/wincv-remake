package pdfdoc

import (
	"sort"
	"strings"

	"github.com/wicanr2/wincv-remake/internal/pdf"
)

// spaceRatio 是「多寬的間距算一個空格」,以字級為單位。
//
// 取得比實際的空格窄(一個空格大約是字級的四分之一)是刻意的:
// 多一個空格只是版面鬆一點,少一個空格會讓兩個詞黏成一個看不懂的字串。
const spaceRatio = 0.2

// layout 把散落的字組回一列一列,必要時先分欄。
func layout(ts []pdf.Text, x0, x1 float64) []Line {
	if len(ts) == 0 {
		return nil
	}
	var out []Line
	for _, col := range columns(ts, x0, x1) {
		out = append(out, linesOf(col)...)
	}
	return out
}

type row struct {
	y     float64
	items []pdf.Text
}

// groupRows 把字依縱座標分成列。
//
// 容差用字級的一部分而不是固定值:固定值在放大過的頁面上會把兩列
// 併成一列,在小字的頁面上會把一列拆成兩列。
func groupRows(ts []pdf.Text) []row {
	var rows []row
	for _, t := range ts {
		tol := t.Size * 0.4
		if tol < 1 {
			tol = 1
		}
		placed := false
		for i := range rows {
			if abs(rows[i].y-t.Y) <= tol {
				rows[i].items = append(rows[i].items, t)
				placed = true
				break
			}
		}
		if !placed {
			rows = append(rows, row{t.Y, []pdf.Text{t}})
		}
	}
	// 由上往下:PDF 的 Y 軸由下往上長。
	sort.SliceStable(rows, func(i, j int) bool { return rows[i].y > rows[j].y })
	for i := range rows {
		sort.SliceStable(rows[i].items, func(a, b int) bool {
			return rows[i].items[a].X < rows[i].items[b].X
		})
	}
	return rows
}

// linesOf 把一欄的字排成一列一列的文字。
func linesOf(ts []pdf.Text) []Line {
	rows := groupRows(ts)
	left := 1e18
	for _, r := range rows {
		if len(r.items) > 0 && r.items[0].X < left {
			left = r.items[0].X
		}
	}
	var out []Line
	for _, r := range rows {
		var sb strings.Builder
		var prevEnd, size float64
		havePrev := false
		for _, t := range r.items {
			if t.Size > size {
				size = t.Size
			}
			// 上一個字沒有寬度資訊時不判斷間距。硬算的話會拿「上一個字的
			// 原點」當結尾,每一個字後面都出現一個假的缺口 ——
			// 結果是每個字之間都被塞一個空格。
			// [雷] PDF 裡**沒有空格字元**,字與字之間的空白是靠座標
			// 間距表示的。不還原的話整頁會變成
			// 「Trace-basedJust-in-TimeTypeSpecialization」這樣。
			if havePrev && t.X-prevEnd > t.Size*spaceRatio {
				sb.WriteByte(' ')
			}
			sb.WriteString(t.S)
			prevEnd = t.X + t.W
			havePrev = t.W > 0
		}
		text := trimRight(sb.String())
		if strings.TrimSpace(text) == "" {
			continue
		}
		indent := 0
		if size > 0 && len(r.items) > 0 && left < 1e17 {
			if x := r.items[0].X - left; x > 0 {
				indent = int(x / (size * 0.5))
			}
		}
		out = append(out, Line{Y: r.y, Indent: indent, Text: text})
	}
	return out
}

// 分欄偵測的門檻。
const (
	// minColRows 是一欄至少要有幾列才算數。
	minColRows = 8
	// minPageRows 是整頁至少要有幾列才考慮分欄。
	minPageRows = 15
	// fullLineRatio 是「這一列算填滿」的門檻(佔欄寬的比例)。
	fullLineRatio = 0.6
	// minFullRatio 是一欄裡至少要有多少比例的列是填滿的。
	minFullRatio = 0.6
	// maxColumns 是最多分成幾欄。
	maxColumns = 3
)

// columns 把一頁的字分成欄。
//
// 判準是「一條從頁首通到頁尾、完全沒有字跨過的縱向空白帶」。這個條件
// 本身很好判斷,難的是**它同時也是表格的樣子** —— 一份滿版的表格,
// 欄與欄之間也是這樣的空白帶,而表格要橫著讀,分欄要直著讀,兩者
// 剛好相反。分錯的話讀出來的每一句話都是斷的,但畫面上看起來很正常。
//
// 所以除了空白帶還加三道條件:整頁夠多列、每一欄夠多列、而且每一欄
// 大部分的列都接近欄寬(內文會填滿一欄,表格的儲存格不會)。
// 寧可漏判 —— 漏判只是把兩欄併成一列讀,還看得懂。
func columns(ts []pdf.Text, x0, x1 float64) [][]pdf.Text {
	single := [][]pdf.Text{ts}
	rows := groupRows(ts)
	if len(rows) < minPageRows || x1 <= x0 {
		return single
	}
	med := medianSize(ts)
	if med <= 0 {
		return single
	}
	// 欄間距通常是一個半到兩個字寬。門檻訂得太高會漏掉真正的雙欄
	// (一公分的欄距在 12 點字下只有 2.4 個字寬),而擋掉表格靠的是
	// 下面那道「有沒有填滿」的檢查,不是靠這個。
	gapMin := med * 1.5
	if gapMin < 10 {
		gapMin = 10
	}

	// 佔用圖:每一點寬一格,有字經過就標起來。
	n := int(x1-x0) + 2
	if n < 16 || n > 20000 {
		return single
	}
	occ := make([]bool, n)
	for _, t := range ts {
		a := int(t.X - x0)
		b := int(t.X + t.W - x0)
		if b < a {
			a, b = b, a
		}
		for i := a; i <= b; i++ {
			if i >= 0 && i < n {
				occ[i] = true
			}
		}
	}
	// 找出兩側都有字的空白帶。
	var cuts []float64
	i := 0
	for i < n && !occ[i] {
		i++
	}
	last := i
	for i < n {
		if occ[i] {
			i++
			continue
		}
		start := i
		for i < n && !occ[i] {
			i++
		}
		if i >= n {
			break // 右邊界的空白不是欄間距
		}
		if float64(i-start) >= gapMin && start > last {
			cuts = append(cuts, x0+float64(start+i)/2)
		}
	}
	if len(cuts) == 0 || len(cuts) >= maxColumns {
		return single
	}

	groups := make([][]pdf.Text, len(cuts)+1)
	for _, t := range ts {
		g := sort.SearchFloat64s(cuts, t.X)
		groups[g] = append(groups[g], t)
	}
	// 每一欄可以用到多寬,由切點與整頁文字的左右邊界決定。
	// 拿「這一欄實際最寬的一列」當分母是不行的:表格的儲存格
	// 每一列一樣寬,除下來永遠是 100%,兩者就分不出來了。
	minX, maxX := textBounds(ts)
	bounds := append([]float64{minX}, cuts...)
	bounds = append(bounds, maxX)
	for i, g := range groups {
		if !looksLikeColumn(g, bounds[i+1]-bounds[i]) {
			return single
		}
	}
	return groups
}

// textBounds 是整頁文字的左右邊界。
func textBounds(ts []pdf.Text) (float64, float64) {
	lo, hi := 1e18, -1e18
	for _, t := range ts {
		if t.X < lo {
			lo = t.X
		}
		if t.X+t.W > hi {
			hi = t.X + t.W
		}
	}
	return lo, hi
}

// looksLikeColumn 判斷一群字看起來像不像一欄內文。
//
// 判準是「大部分的列有沒有把這一欄填滿」。內文會填滿一欄(最後一列
// 除外),表格的儲存格不會 —— 而兩者在版面上都是一條通到底的空白帶
// 隔開的兩堆字,只有這一點分得出來。
func looksLikeColumn(ts []pdf.Text, regionW float64) bool {
	rows := groupRows(ts)
	if len(rows) < minColRows || regionW <= 0 {
		return false
	}
	full := 0
	for _, r := range rows {
		if rowWidth(r) >= regionW*fullLineRatio {
			full++
		}
	}
	return float64(full)/float64(len(rows)) >= minFullRatio
}

func rowWidth(r row) float64 {
	if len(r.items) == 0 {
		return 0
	}
	last := r.items[len(r.items)-1]
	return last.X + last.W - r.items[0].X
}

func medianSize(ts []pdf.Text) float64 {
	sizes := make([]float64, 0, len(ts))
	for _, t := range ts {
		if t.Size > 0 {
			sizes = append(sizes, t.Size)
		}
	}
	if len(sizes) == 0 {
		return 0
	}
	sort.Float64s(sizes)
	return sizes[len(sizes)/2]
}

func abs(f float64) float64 {
	if f < 0 {
		return -f
	}
	return f
}
