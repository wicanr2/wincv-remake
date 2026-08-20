package vfs

import (
	"syscall"
	"unsafe"
)

var (
	kernel32            = syscall.NewLazyDLL("kernel32.dll")
	procGetDiskFreeSpce = kernel32.NewProc("GetDiskFreeSpaceExW")
)

// DiskUsage 回傳 path 所在磁碟的可用與總位元組。
//
// 走 GetDiskFreeSpaceExW 而不是 GetDiskFreeSpace:後者的欄位是 32 位元,
// 超過 2 TB 的磁碟會回錯的值。
//
// 第一個輸出參數是「呼叫者的配額內可用」,第三個才是磁碟的實際剩餘;
// 有磁碟配額時兩者不同,顯示給使用者看的是前者。
func DiskUsage(path string) (free, total uint64, err error) {
	p, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return 0, 0, err
	}
	var avail, totalBytes, totalFree uint64
	r, _, e := procGetDiskFreeSpce.Call(
		uintptr(unsafe.Pointer(p)),
		uintptr(unsafe.Pointer(&avail)),
		uintptr(unsafe.Pointer(&totalBytes)),
		uintptr(unsafe.Pointer(&totalFree)),
	)
	if r == 0 {
		return 0, 0, e
	}
	return avail, totalBytes, nil
}
