// Package fileop 是檔案操作:拷貝、移動、改名、刪除、比對。
//
// 對應原版主畫面的 C / M / R / D 與 Alt-C。行為上有幾件事是從原版的
// 字串與 changelog 推出來的:
//
//   - 覆蓋詢問有「不覆蓋」與「全部覆蓋」兩種答案
//     (image 字串「&N 不覆蓋」「&A 全部覆蓋」)
//   - 刪除可以選「不放資源回收桶時,先把內容填 0 再刪」
//     (0.5 版新增的選項)
//   - 連續編號改名(image 字串「連續編號改名」「編號起始值:」「&D 遞減編號」)
//   - Alt-C 比對兩個標記檔案的內容是否相同(binary compare)
package fileop

import (
	"bytes"
	"crypto/rand"
	"errors"
	"fmt"
	"github.com/wicanr2/wincv-remake/internal/i18n"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// Overwrite 是遇到同名檔案時的處置。
type Overwrite int

const (
	// Ask 表示要問呼叫端。Op 會透過 Confirm 回呼詢問。
	Ask Overwrite = iota
	// Skip 是「不覆蓋」,跳過這一個繼續下一個。
	Skip
	// All 是「全部覆蓋」,之後都不再問。
	All
)

// Confirm 是覆蓋詢問的回呼。回傳這一次要不要覆蓋,以及要不要
// 把答案套用到之後所有檔案。
type Confirm func(dst string) (overwrite bool, applyToAll bool)

// Progress 每處理完一個檔案呼叫一次。
type Progress func(name string, done, total int)

// Options 是一次操作的設定。
type Options struct {
	Overwrite Overwrite
	Confirm   Confirm
	Progress  Progress

	// ZeroFill 為 true 時,刪除前先把檔案內容填 0。
	// 對應原版的「刪除檔案時若不放到資源回收桶,則使用填0方式刪除?」。
	ZeroFill bool
}

// Result 是一次批次操作的結果。
type Result struct {
	Done    []string
	Skipped []string
	Errors  map[string]error
}

func newResult() *Result {
	return &Result{Errors: map[string]error{}}
}

// Failed 回傳有沒有任何一個檔案失敗。
func (r *Result) Failed() bool { return len(r.Errors) > 0 }

// Summary 給狀態列用的一行摘要。
func (r *Result) Summary(verb string) string {
	s := fmt.Sprintf(i18n.T("%s %d 個檔案"), verb, len(r.Done))
	if len(r.Skipped) > 0 {
		s += fmt.Sprintf(i18n.T(",跳過 %d 個"), len(r.Skipped))
	}
	if len(r.Errors) > 0 {
		s += fmt.Sprintf(i18n.T(",失敗 %d 個"), len(r.Errors))
	}
	return s
}

// Copy 把 srcDir 底下的 names 拷貝到 dstDir。
func Copy(srcDir, dstDir string, names []string, o Options) *Result {
	return batch(srcDir, dstDir, names, o, copyOne)
}

// Move 搬移。同一個檔案系統上用 rename,跨檔案系統退回「拷貝後刪除」。
func Move(srcDir, dstDir string, names []string, o Options) *Result {
	return batch(srcDir, dstDir, names, o, moveOne)
}

func batch(srcDir, dstDir string, names []string, o Options,
	fn func(src, dst string, o *Options) error) *Result {

	res := newResult()
	if srcDir == dstDir {
		res.Errors["*"] = errors.New(i18n.T("來源與目的是同一個目錄"))
		return res
	}
	for i, n := range names {
		src := filepath.Join(srcDir, n)
		dst := filepath.Join(dstDir, n)

		if !decideOverwrite(dst, &o) {
			res.Skipped = append(res.Skipped, n)
			continue
		}
		if err := fn(src, dst, &o); err != nil {
			res.Errors[n] = err
		} else {
			res.Done = append(res.Done, n)
		}
		if o.Progress != nil {
			o.Progress(n, i+1, len(names))
		}
	}
	return res
}

// decideOverwrite 回傳要不要繼續處理這個目的檔案。
func decideOverwrite(dst string, o *Options) bool {
	if _, err := os.Stat(dst); err != nil {
		return true // 不存在,直接做
	}
	switch o.Overwrite {
	case All:
		return true
	case Skip:
		return false
	}
	if o.Confirm == nil {
		return false // 沒人可問就當作不覆蓋 —— 不要默默蓋掉使用者的檔案
	}
	ow, all := o.Confirm(dst)
	if all {
		if ow {
			o.Overwrite = All
		} else {
			o.Overwrite = Skip
		}
	}
	return ow
}

func copyOne(src, dst string, _ *Options) error {
	si, err := os.Stat(src)
	if err != nil {
		return err
	}
	if si.IsDir() {
		return copyDir(src, dst)
	}
	return copyFile(src, dst, si)
}

func copyFile(src, dst string, si os.FileInfo) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	// 先寫到暫存檔再改名。中途失敗時目的地不會留下半個檔 ——
	// 半個檔比沒有檔更糟,因為它看起來是成功的。
	tmp := dst + ".wincv-tmp"
	out, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, si.Mode().Perm())
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		os.Remove(tmp)
		return err
	}
	if err := out.Close(); err != nil {
		os.Remove(tmp)
		return err
	}
	if err := os.Rename(tmp, dst); err != nil {
		os.Remove(tmp)
		return err
	}
	// 保留時間戳 —— 檔案管理程式把日期弄丟是很惱人的事。
	return os.Chtimes(dst, si.ModTime(), si.ModTime())
}

func copyDir(src, dst string) error {
	si, err := os.Stat(src)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dst, si.Mode().Perm()); err != nil {
		return err
	}
	entries, err := os.ReadDir(src)
	if err != nil {
		return err
	}
	for _, e := range entries {
		s, d := filepath.Join(src, e.Name()), filepath.Join(dst, e.Name())
		if e.IsDir() {
			if err := copyDir(s, d); err != nil {
				return err
			}
			continue
		}
		info, err := e.Info()
		if err != nil {
			return err
		}
		if err := copyFile(s, d, info); err != nil {
			return err
		}
	}
	return nil
}

func moveOne(src, dst string, o *Options) error {
	if err := os.Rename(src, dst); err == nil {
		return nil
	}
	// 跨檔案系統的 rename 會失敗,退回拷貝後刪除。
	if err := copyOne(src, dst, o); err != nil {
		return err
	}
	return os.RemoveAll(src)
}

// Rename 改一個檔案的名字。目的地已存在時一律拒絕 ——
// 改名不像拷貝,沒有「批次覆蓋」的合理情境。
func Rename(dir, from, to string) error {
	if to == "" || strings.ContainsAny(to, `/\`) {
		return fmt.Errorf(i18n.T("檔名不合法: %q"), to)
	}
	dst := filepath.Join(dir, to)
	if _, err := os.Stat(dst); err == nil {
		return fmt.Errorf(i18n.T("%s 已存在"), to)
	}
	return os.Rename(filepath.Join(dir, from), dst)
}

// Delete 刪除。ZeroFill 為 true 時先把內容覆寫成 0 再刪。
func Delete(dir string, names []string, o Options) *Result {
	res := newResult()
	for i, n := range names {
		p := filepath.Join(dir, n)
		var err error
		if o.ZeroFill {
			err = zeroFill(p)
		}
		if err == nil {
			err = os.RemoveAll(p)
		}
		if err != nil {
			res.Errors[n] = err
		} else {
			res.Done = append(res.Done, n)
		}
		if o.Progress != nil {
			o.Progress(n, i+1, len(names))
		}
	}
	return res
}

// zeroFill 把一個檔案的內容覆寫成 0。目錄則遞迴處理裡面的檔案。
func zeroFill(p string) error {
	si, err := os.Stat(p)
	if err != nil {
		return err
	}
	if si.IsDir() {
		entries, err := os.ReadDir(p)
		if err != nil {
			return err
		}
		for _, e := range entries {
			if err := zeroFill(filepath.Join(p, e.Name())); err != nil {
				return err
			}
		}
		return nil
	}
	f, err := os.OpenFile(p, os.O_WRONLY, 0)
	if err != nil {
		return err
	}
	defer f.Close()
	zero := make([]byte, 64*1024)
	left := si.Size()
	for left > 0 {
		n := int64(len(zero))
		if n > left {
			n = left
		}
		if _, err := f.Write(zero[:n]); err != nil {
			return err
		}
		left -= n
	}
	return f.Sync()
}

// Compare 比對兩個檔案的內容是否完全相同(原版 Alt-C)。
// 回傳第二個值是第一個不同的位移,相同時為 -1。
func Compare(a, b string) (same bool, at int64, err error) {
	fa, err := os.Open(a)
	if err != nil {
		return false, 0, err
	}
	defer fa.Close()
	fb, err := os.Open(b)
	if err != nil {
		return false, 0, err
	}
	defer fb.Close()

	sa, _ := fa.Stat()
	sb, _ := fb.Stat()
	if sa != nil && sb != nil && sa.Size() != sb.Size() {
		// 大小不同就一定不同,但仍然回報第一個相異位移,
		// 讓使用者知道是「前面就不一樣」還是「只是長度不同」。
		at, _ := firstDiff(fa, fb)
		return false, at, nil
	}
	at, err = firstDiff(fa, fb)
	if err != nil {
		return false, 0, err
	}
	return at < 0, at, nil
}

func firstDiff(a, b io.Reader) (int64, error) {
	const chunk = 64 * 1024
	ba, bb := make([]byte, chunk), make([]byte, chunk)
	var off int64
	for {
		na, ea := io.ReadFull(a, ba)
		nb, eb := io.ReadFull(b, bb)
		n := na
		if nb < n {
			n = nb
		}
		if i := bytes.Compare(ba[:n], bb[:n]); i != 0 {
			for k := 0; k < n; k++ {
				if ba[k] != bb[k] {
					return off + int64(k), nil
				}
			}
		}
		if na != nb {
			return off + int64(n), nil
		}
		off += int64(n)
		if ea != nil || eb != nil {
			if isEOF(ea) && isEOF(eb) {
				return -1, nil
			}
			if !isEOF(ea) {
				return 0, ea
			}
			return 0, eb
		}
	}
}

func isEOF(err error) bool {
	return err == nil || errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF)
}

var _ = rand.Read
