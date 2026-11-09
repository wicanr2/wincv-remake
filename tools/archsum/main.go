// archsum 列出壓縮檔內每個檔案的 sha256,當 Rust 改寫的對照真值。
//
//	tools/go.sh run ./tools/archsum [-n 次數] 壓縮檔...
//
// 每列是「名稱的 hex<TAB>大小<TAB>sha256」,依名稱排序;名稱用 hex
// 是因為壓縮檔裡的檔名是原始位元組(常是 Big5),不一定是合法 UTF-8。
// Rust 端的 archsum 輸出同樣格式,兩邊直接 diff。
//
// -n 大於 1 時整個流程(打開壓縮檔、列目錄、讀出每個檔案)重做 n 次,
// 在 stderr 印出最快的一次花多久。打開要算進去:有些格式(LZH、ARJ、
// ARC…)在 archive.Open 時就把內容全部解好,之後的讀取只是讀記憶體。
package main

import (
	"crypto/sha256"
	"encoding/hex"
	"flag"
	"fmt"
	"io"
	"os"
	"sort"
	"time"

	"github.com/wicanr2/wincv-remake/internal/archive"
)

type file struct {
	path string
	size int64
	sum  string
}

func walk(a *archive.FS, dir string, out *[]string) error {
	es, err := a.ReadDir(a.Path(dir))
	if err != nil {
		return err
	}
	for _, e := range es {
		p := e.Name
		if dir != "" {
			p = dir + "/" + e.Name
		}
		if e.IsDir {
			if err := walk(a, p, out); err != nil {
				return err
			}
			continue
		}
		*out = append(*out, p)
	}
	return nil
}

// one 處理一個壓縮檔。解碼器 panic 時(例如 bodgit/sevenzip 在某些 7z 的
// 標頭上 index out of range)只記這一個檔失敗,其餘的照跑。
func one(name string, n int) (files []file, best time.Duration, failed bool) {
	defer func() {
		if r := recover(); r != nil {
			fmt.Fprintln(os.Stderr, name, "panic:", r)
			files, failed = nil, true
		}
	}()
	for round := 0; round < n; round++ {
		files = files[:0]
		t0 := time.Now()
		a, err := archive.Open(name)
		if err != nil {
			fmt.Fprintln(os.Stderr, name, err)
			return files, best, true
		}
		var paths []string
		if err := walk(a, "", &paths); err != nil {
			fmt.Fprintln(os.Stderr, name, err)
			return files, best, true
		}
		for _, p := range paths {
			rc, err := a.Open(a.Path(p))
			if err != nil {
				fmt.Fprintln(os.Stderr, name, p, err)
				failed = true
				continue
			}
			h := sha256.New()
			size, err := io.Copy(h, rc)
			rc.Close()
			if err != nil {
				// 讀到一半出錯的不列:那不是這個檔的內容(Rust 版相同)。
				fmt.Fprintln(os.Stderr, name, p, err)
				failed = true
				continue
			}
			files = append(files, file{p, size, hex.EncodeToString(h.Sum(nil))})
		}
		if d := time.Since(t0); round == 0 || d < best {
			best = d
		}
	}
	return files, best, failed
}

func main() {
	n := flag.Int("n", 1, "解壓縮重做幾次(計時取最快)")
	flag.Parse()
	failed := false
	for _, name := range flag.Args() {
		files, best, bad := one(name, *n)
		failed = failed || bad
		sort.Slice(files, func(i, j int) bool { return files[i].path < files[j].path })
		for _, f := range files {
			fmt.Printf("%x\t%d\t%s\n", f.path, f.size, f.sum)
		}
		fmt.Fprintf(os.Stderr, "%s: %d 個檔案,最快一次 %v\n", name, len(files), best)
	}
	if failed {
		os.Exit(1)
	}
}
