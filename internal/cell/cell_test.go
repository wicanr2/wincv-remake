package cell

import "testing"

// keyword_*.cfg 裡的 ltgray 要對到 #C5C5C5 那一個定義,不是檔案清單用的 #C0C0C0。
// 兩個值差 5/255,眼睛看不出來,但它們在 image 裡是兩個不同的 word ——
// 這條擋的是「反正是灰色」的簡化。
func TestByNameLtGrayPicksLaterDefinition(t *testing.T) {
	got, ok := ByName("ltgray")
	if !ok {
		t.Fatal("ltgray 查不到")
	}
	if got != LtGray2 {
		t.Errorf("ByName(\"ltgray\") = %v, 想要 LtGray2(#C5C5C5)", got)
	}
	// 其餘名字照舊,不能被上面那條特例波及。
	for _, c := range []struct {
		name string
		want Color
	}{{"black", Black}, {"ltgreen", LtGreen}, {"white", White}, {"gray", Gray}} {
		if got, ok := ByName(c.name); !ok || got != c.want {
			t.Errorf("ByName(%q) = %v,%v, 想要 %v", c.name, got, ok, c.want)
		}
	}
	if _, ok := ByName("nosuchcolor"); ok {
		t.Error("不存在的顏色名應該回 false")
	}
}
