# go-sort.txt

Go 版 `internal/vfs.Sort` 對 45 個項目(名字只差大小寫、混合目錄、大小與時間有同值)
在六種排序設定下的實際輸出,Go 1.22.12 產生,2026-11-09。`gosort_matches_go` 測試
用同樣的資料在 Rust 端排序,逐列比對。

資料的產生方式與當時的 Go 小程式相同(見測試裡的 `data()`):第 i 項的名字取自
`readme README Readme.txt readme.TXT a.c A.C b.h B.H zz ZZ` 循環,i ≥ 10 時加上 i/10,
i 是 11 的倍數時換成第 (i/11) 個名字的大寫;大小 (i*7)%5,i 是 9 的倍數時是目錄,
修改時間 Unix 1000+(i*13)%4 秒。每列是「排序鍵 遞減 名字,...」,排序鍵 0–3 依序是
名字、副檔名、大小、時間。
