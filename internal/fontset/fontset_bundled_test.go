//go:build fonts

package fontset

import "testing"

func TestEten24FallsBackToBundledSources(t *testing.T) {
	for _, tc := range []struct{ std, spc string }{
		{"/missing/STD.24M", "/missing/SPCFONT.24"},
		{"/missing/stdfont.24f", "/missing/spcfont.24"},
	} {
		f, err := Eten24(tc.std, tc.spc)
		if err != nil {
			t.Fatalf("Eten24(%q): %v", tc.std, err)
		}
		if f.W != 24 || f.H != 24 {
			t.Fatalf("Eten24(%q) 尺寸=%dx%d", tc.std, f.W, f.H)
		}
	}
}
