//go:build !windows

package vfs

// Drives 列出可以切過去的磁碟或掛載點。
func Drives() []Drive { return unixDrives() }
