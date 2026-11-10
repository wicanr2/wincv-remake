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
| `oksvg` | SVG | resvg 0.49（usvg 排版 `<text>`） | 低：已對 224 個真實 SVG 以 Chromium 為真值驗過 | 見「第二階段 7」 |
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
| 第一階段 2：視窗與五個平台的建置量測 | Linux、Windows、macOS、wasm（含瀏覽器實跑）完成（見下）；Android 延後 | `rust/crates/wincv`、`tools/rust-web-smoke.sh` |
| 第一階段 3：RAR 候選 crate 對語料 | 完成：rars 0.12.4 在 28 個真實 RAR、1,496 個檔案上與 Go 版完全相同，速度約 2 倍（見下） | `tools/archive-parity.sh` |
| 第一階段 4：PDF（hayro）對測試檔 | 完成：9 個測試檔上與 Go 版相當，一項較好（見下） | `tools/pdf-parity.sh` |
| 第一階段 5：LZH 搬移與解碼時間 | 完成：675 個成員與 Go 版逐位元組相同；速度相當（見下） | `tools/archive-parity.sh` |
| 第二階段 1：`keys`、`textenc`、`i18n` | 完成（見下） | `tools/textenc-parity.sh` |
| 第二階段 2：`vfs`、`browser`（目錄畫面） | 完成：5 個目錄場景與 Go 版逐像素相同（見下） | `tools/rust-parity.sh` |
| 第二階段 3：`viewer`、`hexview`、`session` | 完成（見下） | `tools/rust-parity.sh` |
| 第二階段 4：壓縮檔（`wincv-archive`） | 完成：12 種格式；737 個真實壓縮檔的 116,959 個檔案與 Go 版相同，另外多解出 2,839 個 Go 版解不開的檔（見下） | `tools/archive-parity.sh`、`tools/rust-parity.sh` |
| 第二階段 5a：`syntax`、`editor`、`checksum`、`convert`、`note`、`search`、`fileop`、`dict` | 完成：Go 版的單元測試全部照搬並通過；4 個編輯器場景與 Go 版逐像素相同（見下） | `cargo test`、`tools/rust-parity.sh` |
| 第二階段 5b：`imgfmt`、`imgview`、`thumbs`（SVG 除外） | 完成：1,922 張真實圖檔與 Go 版比對，JPEG 與 WebP 另以 Pillow 當第三方真值；7 個看圖與縮圖場景逐像素相同（見下） | `tools/img-parity.sh`、`tools/rust-parity.sh` |
| 第二階段 6：字型（`ttf`、`vecfont`、`fontset`、`bundled`、`datadir`，含 24 點倚天字庫） | 完成：Go 版讀得了的字型，點陣字模逐位元相同、向量外框逐段相同；9 個字級與後備字型場景逐像素相同（見下） | `tools/glyph-parity.sh`、`tools/rust-parity.sh` |
| 第二階段 7：SVG（resvg） | 完成：224 個真實 SVG 以 Chromium 為真值，Rust 全部解得開、平均差 2.38（Go 10.04，另有 21 個解不開）（見下） | `tools/svg-parity.sh` |

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

wasm 在瀏覽器（2026-11-09，`tools/rust-web-smoke.sh`）：

- `fonts` 功能把原版字型內嵌進 wasm（網頁上沒有檔案系統），經 wasm-bindgen 0.2.130 處理後 932 KB，gzip 530 KB。
  比沒有字型的 191 KB 大，增加的是倚天字庫，點陣資料不好壓縮。這種產物含第三方字型，只能留在本機。
- Playwright 映像裡的 headless Chromium 載入後畫面與原生版相同，倚天全形字正確。
- wasm-bindgen 提醒 `strip = true` 會拿掉 `target_features` 區段，網頁版的發行設定要改成不 strip、另跑 wasm-opt。

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

### 第一階段的結論（2026-11-09）

五項都有數字，產物大小有實質改善，RAR 與 PDF 各有候選在語料上通過，進入第二階段。
Android 延後（使用者決定）：要重建 NDK、SDK、JDK、Gradle 與模擬器，等功能夠多、值得在手機上試時再做。

### 第二階段 1：`keys`、`textenc`、`i18n`（2026-11-09）

- `wincv-i18n` 直接讀 Go 版的 `internal/i18n/locales/*.json`，兩邊共用同一份翻譯。key 裡的 Go 格式動詞由執行期格式器展開
  （`%d %s %v %w %q %c %x %X %f %g %%`、寬度、補零、精度），14 個案例與 Go 1.22.12 的 `fmt.Sprintf` 實際輸出相同。
- `wincv-keys`、`wincv-textenc` 照搬 Go 版的規則與測試。
- `textenc` 與 Go 版對照：原版 `original/app/` 62 個檔、repo 的文件、300 個固定種子的隨機檔，共 388 個檔案。
  - 判讀結果全部相同。
  - 解碼器改用 encoding_rs（WHATWG）後有兩類差異，都接受：
    - 多解出字：GBK 的使用者自訂區（對到 Unicode 私用區）、GB18030-2005 新增的字（直排標點 U+FE10–FE19、U+9FB4 起的漢字等）、
      Shift_JIS 的部分擴充字，Go 的 x/text 解成 U+FFFD，Rust 解出字。原版 `original/app/` 被判成 GBK 的對照表就有這種字。
    - 取代粒度不同：一段不合法位元組換成幾個 U+FFFD 的規則不同。只發生在用錯的編碼硬解（例如 UTF-8 文件強制用 Big5 看）或隨機資料。
  - 沒有「兩邊把同一段位元組解成不同字」的情況。
  - 使用者決定（2026-11-09）：文字解碼以 Rust 版（WHATWG）為準，不必與 Go 版一致。
- Go 的字串可以裝不合法的 UTF-8，range 時每個壞位元組各算一個 U+FFFD；Rust 端的 `utf8_lossy_go` 照這個規則做，
  不用 `from_utf8_lossy`（後者把一段壞位元組併成一個）。

### 第二階段 2：`vfs`、`browser`（2026-11-09）

- `tools/rust-parity.sh` 擴充成多場景：示範畫面、`docs/`、`testdata/lzh/`、repo 根目錄，以及特製目錄
  （`tools/parity-fixture.py` 產生：Big5 原始位元組與不合法 UTF-8 的檔名、只差大小寫的同名檔、超長檔名、全形檔名、
  隱藏檔、符號連結、足以出現捲軸的數量），後者另跑 60×12 的小畫面。六個場景與 Go 版逐像素相同。
- 要與 Go 版逐項相同，有三處不能用 Rust 的慣用寫法：
  - 檔名保留原始 `OsString`，顯示時每個壞位元組換一個 U+FFFD（Go 字串的行為）。
  - `read_dir` 的結果先依原始位元組排序：Go 的 `os.ReadDir` 回傳前就排好了，之後的穩定排序依賴這個初始順序。
  - 排序用照搬的 Go 1.22 `sort.SliceStable` 演算法（`wincv-vfs` 的 `gosort`）。Go 版遞減排序的 less 是 `!less`，
    只差大小寫的同名項會被顛倒；Rust 的排序要求全序，結果不同。`testdata/go-sort.txt` 是 Go 版在 45 項、
    六種設定下的實際輸出，Rust 測試逐列比對。
- 本地時間用 chrono（讀 `TZ` 與 `/etc/localtime`），格式「MM-DD-YY」「HH:MM」。

### 第二階段 3：`viewer`、`hexview`、`session`（2026-11-09）

- celldump 加上 `-file`（文字用 viewer，二進位用 hexview）。`tools/rust-parity.sh` 增加 6 個看檔場景：原版的兩個 Big5
  文字檔、UTF-8 文件、帶 ANSI 色碼的檔（亮色、背景、重設、CR/CRLF、沒有結束字元的序列）、兩個二進位檔。
  6 個都與 Go 版逐像素相同（一開始有 2 個差在「包」字，原因是 Go 版的 bug，見下一節，已在 Go 版修正）。
- `session` 的檔案格式與 Go 版相同：`testdata/go-session.json` 是 Go 版實際寫出的檔，Rust 讀得回來，寫出來逐字相同。
- viewer 狀態列右側的位置，Go 版用位元組長度計算（有搜尋結果時含中文會偏），Rust 改用顯示格數。

### 第二階段 4：壓縮檔（2026-11-09）

`wincv-archive` 對應 Go 版 `internal/archive`：格式表、「壓縮檔路徑!內部路徑」的 vfs、解壓縮到目錄、打包成 ZIP。

| 格式 | Rust 版用的實作 |
|---|---|
| ZIP | zip 9.5（deflate、deflate64、bzip2、lzma、xz，另開 `legacy-zip`：shrink、reduce、implode） |
| TAR、GZIP、BZIP2 | tar 0.4、flate2 1.1（zlib-rs）、bzip2 0.6（libbz2-rs-sys） |
| RAR | rars 0.12 |
| 7z | sevenz-rust2 0.25（bzip2、ppmd、deflate、brotli、lz4） |
| LHA | `wincv-lzh`（第一階段搬的） |
| ARJ、ACE、CAB、ARC/PAK、.Z | 從 Go 版的自寫解碼器逐段搬過來，單元測試用同一批參考檔與期望值 |

全部是純 Rust。7z 的 zstd 沒開，因為 sevenz-rust2 的 zstd 接的是 C 函式庫；Go 版的 bodgit 支援 zstd，這是目前唯一 Go 有、Rust 沒有的壓縮法。

驗證：

- `tools/archive-parity.sh` 走兩邊的 vfs 介面（打開、ReadDir 遞迴、逐檔讀出）比 sha256。語料是 `~/cht` 底下的真實壓縮檔：
  7z、bz2、lha、lzh、rar、Z 全收，zip、gz、tar 依路徑等距抽樣，另加真實的 ARC、抽樣的 `.PAK`、125 個 Borland／Watcom 磁片的 ZIP、
  acefile-testdata 與 repo 的測試檔，共 810 個。
  - 有輸出的 737 個壓縮檔：116,959 個檔案兩邊相同。
  - Go 獨有 13 個：`git archive` 產生的 tar 開頭的 PAX 全域標頭，Go 的 `archive/tar` 把它列成一個叫 `pax_global_header` 的空檔。
    它是中繼資料，GNU tar 解壓時也不產生，Rust 版不列。
  - Rust 獨有 2,839 個：2,824 個是 1991 年前後磁片上 implode／shrink 壓縮的 ZIP 成員（Go 的 `archive/zip` 不支援），
    以 Info-ZIP UnZip 6.00 對過，2,974 個成員中 2,970 個逐位元組相同，其餘 4 個是同一種空的磁碟標籤項目，差在 unzip 顯示時去掉了點；
    15 個是 Go 解不開的 7z（ARM64 BCJ、PPMd，以及讓 bodgit/sevenzip panic 的檔）。
  - 兩邊都失敗的：加密的 RAR 與 7z、不是 ARC 格式的遊戲 `.PAK`、不是 compress 格式的 `.z`。
- ACE 完整語料（acefile-testdata，`WINCV_ACE_CORPUS`）：269 個成員全部通過 CRC-32。
- `tools/rust-parity.sh` 加 11 個壓縮檔場景（每種格式一個，ZIP 另有 Big5 原始位元組檔名、只存檔案不存目錄項的情況），
  與既有的 12 個場景共 23 個，全部與 Go 版逐像素相同。

速度（同一批檔、2 核容器、各跑一次，含打開與列目錄）：除 7z 以外每種格式都比 Go 版快，zip 約 1.9 倍、rar 約 2 倍、gz 約 2.2 倍，
自寫的格式 1.1 到 5 倍。7z 一開始反而慢 4 倍：solid 壓縮檔一個區塊要從頭解，而成員依檔名排序後常在幾個區塊之間交錯，
只快取最近一塊會整塊反覆重解。改成最多留 512 MB 已解好的區塊之後，2 個區塊、14,413 個檔的 7z 從 76 秒降到 3.2 秒（Go 版 10.3 秒）。

比對時找到的 Go 版問題，已在 master 修正（2026-11-09）：

- 成員路徑帶 `./`（`tar czf x.tgz .` 打包的就是這樣）時，最上層多一個「.」目錄，補出來的中間層目錄進去是空的；絕對路徑的成員看不到。
  兩邊都改成讀進來時正規化路徑。
- bodgit/sevenzip 在部分 7z 的標頭上 panic，整個程式結束。`archive.Open` 與 `FS.Open` 改成把 panic 轉成錯誤。

### 第二階段 5a：編輯器與檔案工具（2026-11-09）

八個套件照搬 Go 版，單元測試也逐條搬過來（syntax 8、editor 20、checksum 3、convert 8、note 4、search 5、fileop 9、dict 9），
字典另外用原版的 5.6 MB 真實資料驗。`tools/rust-parity.sh` 加 4 個編輯器場景（C 的跨行註解、字串、數字、Tab；Pascal；
原版 Big5 的 `whatsnew.txt`；30×6 的小畫面），全部 27 個場景與 Go 版逐像素相同。

與 Go 版刻意不同的地方：

- 編輯器存 UTF-16 檔時寫回 UTF-16（帶 BOM）。Go 版的編碼器對 UTF-16 直接輸出 UTF-8 位元組。
- `convert.rename_case` 的首字母大寫以字元為單位。Go 版取第一個位元組，首字是中文或 é 時會把檔名弄壞（已在 master 修正）。
- 狀態列靠右對齊用顯示寬度。Go 版用位元組數，含中文時離右緣空出幾格（已在 master 修正，新增 `cell.Width`）。

### 第二階段 5b：看圖與縮圖（2026-11-09）

- JPEG、PNG、GIF、BMP、TIFF、WebP 用 image 0.25（zune-jpeg、image-webp 等純 Rust 解碼器）；PCX、TGA、PNM、ICO、Koala 從 Go 版搬過來。
  image crate 解不開的 TIFF（調色盤、CCITT Group 3）由自寫的備援解碼器接手，Group 3 交給 fax crate。
- 自寫的解碼器認不得時，再依 magic number 試一次（副檔名寫錯的檔案很常見：`.ico` 其實是 PNG、`.gif` 其實是 JPEG）。
- SVG 要等字型模組（`ttf`、`vecfont`）搬完，格式表暫時標成未支援。字型模組已在第二階段 6 搬完。

`tools/img-parity.sh` 用 `~/cht` 底下 1,922 張真實圖檔（依格式分層抽樣）比較兩邊解出的像素，比的是疊在黑底上的結果，
全透明像素的 RGB 不算：

| 格式 | 結果 |
|---|---|
| PNG、PPM、PGM、PBM、PCX、TGA、ICO | 兩邊都解得開的全部逐像素相同 |
| BMP | 117 張相同；另 33 張（1bpp、OS/2 格式等）Go 版解不開，Rust 解得開 |
| TIFF | 142 張相同；另 8 張 Go 版解不開；FM Towns 的 16 張灰階圖差在 16 位元轉 8 位元的進位（最多差 2） |
| GIF | 136 張相同，11 張只差在透明像素；另 3 張其實是 JPEG 與 WebP |
| JPEG、WebP | 兩邊不同，改用 Pillow 11.3.0（libjpeg-turbo、libwebp）當真值比較，見下表 |

| 格式 | 張數 | 與 Pillow 的平均差（Go / Rust） | 平均最大差（Go / Rust） | 與 Pillow 完全相同（Go / Rust） |
|---|---|---|---|---|
| JPEG | 399 | 0.84 / 0.10 | 46.4 / 3.6 | 0 / 0 |
| WebP | 149 | 3.03 / 0.000 | 36.6 / 0.0 | 49 / 149 |

Rust 版在兩種格式上都比較接近參考實作：WebP 與 libwebp 逐像素相同；Go 版的有損 WebP 顏色偏移（最大差 36），
JPEG 的色度上採樣也比較粗。

`tools/rust-parity.sh` 加 7 個場景（PNG、ICO、帶透明的 PNG、32 位元 TGA 的 alpha 漸層、PGM、兩個縮圖列表），
全部 34 個場景與 Go 版逐像素相同。

比對時找到的 Go 版問題（已在 master 修正）：ICO 與 TGA 的 32 位元圖把非預乘的 BGRA 存進 `image.RGBA`，
半透明像素（圖示的反鋸齒邊緣）顏色算錯。

### 第二階段 6：字型（2026-11-09）

- `wincv-ttf`：外框用 ttf-parser 讀，光柵器（`gofix.rs`）與 TrueType 外框解碼（`glyf.rs`）逐段移植自 Go 的
  x/image v0.20.0，座標量化照 Go 的 sfnt。後備鏈、系統字型目錄的掃描、半形等寬字型（CP437）也搬過來。
- `wincv-eten`：ETUNPACK 解壓（倚天 3.53 的 `STD.24x`）與 24 點字庫，`cjk24` 是可存進 session 的字體代號。
- `wincv-vecfont`：任意字級的外框，給 SVG 的文字用。`wincv-fontset`：字級、全形來源與後備鏈的組合規則。
- `wincv-bundled`：`fonts` 功能開著時由 `build.rs` 把 `internal/bundled/assets/` 整個目錄嵌進來，與 Go 的
  `//go:embed` 同一份素材。`wincv-datadir`：原版素材的搜尋目錄，順序與 Go 版相同。

TrueType 外框不用 ttf-parser 的 `outline_glyph`，是因為複合字形（Ã、Ñ、Ğ 這類字母加附加符號）的元件有位移：
Go 版在元件自己的座標裡算隱含中點（整數除 2，向零截斷）再加位移，ttf-parser 先位移再算。附加符號在元件座標裡
常是負的 x，兩種順序的截斷方向不同，差一個字型單位。DejaVu Sans 有 11 個字因此外框不同，改成照搬 Go 的解碼後全部相同。

`tools/glyph-parity.sh` 用 1,068 個字（ASCII、拉丁擴充、希臘、西里爾、框線、繁簡中文、假名、韓文、符號）比對：

| 項目 | 結果 |
|---|---|
| 點陣字模，10 組字型與字級（Noto Sans CJK 的 CFF 外框、DejaVu、Liberation、Noto Mono；含半形縮字級與反鋸齒） | 全部逐位元相同 |
| 向量外框，5 份字型 | 全部逐段相同（座標以 26.6 定點整數比） |
| uming.ttc、wqy-zenhei.ttc | Go 版讀不了（`sfnt: invalid table offset`），Rust 版畫得出 1,006 與 956 個字 |
| 向量外框，預設字型組（內嵌加系統字型） | 120 個字不同，原因是字型清單不同：Go 版跳過讀不了的 uming、ukai、wqy-zenhei、wqy-microhei，前 8 份字型的組合因此不一樣 |

`tools/rust-parity.sh` 加 9 個場景：10×18 與 12×24 字級（倚天縮放、原生 24 點字庫），以及指定 Noto Sans CJK 或
DejaVu Sans 當後備字型的 Big5 以外文字（含非原生字級走向量字型反鋸齒、`-bitmap-cjk`），全部 43 個場景逐像素相同。
兩個容器的工作目錄不同，素材目錄改用 `WINCV_HOME` 指定；`tools/cargo.sh` 也唯讀掛入 `/usr/share/fonts`，與 `tools/go.sh` 相同。

### 第二階段 7：SVG（2026-11-09）

Go 版用 oksvg 畫路徑，`<text>` 自己排版、用 vecfont 的外框補畫。Rust 版改用 resvg：usvg 本身會排版文字
（text-anchor、tspan、變換、字距、shaping），不另做一套，所以 Rust 版的 SVG 沒有用到 `wincv-vecfont`。

- 字型來源與點陣那一路相同（內嵌的後備字型，再 `wincv_ttf::candidates`），桌面平台以 mmap 載入。
- usvg 預設的泛用字族是 Windows 字型名稱（Times New Roman 等），Linux 上沒有的話整段字不畫。改成指向實際有的字型，
  並把 Arial、Helvetica、Courier 這類常見名稱對應到泛用字族，做法與 fontconfig 的別名相同。
- 沒有 width、height、viewBox 時，尺寸取原點到內容右下角的範圍（usvg 的做法），Go 版一律是 256×256。

兩邊的光柵器不同，不可能逐像素相同，所以 `tools/svg-parity.sh` 改用 Chromium（playwright 映像的 headless_shell）
當第三方真值：從 `~/cht` 依專案分層抽樣、依內容去重，以 Rust 版的尺寸在 Chromium 畫同一個檔案，三者疊在黑底上比 RGB 差值。
三個容器掛同一份 `/usr/share/fonts`。

| 項目 | Go | Rust |
|---|---|---|
| 解不開（224 個 SVG） | 21（多為 oksvg 的 `param mismatch`） | 0 |
| 與 Chromium 的平均差（兩邊尺寸相同的 140 個） | 10.04 | 2.38 |
| 平均最大差 | 216.3 | 174.0 |
| 比較接近 Chromium 的檔案數 | 12 | 128 |

Rust 差最多的幾個是小尺寸的文字（例如 143×26 的標誌字），差在字形與反鋸齒，版面一致。

### 比對時找到的 Go 版 bug：Big5 編碼器的 HKSCS 碼

Go 版原本用 x/text 的 Big5 編碼器查倚天字形、存檔與轉碼。它在 33 個字上給的是香港增補字符集（HKSCS）的 FA–FE 碼，
例如「包」給 FABD（標準是 A55D）、「港」給 FDBB，倚天字庫只到 F9，所以畫成缺字框。Rust 版用 encoding_rs
（WHATWG 規範）沒有這個問題，逐像素比對因此抓到差異。

Go 版已改用照 WHATWG 規則建的對照表（`internal/textenc/big5.go`）：v.0.53.4 修正這 33 個字，v.0.53.5 補回只在
HKSCS 區的 764 個字。現在 Go 與 encoding_rs 在全部 14,653 個字上的編碼結果相同，`tools/rust-parity.sh` 不再有已知差異。

Rust 程式在 `rust/`（cargo workspace），工具鏈映像由 `tools/docker/rust.Dockerfile` 建成 `wincv-rust:1`，
cargo 一律經 `tools/cargo.sh` 在容器裡跑。

## 第三階段：app 層（規劃，2026-11-10）

第二階段搬完的是底層模組，Rust 的視窗程式還只有示範畫面。合併進 master 與 Rust preview 發布等到這一階段完成，
README 的截圖也那時一起換。依「最快得到能用的程式」排序：

| 段 | 內容 | 完成判準 |
|---|---|---|
| 3a | `launch`、`markdown` | Go 版單元測試照搬並通過；markdown 檢視場景逐像素相同 |
| 3b | `app` 本體：瀏覽、檢視、編輯、選單、按鍵、輸入列、session。PDF、Office、EPUB、網頁先回「尚未搬」 | Go 版 `internal/app` 的測試照搬；`celldump -app -keys` 場景逐像素相同 |
| 3c | 視窗進入點：按鍵轉換、字級切換、選單層、滑鼠 | 三個桌面平台建得出來，實際操作過一輪 |
| 3d | `gopher`、`web`、`epub` | 同 3a |
| 3e | Office：`cfb`、`doc97`、`rtf`、`ooxml`、`docx`、`pptx`、`xlsx`、`officedoc` | 以 LibreOffice 產生的測試檔與 Go 版比對 |
| 3f | PDF（hayro）與 `pdfdoc` | `tools/pdf-parity.sh` 與 Go 版相當 |

### 3a–3c 的進度(2026-11-10)

- 3a 完成:`wincv-launch`;`wincv-markdown` 的 16 條測試照搬並通過,`tools/md-parity.sh` 對 repo 內 37 份 markdown
  (加邊角案例與手組的區塊)在 40、80、120 欄的解析與排版結果與 Go 版逐位元相同。搬移時列出 Go 版 11 個可疑行為
  (折行後的列以空白開頭、清單前不空行、清單記號以字元數當寬度等),Rust 照抄,修正與否另外決定。
- 3b 進行中:`wincv-app` 的瀏覽、檢視、16 進位、看圖、縮圖、編輯、尋找、轉換、解壓、打包、MD5/SFV、選單、輸入列、
  磁碟窗格、預視、session、markdown 檢視與 F1 說明、滑鼠、觸控列已搬;Go 版 10 個測試檔的 97 條測試照搬並通過。
  `tools/rust-parity.sh` 加 19 個 `celldump -app -keys` 場景(含 markdown 內嵌圖),除了「關於」畫面寫的實作
  語言以外逐像素相同。網路瀏覽、EPUB、PDF、Office 還沒接。
- 3c 進行中:`rust/crates/wincv` 改成真正的程式(字級與倍率挑選、選單層合成、按鍵轉換含輸入法、session、全螢幕、
  視窗大小請求、滑鼠的點擊、雙擊、拖曳與滾輪),在 Xvfb 裡實際開過、用鍵盤與滑鼠操作過。Xvfb 裡 xdotool
  滾一格時游標移了 6 列(預期 3 列),疑似測試裝置同時送出兩種捲動事件,待實機確認。還沒有程式圖示、`-menu-font`;網頁版的冒煙測試
  (`tools/rust-web-smoke.sh`)還用舊的 `WINCV_FONT_DIR`,要改成 `wincv-bundled` 的內嵌方式。

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
