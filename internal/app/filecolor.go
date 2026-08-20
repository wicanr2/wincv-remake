package app

import (
	"path"
	"strings"

	"github.com/wicanr2/wincv-remake/internal/archive"
	"github.com/wicanr2/wincv-remake/internal/browser"
	"github.com/wicanr2/wincv-remake/internal/cell"
	"github.com/wicanr2/wincv-remake/internal/imgfmt"
)

// execExt 是原版畫成亮紅色的副檔名(可執行檔)。
var execExt = map[string]bool{".exe": true, ".com": true}

// batchExt 是原版畫成亮洋紅的副檔名(批次檔)。
var batchExt = map[string]bool{".bat": true, ".cmd": true}

// fileColor 依副檔名決定一列的顏色。
//
// 顏色是從原版量出來的(docs/ui/oracle-ext.png 與 oracle-ext2.png,
// 一個副檔名放一個檔再逐列取樣):
//
//	目錄      dir-green   #14BE00
//	exe com   ltred       #FF0000
//	bat cmd   ltmagenta   #FF00FF
//	壓縮檔    dir-cyan    #1EBEBE   實測 7z ace arj gz lzh rar zip
//	圖檔      dir-ltgreen #00F000   實測 bmp gif ico jpg png
//	其他      ltgray      #C0C0C0   實測 asm c cfg cpp dll doc h htm inf lnk sys
//
// 壓縮檔與圖檔這兩類,實測的副檔名不是全部 —— 這裡直接沿用
// archive.IsArchive 與 imgfmt.IsImage 的判斷,把 cab / tar / tiff 這些
// 沒實測到的也歸進去。那是推廣,不是量到的;真要逐一確認就照
// docs/ui/main-screen.md 的作法再跑一次。
func fileColor(e browser.Entry) cell.Color {
	if e.IsDir {
		return cell.DirGreen
	}
	ext := strings.ToLower(path.Ext(e.Name))
	switch {
	case execExt[ext]:
		return cell.LtRed
	case batchExt[ext]:
		return cell.LtMagenta
	case archive.IsArchive(e.Name):
		return cell.DirCyan
	case imgfmt.IsImage(e.Name):
		return cell.DirLtGreen
	}
	return cell.LtGray
}
