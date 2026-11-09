# go-session.json

Go 版 `internal/session` 以 `SaveTo` 實際寫出的檔案(Go 1.22.12,2026-11-09)。
`same_format_as_go` 測試確認 Rust 版讀得回來,而且寫出來逐字相同:使用者從 Go 版
換成 Rust 版時,上次的位置不會遺失。
