package vfs

import (
	"path/filepath"
	"testing"
)

func TestParentUsesNativePathSemantics(t *testing.T) {
	root := t.TempDir()
	child := filepath.Join(root, "child")
	if got, want := Parent(child), root; got != want {
		t.Fatalf("Parent(%q) = %q, want %q", child, got, want)
	}
	if got := Parent(root); got != filepath.Dir(root) {
		t.Fatalf("Parent(%q) = %q, want native parent %q", root, got, filepath.Dir(root))
	}
}
