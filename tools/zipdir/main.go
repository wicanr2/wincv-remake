// zipdir 將一個目錄壓成 zip，保留 Unix 執行權限。
package main

import (
	"archive/zip"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

func main() {
	if len(os.Args) != 3 {
		fmt.Fprintln(os.Stderr, "用法: zipdir <輸出.zip> <目錄>")
		os.Exit(2)
	}
	dest, root := os.Args[1], filepath.Clean(os.Args[2])
	info, err := os.Stat(root)
	if err != nil || !info.IsDir() {
		fmt.Fprintf(os.Stderr, "不是目錄: %s\n", root)
		os.Exit(1)
	}
	out, err := os.Create(dest)
	if err != nil {
		panic(err)
	}
	zw := zip.NewWriter(out)
	base := filepath.Base(root)
	err = filepath.Walk(root, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		name := base
		if rel != "." {
			name += "/" + filepath.ToSlash(rel)
		}
		if info.IsDir() {
			name += "/"
		}
		h, err := zip.FileInfoHeader(info)
		if err != nil {
			return err
		}
		h.Name = strings.TrimPrefix(name, "./")
		h.Method = zip.Deflate
		h.SetMode(info.Mode())
		w, err := zw.CreateHeader(h)
		if err != nil || info.IsDir() {
			return err
		}
		in, err := os.Open(path)
		if err != nil {
			return err
		}
		_, copyErr := io.Copy(w, in)
		closeErr := in.Close()
		if copyErr != nil {
			return copyErr
		}
		return closeErr
	})
	if closeErr := zw.Close(); err == nil {
		err = closeErr
	}
	if closeErr := out.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		_ = os.Remove(dest)
		panic(err)
	}
}
