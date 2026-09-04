package archive

import (
	"archive/zip"
	"fmt"
	"github.com/wicanr2/wincv-remake/internal/i18n"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"
)

// Extract 把壓縮檔裡的東西解到 dstDir。
//
// names 為空時解全部;否則只解這些(相對壓縮檔內部路徑)。
// 回傳解出來的檔案數。
func (a *FS) Extract(dstDir string, names []string, progress func(string, int, int)) (int, error) {
	want := map[string]bool{}
	for _, n := range names {
		want[strings.Trim(n, "/")] = true
	}

	var todo []entry
	for _, e := range a.entries {
		if e.isDir {
			continue
		}
		if len(want) > 0 && !want[e.path] && !underAny(e.path, want) {
			continue
		}
		todo = append(todo, e)
	}

	n := 0
	for i, e := range todo {
		if err := extractOne(dstDir, e); err != nil {
			return n, fmt.Errorf("%s: %w", e.path, err)
		}
		n++
		if progress != nil {
			progress(e.path, i+1, len(todo))
		}
	}
	return n, nil
}

func underAny(p string, want map[string]bool) bool {
	for w := range want {
		if strings.HasPrefix(p, w+"/") {
			return true
		}
	}
	return false
}

// extractOne 解一個項目。
//
// [HARD] 壓縮檔裡的路徑不可信。含有 `..` 的路徑可以寫到目標目錄之外
// (zip slip),所以每一個目的路徑都要確認仍在 dstDir 底下。
func extractOne(dstDir string, e entry) error {
	clean := path.Clean("/" + e.path)[1:]
	if clean == "" || clean == "." {
		return nil
	}
	dst := filepath.Join(dstDir, filepath.FromSlash(clean))

	absRoot, err := filepath.Abs(dstDir)
	if err != nil {
		return err
	}
	absDst, err := filepath.Abs(dst)
	if err != nil {
		return err
	}
	if absDst != absRoot && !strings.HasPrefix(absDst, absRoot+string(os.PathSeparator)) {
		return fmt.Errorf(i18n.T("路徑跑到目標目錄之外了: %q"), e.path)
	}

	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	rc, err := e.open()
	if err != nil {
		return err
	}
	defer rc.Close()

	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, rc); err != nil {
		out.Close()
		return err
	}
	if err := out.Close(); err != nil {
		return err
	}
	if !e.modTime.IsZero() {
		os.Chtimes(dst, e.modTime, e.modTime)
	}
	return nil
}

// CreateZip 把 srcDir 底下的 names 打包成一個 zip。
//
// 只做 ZIP。原版可以建 RAR 與其他格式(靠外掛 DLL),但那些格式的
// **編碼器**在 Go 這邊沒有,而且 RAR 的壓縮演算法是專有的。
// 讀得到不代表寫得出來 —— 這一點在 Formats 表裡沒有分開標示,
// 因為原版對使用者也是同一個「Z / Alt-Z」介面。
func CreateZip(dst, srcDir string, names []string, progress func(string, int, int)) error {
	f, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer f.Close()
	zw := zip.NewWriter(f)

	total := len(names)
	for i, n := range names {
		if err := addToZip(zw, srcDir, n, ""); err != nil {
			zw.Close()
			return err
		}
		if progress != nil {
			progress(n, i+1, total)
		}
	}
	return zw.Close()
}

func addToZip(zw *zip.Writer, root, name, prefix string) error {
	full := filepath.Join(root, name)
	si, err := os.Stat(full)
	if err != nil {
		return err
	}
	inner := path.Join(prefix, filepath.ToSlash(name))

	if si.IsDir() {
		entries, err := os.ReadDir(full)
		if err != nil {
			return err
		}
		for _, e := range entries {
			if err := addToZip(zw, full, e.Name(), inner); err != nil {
				return err
			}
		}
		return nil
	}

	h := &zip.FileHeader{Name: inner, Method: zip.Deflate}
	h.Modified = si.ModTime()
	h.SetMode(si.Mode().Perm())
	w, err := zw.CreateHeader(h)
	if err != nil {
		return err
	}
	in, err := os.Open(full)
	if err != nil {
		return err
	}
	defer in.Close()
	_, err = io.Copy(w, in)
	return err
}

var _ = time.Time{}
