# WinCV Remake(Rust)

Go 版的 Rust 改寫,進行中。計畫與評估見 [`docs/plan/rust-rewrite.md`](../docs/plan/rust-rewrite.md)。

每一個 crate 對應 Go 版的一個 `internal/` 套件,搬完的標準是與 Go 版的輸出逐位元組、逐像素相同。

```bash
WINCV_NET=1 tools/cargo.sh fetch     # 第一次:抓相依套件
tools/cargo.sh test                  # 單元測試
tools/rust-parity.sh                 # 與 Go 版的 celldump 逐像素比對
```

| crate | 對應 Go 套件 |
|---|---|
| `wincv-cell` | `internal/cell` |
| `wincv-fnt` | `internal/fnt` |
| `wincv-eten` | `internal/eten` |
| `wincv-render` | `internal/render` |
| `wincv-celldump` | `cmd/celldump`(目前只有示範畫面) |
