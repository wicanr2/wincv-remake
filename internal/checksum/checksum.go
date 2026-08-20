// Package checksum 做 MD5 與 SFV(CRC32)檢驗 —— 原版列出的功能之一。
//
// SFV 是 1990s 檔案交換圈的慣例:一個純文字檔,每行「檔名 CRC32」,
// 分號開頭是註解。它是校驗 usenet / BBS 下傳檔案完整性的標準做法。
package checksum

import (
	"bufio"
	"crypto/md5"
	"encoding/hex"
	"fmt"
	"hash/crc32"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// MD5File 算一個檔案的 MD5。
func MD5File(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := md5.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// CRC32File 算一個檔案的 CRC32(IEEE),回傳 8 位大寫十六進位。
func CRC32File(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := crc32.NewIEEE()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return fmt.Sprintf("%08X", h.Sum32()), nil
}

// SFVEntry 是 .sfv 裡的一行。
type SFVEntry struct {
	Name string
	CRC  string // 8 位大寫十六進位
}

// ParseSFV 讀一個 .sfv。分號開頭是註解,空行略過。
//
// 檔名可能含空白,所以以**最後一個空白**分隔檔名與 CRC,
// 不是第一個 —— 用第一個的話 "my file.zip ABCD1234" 會拆錯。
func ParseSFV(r io.Reader) ([]SFVEntry, error) {
	var out []SFVEntry
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for sc.Scan() {
		line := strings.TrimRight(sc.Text(), " \t\r")
		if line == "" || strings.HasPrefix(strings.TrimSpace(line), ";") {
			continue
		}
		i := strings.LastIndexAny(line, " \t")
		if i < 0 {
			continue
		}
		name := strings.TrimSpace(line[:i])
		crc := strings.TrimSpace(line[i+1:])
		if name == "" || len(crc) != 8 {
			continue
		}
		if _, err := strconv.ParseUint(crc, 16, 32); err != nil {
			continue
		}
		out = append(out, SFVEntry{Name: name, CRC: strings.ToUpper(crc)})
	}
	return out, sc.Err()
}

// WriteSFV 產生一個 .sfv。
func WriteSFV(w io.Writer, entries []SFVEntry) error {
	for _, e := range entries {
		if _, err := fmt.Fprintf(w, "%s %s\r\n", e.Name, e.CRC); err != nil {
			return err
		}
	}
	return nil
}

// Result 是一筆檢驗結果。
type Result struct {
	Name    string
	Want    string
	Got     string
	OK      bool
	Missing bool
}

// VerifySFV 依 .sfv 檢驗同目錄下的檔案。
func VerifySFV(sfvPath string) ([]Result, error) {
	f, err := os.Open(sfvPath)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	entries, err := ParseSFV(f)
	if err != nil {
		return nil, err
	}
	dir := filepath.Dir(sfvPath)

	out := make([]Result, 0, len(entries))
	for _, e := range entries {
		r := Result{Name: e.Name, Want: e.CRC}
		got, err := CRC32File(filepath.Join(dir, e.Name))
		if err != nil {
			r.Missing = true
		} else {
			r.Got = got
			r.OK = got == e.CRC
		}
		out = append(out, r)
	}
	return out, nil
}

// MakeSFV 對一批檔案算出 CRC 並依檔名排序。
func MakeSFV(dir string, names []string) ([]SFVEntry, error) {
	out := make([]SFVEntry, 0, len(names))
	for _, n := range names {
		crc, err := CRC32File(filepath.Join(dir, n))
		if err != nil {
			return nil, err
		}
		out = append(out, SFVEntry{Name: n, CRC: crc})
	}
	sort.Slice(out, func(i, j int) bool {
		return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name)
	})
	return out, nil
}

// Summary 把結果數成「幾個對、幾個錯、幾個缺」。
func Summary(rs []Result) (ok, bad, missing int) {
	for _, r := range rs {
		switch {
		case r.Missing:
			missing++
		case r.OK:
			ok++
		default:
			bad++
		}
	}
	return
}
