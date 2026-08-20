// Package vfs 是真實目錄與壓縮檔內部共用的檔案系統介面。
//
// 「壓縮檔當目錄瀏覽」是 CView 的核心手感:游標移到 .zip 上按 Enter 就像進目錄。
// 如果目錄與壓縮檔走兩套路徑,browser 會被分岔汙染,所以兩者都實作同一個 FS。
package vfs

import (
	"io"
	"io/fs"
	"os"
	"path"
	"sort"
	"strings"
	"time"
)

// Entry 是列表裡的一筆。
type Entry struct {
	Name    string // 完整名稱,含副檔名
	Size    int64
	ModTime time.Time
	IsDir   bool

	// Link 是顯示在最右欄的補充資訊。原版在這一欄放「長檔名」
	// (主欄位是 8.3 短名);沒有短名的系統上放不下的長名也走這裡。
	Link string
}

// Base 回傳不含副檔名的主檔名。目錄整個名稱都算主檔名。
func (e Entry) Base() string {
	if e.IsDir {
		return e.Name
	}
	if i := strings.LastIndexByte(e.Name, '.'); i > 0 {
		return e.Name[:i]
	}
	return e.Name
}

// Ext 回傳不含點的副檔名。
func (e Entry) Ext() string {
	if e.IsDir {
		return ""
	}
	if i := strings.LastIndexByte(e.Name, '.'); i > 0 {
		return e.Name[i+1:]
	}
	return ""
}

// FS 是 browser 唯一認得的檔案系統。
type FS interface {
	// ReadDir 列出一個目錄。回傳的順序不保證,排序是 browser 的事。
	ReadDir(dir string) ([]Entry, error)
	// Open 讀一個檔案的內容,給 viewer 用。
	Open(name string) (io.ReadCloser, error)
	// Label 是顯示在路徑列的名字,例如 "C:\wincv" 或 "a.zip:/docs"。
	Label(dir string) string
}

// OS 是真實檔案系統。
type OS struct{}

func (OS) ReadDir(dir string) ([]Entry, error) {
	des, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	out := make([]Entry, 0, len(des))
	for _, de := range des {
		info, err := de.Info()
		if err != nil {
			continue // 列表途中檔案被刪掉是正常的,跳過而不是整個失敗
		}
		out = append(out, entryFromInfo(de.Name(), info))
	}
	return out, nil
}

func entryFromInfo(name string, info fs.FileInfo) Entry {
	return Entry{
		Name:    name,
		Size:    info.Size(),
		ModTime: info.ModTime(),
		IsDir:   info.IsDir(),
	}
}

func (OS) Open(name string) (io.ReadCloser, error) { return os.Open(name) }

func (OS) Label(dir string) string { return dir }

// SortKey 是排序方式。原版可在執行期切換。
type SortKey int

const (
	ByName SortKey = iota
	ByExt
	BySize
	ByTime
)

// Sort 依 key 排序,目錄永遠排在檔案前面。
//
// 名稱比較不分大小寫 —— 原版跑在 Windows 上,檔名大小寫不敏感,
// 排出來的順序是 7-zip32、aunzip32、big52gbk、BIG5…這種混合大小寫的字典序。
func Sort(es []Entry, key SortKey, desc bool) {
	sort.SliceStable(es, func(i, j int) bool {
		a, b := es[i], es[j]
		if a.IsDir != b.IsDir {
			return a.IsDir // 目錄在前,不受 desc 影響
		}
		var less bool
		switch key {
		case ByExt:
			ea, eb := strings.ToLower(a.Ext()), strings.ToLower(b.Ext())
			if ea != eb {
				less = ea < eb
			} else {
				less = strings.ToLower(a.Name) < strings.ToLower(b.Name)
			}
		case BySize:
			if a.Size != b.Size {
				less = a.Size < b.Size
			} else {
				less = strings.ToLower(a.Name) < strings.ToLower(b.Name)
			}
		case ByTime:
			if !a.ModTime.Equal(b.ModTime) {
				less = a.ModTime.Before(b.ModTime)
			} else {
				less = strings.ToLower(a.Name) < strings.ToLower(b.Name)
			}
		default:
			less = strings.ToLower(a.Name) < strings.ToLower(b.Name)
		}
		if desc {
			return !less
		}
		return less
	})
}

// Parent 回傳上一層目錄。已經在根目錄就回自己。
func Parent(dir string) string {
	p := path.Dir(path.Clean(dir))
	if p == dir {
		return dir
	}
	return p
}
