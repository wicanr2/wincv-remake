package vfs

import "testing"

// 這條驗的是「有回答、而且答案自洽」,不驗絕對數字 ——
// 任何一台機器的剩餘空間都不是常數。
//
// 沒有這條的話,平台分歧最容易出的錯(Bsize 型別轉換、
// 欄位順序拿錯)會安靜地變成一個看起來合理的數字。
func TestDiskUsage(t *testing.T) {
	free, total, err := DiskUsage(t.TempDir())
	if err != nil {
		t.Fatalf("DiskUsage: %v", err)
	}
	if total == 0 {
		t.Fatal("總容量是 0")
	}
	if free > total {
		t.Errorf("可用 %d 比總量 %d 還大", free, total)
	}
	// 一個現代檔案系統不會小於 1 MB。抓的是「單位算錯」——
	// 少乘一次 block size 會得到一個小得離譜但非零的數字。
	if total < 1<<20 {
		t.Errorf("總容量 %d 太小,單位可能算錯", total)
	}
}

func TestDiskUsageMissingPath(t *testing.T) {
	if _, _, err := DiskUsage("/this/path/does/not/exist"); err == nil {
		t.Error("路徑不存在卻沒有回錯誤")
	}
}
