# CONTEXT

## Glossary

專案內一律用左欄的詞,不要在文件、變數名、commit 之間混用同義詞。

| 詞 | 指的是 | 不要寫成 |
|---|---|---|
| **原版** | WinCV 0.52,由 `wincv.exe` + `WINCV.IMG` 組成的整體 | 舊版、original app |
| **kernel** | `wincv.exe`,Win32Forth v4 STC 的載入器與 VM | 主程式 |
| **image** | `WINCV.IMG`,Forth dictionary image,應用邏輯全在此 | 資料檔 |
| **word** | Forth 的一個定義(相當於函式) | function、副程式 |
| **xt** | execution token,word 的執行位址 | 函式指標 |
| **code space** | image 中放 word body 的區間(0x000000–0x12334c) | 程式區 |
| **header space** | image 中放 word 名稱與 xt 的區間(0x122794 起) | 符號區、字典區 |
| **STC** | subroutine-threaded code,Forth 的一種編譯方式,產生真 x86 `call` | threaded code |
| **cell** | 畫面上一個半形格。全形字佔兩個 cell | 字元、格子 |
| **cell grid** | 整個畫面的 cell 陣列,`internal/cell` 的 `Screen` | 畫布、buffer |
| **oracle** | 跑在 Wine 上的原版,用來產生對照截圖與行為真值 | 參考版、原始版 |
| **格點比對** | 依 cell grid 切格逐格比對兩張截圖 | pixel diff(這是另一件事) |
| **半形字型** | 隨附的 `.FON`,只有 0x00–0xFF | ASCII 字型 |
| **CJK 點陣字** | remake 自備的全形點陣字形 | 中文字型(太籠統) |
| **vfs** | 真實目錄與壓縮檔內部共用的檔案系統介面 | 檔案抽象層 |

模式前綴(來自符號表,語意**假設待驗**,見 CLAUDE.md §9 A4):

| 前綴 | 疑似對應 |
|---|---|
| `VF-` | view file |
| `EF-` | edit file |
| `VP-` | view picture |

---

## 已被推翻的斷言

斷言被推翻時,正文直接改成正確答案,只在這裡留一行紀錄。正文不留檢討敘述。

| 日期 | 曾經寫過 | 實際 | 怎麼發現的 |
|---|---|---|---|
| 2026-08-20 | `wincv052a.exe` 是程式本體,可直接反組譯 | 是 Inno Setup 5.3.8 安裝檔;本體是裡面的 `wincv.exe` + `WINCV.IMG` | PE section 資料總和僅 54 KB,而檔案 5.8 MB;strings 找到 `Inno Setup Setup Data (5.3.8)` |
| 2026-08-20 | 逆向對象是 `wincv.exe` 這個 PE | `wincv.exe` 只是 Win32Forth kernel;應用邏輯全在 `WINCV.IMG` | `WinCV.txt` 註明開發環境 Win32Forth 4.2;image 內有 `#Win32forth v4stc 0.1d by Lcc Wizard` |
| 2026-08-20 | 符號被 strip,只剩零星名稱殘留 | 完整 header space 存在,9497 筆 | image header 0x0c 的 `0x122794` 指向 header space;走訪後 3509 筆 xt 命中 code body |
| 2026-08-20 | 這台機器的 Wine 壞了,跑不了 GUI(連 `winver` 都失敗) | Wine 正常;WINEPREFIX 放 `/tmp` 才會讓 `winex11` 載入失敗 | 依 `rulebook/82` 的記載改放 `$HOME/.wine-wincv`,一次就跑起來 |
| 2026-08-20 | oracle 截圖可以拿來做字模的 pixel diff | 只能當版面與配色真值。Wine 用自己的 `cvgasys.fon`(16 px)替換掉 app 的 `cvga`(15 px) | 拿真實 cvga 字模去截圖上做樣板比對,3663 個位置零命中;Wine log 的 `get_fontsig pix_h 16` 與量到的列距 16 對上 |
| 2026-08-20 | 15/18 px 檔位「需要 16×16 級 CJK 點陣字」 | 是 **16×15**。倚天 `STDFONT.15` 正好是 16×15,與 `cvga` 8×15 完全對齊 | 解出 `.FON` 的真實 metrics(pixHeight 15、定寬 8)後對照倚天字庫規格 |
| 2026-08-20 | WinCV 用 16 色 | **29 個具名顏色**,含 mildyellow / gooseyellow / inkgreen / ltorange 等 | `keyword_*.cfg` 用到的顏色名超過 16 個;image 0x5692d 有完整的斜線分隔清單 |
| 2026-08-20 | 二進位檔按 Enter 顯示「這是二進位檔」訊息 | 直接開 16 進位檢視 | image 的 `&Hex模式` 標籤 + 0.5 版 changelog「按 enter 看檔時自動將可能為執行檔的檔案以 16 進位方式看檔」 |

---

## 決策紀錄

| 日期 | 決策 | 理由 |
|---|---|---|
| 2026-08-20 | UI 做點陣像素級重現,不用現代 TTF | 才能做格點/像素比對當客觀驗收訊號;點陣字本身是當年感的來源 |
| 2026-08-20 | 第一個里程碑鎖定檔案瀏覽器主畫面 | 它是其他所有功能的容器,也是操作手感的核心 |
| 2026-08-20 | 就地 `git init`,repo 私有 | 私人使用;原版素材與資料檔不對外 |
| 2026-08-20 | 資料檔格式先逆向並寫成 `docs/formats/*.md` | 格式規格是純技術描述,與後續要用哪份資料無關,可先做 |
| 2026-08-20 | 只版控 `wincv052a.exe`,`original/app/` 進 gitignore | 安裝檔是唯一真相,其餘皆可由 `tools/setup-wine-oracle.sh` 重建 |
| 2026-08-20 | 解壓縮改用純 Go,不透過 CGO 包原版 DLL | DLL 是 Win32-only,包了就失去 Linux/macOS |
| 2026-08-20 | 全形字用倚天 `STDFONT.15` + `SPCFONT.15` (16×15) | 尺寸與 `cvga` 8×15 完全對齊;原版的 CJK 字形本來就隨使用者 Windows 而異,沒有單一「原版字形」可對 |
| 2026-08-20 | 光柵器切成純 CPU(`render/raster.go`)+ Ebiten 殼 | 驗收不需要顯示器,且 headless 與視窗走同一份像素路徑 |
| 2026-08-20 | `cell.Color` 用 WinCV 自己的 29 個具名顏色與順序 | `keyword_*.cfg` 就是用那些名字;用自己的 16 色會在兩套命名之間反覆換算出錯 |
| 2026-08-20 | 字典不用原版的 `.idx`,自己建索引 | 最大的 `.dat` 只有 5.6 MB;不必依賴一個沒完全解出來的格式 |
| 2026-08-20 | 符號補充區(C6A1–C8FE)一律回缺字 | 408 個碼位對 365 個字模,線性索引會取到別的字;錯字比缺字難發現 |
| 2026-08-20 | macOS 用本機既有的 `wolong-osxcross-go` image | 已有 osxcross + Go 1.25,不必另建;只用不改動共用 image |

---

## 進行中

- [x] P0 Wine oracle 管線可跑
- [x] P0 Wine prefix 裝中文字型,截出中文選單的原版畫面
- [x] P2 `.FON` 格式規格 + 解析器(`tools/fnt.py` 與 `internal/fnt` 互為對照)
- [x] P2 image 內 Big5 字串表(`docs/re/big5-strings.tsv`,845 筆)
- [x] 渲染鏈:`cell` + `fnt` + `eten` + `render` 可 headless 出圖(`cmd/celldump`)
- [ ] P0 IDA 載入 `WINCV.IMG` 並套 `docs/re/symbols.tsv`
- [ ] P1 word 呼叫圖
- [ ] P3 主畫面格點量測(要先讓 Wine 用真正的 cvga 才算得準)
- [x] P4 keymap 規格(`docs/ui/keymap.md`,證據分三級)
- [x] M1 檔案瀏覽器(列表、游標、標記、排序、進出目錄)
- [x] M2 文字檢視器(編碼判讀、ANSI 色碼、換行、搜尋)
- [x] M3 壓縮檔當目錄:ZIP / TAR / GZ / BZ2 / RAR / 7z
- [x] M4 16 進位檢視、MD5 / SFV、轉換(換行、編碼、去 HTML/ANSI、批次改名)
- [x] M3 壓縮檔補 RAR / 7z(共六種)
- [x] M4 看圖(JPEG/PNG/GIF/BMP/TIFF/WebP/PCX/TGA/PNM/ICO)與縮圖列表
- [x] M4 PE2 式區塊文字編輯器 + 語法上色
- [x] M4 英漢字典 + KK 音標 + 國字解釋 + 不規則動詞
- [x] M4 MD5 / SFV、換行與編碼轉換、去 HTML/ANSI、批次改名
- [x] P2 檔案格式規格文件(docs/formats/,七份)
- [x] M5 三平台打包:Linux / Windows(mingw)/ macOS universal(osxcross)
- [ ] M3 其餘格式:LZH / ARJ / ACE / CAB / .Z / ARC
- [ ] 尋找/取代與各種輸入對話框(目前只有骨架)
- [ ] 檔案操作:拷貝 / 移動 / 改名 / 刪除(C M R D 鍵)
- [ ] 建立壓縮檔(Alt-Z)
- [ ] KOA / PCD 圖檔
- [ ] P0 IDA 載入 WINCV.IMG 並套符號 → P1 呼叫圖 → 解出真實色表
