package cjk24

import "testing"

func TestSourcesHaveStableFiles(t *testing.T) {
	if s, ok := Find(Default); !ok || s.Std != "STD.24M" || s.Spc != "SPCFONT.24" {
		t.Fatalf("預設來源不正確: %+v, %v", s, ok)
	}
	if s, ok := Find("guoqiao"); !ok || s.Std != "stdfont.24f" || s.Spc != "spcfont.24" {
		t.Fatalf("國喬來源不正確: %+v, %v", s, ok)
	}
	if _, ok := Find("任意路徑"); ok {
		t.Fatal("未知值不可被當成可持久化代號")
	}
}
