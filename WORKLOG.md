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

## 2026-11-09：Big5 對照表補回 HKSCS 區（v.0.53.5-20261109）

- v.0.53.4 的 `textenc.Big5Code` 只收 A1–F9 的碼，只在 HKSCS 區（FA–FE）的 764 個字（香港用字）因此編不出來：轉碼成 Big5、以 Big5 存檔時，v.0.53.3 會寫出 FA–FE 碼，v.0.53.4 改成失敗或換成替代字。倚天字形不受影響（字庫本來就沒有那一區）。
- 發現方式：把 Go 的對照表與 Rust 改寫用的 encoding_rs 在全部 14,653 個字上逐一比對，前 13,889 個相同，差在這 764 個。
- 修法：建表範圍擴到 A1–FE，照 WHATWG 依碼的順序「取第一個」：同時在標準區與 HKSCS 區的字（包、港…）拿到標準碼，只在 HKSCS 區的字編成 FA–FE 碼。修正後與 encoding_rs 在全部字上相同；`TestBig5CodeRoundTrip` 改成檢查「標準區有的字不可拿到 HKSCS 碼」與「HKSCS 字編得出來」。
- 發布：來源提交 `9ee8758ba5a0c584f1d4c93461b5c776d913f2c7` 與同名 tag 已推送；[v.0.53.5-20261109](https://github.com/wicanr2/wincv-remake/releases/tag/v.0.53.5-20261109) 於 2026-11-09 19:13（Asia/Taipei）公開，三個 ZIP、`MANIFEST.txt`、`SHA256SUMS-zip` 的 GitHub SHA-256 與本機一致；`tools/verify-dist.sh` 靜態驗收通過。
- 固定入口：`dist-all/wincv-linux-amd64-full` 已換成這一版（SHA-256 `701d820e94fba865b3a284b80877117eaf1c42458fba4042f455c43af20cd3f3`），與 `full-local/` 內相同；公開版與完整版 `-version` 都回報 `v.0.53.5-20261109`。`BUILT-FROM-full` 的 `-dirty` 來自未追蹤的 `AGENTS.md`。

## 2026-11-09：壓縮檔成員路徑正規化、解碼器 panic 不再讓程式結束（v.0.53.6-20261109）

- 由 Rust 改寫的壓縮檔比對（`rust-rewrite` 分支，`tools/archive-parity.sh` 與 `tools/rust-parity.sh` 的壓縮檔場景）發現兩個 Go 版問題。
- `./` 開頭的成員路徑（`tar czf x.tgz .` 打包出來就是這樣）：最上層多一個「.」目錄，檔案藏在底下；`buildDirs` 用 `path.Dir`（會 Clean）補出來的中間層目錄與成員路徑對不上，進去是空的。絕對路徑的成員整個看不到。`archive.Open` 讀完後把成員路徑換成 `path.Clean("/"+p)` 去掉開頭的 `/`，空字串的（壓縮檔根目錄自己）不列。`TestMemberPathsNormalized` 盯著。
- `bodgit/sevenzip` v1.5.2 在部分 7z 的標頭上 panic（`types.go:846` index out of range；實例是 sevenz-rust2 0.24.1 的 `examples/data/sample.7z`），使用者在這種檔上按 Enter 整個程式結束。`archive.Open` 與 `FS.Open` 把解碼器 panic 轉成錯誤「讀取 7-Zip 時發生內部錯誤」。實測該檔改為回錯誤、程式繼續。
- PAX 全域標頭：`git archive` 產生的 tar 開頭有一筆 typeflag `g` 的中繼資料，`archive/tar` 會把它當成一個成員交出來，畫面上多一個叫 `pax_global_header` 的空檔。改成略過，與 GNU tar 解壓的結果一致；`TestPaxGlobalHeaderIsNotAFile` 盯著。
- 發布：來源提交 `e8ce4c21b8b69b6ebccb4d388a443b6e467f0a32` 與同名 tag 已推送；[v.0.53.6-20261109](https://github.com/wicanr2/wincv-remake/releases/tag/v.0.53.6-20261109) 於 2026-11-09 20:21（Asia/Taipei）公開，三個 ZIP、`MANIFEST.txt`、`SHA256SUMS-zip` 的 GitHub SHA-256 與本機一致；`tools/verify-dist.sh` 靜態驗收通過。
- 固定入口：`dist-all/wincv-linux-amd64-full` 已換成這一版（SHA-256 `a75d0bcd0223334f703e46c6575cf55fec89df4ddcf080ae60ae66362e9b6715`），與 `full-local/` 內相同；公開版與完整版 `-version` 都回報 `v.0.53.6-20261109`。

## 2026-11-09：狀態列靠右對齊改用顯示寬度

- 編輯器、文字檢視（有搜尋結果時）、Markdown 檢視的狀態列右側文字含中文，位置用 `len`（位元組數）算，一個中文字 3 個位元組只佔 2 格，所以離右緣空出幾格。例：編輯器的「1 行 1 欄 / 10」是 16 個位元組、14 格，右邊空 2 格。
- 新增 `cell.Width`，六處靠右對齊（editor、viewer、app/mdview、app/actions、hexview、thumbs）都改用它；後三處目前只有 ASCII，一起改以免日後加中文再出錯。`TestWidth` 盯著。
- 由 Rust 改寫的編輯器逐像素比對發現（Rust 版一開始就用顯示寬度）。

## 2026-11-09：`convert.RenameCase` 首字母大寫以字元為單位

- `CaseTitle` 以前用 `low[:1]` 取第一個位元組轉大寫。首字是中文或 é 時只拿到半個 UTF-8 字，`strings.ToUpper` 把它換成 U+FFFD，檔名被弄壞（實測 `中文.TXT` → `"�\xb8\xad文.txt"`）。改成取第一個字元。
- 目前 app 沒有用到 `RenameCase`（批次改名的大小寫還沒接上介面），使用者碰不到；先修好，免得接上時寫壞檔名。由 Rust 改寫 `convert` 時發現。

## 2026-11-09：ICO 與 TGA 的半透明像素改用非預乘

- `decodeICOBMP`（32bpp）與 `DecodeTGA`（32 位元）把檔案裡非預乘的 BGRA 存進 `image.RGBA`。Go 把 `image.RGBA` 當成預乘資料，所以半透明像素（圖示的反鋸齒邊緣）的顏色算錯：畫到畫面上偏亮，轉成 NRGBA 時則被放大。例：dosbox 的 `cfggui.ico` 一個 alpha 18 的像素，檔案裡是 (195,199,196)，以前解成 (213,14,227)。
- 改成 `image.NRGBA` 與 `color.NRGBA`。不透明與全透明的像素不受影響。
- 由 Rust 改寫的圖檔比對（`tools/img-parity.sh`）發現：ICO 有 13 張只差在半透明像素，alpha 相同、顏色不同。

## 2026-11-09：v.0.53.7-20261109

- 收錄上面三條修正：狀態列靠右對齊改用顯示寬度、`convert.RenameCase` 首字母大寫以字元為單位、ICO 與 TGA 的半透明像素改用非預乘。
- 發布：來源提交 `3d1394a1704be69fdeb3e9b29718ae40245c9a39` 與同名 tag 已推送；[v.0.53.7-20261109](https://github.com/wicanr2/wincv-remake/releases/tag/v.0.53.7-20261109) 於 2026-11-09 21:43（Asia/Taipei）公開，三個 ZIP、`MANIFEST.txt`、`SHA256SUMS-zip` 的 GitHub SHA-256 與本機一致；`tools/verify-dist.sh` 靜態驗收通過。
- 固定入口：`dist-all/wincv-linux-amd64-full` 已換成這一版（SHA-256 `dab3b31de88e86d1312f4bf41378b2a62651bde46bf30336fd83f6ab931de216`），與 `full-local/` 內相同；公開版與完整版 `-version` 都回報 `v.0.53.7-20261109`。

## 2026-11-10：v.0.53.8-preview-20261110（Rust 版預覽）

- 內容：Rust 改寫的 app 層搬完（瀏覽、gopher/http、EPUB、Office、PDF），`rust-rewrite` 快轉合併進 master。畫面 82 個場景與 Go 逐像素相同（「關於」畫面的實作語言除外）；各層對照數字見 `docs/plan/rust-rewrite.md` 與 README「Rust 改寫」一節。
- 一起修的 Go 版問題：pptx 換行後內容遺失、PDF 具名目的地書籤 panic、doc97 的 CLX 與樣式表越界、rtf `\bin` 溢位、gopher/網頁/EPUB 的編碼判讀、Office 狀態列多出「— doc:」、子選單項目畫出 U+0000。清單與狀態在 `docs/plan/go-suspects.md`。Go 的修正已在 master，尚未發 Go 版。
- 建置：`tools/rust-release.sh v.0.53.8-preview-20261110`，來源提交 `c07f5b560976d273b0e4faf8dfc770a016b92a47`。三個執行檔都含版本字串；拿原版與倚天字型檔的位元組片段比對，三個執行檔都沒有內嵌字型資料；Linux 版從 zip 解出後在容器 Xvfb 不帶任何字型啟動，改用系統字型，「關於」畫面顯示版本號。
- 發布：[v.0.53.8-preview-20261110](https://github.com/wicanr2/wincv-remake/releases/tag/v.0.53.8-preview-20261110) 標成 prerelease，Go 的 v.0.53.7 仍是 Latest。三個 ZIP、`MANIFEST.txt`、`SHA256SUMS-zip`；從 GitHub 下載回來的 ZIP 校驗與本機一致。
- 未驗證：Windows 與 macOS 版沒有實機啟動；macOS 版的 ad-hoc 簽章在 `strip` 之後是否仍有效要在 Mac 上用 `codesign -v` 確認。
