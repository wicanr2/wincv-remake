package main

import (
	"github.com/wicanr2/wincv-remake/internal/i18n"
)

type startupMode uint8

const (
	startupDefault   startupMode = iota // 不帶旗標的檔案或目錄
	startupDirectory                    // 原版 /p
	startupEdit                         // 原版 /e
)

type startupRequest struct {
	target string
	mode   startupMode
}

// parseStartupArgs 解讀原版保留的啟動語法。一般 Go 旗標已經由 flag
// 套件先處理完；/p 與 /e 保持原版寫法，因為 Go 的 flag 不把 / 當旗標。
func parseStartupArgs(args []string) (startupRequest, error) {
	if len(args) == 0 {
		return startupRequest{}, nil
	}
	if args[0] == "/p" || args[0] == "/e" {
		if len(args) != 2 || args[1] == "" {
			return startupRequest{}, i18n.Errorf("%s 需要恰好一個路徑", args[0])
		}
		mode := startupDirectory
		if args[0] == "/e" {
			mode = startupEdit
		}
		return startupRequest{target: args[1], mode: mode}, nil
	}
	if len(args) != 1 || args[0] == "" {
		return startupRequest{}, i18n.Errorf("預期是一個啟動路徑，或 /p／/e 加一個路徑")
	}
	return startupRequest{target: args[0], mode: startupDefault}, nil
}
