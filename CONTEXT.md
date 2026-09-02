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
| **瀏覽模式** | `ModeBrowse`,gopher 與 http 共用的那一個畫面 | gopher 模式(它現在只是其中一種協定) |
| **選單列** | 畫面最上方常駐的那一列(檔案/檢視/工具/設定/說明) | 選單(那是展開的下拉) |
| **下拉** | 選單列展開之後的項目清單 | 選單視窗、彈出選單 |
| **選單層** | 選單列與下拉那一層,有自己的格點與字型,由外殼分開光柵化 | 選單畫面 |
| **後備字型鏈** | `internal/ttf` 的 `Chain`,倚天沒有的字依序往下找 | fallback(中文文件裡)|
| **取文字** | 從 PDF / Office 文件抽出內容排成區塊那一路 | 解析(太籠統) |
| **頁面圖** | 把 PDF 的一頁整頁畫成點陣圖那一路(瀏覽模式按 `V`) | 預覽、截圖 |
| **字級** | 換的是**點陣字本身**(8×15 / 10×18 / 12×24) | 字體大小(會與放大倍率混淆) |
| **放大倍率** | 使用者要的顯示尺寸,0.1 為一階。**不等於把字模拉大** —— 實作會先挑一個尺寸相近的原生字級(`pickLevel`),剩下的才用整數倍放大 | 縮放、scale(中文文件裡) |
| **有效字級 / 有效倍率** | `pickLevel` 挑出來的「實際用哪一份字模、還要再放大幾倍」(`effIdx` / `effScale`)。使用者看到的是放大倍率,畫出來的依據是這兩個 | — |

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
| 2026-09-02 | macOS 交叉編譯借用 `wolong-osxcross-go:20260811-event10-r4` 就夠,只用不改動共用 image | 共用 image 的 tag 會被重新建。那個 tag 已經不存在,三支腳本的 macOS 那一步全部失敗 | 重建 `dist-all` 時 darwin 那步報 `pull access denied`;`docker images` 看到同名 image 只剩 `20260828` |
| 2026-09-02 | 拿 `strings` 抓執行檔裡的特徵字串,可以確認建的是哪一版原始碼 | `strings` 預設把 >= 0x80 的位元組當不可列印,中文會把字串切斷 —— 明明在裡面卻找不到。要用 `grep -a` | 三個執行檔全都回報「沒有」,而其中一個的行為剛剛才親眼驗過 |
| 2026-09-02 | 用檔案時間(產物 mtime 比 HEAD 的 commit 時間新)可以擋住「產物是上一版建的」 | `--no-build` 會把 dist/ 重新複製一次,`cp` 不帶 `-p`,mtime 變成複製的當下 —— 那道檢查在 `--no-build` 這條路上永遠會過,而那正是它唯一要防的情境。時間本來也只是代理指標:「比 HEAD 新」是必要條件不是充分條件 | 發 v0.52-remake.2 時 MANIFEST 記的 commit 與實際建置的commit 對不上,而檢查沒有出聲 |
| 2026-09-02 | Type1 的 `seac` 這台機器上「3,700 多個字形裡 0 個用它」,沒有可驗的樣本 | 43 份系統 Type1 字型、30,442 個字形裡有 **448 個**用 seac(全在 8 份 Bitstream Charter 裡,每份 56 個) | 前一次只掃了 URW C059 與 D050000L 幾份。把整個 `/usr/share/fonts/X11/Type1` 掃過一遍就出現了 —— 零結果要先做正對照(`TestSeacScanners`),不然「沒有」與「掃錯了」長得一樣 |
| 2026-09-02 | `.doc` 的清單只能當成項目符號,編號資訊不在檔案裡 |編號在 PlfLst 的 LVL 裡,讀得到 —— **lcbPlfLst 只涵蓋 cLst + LSTF 陣列**,後面的 LVL 陣列不算在它裡面 | 照 lcb 切下去剛好切在第一組 LVL 之前:讀到的 LSTF 完全正確、數量也對,只是每一串都是零層,沒有任何地方會報錯。先問 LibreOffice 讀同一份檔案得到什麼(「1. 條列一」),才知道資料在檔案裡 |
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
| 2026-09-02 | PDF 取文字用 `rsc.io/pdf` 就夠,限制只有「分欄會交錯」與「沒嵌字型還原不出空白」 | 中文 PDF **整頁是亂碼**。那個套件把 Identity-H 當成不用轉,而且只在沒有 Encoding 時才查 ToUnicode —— 中文 PDF 幾乎都是「Identity-H + ToUnicode」的組合 | 用 LibreOffice 產一份中英混排的 PDF 當對照。同一份檔案抽出來是 `"������"`,而預期是「第一章 標題」。之前沒有中文的測試檔,所以這個缺口一直沒現形 |
| 2026-09-02 | 要在畫面上重現 PDF 版面等於要寫一個渲染器,而純 Go 沒有堪用的,接 C 函式庫又會破掉四平台交叉編譯,所以不做 | 自己寫得出來,而且不必加相依:光柵器用已經在相依裡的 `rasterx`(SVG 那條路本來就在用),字形外框用 `x/image/font/sfnt`。對照 LibreOffice 的渲染,墨水分布相關係數 0.99 | 把「不做」的理由拆開看:難的不是光柵化(現成的),是內容資料流的解譯 —— 而那一份在做取文字時已經寫好了,只差換一個輸出裝置 |
| 2026-09-02 | 嵌入字型查不到字形時,可以拿字碼當字形編號(子集化的字型常常這樣排) | 只有在字型**沒有字碼對照表**時才成立。有對照表卻查不到,表示這個字真的不在裡面 —— 拿字碼去猜會取到別的字形 | 雙欄測試檔畫出來每個空格都變成數字。子集化時空白沒有外框所以被拿掉,而第 32 號字形剛好是個數字。畫面看起來很正常,只是多了一堆數字 |

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
| 2026-09-02 | macOS 工具鏈自建成 `wincv-osxcross-go`(`tools/docker/osxcross.Dockerfile`)| 借別人專案的 image 等於把自己的可重現性交給別人:那個 tag 被重新建過,整條 macOS 建置線就斷,而斷的時候只說 pull access denied,看起來像網路問題。底座疊在公開的 `crazymax/osxcross:15.5-debian` 上,Go 釘成與 `Dockerfile.build` 相同的 1.22.12 —— 三個桌面平台由同一版編譯器產出,才不會有「只有 macOS 那份行為不一樣」這種查不出來的差異 |
| 2026-09-02 | `tools/release.sh` 不再清空 `dist-all/`,只刪自己要重出的那幾個檔| 完整版也放在同一個目錄,而它不是 release.sh 產的。整個刪掉的話,兩支腳本的執行順序就變成一個沒有人會記得的隱藏相依 —— 反過來跑會安靜地少掉四個檔,而目錄看起來仍然是滿的。「發布物對得上同一個 commit」這個保證改由建置印記(`dist/BUILT-FROM`)撐著:建完寫下當時的 commit,`--no-build` 直接比對 |
| 2026-09-02 | 五種 Office 格式收在 `internal/officedoc` 一個窄介面後面 | 解析差異很大但對畫面來說是同一種東西(幾段內容 + 幾張圖)。`internal/app` 只認得這一層,加第六種格式時 app 一行都不用改 |
| 2026-09-02 | PDF 的取文字與畫頁面共用同一個解譯器,只換輸出裝置 | 文字定位是整份規格裡最容易寫錯的一段(六個矩陣互相相乘)。兩邊各寫一次的話會慢慢分岔,而症狀是「畫出來的位置跟抽出來的位置不一樣」,很難查 |
| 2026-09-02 | 嵌入字型解不開時用系統字型照同樣的位置與字級補畫,不留白 | 字形不同使用者看不出來,整段文字消失看得出來。畫面上會說明換過哪些字型,不假裝那是原檔的樣子 |
| 2026-09-02 | 奇偶填法用「一圈一圈各自光柵化再互斥或」,不自己寫掃描線填色器 | 奇偶的定義就是「包住這個點的圈數是奇數」,而互斥或正是那個定義。光柵器現成的非零繞組對單一圈來說就是「在這一圈裡面」,所以拼得起來。代價是自己交叉的單一圈會退化成非零繞組 —— 那是可以說清楚的邊界,不是隨機的錯 |
| 2026-09-02 | 渲染的驗收用「墨水密度格點比對」(`tools/inkdiff`),不逐像素比 | 兩個渲染器對反鋸齒、字型微調的處理一定不同,逐像素比永遠是紅的,而且紅在無關緊要的地方。要驗的是「東西有沒有畫、畫在哪一區、濃淡對不對」 |
| 2026-09-02 | PDF 頁面圖要做哪些功能,由 `tools/pdfprobe scan` 掃真實檔案的分布決定,不照規格的功能清單 | 規格列了七種漸層、四種函式,105 份真實 PDF 裡只出現軸向與放射兩種漸層、指數/接合/取樣三種函式。照清單做會把力氣花在一次都不會遇到的型別上,而真正常見的缺口(軟遮罩)反而排在後面 |
| 2026-09-02 | 頁面圖的對照組加一個 poppler,不只 LibreOffice | LibreOffice 走的是 PDF 匯入再重新排版,對版面夠用但對顏色與幾何不準(它自己把漸層畫成一條條色帶)。poppler 是獨立的 PDF 渲染器,而且本機已有 `minidocks/poppler`,不必另建 |
| 2026-09-02 | `sh` 記下裁剪路徑本身,其餘運算子仍只取外接矩形 | 填色的形狀由路徑自己決定,裁剪多半只是保險;`sh` 沒有自己的形狀,**裁剪就是形狀**。只為它多存一條路徑,不必把整個渲染器改成遮罩式裁剪 |
| 2026-09-02 | CFF 的 seac 不抄 391 個標準字串的表,用「前 149 個標準字串就是 StandardEncoding 的順序」這個規律算 SID | 那個順序是規格當初就這樣訂的,不是巧合;算出來的對應拿真實字型驗過(`TestStandardSIDAgainstRealFont`,10 個字碼含上半部的 acute / dieresis / AE)。抄表的話多 391 行資料,而且抄錯一格不會有任何徵兆 |
| 2026-09-02 | 顏色一律用**預乘**的 `color.RGBA`(Go 的定義),合成算式配合它改 | 原本各通道沒有乘 alpha,交給光柵器畫半透明的紅會變成**灰色** ——顏色完全不對,而畫出來仍然是一塊實心的方塊,不會有任何錯誤。`image/draw` 與 `x/image/vector` 全都照預乘的定義走,順著它才不會兩套慣例互撞 |
| 2026-09-02 | 軟遮罩與透明群組都用「整頁的暫時畫布」,不做局部緩衝 |一層 A4 96 dpi 約 3.4 MB,而巢狀有上限(8 層)。局部緩衝要處理座標平移與邊界框相交,錯一格的症狀是「遮罩整片位移」—— 而畫出來仍然是一片有濃淡的東西,看不出位移 |
| 2026-09-02 | 「這份字型有沒有可用的字碼對照表」改成拿**字型自己用到的字碼**去問,大部分查得到才算數 | 原本只問「認不認得幾個常見的字」。子集化的字型常常留著一張只涵蓋少數幾個字的表(實測:56 個字形的子集,表裡只有兩個數字),其餘的字是用字碼直接定址的 —— 有一個字查得到就相信整張表的話,那份字型會整個改用系統字型畫,而位置、字級、內容全對,只有字形換了一套,看起來完全正常 |
| 2026-09-02 | 原版素材的位置改由 `internal/datadir` 決定(環境變數 > 執行檔旁邊 >個人設定目錄 > 工作目錄),不再是相對於工作目錄的 `original/app/` | 那個相對路徑只有「從 repo 根目錄跑」才成立。打包安裝之後,Windows 按兩下的工作目錄是桌面或system32、macOS 是 `/`、Linux 是啟動器給的任何地方 —— 使用者把字型放在執行檔旁邊完全沒有作用,而畫面照樣出得來(改用系統字型),看起來像「這個功能沒做」 |
| 2026-09-02 | 平台字型目錄改成由環境變數推導,掃描認得的檔名涵蓋四個平台 |原本 Windows 寫死 `C:\Windows\Fonts`(Windows 不一定裝在 C:)、漏掉「只為我安裝」的使用者字型目錄;掃描認得的名字只有 Noto 與幾個 Linux 發行版的字型,於是**掃描在 Windows 與 macOS 上一個字型都找不到** —— 那兩個平台只剩寫死清單能用,而寫死清單追不上系統版本變動,正是掃描存在的理由 |
| 2026-09-02 | 文字檢視器加游標與光棒,方向鍵從「捲畫面」改成「移游標」|原版本來就有游標(工具列報「1 字 1 行/ 626」),只是沒有把那一列畫出來。長檔案捲到一半時「我剛才看到哪裡」是靠一條線記住的。翻頁另外走 PageBy —— 「游標往下移一整頁」會讓游標從畫面頂端跑到底端而畫面只捲一列,那不是翻頁 |
| 2026-09-02 | SVG 的 `<text>` 自己畫,不換掉 oksvg | oksvg 只認得路徑,碰到 `<text>` 直接跳過 —— 圖表的長條畫得出來,標題、座標軸刻度、資料標籤全部不見,而畫面看起來是完整的,不像出錯。換一套會畫字的 SVG 套件要多一大包相依(還多半綁 CGO),而缺的只是「把字排到指定位置」:位置、字級、對齊、顏色都寫在屬性裡,字形外框 `internal/vecfont` 已經拿得到。實測兩張真實圖表對使用者自己那條匯出管線的算繪相關係數 0.9304 → 0.9956、0.9983 |

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
- [x] Office 文件:`.docx` / `.doc` / `.rtf` / `.pptx` / `.xlsx`(對 LibreOffice 驗)
- [x] PDF 取文字:CID 字型與 ToUnicode、預先定義的中日韓 CMap、分欄、書籤目錄
- [x] PDF 頁面圖:路徑填色與描邊、色彩空間、影像、嵌入 TrueType 的字形外框
      (瀏覽模式按 `V`;對照 LibreOffice 的渲染,相關係數 0.99)
- [x] PDF:CFF(Type1C / CIDFontType0C)與 Type1 嵌入字型的外框。
      CFF 拿系統上 OpenType 字型的 CFF 表跟 `x/image/font/sfnt` 對比(199 個字形
      的外接矩形一致);Type1 用 testdata 的真檔驗(LibreOffice 把中日韓子集
      嵌成 Type1)。兩者都做完之後 rich.pdf 不再需要系統字型補畫
- [x] PDF:漸層(shading)與漸層圖樣 —— 軸向與放射兩種,顏色由指數 / 接合 /
      取樣三種函式算。`sh` 照裁剪路徑填(不是外接矩形),漸層圖樣的座標系綁在
      頁面上。對 poppler 的算繪相關係數 0.9999
- [x] PDF:`seac`(用兩個標準字形拼出重音字)—— Type1 的 seac 與 CFF 的
      四參數 endchar 兩條路。對 poppler 的算繪相關係數 0.9994(拿系統上真的
      有用 seac 的 Bitstream Charter 驗)
- [x] PDF:透明度 —— 常數透明度、亮度與 alpha 兩種軟遮罩、透明群組。
      行銷型錄那一頁對 poppler 從 0.24 升到 **0.9949**
- [x] 跨平台的字型與素材位置:`internal/datadir` 決定原版素材去哪找,
      `internal/ttf` 的平台目錄由環境變數推導。四個平台的路徑組法都有測試
      (用假的 GOOS 與環境變數,在任何一台機器上都驗得動)
- [x] PDF:README 的 Office 與 PDF 功能截圖,示範文件在 `docs/demo/office/`
      (內容自己寫的,LibreOffice 轉出;重建與重拍的指令在該目錄的 README)
- [x] PDF:物件層交不出來的影像自己解(ICCBased 色彩空間會讓它回空但不報錯)。
      DCTDecode 直接當 JPEG,其餘走原始取樣值那條路,影像自己的 /SMask 也套上去。
      六個真實頁面對 poppler 全部 ≥ 0.95(原本 0.24–0.99)
- [x] PDF:內嵌影像(`BI`/`ID`/`EI`)—— 參數字典、五種濾鏡、1/2/4/8/16 位元的
      取樣值、索引色與遮罩影像。對 LibreOffice 的算繪相關係數 0.9998
- [x] PDF:奇偶填法(`f*`)—— 拆成一圈一圈各自光柵化再逐像素互斥或。
      對 LibreOffice 的算繪相關係數 1.0000、墨水量完全相同
- [x] SVG:`<text>` / `<tspan>` —— 位置、對齊(`text-anchor`)、字級、字重、
      顏色、字距、繼承與 `transform`。字形來自 `internal/vecfont`(系統或內嵌
      字型的外框)。兩張真實圖表對匯出的 PNG 相關係數 0.9956 與 0.9983

### 不打算做的(理由寫在對應的 Formats 表)

- **PDF 的拼貼圖樣**(PatternType 1)、**網格類漸層**(ShadingType 4–7)、
  **PostScript 計算函式**(FunctionType 4):`tools/pdfprobe scan` 掃這台機器上
  105 份真實 PDF,這三類**一次都沒有出現**。看不懂的漸層一律留白,
  `internal/pdf/testdata/shading-unsupported.pdf` 盯著這個行為。
- **PCD**(Photo CD):Huffman 編碼的 YCC + 多組解析度,同樣沒有 oracle。
- **CAB 的 LZX / Quantum**、**ARC 的方法 4 與 7**、**LHA 的 -lh1-**、
  **ACE 的 SOUND / PIC**:同理,沒有可驗證的測試資料就不寫。

共同的判準:**沒有 oracle 就不寫解碼器**。塞一個沒驗過的實作進去,
使用者會拿到「看起來有東西但內容是錯的」檔案,比明講不支援更糟。
