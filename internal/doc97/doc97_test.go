package doc97

import (
	"strings"
	"testing"

	"github.com/wicanr2/wincv-remake/internal/markdown"
)

// testdata 的兩份 .doc 是 LibreOffice 產生的真實 Word 97 檔案,
// 重建方式見 testdata/README.md。用真檔而不是自己組的最小檔案是必要的:
// FIB、piece table、屬性頁三者互相指位置,自己組的檔案只會驗到
// 自己對格式的理解,驗不到「真的 Word 檔長這樣」。
func openDoc(t *testing.T, name string) *Doc {
	t.Helper()
	d, err := Open("testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	return d
}

func text(bs []markdown.Block) string {
	var sb strings.Builder
	for _, b := range bs {
		for _, s := range b.Spans {
			sb.WriteString(s.Text)
		}
		for _, r := range b.Rows {
			sb.WriteString(strings.Join(r, "|"))
		}
		sb.WriteString("\n")
	}
	return sb.String()
}

func TestRichDocument(t *testing.T) {
	bs := openDoc(t, "rich.doc").Blocks()

	// 對照 LibreOffice 轉出來的純文字:內容要一字不差。
	want := []string{"第一章 標題", "這是一段粗體與斜體的中文內文。", "1.1 小節",
		"條列一", "條列二", "甲|乙丙|丁", "A plain ASCII paragraph with a link."}
	got := strings.Split(strings.TrimRight(text(bs), "\n"), "\n")
	if len(got) != len(want) {
		t.Fatalf("區塊數不對,拿到 %d:%q", len(got), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("第 %d 個區塊是 %q,要 %q", i, got[i], want[i])
		}
	}

	// 標題靠樣式識別碼認出來,與介面語言無關。
	if bs[0].Kind != markdown.Heading || bs[0].Level != 1 {
		t.Errorf("第一章:kind=%v level=%d", bs[0].Kind, bs[0].Level)
	}
	if bs[2].Kind != markdown.Heading || bs[2].Level != 2 {
		t.Errorf("小節:kind=%v level=%d", bs[2].Kind, bs[2].Level)
	}
	if bs[3].Kind != markdown.List || bs[4].Kind != markdown.List {
		t.Errorf("條列沒有認出來:%v / %v", bs[3].Kind, bs[4].Kind)
	}
	if bs[5].Kind != markdown.Table || len(bs[5].Rows) != 2 || bs[5].Rows[1][1] != "丁" {
		t.Errorf("表格不對:%v", bs[5].Rows)
	}
}

// 中文走 UTF-16 的片段,英文走單位元組的片段。同一份文件裡兩種都有,
// 而單位元組那一種一律是 cp1252 —— 不是文件的語系字碼頁。
func TestBothPieceKinds(t *testing.T) {
	got := text(openDoc(t, "rich.doc").Blocks())
	if !strings.Contains(got, "的中文內文") {
		t.Errorf("UTF-16 片段沒解出來:%q", got)
	}
	if !strings.Contains(got, "A plain ASCII paragraph") {
		t.Errorf("單位元組片段沒解出來:%q", got)
	}
}

func TestCharacterFormatting(t *testing.T) {
	bs := openDoc(t, "rich.doc").Blocks()
	var bold, italic, link string
	for _, b := range bs {
		for _, s := range b.Spans {
			switch {
			case s.Style&markdown.Bold != 0:
				bold += s.Text
			case s.Style&markdown.Italic != 0:
				italic += s.Text
			case s.Style&markdown.Link != 0:
				link += s.Text
			}
		}
	}
	if bold != "粗體" || italic != "斜體" {
		t.Errorf("粗體 %q,斜體 %q", bold, italic)
	}
	if link != "link" {
		t.Errorf("超連結的文字是 %q", link)
	}
}

func TestFootnotes(t *testing.T) {
	bs := openDoc(t, "notes.doc").Blocks()
	got := text(bs)
	if !strings.Contains(got, "[註 1]") {
		t.Errorf("正文少了註腳標記:%q", got)
	}
	if !strings.Contains(got, "這是註腳內容") {
		t.Errorf("文末少了註腳內容:%q", got)
	}
	// 註腳的文字接在正文後面,不能混進正文那一段。
	if strings.Contains(text(bs[:1]), "這是註腳內容") {
		t.Errorf("註腳跑進正文了:%q", got)
	}
}

func TestNotADoc(t *testing.T) {
	if _, err := Open("testdata/rich.html"); err == nil {
		t.Fatal("不是複合文件應該要失敗")
	}
}

// 有序清單要有編號。
//
// [雷] 編號方式不在段落屬性上:段落只說得出「屬於第幾串、第幾層」,
// 是編號還是項目符號要去清單定義表(PlfLst)裡查。而 lcbPlfLst **只涵蓋
// 前半段**(cLst + LSTF 陣列),後面接著的 LVL 陣列不算在裡面 ——
// 照 lcb 切下去剛好切在第一組 LVL 之前,讀到的 LSTF 完全正確、數量也對,
// 只是每一串都是零層,於是所有編號清單都變成項目符號。
// 沒有任何一個地方會報錯,因為切出來的那一段本身是完好的。
//
// 對照組是 LibreOffice 讀同一份檔案:它轉出的純文字是「1. 條列一」。
func TestOrderedListNumbers(t *testing.T) {
	bs := openDoc(t, "rich.doc").Blocks()
	var got []int
	for _, b := range bs {
		if b.Kind != markdown.List {
			continue
		}
		if !b.Ordered {
			t.Errorf("清單 %q 沒有被認成有序清單", text([]markdown.Block{b}))
			continue
		}
		got = append(got, b.Num)
	}
	if len(got) != 2 || got[0] != 1 || got[1] != 2 {
		t.Errorf("編號 = %v,預期 [1 2]", got)
	}
}

// 沒有清單定義的段落仍然要當清單畫出來:段落自己已經說了它屬於某一串,
// 那個事實比「查不到格式」更可靠 —— 至少縮排與符號是對的。
func TestListWithoutFormatStillLists(t *testing.T) {
	d := openDoc(t, "rich.doc")
	d.lists = map[uint32]*lstInfo{} // 假裝定義表讀不到
	d.lfo = nil
	n := 0
	for _, b := range d.Blocks() {
		if b.Kind == markdown.List {
			n++
			if b.Ordered {
				t.Error("查不到定義時不該自己宣稱是有序清單")
			}
		}
	}
	if n != 2 {
		t.Errorf("認出 %d 段清單,預期 2", n)
	}
}
