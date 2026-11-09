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

// 倚天 2000 的檔名排在 3.53 之後,當作同一個來源的候選。
func TestStdNamesIncludeEten2000(t *testing.T) {
	m, _ := Find("eten-m")
	if got := m.StdNames(); len(got) < 2 || got[0] != "STD.24M" || got[1] != "STDFONT.24" {
		t.Fatalf("明體的候選檔名不對: %v", got)
	}
	k, _ := Find("eten-k")
	if got := k.StdNames(); len(got) != 2 || got[1] != "STDFONT.24K" {
		t.Fatalf("楷體的候選檔名不對: %v", got)
	}
}
