# WinCV Remake

WinCV（CView for Windows）是台灣的共享軟體，作者 Lcc Wizard（林健總）一個人從 1999 年
維護到 2011 年，站台 cview.com.tw，最後一版 0.52 更新於 2011 年 11 月 24 日。
它的執行檔裡沒有應用邏輯，真正的程式是隨附的一份 Forth image。這個 repo 把那份 image
逆向出來，用 Go 重寫成不綁在 Windows 上的程式。標準不是「功能差不多就好」。
原版畫面上哪一格擺什麼字、那一格是什麼顏色，都是對著原版量過再寫的，
8×16 的格子一格不動。

## 完成了什麼

2004 年 8 月，有人在部落格上寫 CView 是「以前 DOS 時代最喜歡的一套軟體」，
看圖「超快」，文章的最後一句是：「真希望他能開發 Linux 版~~」。

原版停在 2011 年 11 月 24 日。它是 32 位元 Windows 執行檔，解壓縮與看圖靠一批
Windows DLL——`unrar.dll`、`unlha32.dll`、`CAB32.DLL`、`FreeImage.dll`。
那些依賴綁死在一個平台上，沒有人保證它們在下一版 Windows 上還載得起來。

留著執行檔不等於留著這支軟體。要保住的是行為：排序規則、欄位怎麼對齊、
編碼怎麼判讀、按哪個鍵會發生什麼事。這些只有重新實作一次才會留下來。

所以逆向的對象不是 52 KB 的 `wincv.exe`（那只是 Forth kernel，沒有應用邏輯），
而是 1.52 MB 的 `WINCV.IMG`：3663 個 word、9497 筆符號、29 個具名顏色的 RGB
一個一個從 word body 裡取出來。檔案清單每一欄的顏色是把兩邊的截圖切成同一套
格點、逐格量出來的——指示欄是一整條深藍而且游標經過也不變、日期固定綠色、
長檔名欄帶底線。照終端機慣例猜，沒有一項會猜對。

12 種壓縮格式全部支援，其中 LZH、`.Z`、CAB、ARJ、ARC、ACE 六種是自寫的，
每一種都對照參考實作逐檔驗過 sha256——p7zip 675 個成員、ncompress、cabextract、
arj 3.10、arc 5.21、acefile 269 個成員。沒有 oracle 可比對的格式就不寫解碼器：
一個沒驗過的解碼器會安靜地解出看起來有內容、實際上是錯的檔案。

現在它跑在 Linux、Windows、macOS 和 Android 上。2004 年那句「真希望他能開發
Linux 版」，隔了二十二年有了回應。

說這是台灣軟體的文藝復興，太重了——這只是一支軟體。但它當年是很多人每天在用的
東西，而它本來會消失在一個沒人裝得起 32 位元 Windows 的年代。現在不會了。

> Remake by 王俊又 —— 為保存台灣中文軟體盡一份心力

---

把 1999–2011 年的台灣共享軟體 **WinCV 0.52（CView for Windows）** 逆向，
用 Go + Ebiten 重寫成 Linux / Windows / macOS / Android 都能跑的版本，
介面與原版點陣像素對齊。原作者是 Lcc Wizard（林健總），原站 `cview.com.tw`。

WinCV 是一支在同一個畫面裡做完很多事的中文工具：瀏覽目錄、看文字檔、看圖與縮圖、
把壓縮檔當目錄走進去、編輯文字、看 HEX、在 Big5 / GB / SJIS / KOR / Unicode 之間轉碼、
查英漢字典與 KK 音標、算 MD5 與 SFV。它的最後一次更新停在 2011 年 11 月 24 日
（`whatsnew.txt` 的最後一筆），之後沒有 Linux 版，也沒有 macOS 版。

下載：[v0.52-remake](https://github.com/wicanr2/wincv-remake/releases/tag/v0.52-remake)

remake 的原始碼、工具與文件走 [MIT](LICENSE)。**原版 WinCV 的著作權屬於原作者
Lcc Wizard（林健總）**，MIT 不涵蓋它——重寫是為了保存這份文化資產，不是取得原版的權利。
repo 裡保留原版安裝檔與由它抽出的資料（符號表、UI 字串、參考截圖），
是為了讓逆向結論可以被重跑驗證。原版隨附的字型、字典資料與倚天字庫是第三方版權物，
**不打包進任何產物**，由使用者自備。

![檔案瀏覽器](docs/ui/shot-main.png)

*重製版瀏覽原版自己的安裝目錄。欄位、對齊、配色與原版同格點。*

---

## CView 是什麼

CView 是 DOS 時代的檔案瀏覽器，用 F-PC Forth 寫成；進到 Windows 之後，
作者改用 Win32Forth v4 的 subroutine-threaded code 重寫，成為 WinCV。
兩個版本的手感是同一套：滿版的字元格點畫面，游標在檔案列表上跑，
按一下就把文字檔攤開，再按一下就進到壓縮檔裡面，不必先解到某個暫存目錄。

當年用過的人記得的多半是速度。2004 年 Tsung 在自己的部落格寫，
CView 是「以前 DOS 時代最喜歡的一套軟體」，看圖「超快」，
文章結尾那句是：「真希望他能開發 Linux 版~~」
（[Tsung's Blog, 2004-08](https://blog.longwin.com.tw/2004/08/cview/)）。

也有人花了好幾年找替代品。大丙從 2006 年找到 2009 年，
先後試過 WinCV 0.5、FreeCommander、Universal Viewer 5.1.0 和 Directory Opus 9.5，
多數敗在中文換行或編碼上；他要的是「一套像 DOS 時代的 CVIEW 一樣，
能快速看文字檔的軟體」，中間還抱怨了一句「可惜的是 ACDSee 沒法看文字啊」，
最後的結論是只有 Directory Opus 勉強堪用
（[大丙的筆記](https://blog.dabinn.net/cview替代軟體/)）。

作者這一側的紀錄也還在。2012 年 1 月，有人在他的部落格留言板留下
「你的 wincv 真的很棒…每天都在用」，作者回覆時提到自己時間有限、更新很慢
（[lcc-wizard.blogspot.com](http://lcc-wizard.blogspot.com/2012/01/blog-post.html)）。
軟體王的 [WinCV 0.52 下載頁](https://www.softking.com.tw/7555/) 到今天還在線上，
列在繁體中文的檔案管理工具底下，檔案大小 5.56 MB。

![文字檢視](docs/ui/shot-viewer.png)

*重製版顯示原版自己的 `whatsnew.txt`：Big5 編碼判讀、語法上色、Big5 全形字。*

## 為什麼重寫

原版是 32 位元 Windows 執行檔，解壓縮和看圖都靠一批 Windows DLL：
`unrar.dll`、`unlha32.dll`、`unarj32j.dll`、`unacev2.dll`、`CAB32.DLL`、`7-zip32.dll`，
以及 `FreeImage.dll` 和 Intel 的 `ijl15.dll`。這些依賴綁死在一個平台上，
而且沒有人能保證它們在下一版 Windows 上還能載入。

要保住的是行為——排序規則、欄位怎麼對齊、編碼怎麼判讀、按哪個鍵會發生什麼事——
這些東西只有重新實作一次才會留下來。所以逆向的對象不是 `wincv.exe`
（那只有 52 KB，是 Forth kernel，沒有應用邏輯），而是 1.52 MB 的 `WINCV.IMG`，
程式本體在裡面。

2004 年那句「真希望他能開發 Linux 版」沒有等到原作者的回應。這個 repo 是自己動手的版本。

## 與原版的差異

| 項目 | 原版 WinCV 0.52 | 本重製版 | 說明 |
|---|---|---|---|
| 平台 | 32 位元 Windows | Linux / Windows / macOS | 純 Go 不用 CGO，同一份程式碼編三個平台 |
| 解壓縮 | 外掛 Windows DLL | 純 Go：標準庫、第三方套件，加上六個自寫解碼器 | 12 種格式全部支援 |
| 圖檔解碼 | `FreeImage.dll` + Intel `ijl15.dll` | 純 Go 解碼 | 12 種格式支援 11 種，缺 Photo CD |
| 半形字型 | 自帶點陣字型 `cvga.fon`，8×15 | 直接解析原版的 `.FON` 取字模 | 字模真值來自字型檔本身 |
| 全形中文字形 | 交給 Windows GDI 用系統字型畫（image 內指名「新細明體」） | 倚天（ETEN）點陣字庫 `STDFONT.15`，16×15 | 原版的中文長相隨每個人的 Windows 而異，重製版需要一個確定的來源 |
| 格子大小 | 8 × 16 | 8 × 16 | 字模 8×15 靠上對齊，下方留一列。原版列高取自它同時要求的 16 px 全形字 |
| 選單與工具列 | Win32 選單列 + 圖示工具列 | 自繪的 `F1` 選單 | 全畫面都是自己畫的格點，不用原生控制項 |
| 預視窗格 | 有 | `Alt-P`，底部 8 列 | 文字顯示前幾行、二進位排 16 進位、圖檔顯示格式與尺寸 |
| 狀態列 | 兩列：檔案欄位 + 完整檔名 | 同樣兩列，逐格對過 | 第二列與原版**逐像素相同** |
| 左側磁碟欄 | Win32 工具列旁的窗格，列出磁碟機代號 | 清單左邊的格子窗格，`Alt-D` 開關 | Linux / macOS 沒有磁碟機代號，改列掛載點；可卸除的另外上色 |
| 捲軸 | Win32 控制項 | 最右邊一欄，箭頭 + 滑塊 | 原版那根用系統顏色畫，做不到像素等價 |
| 半形字碼表 | CP437 | 同樣走 CP437 | ANSI art 用的 `░▒▓█│─` 都在 0xB0–0xDF，當成 Latin-1 會整批畫成別的字 |
| 副檔名配色 | 有（`.bat` 整列洋紅） | 尚未實作 | 原版可在設定裡改 |
| KK 音標 | 自訂編碼，一個位元組一個音素 | 轉成 IPA 顯示（`ˈlɪtl̩`、`ˈbʌtn̩`） | 對照表解在 `internal/dict/kk.go` |
| 字級 | 設定裡選三種點陣字型之一 | `Ctrl-+` / `Ctrl--` 即時切換同樣三種 | 24 點的全形漢字倚天光碟上是壓縮的，那一級的中文由 16×15 縮放 |
| 視窗 | 固定版面 | 放大視窗就多幾格 | 格子大小不變，多出來的空間變成更多欄列 |
| Big5 以外的字 | 交給 Windows 系統字型 | 倚天沒有的字改用系統 TrueType 補 | 简体字、韓文、希臘文、多數符號。真的缺字畫成空框，不留白 |
| 散布方式 | 共享軟體，作者對外發行 | MIT，產物不含原版素材 | 字型、字典資料與倚天字庫都是第三方版權物，由使用者自備 |

倚天那一列決定了字怎麼排：`STDFONT.15` 的全形格正好是 16×15，
剛好等於 `cvga` 半形 8×15 的兩倍寬、同高，中文與英數落在同一套格點上，不必縮放或補白。

畫面配色沿用原版：29 個具名顏色的名稱與 RGB 值都是從 `WINCV.IMG` 裡解出來的，
不是重新配的一套色。檔案清單每一欄用哪個顏色則是把兩邊的截圖切成同一套格點、
逐格比對量出來的——指示欄是一整條深藍而且游標不覆蓋它、日期固定綠色、
時間跟著檔案自己的顏色走、長檔名欄帶底線，這些照慣例猜都會猜錯。
量測結果與重跑指令記在 `docs/ui/main-screen.md`。

![F1 選單](docs/ui/shot-menu.png)

*`F1` 選單同時是說明書：每一列右邊標著對應的按鍵，在選單裡直接按那顆鍵等同選它。*

## 壓縮格式

五個自寫的解碼器都對照參考實作逐檔驗過 sha256，不是拿自己的輸出當期望值：

| 格式 | 實作 | 驗證對象 |
|---|---|---|
| ZIP / TAR / GZ / BZ2 | Go 標準庫 | — |
| RAR / 7z | `nwaples/rardecode`、`bodgit/sevenzip` | 該套件 |
| LHA / LZH | 自寫 | p7zip，675 個成員 |
| `.Z` | 自寫 | ncompress |
| CAB（MSZIP） | 自寫 | gcab / cabextract |
| ARJ（方法 0–4） | 自寫 | arj 3.10 |
| ARC / PAK | 自寫 | arc 5.21 |
| ACE | 自寫 | acefile，269 個成員 |

沒有 oracle 可比對的格式就不寫解碼器。一個沒驗過的解碼器會安靜地解出
看起來有內容、實際上是錯的檔案，那比明講不支援更麻煩。

ACE 沒有內建在原版裡——原版載入 WinACE 原廠的 `unace.dll`（1999，v1 API）
或 `unacev2.dll`（2002，v2 API），`WINCV.IMG` 裡只有綁定層。這裡照
Marcel Lemke 1998 年的〈Technical information of the archiver ACE v1.2〉
自己寫，拿 BSD 授權的 [acefile](https://github.com/droe/acefile) 對答案，
測試資料用 [acefile-testdata](https://github.com/droe/acefile-testdata)。
支援 stored、ACE 1.0 的 LZ77、ACE 2.0 的 blocked（含 LZ77 / DELTA / EXE
三種子模式）；SOUND 與 PIC 兩種子模式、加密、跨片壓縮檔還沒做。

## 建置與驗收

```bash
tools/build-all.sh      # 三平台產物到 dist/
tools/verify-dist.sh    # 靜態驗收:檔案格式、macOS 逐弧簽章、動態庫依賴
tools/go.sh test ./...  # 測試(全部在 docker 裡跑)
```

原版當 oracle：

```bash
tools/setup-wine-oracle.sh                    # 解安裝檔 + 建 Wine prefix
tools/oracle-measure.sh docs/ui/oracle.png    # 量視窗幾何、截圖、印出選中的字型
```

不開視窗檢查重製版的畫面：

```bash
tools/go.sh run ./cmd/celldump -app <目錄> -keys "F1" -o shot.png -cols 93 -rows 30
```

`-keys` 的寫法與 `docs/ui/keymap.md` 的按鍵欄一致（`Ctrl-O`、`Alt-Z`、`F6`、`Down`…）。

## 移植成果

![markdown 檢視模式](docs/ui/showcase-markdown.png)

上面這張是重製版**自己畫出來的**——文件是 `docs/demo/showcase.md`，
裡面的字模圖集（PNG）與 29 色色票（SVG）不是貼上去的，
是 markdown 檢視模式把 `![...](...)` 解出來、光柵化、再嵌進字元格點裡。
重跑：

```bash
tools/go.sh run ./cmd/celldump -app docs/demo -cols 104 -rows 56 \
    -keys "Down,Down,Down,Enter" -o docs/ui/showcase-markdown.png
```

同一份 `render.Rasterizer` 也是 Ebiten 視窗用的那一份，
所以這張 PNG 與視窗裡看到的是同一批像素。

### 檔案瀏覽器（與原版逐格比對過）

| 重製版 | 原版（Wine） |
|---|---|
| ![重製版主畫面](docs/ui/shot-main.png) | ![原版主畫面](original/ref-shots/main-cjk.png) |

檔案清單那 16 列與原版的**屬性差是 0**，狀態列第二列連字模都完全相同。
量測方式與數字記在 [`docs/ui/main-screen.md`](docs/ui/main-screen.md)。

## 重製版多做的東西

原版沒有、但這一版有的：

| 功能 | 說明 |
|---|---|
| markdown 檢視 | 開 `.md` 直接排版：標題、清單、表格、引言、程式碼區塊，**圖片嵌在文件裡顯示**（PNG / GIF / SVG 都可以），`Enter` 把圖放大 |
| SVG | 用 oksvg 光柵化。原版是 2011 年的軟體，那時 SVG 還不流行 |
| UTF-8 後備字型 | 倚天沒有的字改用系統 TrueType 補 |
| 字級與倍率 | `Ctrl-+`/`Ctrl--` 換點陣字級，`Alt-+`/`Alt--` 整數倍放大 |
| 視窗大小 | `F1` → 視窗大小，有原版版面 93×21 等檔位，也可以自訂欄列數 |

markdown 裡的圖片只認**文件所在目錄底下**的相對路徑，而且不下載遠端圖片——
看一份文件不該變成連外行為，也不該讓一份來路不明的 `.md` 讀到別處的檔案。

## 署名

原作是 Lcc Wizard（林健總）的 WinCV 0.52，最後更新 2011-11-24。
這個 repo 是重製，署名的是重製這件事：

> Remake by 王俊又 —— 為保存台灣中文軟體盡一份心力

程式裡按 `F1` → 關於 可以看到同樣的內容。

### Android

![Android 實機畫面](docs/ui/android-emulator.png)

這張是 Android 14 模擬器（Pixel 5 profile，1080×2340）上的真實截圖。
畫面上看得到的：目錄列表、`剩餘: 5,114MB / 5,939MB` 的容量、
底部兩列觸控功能列，以及訊息列講「現在用的是系統字型 `DroidSansMono.ttf`」
——APK 裡不放原版的 `cvga.fon`（第三方版權物），使用者自己放進
`wincv/` 才會換成點陣像素對齊的版本。

`tools/build-android.sh` 建 APK（四個 ABI，minSdk 21），
`tools/run-android-emulator.sh` 把它裝進模擬器跑起來、截圖、收 logcat。
**還沒在實機上跑過，觸控輸入也還沒實測**——截圖證明畫得出來、讀得到檔案，
不證明點下去會動。

功能列隨模式換內容（瀏覽時是拷貝／移動／更名，讀文件時是尋找／編碼／中英），
底下那排導覽的**格位**固定、標籤隨模式換——位置固定是為了讓拇指記得住，
但讀文件時擺一個「標記」按鈕是按了沒反應的按鈕，那比位置變動更糟。

搬上 Android 真正花時間的不是 `internal/` 底下那些套件（它們一行都沒改就編過了），
是三個藏在 Ebiten 的 Android 那層裡的行為：gomobile 的原生層要自己餵
`Context`、Activity 被重建一次就等於 app 結束、`Layout` 不能把收到的尺寸
原樣傳回去。三條的成因與症狀都寫在
[`docs/plan/android.md`](docs/plan/android.md)。

## 規劃中

- [Android 版評估與規劃](docs/plan/android.md)——第一版**只做唯讀瀏覽**。
  私人 sideload 走「所有檔案存取權」，`vfs.OS` 直接可用；
  SAF 是上架 Play 才需要的替代路徑。

## 文件

- `CLAUDE.md` — 目標軟體的已查證事實、逆向方法、架構、硬規則
- `CONTEXT.md` — 統一語言、已被推翻的斷言、決策紀錄
- `docs/ui/main-screen.md` — 原版主畫面的格點量測
- `docs/ui/keymap.md` — 按鍵表，證據分三級
- `docs/formats/` — 資料檔格式規格
- `docs/re/` — 符號表、word 清單
