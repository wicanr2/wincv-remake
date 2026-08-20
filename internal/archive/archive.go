// Package archive 讓壓縮檔可以像目錄一樣被瀏覽。
//
// 原版靠一堆 Windows DLL(unrar.dll、unlha32.dll、CAB32.DLL…)。
// 那些是 Win32-only,包進來就失去 Linux 與 macOS,所以這裡一律用純 Go。
//
// 格式支援是逐步補齊的,不是一次到位;但**不會因為冷門就砍掉**
// (rulebook/83:完整性優先於投報)。目前狀態見 Formats。
package archive

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/bzip2"
	"compress/gzip"
	"fmt"
	"io"
	"os"
	"path"
	"sort"
	"strings"
	"time"

	"github.com/wicanr2/wincv-remake/internal/vfs"
)

// Format 是一種壓縮格式的支援狀態。
type Format struct {
	Ext       []string
	Name      string
	Supported bool
	Note      string
}

// Formats 是原版支援的全部格式與 remake 目前的狀態。
// 這張表是進度的唯一真相,不要另外在文件裡重複維護一份。
var Formats = []Format{
	{[]string{".zip"}, "ZIP", true, "archive/zip"},
	{[]string{".tar"}, "TAR", true, "archive/tar"},
	{[]string{".gz", ".tgz", ".tar.gz"}, "GZIP", true, "compress/gzip"},
	{[]string{".bz2", ".tbz", ".tar.bz2"}, "BZIP2", true, "compress/bzip2"},
	{[]string{".rar"}, "RAR", false, "待接 nwaples/rardecode"},
	{[]string{".7z"}, "7-Zip", false, "待接 bodgit/sevenzip"},
	{[]string{".lzh", ".lha"}, "LHA", false, "Go 實作稀少,可能自寫"},
	{[]string{".arj"}, "ARJ", false, "無成熟 Go 實作,需自寫"},
	{[]string{".ace"}, "ACE", false, "格式封閉,排在最後"},
	{[]string{".cab"}, "CAB", false, "待寫 MSZIP"},
	{[]string{".z", ".taz"}, "compress", false, "LZW,需自寫"},
	{[]string{".arc", ".pak"}, "ARC/PAK", false, "原版也是外掛,排在最後"},
}

// DetectFormat 依副檔名判斷格式。回傳的第二個值表示認不認得。
func DetectFormat(name string) (Format, bool) {
	low := strings.ToLower(name)
	// 先比長的(.tar.gz 要贏過 .gz)
	var best Format
	var bestLen int
	found := false
	for _, f := range Formats {
		for _, e := range f.Ext {
			if strings.HasSuffix(low, e) && len(e) > bestLen {
				best, bestLen, found = f, len(e), true
			}
		}
	}
	return best, found
}

// IsArchive 判斷這個檔名看起來是不是壓縮檔(不論支不支援)。
func IsArchive(name string) bool {
	_, ok := DetectFormat(name)
	return ok
}

// entry 是壓縮檔裡的一筆。
type entry struct {
	path    string // 檔案內的完整路徑,以 / 分隔
	size    int64
	modTime time.Time
	isDir   bool
	open    func() (io.ReadCloser, error)
}

// FS 把一個壓縮檔包成 vfs.FS。
//
// 路徑形式是 "<壓縮檔路徑>!<內部路徑>",例如 "/w/a.zip!docs"。
// 這樣 browser 完全不必知道自己在壓縮檔裡。
type FS struct {
	name    string // 壓縮檔本身的路徑
	entries []entry
	dirs    map[string]bool
}

const sep = "!"

// Open 開啟一個壓縮檔。
func Open(name string) (*FS, error) {
	f, ok := DetectFormat(name)
	if !ok {
		return nil, fmt.Errorf("%s: 認不出壓縮格式", path.Base(name))
	}
	if !f.Supported {
		return nil, fmt.Errorf("%s: %s 格式還沒實作(%s)", path.Base(name), f.Name, f.Note)
	}

	a := &FS{name: name, dirs: map[string]bool{}}
	var err error
	switch f.Name {
	case "ZIP":
		err = a.loadZip(name)
	case "TAR":
		err = a.loadTarFile(name, nil)
	case "GZIP":
		err = a.loadCompressed(name, func(r io.Reader) (io.Reader, error) { return gzip.NewReader(r) })
	case "BZIP2":
		err = a.loadCompressed(name, func(r io.Reader) (io.Reader, error) { return bzip2.NewReader(r), nil })
	default:
		err = fmt.Errorf("%s 沒有對應的讀取器", f.Name)
	}
	if err != nil {
		return nil, err
	}
	a.buildDirs()
	return a, nil
}

func (a *FS) loadZip(name string) error {
	zr, err := zip.OpenReader(name)
	if err != nil {
		return err
	}
	// zip.ReadCloser 要活到所有 entry 都讀完,所以不在這裡關。
	for _, f := range zr.File {
		f := f
		a.entries = append(a.entries, entry{
			path:    strings.TrimSuffix(f.Name, "/"),
			size:    int64(f.UncompressedSize64),
			modTime: f.Modified,
			isDir:   f.FileInfo().IsDir(),
			open:    func() (io.ReadCloser, error) { return f.Open() },
		})
	}
	return nil
}

// loadCompressed 處理單檔壓縮(.gz / .bz2)。裡面若是 tar 就展開成多筆,
// 否則就是一個檔 —— 這正是 .tar.gz 與 .txt.gz 的差別。
func (a *FS) loadCompressed(name string, wrap func(io.Reader) (io.Reader, error)) error {
	raw, err := os.ReadFile(name)
	if err != nil {
		return err
	}
	zr, err := wrap(bytes.NewReader(raw))
	if err != nil {
		return err
	}
	data, err := io.ReadAll(zr)
	if err != nil {
		return err
	}
	if a.loadTarBytes(data) == nil && len(a.entries) > 0 {
		return nil
	}
	a.entries = nil
	inner := strings.TrimSuffix(path.Base(name), path.Ext(name))
	st, _ := os.Stat(name)
	mt := time.Time{}
	if st != nil {
		mt = st.ModTime()
	}
	a.entries = append(a.entries, entry{
		path:    inner,
		size:    int64(len(data)),
		modTime: mt,
		open:    func() (io.ReadCloser, error) { return io.NopCloser(bytes.NewReader(data)), nil },
	})
	return nil
}

func (a *FS) loadTarFile(name string, _ any) error {
	data, err := os.ReadFile(name)
	if err != nil {
		return err
	}
	return a.loadTarBytes(data)
}

func (a *FS) loadTarBytes(data []byte) error {
	tr := tar.NewReader(bytes.NewReader(data))
	var got []entry
	for {
		h, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		off, size := int64(0), h.Size
		_ = off
		body := make([]byte, size)
		if _, err := io.ReadFull(tr, body); err != nil && err != io.EOF {
			return err
		}
		b := body
		got = append(got, entry{
			path:    strings.TrimSuffix(h.Name, "/"),
			size:    h.Size,
			modTime: h.ModTime,
			isDir:   h.Typeflag == tar.TypeDir,
			open:    func() (io.ReadCloser, error) { return io.NopCloser(bytes.NewReader(b)), nil },
		})
	}
	a.entries = got
	return nil
}

// buildDirs 補出中間層目錄。很多壓縮檔只存檔案、不存目錄項,
// 沒補的話 "docs/a.txt" 會讓 "docs" 這一層看起來不存在。
func (a *FS) buildDirs() {
	for _, e := range a.entries {
		if e.isDir {
			a.dirs[e.path] = true
		}
		for d := path.Dir(e.path); d != "." && d != "/" && d != ""; d = path.Dir(d) {
			a.dirs[d] = true
		}
	}
}

// Path 組出這個壓縮檔內某個目錄的 vfs 路徑。
func (a *FS) Path(inner string) string {
	if inner == "" || inner == "." {
		return a.name + sep
	}
	return a.name + sep + inner
}

// Root 是壓縮檔的最上層。
func (a *FS) Root() string { return a.Path("") }

// split 把 "a.zip!docs/x" 拆成 ("a.zip", "docs/x")。
func split(p string) (string, string) {
	if i := strings.Index(p, sep); i >= 0 {
		return p[:i], strings.Trim(p[i+1:], "/")
	}
	return p, ""
}

func (a *FS) ReadDir(dir string) ([]vfs.Entry, error) {
	_, inner := split(dir)
	seen := map[string]bool{}
	var out []vfs.Entry

	add := func(name string, isDir bool, size int64, mt time.Time) {
		if name == "" || seen[name] {
			return
		}
		seen[name] = true
		out = append(out, vfs.Entry{Name: name, IsDir: isDir, Size: size, ModTime: mt})
	}

	for _, e := range a.entries {
		rel, ok := under(e.path, inner)
		if !ok {
			continue
		}
		if i := strings.IndexByte(rel, '/'); i >= 0 {
			add(rel[:i], true, 0, time.Time{}) // 中間層目錄
			continue
		}
		add(rel, e.isDir, e.size, e.modTime)
	}
	for d := range a.dirs {
		if rel, ok := under(d, inner); ok && rel != "" && !strings.Contains(rel, "/") {
			add(rel, true, 0, time.Time{})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// under 判斷 p 是不是在 dir 底下,是的話回傳相對路徑。
func under(p, dir string) (string, bool) {
	if dir == "" {
		return p, true
	}
	if !strings.HasPrefix(p, dir+"/") {
		return "", false
	}
	return p[len(dir)+1:], true
}

func (a *FS) Open(name string) (io.ReadCloser, error) {
	_, inner := split(name)
	for _, e := range a.entries {
		if e.path == inner {
			return e.open()
		}
	}
	return nil, fmt.Errorf("%s: 壓縮檔內找不到這個項目", inner)
}

// Label 顯示成 "a.zip!/docs",讓使用者一眼看出自己在壓縮檔裡。
func (a *FS) Label(dir string) string {
	outer, inner := split(dir)
	if inner == "" {
		return path.Base(outer) + sep
	}
	return path.Base(outer) + sep + inner
}

// Name 是壓縮檔本身的路徑。
func (a *FS) Name() string { return a.name }

// IsRoot 判斷某個路徑是不是這個壓縮檔的最上層 —— 在最上層再往上
// 就要跳回真實檔案系統。
func (a *FS) IsRoot(dir string) bool {
	_, inner := split(dir)
	return inner == ""
}
