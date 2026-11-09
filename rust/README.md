# WinCV Remake(Rust)

Go 版的 Rust 改寫,進行中。計畫與評估見 [`docs/plan/rust-rewrite.md`](../docs/plan/rust-rewrite.md)。

每一個 crate 對應 Go 版的一個 `internal/` 套件,搬完的標準是與 Go 版的輸出逐位元組、逐像素相同。

```bash
WINCV_NET=1 tools/cargo.sh fetch     # 第一次:抓相依套件
tools/cargo.sh test                  # 單元測試
tools/rust-parity.sh                 # 與 Go 版的 celldump 逐像素比對(示範畫面與多個檔案列表)
tools/archive-parity.sh <語料目錄> <壓縮檔>...   # 與 Go 版逐檔比 sha256 並計時
tools/textenc-parity.sh              # 編碼判讀與解碼與 Go 版對照
tools/pdf-parity.sh                  # PDF 渲染:LibreOffice、Go 版、hayro 三方對照
```

| crate | 對應 Go 套件 |
|---|---|
| `wincv-cell` | `internal/cell` |
| `wincv-fnt` | `internal/fnt` |
| `wincv-eten` | `internal/eten` |
| `wincv-render` | `internal/render` |
| `wincv-celldump` | `cmd/celldump`(目前有示範畫面與 `-dir` 檔案列表) |
| `wincv` | `cmd/wincv`(目前是 winit + softbuffer 的視窗試作) |
| `wincv-lzh` | `internal/archive/lzh` |
| `wincv-i18n` | `internal/i18n`(語系檔直接讀 Go 版的 `locales/*.json`) |
| `wincv-keys` | `internal/keys` |
| `wincv-textenc` | `internal/textenc` |
| `wincv-vfs` | `internal/vfs`(含照搬的 Go 穩定排序) |
| `wincv-browser` | `internal/browser` |
| `wincv-oracle` | 與 Go 版對照的工具(`archsum`,對應 `tools/archsum`) |
