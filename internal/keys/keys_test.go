package keys

import "testing"

func TestParse(t *testing.T) {
	cases := []struct {
		in   string
		want Key
	}{
		{"a", Ch('a')},
		{"F1", Named(F1)},
		{"F11", Named(F11)},
		{"Down", Named(Down)},
		{"Enter", Named(Enter)},
		{"Space", Ch(' ')},
		{"C-o", CtrlCh('o')},
		{"A-e", AltCh('e')},
		{"BackSpace", Named(Backspace)},
	}
	for _, c := range cases {
		got, ok := Parse(c.in)
		if !ok {
			t.Errorf("%q 解不出來", c.in)
			continue
		}
		if got != c.want {
			t.Errorf("%q = %+v,期望 %+v", c.in, got, c.want)
		}
	}
	if _, ok := Parse("Nope"); ok {
		t.Error("看不懂的名字應該回 false")
	}
}

// String 與 Parse 要能來回:keymap 文件裡的寫法就是 String 的輸出。
func TestParseStringRoundTrip(t *testing.T) {
	for _, k := range []Key{
		Ch('a'), Named(F6), CtrlCh('O'), AltCh('Z'), Named(PgDn),
	} {
		got, ok := Parse(k.String())
		if !ok || got != k {
			t.Errorf("%v -> %q -> %+v(ok=%v)", k, k.String(), got, ok)
		}
	}
}

func TestParseAll(t *testing.T) {
	ks, err := ParseAll("F1, Down ,Enter")
	if err != nil {
		t.Fatal(err)
	}
	if len(ks) != 3 || ks[0] != Named(F1) || ks[2] != Named(Enter) {
		t.Errorf("= %+v", ks)
	}
	if _, err := ParseAll("F1,???"); err == nil {
		t.Error("看不懂的按鍵應該報錯")
	}
}
