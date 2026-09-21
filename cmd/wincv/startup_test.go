package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/wicanr2/wincv-remake/internal/datadir"
)

func TestParseStartupArgs(t *testing.T) {
	for _, tc := range []struct {
		name string
		args []string
		want startupRequest
	}{
		{"empty", nil, startupRequest{}},
		{"plain target", []string{"c:/work/a.txt"}, startupRequest{target: "c:/work/a.txt"}},
		{"original directory", []string{"/p", "d:/game"}, startupRequest{target: "d:/game", mode: startupDirectory}},
		{"original editor", []string{"/e", "c:/abc.txt"}, startupRequest{target: "c:/abc.txt", mode: startupEdit}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := parseStartupArgs(tc.args)
			if err != nil {
				t.Fatal(err)
			}
			if got != tc.want {
				t.Fatalf("parseStartupArgs(%q) = %#v, want %#v", tc.args, got, tc.want)
			}
		})
	}
}

func TestParseStartupArgsRejectsAmbiguousInput(t *testing.T) {
	for _, args := range [][]string{{"/p"}, {"/e", ""}, {"one", "two"}, {"/p", "one", "two"}} {
		if _, err := parseStartupArgs(args); err == nil {
			t.Errorf("parseStartupArgs(%q) should reject ambiguous input", args)
		}
	}
}

func TestCJK24PathsUsePortableDataDir(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"stdfont.24f", "spcfont.24"} {
		if err := os.WriteFile(filepath.Join(dir, name), nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv(datadir.EnvHome, dir)
	std, spc, ok := cjk24Paths("guoqiao")
	if !ok || std != filepath.Join(dir, "stdfont.24f") || spc != filepath.Join(dir, "spcfont.24") {
		t.Fatalf("國喬路徑不受 WINCV_HOME 控制: %q %q %v", std, spc, ok)
	}
}
