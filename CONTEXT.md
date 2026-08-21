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
| 2026-08-20 | Ebiten 視窗的路徑列畫成 R/G/B 直條紋,以為是渲染 bug | 程式是對的,`tools/xwd2png.py` 寫死每像素 4 bytes,而容器裡的 Xvfb 用 24 bpp(3 bytes)→ 通道以 3 為週期輪轉 | 同一個畫面用 celldump(純 CPU)畫出來是正確的純藍底;取原始像素看到週期正好是 3 |
| 2026-08-20 | 15/18 px 檔位「需要 16×16 級 CJK 點陣字」 | 是 **16×15**。倚天 `STDFONT.15` 正好是 16×15,與 `cvga` 8×15 完全對齊 | 解出 `.FON` 的真實 metrics(pixHeight 15、定寬 8)後對照倚天字庫規格 |
| 2026-08-20 | WinCV 用 16 色 | **29 個具名顏色**,含 mildyellow / gooseyellow / inkgreen / ltorange 等 | `keyword_*.cfg` 用到的顏色名超過 16 個;image 0x5692d 有完整的斜線分隔清單 |
| 2026-08-20 | image 裡沒有靜態的 COLORREF 表,29 色的 RGB 要進 IDA 看 `NEW-COLOR` 的呼叫端才拿得到 | RGB 就在每個顏色 word 的 body 裡:body 有 0x24 個位元組,第 8-10 個就是 R、G、B | 先前只搜「連續 29 個 dword」這種**表**的形狀。改成從 Forth 標頭走 xt(名字結尾 +9 的 dword)進到各自的 body,29 個一次到齊。抽取程式:`tools/palette.py` |
| 2026-08-20 | 符號補充區(C6A1-C8FE)的 43 個洞在哪要逐一 dump 字模比對才知道,在那之前整區當缺字 | 直接問 Big5 解碼器就有:走一次該區,解得開的碼位才佔一個字模,累加即是索引。算出來剛好 365,與 SPCFSUPP.15 的字模數相符 | 洞是 C8A5-C8CC(40 個)與 C8F2-C8F4(3 個)。數量本身就是驗證 —— 對不上就表示假設錯了,不會安靜地取到別的字 |
| 2026-08-20 | oracle 截圖只能當版面與配色真值,不能當字模真值(Wine 用自己的 cvgasys.fon 16 px 替換掉 cvga 15 px) | Wine **確實用 app 自己的 cvga**(log:`Chosen: L"cvga Regular" (C:\wincv\wincv.fon)`);`cvgasys.fon` 是另外給選單列用的 System 字型。拿真實 cvga 字模去比對,每個字元都剛好命中一次 | 當初的「零命中」是 `tools/xwd2png.py` 的 24 bpp 通道輪轉造成的。那個 bug 後來修好了,但這條結論沒有回頭重驗 |
| 2026-08-20 | 主畫面的格子是 8×15(等於半形字身) | 是 **8×16**。多的那一列是程式拿全形字的高度當列高,不是字型要的(`cvga` 的 `dfExternalLeading` 是 0) | 樣板比對量出列起點 40/56/72…,間距 16;Wine 的 font trace 顯示 app 同時要 `pix_h 15 charset 255` 與 `pix_h 16 charset 136` |
| 2026-08-20 | KK 音標的符號要靠一張替換表或專用字型才畫得出來 | 是一個位元組一個音素的自訂編碼,41 個符號。隨附的三個 `.FON` 都是標準 CP437,沒有音標字形 | 作者在 `kk.txt.dat` 裡留了兩筆把整套符號列一遍的條目(`aaaaa` / `aaaaaaaaaaaa`),音值再拿已知發音的單字反推 |
| 2026-08-20 | ACE「沒有公開規格,也造不出測試資料」,所以不做 | 兩點都不成立:有 1998 年的技術文件(Marcel Lemke)、有 BSD 授權的獨立實作(droe/acefile)、也有現成的測試檔(droe/acefile-testdata,實測解得出 268 個成員) | 當初只查了「有沒有 Go 套件」就下結論。改查格式本身之後三樣材料都在。現在的理由改成工作量,不是可行性 |
| 2026-08-20 | 檔案清單的欄位配色照一般終端機的慣例配(日期灰、游標反白、目錄 ltgreen) | 逐格量出來全都不是:指示欄是一整條 `#000080` 且游標不覆蓋、日期固定 `#00FF00`、時間跟著檔案顏色、游標列是白字配 `#800000`、目錄是 `#14BE00`、長檔名欄有底線 | `tools/celldiff.py` 第一次真的跑起來。之前它寫好了卻沒執行過,配色是照慣例填的,而「看起來合理」的配色沒有一項對 |
| 2026-08-20 | `LTGRAY` 在 image 裡定義兩次,不知道哪一個在用 | 兩個都在用:檔案清單 `#C0C0C0`(0x1dc40)、狀態列與筆數 `#C5C5C5`(0x50210) | 逐格量原版截圖,兩個值在同一張畫面上同時出現 |
| 2026-08-20 | `keyword_*.cfg` 的 `ltgray` 用檔案清單量到的 `#C0C0C0` | 是 `#C5C5C5`(0x50210 那個定義)。Forth 的名稱查詢拿到的是**後定義**的 word,舊定義還在、只是被蓋住,編譯期就綁好的參照(檔案清單)照樣指向舊的 | 在原版開一個含 `ltgray` 的 `.cs` 檔。`keyword_csharp.cfg` 開頭那份「顏色名自己就是該顏色的關鍵字」的圖例正好是現成的對照組 |
| 2026-08-20 | 原版設定存在 `%windir%\wincv.cfg` | 是 `%windir%\WinCV-cd-pos.cfg`,而且只存離開時的目錄與游標檔名,不是一般設定 | `WinCVins.bat` 裡的註解行寫的是 `wincv.cfg`,但那是註解;跑一次原版之後去 prefix 的 windows 目錄看實際生成什麼 |
| 2026-08-21 | `WINCV.IMG` 在 IDA 裡把 segment 設成 `set_segm_addressing(seg, 2)` 當 32 位元 | 2 是 **64 位元**,1 才是 32 位元。整批指令沿著錯的邊界解(0x40-0x4F 在 32 位元是 inc/dec,在 64 位元是 REX 前綴),而反組譯**看起來仍然是合理的指令**,只是暫存器變成 rdi/rax | `[edi+X]` 的調查結果印出 `mov eax, [rdi+20h]` 才發現。錯的資料庫下只找到 1 處 `[edi+X]`,修正後是 8703 處 |
| 2026-08-21 | 用 `str.replace('main()', ...)` 給 IDA 腳本加 try/except | 那會連 `def main():` 一起換掉,變成 `def try:`。整份腳本語法錯誤 → IDAPython **一行都沒跑**,而症狀是零輸出零訊息,與「IDAPython 在這個環境不能用」一模一樣 | 讓腳本第一行就寫痕跡檔,分得出「沒被執行」與「執行到一半死掉」。`tools/ida.sh` 現在會先 `ast.parse` 再燒一次 IDA |
| 2026-08-21 | ACE 對 acefile 語料解出 **268** 個成員,分布是 type 2 有 265 個、type 0 有 3 個 | 是 **269**:type 2 有 265 個、type 0 有 **4** 個。語料只有 2 個 `.ace` 檔,`TestFullCorpus` 的 glob 兩個都掃到 | 重新抓 `droe/acefile-testdata` 跑 `WINCV_ACE_CORPUS=<dir> go test ./internal/archive/ace/ -run TestFullCorpus -v`,印出「2 個檔案共 269 個成員全部通過 CRC-32」。268 與 269 是同一個 commit 裡同時寫下的,靠歷史分不出對錯。**用 269-3 反推 type 2 是 266 也是錯的** —— 分布要另外量,不能從總數推 |
| 2026-08-21 | `CLAUDE.md` 的里程碑表寫「12 種格式支援 11 種(ACE 不做)」 | 12 種全支援。同一份文件的 §4.3 早就寫著 ACE 自己實作完並對 acefile 驗過 269 個成員 | 這一輪回頭對 `archive.Formats` 核實時發現;那張表是文件裡的,程式碼裡的表才是真相 |
| 2026-08-21 | Android 版用的是 Ebiten v2.6.6,相依 `golang.org/x/mobile` | 是 **v2.8.8**,相依 `github.com/ebitengine/gomobile`(ebiten 自己 fork 的那支) | 模擬器上的 panic stack trace 印出 `ebiten/v2@v2.8.8`;`go.mod` 核對後確認 |
| 2026-08-21 | APK 驗過四個 ABI、簽章有效,行為只差「還沒實跑」 | 一跑就 panic 死在第一次 Layout。`EbitenView.onLayout` 用「像素 ÷ deviceScale()」算尺寸,而 deviceScale 在 JVM 還沒就緒時是 0,`+Inf` 轉 int 變負數,`mobile.go` 的 `Layout` 又把它原樣傳回去 | `tools/run-android-emulator.sh` 第一次真的把 APK 跑起來。格式驗證(`verify-apk.sh`)對這個問題完全無感 —— 它驗的是檔案內容,不是行為 |
| 2026-08-20 | 二進位檔按 Enter 顯示「這是二進位檔」訊息 | 直接開 16 進位檢視 | image 的 `&Hex模式` 標籤 + 0.5 版 changelog「按 enter 看檔時自動將可能為執行檔的檔案以 16 進位方式看檔」 |

---

## 決策紀錄

| 日期 | 決策 | 理由 |
|---|---|---|
| 2026-08-20 | UI 做點陣像素級重現,不用現代 TTF | 才能做格點/像素比對當客觀驗收訊號;點陣字本身是當年感的來源 |
| 2026-08-20 | 第一個里程碑鎖定檔案瀏覽器主畫面 | 它是其他所有功能的容器,也是操作手感的核心 |
| 2026-08-20 | 就地 `git init` | 逆向結論要能被重跑驗證,所以原版安裝檔與抽出的資料進版控 |
| 2026-08-21 | 瀏覽做 gopher 不做 HTTP | 協定形狀與這個畫面相合(一行一個項目、型別在第一個位元組);HTTP 的難處全在 HTML,而 HTML 要的排版與腳本這個畫面給不了。連外只在使用者輸入位址時發生,不是看檔案的副作用 |
| 2026-08-21 | 走 BSD 2-Clause,repo 公開 | 保存文化資產的前提是有人拿得到。授權只涵蓋 remake 自己寫的部分,原版著作權仍屬原作者;選 2-Clause 是因為 `internal/archive/ace` 有一段從 BSD-2 的 acefile 移植,同款最不容易出錯 |
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
- [x] P0 IDA 載入 `WINCV.IMG` 並套 `docs/re/symbols.tsv`(`tools/ida.sh`,3253 個函式 / 6837 個名字)
- [ ] P1 word 呼叫圖
- [x] P3 主畫面格點量測(`docs/ui/main-screen.md`,格子 8×16)
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
- [x] M3 其餘格式:LZH / ARJ / CAB / .Z / ARC / ACE(各自對參考實作驗過,12 種全支援)
- [x] 底部輸入列取代原版的對話框;編輯器 F6 尋找/取代
- [x] 檔案操作:拷貝 / 移動 / 改名 / 刪除 / 比對(C M R D Alt-C)
- [x] 建立壓縮檔(Alt-Z,.zip)
- [x] KOA 圖檔
- [x] 主畫面按鍵補齊:O 開啟、P 改路徑、G 執行、Alt-E 註解、
      F1 選單、F8 中英文顯示、F11 全螢幕
- [x] Android:`mobile/` 進入點、觸控功能列、`internal/ttf` 系統字型後備
- [x] Android:APK 建得出來(`tools/build-android.sh`,四個 ABI)
- [x] Android N5:APK 在模擬器上實跑(Android 14 / Pixel 5 profile,畫得出畫面、讀得到檔案)
- [ ] Android:觸控輸入實測(目前只證明畫得出來、讀得到,沒證明點下去會動)
- [ ] Android N2:軟鍵盤字元事件(`mobile.Update` 目前完全沒讀鍵盤)

### 不打算做的(理由寫在對應的 Formats 表)

- **PCD**(Photo CD):Huffman 編碼的 YCC + 多組解析度,同樣沒有 oracle。
- **CAB 的 LZX / Quantum**、**ARC 的方法 4 與 7**、**LHA 的 -lh1-**、
  **ACE 的 SOUND / PIC**:同理,沒有可驗證的測試資料就不寫。

共同的判準:**沒有 oracle 就不寫解碼器**。塞一個沒驗過的實作進去,
使用者會拿到「看起來有東西但內容是錯的」檔案,比明講不支援更糟。
