// Package launch 開啟或執行檔案。
//
// 原版的 O(開啟)與 G(執行)在 Win32 下都走 ShellExecute。跨平台的
// 對應物三個系統各不相同,這裡把差異收在一處。
//
// 一律**不等它結束**:原版按下去之後馬上回到檔案列表,而不是卡住。
package launch

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
)

// Open 用系統預設的程式開啟一個檔案或目錄。
func Open(path string) error {
	var c *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		c = exec.Command("open", path)
	case "windows":
		c = exec.Command("cmd", "/c", "start", "", path)
	default:
		c = exec.Command("xdg-open", path)
	}
	return start(c, "")
}

// Run 在 dir 底下執行一行命令。
//
// 交給 shell 跑,因為使用者輸入的是一整行(可能帶參數、管線),
// 不是一個程式名加一串已經切好的參數。
func Run(dir, cmdline string) error {
	if strings.TrimSpace(cmdline) == "" {
		return fmt.Errorf("沒有命令")
	}
	var c *exec.Cmd
	if runtime.GOOS == "windows" {
		c = exec.Command("cmd", "/c", cmdline)
	} else {
		sh := os.Getenv("SHELL")
		if sh == "" {
			sh = "/bin/sh"
		}
		c = exec.Command(sh, "-c", cmdline)
	}
	return start(c, dir)
}

// Executable 判斷這個檔案能不能直接執行。
func Executable(path string) bool {
	fi, err := os.Stat(path)
	if err != nil || fi.IsDir() {
		return false
	}
	if runtime.GOOS == "windows" {
		l := strings.ToLower(path)
		for _, ext := range []string{".exe", ".com", ".bat", ".cmd"} {
			if strings.HasSuffix(l, ext) {
				return true
			}
		}
		return false
	}
	return fi.Mode().Perm()&0o111 != 0
}

func start(c *exec.Cmd, dir string) error {
	c.Dir = dir
	// 不接 Stdin/Stdout:這是 GUI 程式,沒有終端可接,而且接了
	// 之後子程序印東西會卡在管線上。
	if err := c.Start(); err != nil {
		return err
	}
	// 收掉子程序,不要留殭屍。
	go c.Wait()
	return nil
}
