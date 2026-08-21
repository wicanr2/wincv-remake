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

// 印在畫面外的列不該崩潰。
//
// 半形與全形要走同一套邊界規則:原本全形那一支只檢查 x 不檢查 y,
// 於是同一個座標印英數沒事、印中文 panic。
func TestPrintOutOfRangeRowDoesNotPanic(t *testing.T) {
	s := New(80, 25)
	cases := []struct {
		name string
		x, y int
		text string
	}{
		{"全形超出下緣", 2, 26, "中文字"},
		{"半形超出下緣", 2, 26, "abc"},
		{"全形負列", 2, -1, "中文字"},
		{"半形負列", 2, -1, "abc"},
		{"全形剛好在最後一列", 2, 24, "中文字"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			defer func() {
				if r := recover(); r != nil {
					t.Fatalf("panic 了: %v", r)
				}
			}()
			s.Print(c.x, c.y, c.text, LtGray, Black)
		})
	}
	// 最後一列是合法的,要真的寫進去 —— 不能為了不崩潰把它也擋掉。
	if n := s.Print(2, 24, "中", LtGray, Black); n != 2 {
		t.Fatalf("最後一列應該寫得進去,寫了 %d 格", n)
	}
}
