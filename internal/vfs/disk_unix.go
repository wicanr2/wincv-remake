//go:build !windows

package vfs

import "syscall"

// DiskUsage 回傳 path 所在檔案系統的可用與總位元組。
//
// 用 statfs 而不是 statvfs:前者是系統呼叫,不需要 cgo。
// Bsize 在 Linux 是 int64、在 darwin 是 uint32,所以要先轉。
//
// 可用空間取 Bavail(非特權使用者真正拿得到的)而不是 Bfree
// (含保留給 root 的那一塊)—— 兩者在預設 ext4 上差 5%,
// 顯示 Bfree 會讓「剩餘空間」比實際可寫入的多。
func DiskUsage(path string) (free, total uint64, err error) {
	var st syscall.Statfs_t
	if err := syscall.Statfs(path, &st); err != nil {
		return 0, 0, err
	}
	bs := uint64(st.Bsize)
	return uint64(st.Bavail) * bs, uint64(st.Blocks) * bs, nil
}
