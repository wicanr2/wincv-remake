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

## 2026-11-09：處理 GitHub issue #7 到 #11（v.0.53.3-20261109）

- #7 記憶視窗大小：`Snapshot` 存欄數用的是只在縮圖模式才有值的 `thumbCols`，列數存的是內容區列數，改存 `screenCols` / `screenRows`。另外啟動時 `-rows` 會被扣掉選單列，與執行中改尺寸的 `applySize` 算法不一致，記住尺寸後每重開一次少一列；啟動時改成同一算法。實際視窗驗證：自訂 100×30 後連續重開兩次都是 800×496。
- #8 選單鍵盤操作：選單改成原版八類（`WINCV.IMG` 0x101fd0 起的 `&1檔案` 到 `&8說明/註冊/更新版本`），加 `Alt-1`～`Alt-8` 與單按 `Alt`（`keys.MenuKey`，由 `internal/ebikeys` 偵測按下到放開之間沒有別的鍵）。`Esc` 維持退一層（Win32 慣例），單按 `Alt` 一次收起所有層。原版下拉在 Wine/Xvfb 底下畫不出內容，所以分類名稱以 image 字串為據，1、2、4 類的項目沒有抽到。
- #9 應用程式圖示：重製版自繪像素圖示（`internal/appicon`），不用原版 `WinCV.ico`。執行時 `ebiten.SetWindowIcon`；Windows 執行檔由 `tools/win-rsrc.sh` 以 windres 產生 `.syso`，建完就刪；Android manifest 指向 `mipmap/ic_launcher`。macOS 的發行物是裸執行檔，沒有 `.app` 封包，所以沒有圖示。
- #10 字體大小選單卡住：選了一個沒改變任何狀態的項目（字級已到頂）時，`menuKey` 已收起下拉卻回 false，外殼不重繪，畫面停在不存在的下拉上。改成收起下拉一律回 true。實際視窗驗證：字級到頂後選「放大字體」，下拉收起。
- #11 CJK 字型：`cjk24` 加倚天 2000 的檔名（`STDFONT.24`、`STDFONT.24K` 等）當候選；找不到時訊息列出要找的檔名，不再顯示 `original/eten/` 這個相對路徑。`SPCFSUPP` 認兩種排法：3.53 的 365 格，與一碼位一格的 408 格（43 個洞必須全空才接受，否則只放棄這一區，其餘照常載入）。408 格排法依回報者截圖的錯誤訊息推得，沒有倚天 2000 的實檔可對照。
- 選單列功能鍵：使用者要求選單列右側的 F9／F1／F2 可以用滑鼠點。改成八類後 80 欄放不下原本的「F9 選單  F1 說明  F2 網路」，所以放不下時改畫「F9 F1 F2」；每一段都可點，等於按那顆鍵。
- 驗證環境：`wincv-build:1` 映像裡只有 `xvfb-run` 腳本沒有 `Xvfb` 本體，`xvfb-run` 會一直等。cmd 測試與實際視窗驗證改用主機的 `xvfb-run -a -s "-ac …"`，容器以 `DISPLAY` 與 `/tmp/.X11-unix` 連過去。`tools/remake-shot.sh` 也受影響，尚未修。
- 發布：來源提交 `3361667a43d78c385b9a2e89c3dce32da455571d` 與同名 tag 已推送；[v.0.53.3-20261109](https://github.com/wicanr2/wincv-remake/releases/tag/v.0.53.3-20261109) 於 2026-11-09 16:32（Asia/Taipei）公開。三個 ZIP、`MANIFEST.txt`、`SHA256SUMS-zip` 的 GitHub 大小與 SHA-256 均與本機一致；`tools/verify-dist.sh` 靜態驗收通過，Windows exe 有 `.rsrc`。
- 打包：`tools/package.sh` 以 `go run tools/zipdir` 壓 ZIP，主機沒有 go，`release.sh` 在主機跑到打包會失敗。建置在主機跑 `build-all.sh`，打包改在 `wincv-build:1` 容器內跑 `release.sh <版本> --no-build`（帶 `WINCV_RELEASE_COMMIT`，因為未追蹤的 `AGENTS.md` 讓工作區不算乾淨）。
- 固定入口：`tools/build-full.sh linux` 已原子替換 `dist-all/wincv-linux-amd64-full`（`~/.bashrc` 的 `wincv` alias 指向它），SHA-256 `6c0a9829b597eea610bf0397b8e371163581bb67d5546cf9cfb0287f203c2657`，與 `full-local/` 內相同。`BUILT-FROM-full` 帶 `-dirty`，來源是未追蹤的 `AGENTS.md`，產物內容與提交相同。
- 實際視窗驗收：公開版與完整版在主機 Xvfb 下 `-version` 都回報 `v.0.53.3-20261109`；選單列右側顯示「F9 F1 F2」，點 F1 開說明、點 F9 開選單，`Alt-7` 打開設定。Windows 與 macOS 尚未實機啟動，Android APK 本次未建。

## 2026-11-09：Big5 編碼器的 HKSCS 碼（v.0.53.4-20261109）

- 問題：倚天字形查表（`internal/eten` 的 `big5Bytes`）、編輯器存檔（`internal/editor`）、轉碼（`internal/convert`）都用 x/text 的 Big5 編碼器。它在 33 個字上給的是 HKSCS 的 FA–FE 碼，例如「包」給 FABD（標準 A55D）、「港」給 FDBB。倚天字庫只到 F9，這些字畫成缺字框；存檔與轉碼寫出的 FA–FE 碼，一般 Big5 程式也讀不懂。受影響的常用字有包、港、晴、煮、偽、婷、倩、卿、撐、瑜、渝、杞，以及「十」「卅」等。
- 發現方式：Rust 改寫的逐像素比對（`rust-rewrite` 分支的 `tools/rust-parity.sh`），`WinCV.txt` 與 `CLAUDE.md` 的「包」兩邊不同；Go 版畫缺字框，Rust 版（encoding_rs）畫對。
- 修法：`internal/textenc/big5.go` 新增 `Big5Code` 與 `EncodeBig5`，照 WHATWG Encoding Standard 的 Big5 編碼規則建表（A140–F9FE 由解碼方向反查，重複的字取第一個，U+2550、U+255E、U+2561、U+256A、U+5341、U+5345 取最後一個，不含 HKSCS 區）。三個地方都改用它。
- 驗證：`TestBig5CodeRoundTrip` 確認表裡每個碼都解回同一個字且不超出 F9；`TestHKSCSProneCharsHaveGlyphs` 以真實倚天字庫確認 14 個字都取得到字模；修正後 Go 版畫 `WinCV.txt` 與 Rust 版逐像素相同。
- 發布：來源提交 `825bae2ede22d83e2c36b6cb9e3d06f1bfe5a67e` 與同名 tag 已推送；[v.0.53.4-20261109](https://github.com/wicanr2/wincv-remake/releases/tag/v.0.53.4-20261109) 於 2026-11-09 18:42（Asia/Taipei）公開，三個 ZIP、`MANIFEST.txt`、`SHA256SUMS-zip` 的 GitHub 大小與 SHA-256 均與本機一致；`tools/verify-dist.sh` 靜態驗收通過。
- 固定入口：`dist-all/wincv-linux-amd64-full` 已換成這一版（SHA-256 `cbb19cdd56347a95ad4054be5fb974a9289ce5ea4b412434117e0b7e196eea08`），公開版與完整版 `-version` 都回報 `v.0.53.4-20261109`，完整版在主機 Xvfb 下開 `WinCV.txt` 畫面正常。
