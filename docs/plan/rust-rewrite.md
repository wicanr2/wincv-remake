# 用 Rust 重寫的評估

日期：2026-11-09。評估對象是 v.0.53.3-20261109（提交 `3361667`）的 Go 版。

評估標準取自使用者列出的目標：接近 C 的執行效率、較小的產物、正確性與安全、技術學習與展示、可獨立編譯的跨平台支援，以及能在網頁上執行。

斷言分兩級。「已驗證」附重跑方式或來源；「待驗」表示目前只有文件或推論，要在第一階段量測。

## 結論

Rust 重寫可行，現有程式的分層也適合逐塊搬移。建議分階段進行：先做一個有量測出口的試作階段，用數字確認 Rust 在產物大小、wasm 大小與解碼效能上的實際差距，並確認 RAR、PDF 兩個高風險相依套件在本專案語料上的正確率，再決定是否全面重寫。

全面重寫期間，Go 版維持發行，並當作差異比對的 oracle：同一份輸入，Rust 版的畫面格點與解出的位元組必須和 Go 版相同。

有兩點會影響期待：

- 網頁版不一定要換語言。現有 Go 程式補上一個檔案就能編成 WebAssembly（見「現況基準」）。Rust 在這一項的優勢是 wasm 檔案大小，而這個差距還沒有量過。
- 記憶體安全方面，Go 已經是記憶體安全的語言。過去實際踩過的解碼器錯誤（LHA 的 `nc` 少算一格、CAB 的 MSZIP 跨區塊字典）都是邏輯錯誤，換成 Rust 也不會被編譯器擋下。Rust 的增益在型別系統：沒有 nil、`match` 必須窮舉、資料競爭在編譯期就被擋下。

## 現況基準

| 項目 | 數值 | 證據 |
|---|---|---|
| Go 原始碼 | 約 39,200 行（不含測試） | 已驗證：`find . -name '*.go'` 扣掉 `*_test.go`，排除 `.cache/` 與 `original/` |
| 測試 | 88 個測試檔、542 個 `func Test` | 已驗證：同上，`grep -c "func Test"` |
| 公開版執行檔 | Linux 17.9 MB、Windows 18.3 MB、macOS universal 35.0 MB | 已驗證：`dist-all/v.0.53.3-20261109/patch/` |
| 本機完整版（Linux） | 44.5 MB，其中內嵌字型 26 MB | 已驗證：`tools/build-full.sh linux` 的輸出 |
| 啟動到視窗出現 | 約 0.70 秒，含 `docker run` 本身的開銷 | 已驗證：主機 Xvfb、容器 `--cpus 2`，公開版與完整版各跑 3 次，差距在 0.01 秒內 |
| 記憶體 | 公開版約 57 MB，完整版約 125 MB | 已驗證：同上，視窗出現 4 秒後 `docker stats` |
| WebAssembly | 可編譯：31.6 MB，gzip 後 7.2 MB | 已驗證：`GOOS=js GOARCH=wasm go build`。`internal/vfs/disk_unix.go` 用了 `syscall.Statfs`，以 `-overlay` 換成回傳錯誤的替身後通過。尚未在瀏覽器實際執行 |

各模組的原始碼行數（不含測試），列出前幾大：

| 模組 | 行數 | 內容 |
|---|---|---|
| `internal/pdf` | 6,783 | 自寫的內容資料流解譯、字型編碼、頁面渲染 |
| `internal/app` | 6,496 | 模式切換、按鍵分派、選單、輸入列 |
| `internal/archive` | 3,505 | 含自寫的 ACE 1,007、LZH 723、ARC 408、ARJ 276、CAB 196、.Z 162 |
| `internal/imgfmt` | 1,604 | 圖檔解碼 |
| `internal/doc97`、`docx`、`rtf`、`pptx`、`xlsx`、`ooxml`、`cfb` | 合計約 5,300 | Office 文件 |

大部分程式是自己寫的邏輯，第三方相依套件集中在視窗、RAR、7z、PDF 物件層、SVG 與字型外框。所以重寫的主要工作量是逐行搬移自寫邏輯，相依套件的替換只占一小部分，但風險集中在那一小部分。

## 逐項評估

| 目標 | Go 現況 | Rust 預期 | 證據等級 |
|---|---|---|---|
| 執行效率 | 介面沒有量到瓶頸。解碼器與 PDF 渲染有 GC 與邊界檢查的成本 | 沒有 GC；熱迴圈可以去掉邊界檢查。效益集中在解碼與 PDF，介面互動感受不到差別 | 待驗：要用同一批壓縮檔與 PDF 量解碼時間 |
| 產物大小 | 公開版約 18 MB | 開 LTO、`strip`、`panic = "abort"` 通常可以縮小，但各來源的縮小幅度差很多 | 待驗：試作階段直接量。完整版的 26 MB 字型不受語言影響 |
| 記憶體 | 57 MB／125 MB | 沒有 GC 的預留空間，預期較少 | 待驗 |
| 正確性與安全 | 記憶體安全；nil 與未窮舉的 `switch` 只在執行期出錯 | 型別系統擋下 nil 與漏掉的分支；陣列越界仍是執行期 panic | 推論：語言特性，不需實驗 |
| 學習與展示 | 不適用 | 涵蓋解碼器、點陣渲染、跨平台 GUI、wasm，題材完整 | 不適用 |
| 獨立編譯、跨平台 | 建置腳本以 `CGO_ENABLED=1` 建桌面版，Windows 用 mingw、macOS 用 osxcross（`tools/build-all.sh`） | winit 與 softbuffer 是 Rust crate，Linux 與 Windows 預期不必用 C 編譯器；macOS 仍要 Apple SDK 與 osxcross 連結 | 待驗：試作階段在現有 Docker 映像實際交叉編譯 |
| 網頁 | 已能編成 wasm（31.6 MB） | wasm 預期小很多 | 待驗：Rust 版 wasm 大小要實測 |

### 網頁版的限制與語言無關

不管用哪個語言，瀏覽器都有同樣的限制，要另外設計：

- 沒有本機檔案系統。要透過使用者選取的檔案、拖放或 File System Access API，`vfs` 要多一個實作。
- 不能開 TCP 連線。Gopher 要經過 WebSocket 代理；HTTP 受 CORS 限制。
- 不能執行外部程式。`launch`（用系統預設程式開啟）在網頁版不提供。
- 字型授權。倚天、國喬與原版 `.FON` 不能放進公開網站（見 `NOTICE`），網頁版只能讓使用者自己提供，或改用可散布的字型。

## 相依套件對照

| Go 端 | 用途 | Rust 候選 | 風險 | 證據 |
|---|---|---|---|---|
| Ebiten | 視窗、輸入、貼圖、Android、wasm | winit + softbuffer，配合自己的 CPU 光柵器（`internal/render` 本來就不依賴 Ebiten） | 中：輸入法、按鍵重複、Android 生命週期要自己處理 | softbuffer 官方範例涵蓋 Android 與 wasm；Android 端 android-activity 建議用 cargo-ndk 搭 Gradle，cargo-apk 已標示棄用 |
| `archive/zip`、`compress/*`、`archive/tar` | ZIP、GZ、TAR、BZ2 | zip、flate2、tar、bzip2 | 低 | bzip2 0.6 起預設用純 Rust 的 libbz2-rs-sys。zip 的 deflate 是否純 Rust：待驗 |
| `nwaples/rardecode` | RAR | rars（Apache-2.0） | 低：第一階段已對 28 個真實 RAR 驗過 | rar-stream 不在 crates.io 上（2026-11-09 查索引與 API 都是 404），只剩 rars。結果見「第一階段 3 的量測」 |
| `bodgit/sevenzip` | 7z | sevenz-rust2 | 中 | 純 Rust；wasm 要用 `default_wasm` 功能組；是已停止維護的 sevenz-rust 的分支 |
| `pdfcpu` + `rasterx` + `x/image/font/sfnt` | PDF 物件層、光柵、字形外框 | hayro（純 Rust 的 PDF 光柵器）、hayro-syntax | 高 | hayro 的 README 自述仍是實驗性、尚未完整；回歸測試約 1,000 份 PDF；`forbid(unsafe_code)` |
| `x/text` 編碼 | Big5、GBK、Shift_JIS、EUC-KR | encoding_rs（WHATWG 編碼標準，Firefox 使用） | 中：與原版隨附對照表的差異要用 golden data 驗 | 已查證涵蓋這四種編碼 |
| `x/image` 圖檔 | BMP、GIF、TIFF 等 | image crate，加上自寫的冷門格式 | 中 | image 0.25.5 起暫停 PCX；TIFF 解碼不支援傳真壓縮 |
| `oksvg` | SVG | resvg | 待驗 | 本次未查證 |
| `x/net/html` | 網頁、EPUB | html5ever 或同類解析器 | 待驗 | 本次未查證 |
| 自寫：LZH、ARJ、CAB、ARC、.Z、ACE、倚天、FNT、CFB、doc97、RTF、OOXML | | 手動搬移 | 中：搬移時會出現「自洽但錯」 | Go 版就是 oracle |

## 成本與風險

- 工作量：約 39,200 行邏輯與 542 個測試要搬移。測試的輸入與期望值大多可以直接沿用，測試程式本身要重寫。
- 「自洽但錯」的解碼器：ACE 的 Huffman 表依賴一個特定的不穩定 quicksort（CLAUDE.md §4.3），換成 Rust 的 `sort_unstable` 會解出垃圾而不報錯。這類地方要逐行照抄，並用完整語料比對。
- 像素對齊：介面要與原版逐像素對齊。Go 版的 `cmd/celldump` 可以輸出同一畫面的 PNG，Rust 版做同樣的工具，兩邊逐像素比對，差異必須是零。
- 雙線維護：重寫期間仍會收到 issue，修正要在 Go 版發行，同時記進 Rust 版的待辦。
- 介面文字：四種語系的翻譯檔以繁中原文當 key，可以直接沿用。`i18nscan` 那一套檢查要在 Rust 端重做。

## 建議的進行方式

### 第一階段：試作與量測

目的是用數字回答上面標成「待驗」的項目，再決定是否繼續。範圍：

1. 搬移 `cell`、`render`、`fnt`、`eten`，用 Rust 畫出 `celldump` 的示範畫面，與 Go 版的 PNG 逐像素比對。
2. 用 winit + softbuffer 把同一畫面開成視窗，在 Linux、Windows、macOS、wasm、Android 各建一次。記錄執行檔大小、wasm 大小（原始與 gzip）、啟動時間、記憶體，量法與本文件的「現況基準」相同。
3. RAR：rar-stream 與 rars 分別解本專案的 RAR 語料，與 Go 版（rardecode）的輸出比 sha256。
4. PDF：hayro 渲染 `internal/pdf` 的測試檔，用 `tools/inkdiff` 與 Go 版比較。
5. 選一個自寫解碼器（建議 LZH，有 675 個成員的對照語料）搬到 Rust，量解碼時間。

出口條件：五項都有數字。產物大小、wasm 大小或解碼效能至少一項有實質改善，而且 RAR、PDF 各有一個候選在語料上全部通過（或有明確的替代方案），才進入第二階段。

### 第二階段起：依功能逐塊搬移

依相依順序，每一塊搬完都要通過與 Go 版的差異比對才算完成：

1. 核心：`cell`、`render`、字型、`keys`、`textenc`、`i18n`。
2. 檔案瀏覽與文字檢視（對應 M1、M2）：`vfs`、`browser`、`viewer`、`session`。
3. 壓縮檔（M3）：`archive.Formats` 是程式碼裡的資料表，搬成 Rust 的資料表，逐列與 Go 版比對支援狀態。
4. 編輯器、HEX、看圖、縮圖、字典、校驗碼（M4）。
5. Office、PDF、EPUB、網頁、Gopher。
6. 打包：桌面三平台、Android、wasm。

`cell`、`vfs`、`officedoc` 三個深模組（CLAUDE.md §4.2）在 Rust 端寫成 trait，對外介面維持一樣窄。

Go 版在 Rust 版達到功能對等之前持續發行。對等的判準是 `archive.Formats`、`imgfmt.Formats` 這類功能表，加上 `docs/ui/keymap.md` 的按鍵表逐項通過。

## 發布方式

使用者決定（2026-11-09）：第一個 Rust 版的 GitHub Release 標成 preview，因為整個程式是重寫的。
發布時勾選 prerelease，版號加上 `-preview`。`tools/release.sh` 與 `tools/package.sh`
目前的版號檢查只接受 `v.主.次.修-YYYYMMDD`，發布前要一起放寬。

## 進度

| 項目 | 狀態 | 證據 |
|---|---|---|
| 第一階段 1：`cell`、`fnt`、`eten`、`render` 與示範畫面 | 完成，與 Go 版逐像素相同（640×400，256,000 個像素） | `tools/rust-parity.sh` |
| 第一階段 2：視窗與五個平台的建置量測 | Linux、Windows、wasm、macOS 完成（見下）；Android 未做；wasm 尚未在瀏覽器執行 | `rust/crates/wincv` |
| 第一階段 3：RAR 候選 crate 對語料 | 完成：rars 0.12.4 在 28 個真實 RAR、1,496 個檔案上與 Go 版完全相同，速度約 2 倍（見下） | `tools/archive-parity.sh` |
| 第一階段 4：PDF（hayro）對測試檔 | 完成：9 個測試檔上與 Go 版相當，一項較好（見下） | `tools/pdf-parity.sh` |
| 第一階段 5：LZH 搬移與解碼時間 | 完成：675 個成員與 Go 版逐位元組相同；速度相當（見下） | `tools/archive-parity.sh` |

### 第一階段 2 的量測（2026-11-09）

兩邊是功能相同的試作：同一個 80×25 畫面、同一組倚天與 `cvga.fon` 字型，按 Esc 離開。
Go 試作用現有的 `cell`、`fnt`、`eten`、`render` 加 Ebiten；Rust 試作是 `rust/crates/wincv`（winit + softbuffer）。
執行檔都去掉符號（Go `-ldflags "-s -w"`，Rust `strip = true`、LTO）。Go 試作量完已刪除，沒有進版控；重現時照上述條件重寫即可。

| 產物 | Go 試作 | Rust 試作 |
|---|---|---|
| Linux x86-64 | 6.78 MB（gzip 2.61 MB） | 2.67 MB（gzip 1.22 MB） |
| Windows x86-64 | 6.39 MB（gzip 2.43 MB） | 0.79 MB（gzip 0.43 MB） |
| wasm | 9.41 MB（gzip 2.37 MB） | 0.94 MB（gzip 0.19 MB），wasm-bindgen 處理前 |

| 執行（Linux，主機 Xvfb，容器 `--cpus 2`，各 3 次） | Go 試作 | Rust 試作 |
|---|---|---|
| `docker run` 到視窗出現 | 0.70–0.71 秒 | 0.21–0.24 秒 |
| 視窗出現 4 秒後的記憶體（`docker stats`） | 50–88 MB | 3.6 MB |

判讀時要注意兩點：

- Ebiten 經 OpenGL 繪圖，容器裡沒有 GPU，由 Mesa 的軟體實作代勞；softbuffer 只做 CPU 貼圖。記憶體與啟動時間的差距有很大一部分來自繪圖路徑，不全是語言造成的。
- Rust 的 Linux 版在執行時以 dlopen 載入 `libxkbcommon-x11.so`（winit 的 X11 鍵盤處理），Go 版沒有這個相依。一般桌面都有，精簡環境要裝 `libxkbcommon-x11-0`；容器內驗證用 `tools/docker/rust-run.Dockerfile`。

macOS（2026-11-09，`tools/docker/osxcross-rust.Dockerfile` 建的 `wincv-osxcross-rust:1`，SDK 15.5）：

- x86_64 與 arm64 各約 0.72 MB，`lipo` 合成的 universal binary 1.46 MB。
- 兩個架構都只連到系統 framework 與 `/usr/lib` 的系統庫；arm64 有 `LC_CODE_SIGNATURE`（連結器加的 ad-hoc 簽章），`minos 11.0`。
- 待驗：發行設定開了 `strip = true`，要在 Mac 上用 `codesign -v` 確認 strip 之後簽章仍有效；沒有實機啟動過。

其他已驗證的事實：

- Windows 版是 GUI 子系統，只匯入系統 DLL（`x86_64-w64-mingw32-objdump -p`），在本機 Wine 下畫面正常。
- Linux 版在主機 Xvfb 下畫面正常，倚天全形字正確。

### 第一階段 3 的量測（2026-11-09）

語料是本機 `~/cht` 底下的真實 RAR（遊戲與手冊的封存檔），排除三個超過 400 MB 的，共 28 個檔、
約 0.5 GB：23 個 RAR4、5 個 RAR5。另有一個路徑在容器裡讀不到（`~/cht/dosgolem-rust` 連到掛載範圍外），
它與 `mm3/` 下的同名檔大小相同。Go 端是現行的 rardecode，Rust 端是 rars 0.12.4，兩邊都在 2 核容器、
每輪打開到解完全部檔案，重做 3 次取最快。

- 1,496 個檔案的檔名、大小、sha256 兩邊完全相同。
- rars 關掉預設功能（不用 rayon，循序解碼）後，4 個代表檔的時間：

| 檔案 | Go（rardecode） | Rust（rars） |
|---|---|---|
| `第三波-殖民帝國.rar`（RAR4，86 個檔） | 1.08 s | 0.61 s |
| `珍101-幽靈騎士.rar`（RAR4，80 個檔） | 0.86 s | 0.50 s |
| `Civilization 2 (disk).rar`（RAR5，144 個檔） | 0.41 s | 0.20 s |
| `#RICH2.rar`（RAR5，120 個檔） | 0.12 s | 0.06 s |

rars 的 README 自述起源是「agentic development experiment」，也說還需要更多大量測試。
本專案的語料都通過，但 SFX、分卷、加密、RAR 1.x 沒有涵蓋，正式採用前要補這幾類樣本。

### 第一階段 4 的量測（2026-11-09）

`internal/pdf/testdata` 的 9 份 PDF，第 1 頁以 96 dpi 各畫一張：LibreOffice（真值）、Go 版
`tools/pdfshot`、hayro 0.9.0（開 `embed-fonts`，`rust/crates/oracle` 的 `pdfshot`）。
用 `tools/inkdiff`（32×44 格的墨水密度）比對各自與 LibreOffice 的差距。

| 檔案 | Go：平均差／最大差／相關 | hayro：平均差／最大差／相關 |
|---|---|---|
| evenodd | 0.0000／0.0000／1.0000 | 0.0000／0.0000／1.0000 |
| image | 0.0079／0.8654／0.9584 | 0.0082／0.8654／0.9581 |
| inline | 0.0009／0.0750／0.9998 | 0.0009／0.0750／0.9998 |
| rich（CID 中文字型） | 0.0024／0.2169／0.9464 | 0.0024／0.1855／0.9478 |
| seac | 0.0081／1.0000／0.7363 | 0.0080／1.0000／0.7379 |
| shading | 0.0099／0.7200／0.9770 | 0.0101／0.6800／0.9777 |
| shading-unsupported | 0.0344／1.0000／0.0000 | 0.0028／0.4400／0.9850 |
| softmask | 0.0098／1.0000／0.9433 | 0.0098／1.0000／0.9433 |
| twocol | 0.0098／0.0593／0.9952 | 0.0133／0.0755／0.9906 |

- hayro 與 Go 版相當；`shading-unsupported`（Go 版刻意不支援的漸層類型）hayro 畫對了。
- `image`、`seac`、`softmask` 兩邊與 LibreOffice 的差距數字相同，差異在 LibreOffice 那一側。
- hayro 每頁 2.5–5.6 ms（816×1056）。
- 這 9 份是為 Go 版功能寫的小檔，涵蓋面有限。正式採用前要再跑一批真實 PDF（`WINCV_PDF_CORPUS`）。
  hayro 的 README 自述仍缺加密 PDF、混合模式與隔離、knockout 群組等功能。

### 第一階段 5 的量測（2026-11-09）

`rust/crates/lzh` 逐行照 Go 版搬移，Go 版的 uint32 回繞改用 `wrapping_*` 保留同樣語意。
語料是 Go 版當初對 p7zip 驗過的三個檔，加上 testdata 的 `init1181.lzh`。兩邊都在 2 核容器裡，
每一輪打開壓縮檔、列目錄、解出全部檔案，重做 5 次取最快（Go 版在 `archive.Open` 時就解完全部內容，
所以打開必須計入）。

| 檔案 | Go | Rust |
|---|---|---|
| `dosbox.lha`（10 個檔，含 2.6 MB 的成員） | 20.8 ms | 17.2 ms |
| `src.lzh`（653 個小檔） | 3.5 ms | 3.8 ms |
| `MTSRC023.LZH`（11 個檔） | 8.3 ms | 8.8 ms |

輸出 674 列（每列一個檔案的大小與 sha256）兩邊完全相同。直接照搬的解碼器在 Rust 沒有明顯的速度優勢；
要快，得針對熱迴圈另外最佳化（例如逐位元組 `push` 改成整段複製），那是功能對等之後的事。

Rust 程式在 `rust/`（cargo workspace），工具鏈映像由 `tools/docker/rust.Dockerfile` 建成 `wincv-rust:1`，
cargo 一律經 `tools/cargo.sh` 在容器裡跑。

## 待決定事項

- 網頁版是否公開部署。公開部署就不能內含第三方字型，介面會改用可散布字型或由使用者提供。
- Android 是否繼續支援，以及最低支援的 Android 版本。

## 來源

- [rar-stream 架構說明](https://docs.rs/crate/rar-stream/5.3.2/source/ARCHITECTURE.md)、[rars](https://docs.rs/crate/rars/0.7.4)
- [sevenz-rust2 README](https://docs.rs/crate/sevenz-rust2/0.20.2/source/README.md)
- [hayro](https://docs.rs/hayro)、[hayro README](https://docs.rs/crate/hayro/0.7.0/source/README.md)
- [softbuffer README](https://docs.rs/crate/softbuffer/0.4.8/source/README.md)
- [android-activity 0.6.1 README](https://docs.rs/crate/android-activity/0.6.1/source/README.md)、[cargo-apk](https://docs.rs/crate/cargo-apk/latest)、[winit Android 平台說明](https://docs.rs/crate/winit/latest/source/src/platform/android.rs)
- [encoding_rs](https://docs.rs/crate/encoding_rs/latest)、[作者說明](https://hsivonen.fi/encoding_rs/)
- [image 0.25.6 README](https://docs.rs/crate/image/0.25.6/source/README.md)、[image 原始碼中的 PCX 註解](https://doc.servo.org/src/image/lib.rs.html)
- [bzip2-rs（Trifecta Tech）](https://trifectatech.org/projects/bzip2-rs/)
- [Rust 執行檔大小設定（Collabora）](https://www.collabora.com/news-and-blog/blog/2020/04/28/reducing-size-rust-gstreamer-plugin/)、[min-sized-rust](https://github.com/RunOnceEx/min-sized-rust)
- [Rust 交叉編譯到 macOS（2026 指南）](https://oneuptime.com/blog/post/2026-03-02-how-to-cross-compile-rust-applications-on-ubuntu/view)
