# 工作紀錄

## 2026-10-28：選單滑鼠反白與 v.0.53.2-20261028

- 目標：發布已確認的下拉選單滑鼠反白功能，替換 `wincv` alias 使用的本機完整版。
- 修改：桌面滑鼠位置改變時，將下拉內的列傳給 `MenuDropHover`；共用點擊與移入的命中判斷，略過標題、分隔線與捲動提示。滑鼠停止時，鍵盤可繼續移動選取。
- 版號：正式建置以 `-X github.com/wicanr2/wincv-remake/internal/app.Version=v.0.53.2-20261028` 注入；命令列 `-version` 與「關於」顯示相同版號。
- 工具鏈：從 `Dockerfile.build` 重建 `wincv-build:1`，固定 Go 1.22.12，補齊 Python 與 xdotool；從 `tools/docker/osxcross.Dockerfile` 重建 `wincv-osxcross-go:1`。原有同名 image 已不存在。全部建置與驗證在 Docker 內執行。
- 打包修正：`build-full.sh` 將執行檔建到版本目錄，成功後原子替換固定 Linux 入口；正式建置腳本傳遞相同版號。公開版不帶 `fonts` tag。
- 自動驗證：`xvfb-run -a go test ./... -count=1` 通過；[tools/menu-hover-smoke.py](tools/menu-hover-smoke.py) 在公開 Linux 版與本機完整版的正式視窗中，確認反白、鍵盤接續與點擊均通過。macOS universal 包含 arm64 與 x86_64，arm64 有 `LC_CODE_SIGNATURE`，兩側只依賴系統動態庫。Windows 與 macOS 尚未實機啟動。
- 驗證腳本修正：xdotool 預設短按未穩定跨過更新幀，造成鍵盤驗證失敗；改以 80 ms 按下／放開事件後，同一容器與同一命令重跑通過。這是驗證輸入時序問題。
- 發行輸出：`dist-all/v.0.53.2-20261028/patch/`；本機完整版在 `full-local/`；啟動證據在 `smoke/`。執行 `tools/release.sh <版本> --no-build` 與 `tools/package.sh public` 重建公開 ZIP，manifest 記錄來源提交與 SHA-256。
- 發布：來源提交 `18eabd7a5cd23d98cde05aa3f8c994151fd7510f` 與同名 tag 已推送；[v.0.53.2-20261028](https://github.com/wicanr2/wincv-remake/releases/tag/v.0.53.2-20261028) 已於 2026-10-28 17:41（Asia/Taipei）公開。三個 ZIP、`MANIFEST.txt`、`SHA256SUMS-zip` 的 GitHub 大小與 SHA-256 均與本機一致。
- 固定入口：已原子替換 `dist-all/wincv-linux-amd64-full`，`-version` 回報 `v.0.53.2-20261028`，`BUILT-FROM-full` 指向上述來源提交。執行檔 SHA-256 為 `f9e3b423e3e04e0a7f52be86d0835ce0f07548f52b4f64269cd05e52a967ef02`，與版本目錄內的完整版相同；原有 alias 無需修改。
- 證據：`dist-all/v.0.53.2-20261028/SHA256SUMS.json` 記錄所有正式包與本機完整版的大小、雜湊及權利分類；`smoke/` 保存全套測試 log、公開版與完整版的視窗截圖、GUI 結果、macOS 靜態檢查與建置資訊。
- 清理：本次容器均已移除，暫存內嵌字型已清除；完整工作樹檢查未見 root-owned 檔案或誤建的 `.md` 目錄，交付物 UID/GID 為 `1000:1000`。保留目前可重現工具鏈；依專案規則未執行 image prune 或 rmi。使用者提供的未追蹤 `AGENTS.md` 維持原狀。
