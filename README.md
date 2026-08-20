# WinCV Remake

把 1999–2011 年的台灣共享軟體 **WinCV 0.52（CView for Windows）** 逆向，
用 Go + Ebiten 重寫成 Linux / Windows / macOS 三個平台都能跑的版本，
介面與原版點陣像素對齊。原作者是 Lcc Wizard（林健總），原站 `cview.com.tw`。

WinCV 是一支在同一個畫面裡做完很多事的中文工具：瀏覽目錄、看文字檔、看圖與縮圖、
把壓縮檔當目錄走進去、編輯文字、看 HEX、在 Big5 / GB / SJIS / KOR / Unicode 之間轉碼、
查英漢字典與 KK 音標、算 MD5 與 SFV。它的最後一次更新停在 2011 年 11 月 24 日
（`whatsnew.txt` 的最後一筆），之後沒有 Linux 版，也沒有 macOS 版。

這個 repo 是私人使用，不對外散布，原版素材與由它衍生的資料檔都不進版控。

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
| 左側磁碟欄、捲軸 | 有 | 尚未實作 | 版面已量測，記在 `docs/ui/main-screen.md` |
| 副檔名配色 | 有（`.bat` 整列洋紅） | 尚未實作 | 原版可在設定裡改 |
| KK 音標 | 自訂編碼，一個位元組一個音素 | 轉成 IPA 顯示（`ˈlɪtl̩`、`ˈbʌtn̩`） | 對照表解在 `internal/dict/kk.go` |
| 字級 | 設定裡選三種點陣字型之一 | `Ctrl-+` / `Ctrl--` 即時切換同樣三種 | 24 點的全形漢字倚天光碟上是壓縮的，那一級的中文由 16×15 縮放 |
| 視窗 | 固定版面 | 放大視窗就多幾格 | 格子大小不變，多出來的空間變成更多欄列 |
| Big5 以外的字 | 交給 Windows 系統字型 | 倚天沒有的字改用系統 TrueType 補 | 简体字、韓文、希臘文、多數符號。真的缺字畫成空框，不留白 |
| 散布方式 | 共享軟體，作者對外發行 | 私人專案，不散布原版素材 | 字型、字典資料與倚天字庫都是第三方版權物，由使用者自備 |

倚天那一列決定了字怎麼排：`STDFONT.15` 的全形格正好是 16×15，
剛好等於 `cvga` 半形 8×15 的兩倍寬、同高，中文與英數落在同一套格點上，不必縮放或補白。

畫面配色沿用原版：29 個具名顏色的名稱與 RGB 值都是從 `WINCV.IMG` 裡解出來的，
不是重新配的一套色。

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

## 署名

原作是 Lcc Wizard（林健總）的 WinCV 0.52，最後更新 2011-11-24。
這個 repo 是重製，署名的是重製這件事：

> Remake by 王俊又 —— 為保存台灣中文軟體盡一份心力

程式裡按 `F1` → 關於 可以看到同樣的內容。

## 文件

- `CLAUDE.md` — 目標軟體的已查證事實、逆向方法、架構、硬規則
- `CONTEXT.md` — 統一語言、已被推翻的斷言、決策紀錄
- `docs/ui/main-screen.md` — 原版主畫面的格點量測
- `docs/ui/keymap.md` — 按鍵表，證據分三級
- `docs/formats/` — 資料檔格式規格
- `docs/re/` — 符號表、word 清單
