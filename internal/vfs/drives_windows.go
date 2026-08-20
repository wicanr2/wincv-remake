package vfs

import (
	"syscall"
	"unsafe"
)

var (
	procGetLogicalDrives = kernel32.NewProc("GetLogicalDrives")
	procGetVolumeInfo    = kernel32.NewProc("GetVolumeInformationW")
	procGetDriveType     = kernel32.NewProc("GetDriveTypeW")
)

// Windows 的 GetDriveType 回傳值。
const (
	driveRemovable = 2
	driveRemote    = 4
	driveCDROM     = 5
)

// Drives 列出現有的磁碟機代號。
//
// 用 GetLogicalDrives 的位元遮罩,不是逐一 Stat A:\ 到 Z:\ ——
// 後者在沒有磁片的軟碟機上會跳出「請插入磁片」的系統對話框。
func Drives() []Drive {
	mask, _, _ := procGetLogicalDrives.Call()
	var out []Drive
	for i := 0; i < 26; i++ {
		if mask&(1<<uint(i)) == 0 {
			continue
		}
		root := string(rune('A'+i)) + `:\`
		p, err := syscall.UTF16PtrFromString(root)
		if err != nil {
			continue
		}
		label := string(rune('A'+i)) + ":"
		var name [261]uint16
		r, _, _ := procGetVolumeInfo.Call(
			uintptr(unsafe.Pointer(p)),
			uintptr(unsafe.Pointer(&name[0])), uintptr(len(name)),
			0, 0, 0, 0, 0)
		if r != 0 {
			if v := syscall.UTF16ToString(name[:]); v != "" {
				label += " " + v
			}
		}
		t, _, _ := procGetDriveType.Call(uintptr(unsafe.Pointer(p)))
		out = append(out, Drive{
			Label:  label,
			Path:   root,
			Volume: t == driveRemovable || t == driveCDROM || t == driveRemote,
		})
	}
	return out
}
