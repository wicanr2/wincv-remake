package vecfont

import "testing"

func TestGlyphOutline(t *testing.T) {
	s := Default()
	if !s.Ready() {
		t.Skip("這台機器沒有可用的字型")
	}
	segs, adv, ok := s.Glyph('A')
	if !ok {
		t.Fatal("'A' 查不到字形")
	}
	if len(segs) == 0 {
		t.Error("'A' 沒有外框")
	}
	if adv <= 0 || adv > Em*2 {
		t.Errorf("前進寬度 %v 不合理", adv)
	}
	if segs[0].Op != 'm' {
		t.Errorf("外框第一段是 %q,應該是 moveto", segs[0].Op)
	}
}

// 空白有寬度但沒有形狀。兩者混為一談的話,空白會被當成缺字 ——
// 然後整行的字距就錯了。
func TestGlyphSpaceHasWidthButNoOutline(t *testing.T) {
	s := Default()
	if !s.Ready() {
		t.Skip("這台機器沒有可用的字型")
	}
	segs, adv, ok := s.Glyph(' ')
	if !ok {
		t.Fatal("空白查不到")
	}
	if len(segs) != 0 {
		t.Errorf("空白不該有外框,卻有 %d 段", len(segs))
	}
	if adv <= 0 {
		t.Errorf("空白的寬度是 %v", adv)
	}
}

// 查兩次要得到同一份結果(有快取,不能第二次才走別條路)。
func TestGlyphCached(t *testing.T) {
	s := Default()
	if !s.Ready() {
		t.Skip("這台機器沒有可用的字型")
	}
	a, aw, _ := s.Glyph('W')
	b, bw, _ := s.Glyph('W')
	if len(a) != len(b) || aw != bw {
		t.Errorf("兩次查詢不一致:%d/%v 與 %d/%v", len(a), aw, len(b), bw)
	}
}
