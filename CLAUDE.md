# WinCV Remake

把 1999–2011 年的台灣共享軟體 **WinCV 0.52 (CView for Windows)** 逆向並以 Go + Ebiten
乾淨重寫,目標 Linux / Windows / macOS 三平台,介面做到與原版點陣像素對齊,
檔案系統等周邊功能做等價實作。

原作者 Lcc Wizard(林健總),`lccw@ms8.hinet.net`,原站 `cview.com.tw`。
本專案為**私人使用**,repo 私有,不對外散布;原版素材與衍生資料檔一律不進公開通道。

---

## 1. 目標軟體:已查證事實

下表每一列都是實測結果,不是推測。工具與重跑方式見 §8。

| 項目 | 事實 |
|---|---|
| `wincv052a.exe` | Inno Setup **5.3.8** 安裝檔(非程式本體)。PE32 GUI,8 sections,section 資料僅約 54 KB,其餘 5.8 MB 是 overlay |
| 解出內容 | 62 個檔,全部落在 `app/`。`innoextract` 可完整解開 |
| 版本 | WinCV **0.52**,`whatsnew.txt` 最後一筆 2011-11-24;`WINCV.IMG` 時間戳 2011-11-25 |
| `wincv.exe` (52 KB) | **Win32Forth v4 STC kernel loader**。7 sections,`.text` 僅 0x7FED bytes。import 只有 kernel32 / user32 / gdi32 / comdlg32 / advapi32 |
| `WINCV.IMG` (1.52 MB) | **應用程式本體**,Forth dictionary image。內含字串 `#Win32forth v4stc 0.1d by Lcc Wizard` |
| 執行方式 | `wincv.exe` 找不到內嵌 resource 就載入同名 `.IMG`(錯誤訊息 `FindResource: Failed to find the Win32Forth resource or .IMG file`) |
| 程式碼形態 | **subroutine-threaded code (STC)**,真 x86 指令,可反組譯 |
| word 數 | image 內 **3663** 個 code body,其中 **3633** 個帶標準 STC 序言 |
| 符號表 | **未被 strip**。header space 自 `0x122794` 起,解出 **9497 筆 header / 8957 個唯一名稱**,含 xt 位址;3509 筆 xt 直接命中 code body |
| 原版可執行性 | Wine 9.0 + Xvfb 可跑,視窗標題 `WinCV 0.52`,可截圖當 oracle(前提見 §7 雷 1) |
| 點陣字型 | `cvga.fon` / `CVGA1018.FON` / `cvga1224.FON` / `WinCV.fon`,皆 NE FNT 2.0,字元範圍 **只有 0x00–0xFF**,pixel height 15 / 18 / 24 / 15 |
| CJK 字形來源 | 不在隨附字型內 → 原版全形中文由 Windows GDI 用系統字型繪製 |
| 附帶資料檔 | 英漢字典 `eng.txt` (5.5 MB) + `.dat`/`.idx`、`chi.txt.*`、KK 音標 `kk.txt.*`、`origin-verb.txt.*`、big5↔gbk/sjis/kor 對照表、`keyword_*.cfg` 語法上色、`ce.ful` 符號表、`default.fil` 書籤 |
| 解壓縮 | 原版外掛 Windows DLL:`unrar.dll` `unlha32.dll` `unarj32j.dll` `unacev2.dll` `tar32.dll` `CAB32.DLL` `7-zip32.dll` `bszip.dll` `aunzip32.dll` `libbz2` |
| 其他外掛 | `FreeImage.dll`(看圖)、`ijl15.dll`(Intel JPEG)、`cropdll.dll`、`md5.dll` |

功能面(取自 `file_id.diz`,原文為作者所寫,此處只列功能不照抄文案):
文字檔瀏覽、檔案與壓縮檔管理、看圖與縮圖列表、PE2 式區塊文字編輯器、HEX 編輯器、
Big5/GB/SJIS/KOR/Unicode 互轉(含檔名批次轉碼)、ANSI 彩色控制碼顯示、UNIX↔PC 換行轉換、
HTML/ANSI 碼去除、英漢字典與 KK 音標查詢、MD5 與 SFV(CRC32)檢驗。

---

## 2. 執行時架構

```
wincv.exe  (Win32Forth v4 STC kernel, 32 KB text)
   │  啟動 → 找不到內嵌 resource → 載入 WINCV.IMG
   ▼
WINCV.IMG  (1.52 MB)
   ├─ 0x000000            image header(10 個 dword,見下)
   ├─ 0x000000–0x12334c   code space:3663 個 STC word body
   ├─ 0x122794–0x186618   header space:9497 筆 word header(名稱 + xt)
   └─ 執行期再向上延伸(觀察到 xt 值超過 image 長度,如 0x1f3cc0)
   │
   ▼  Forth words 動態 LoadLibrary/GetProcAddress
Win32 API (user32/gdi32/…) + unrar.dll / FreeImage.dll / …
```

image header 實測值:

| offset | 值 | 判讀 |
|---|---|---|
| 0x00 | `0x00000000` | — |
| 0x04 | `0x019211d5` | 疑似 magic / checksum(**假設待驗**) |
| 0x08 | `0x0012334c` | code space 結束 |
| 0x0c | `0x00122794` | **header space 起點**(已用來成功走訪 9497 筆) |
| 0x10 / 0x14 | `0x00063e85` / `0x00063e84` | header space 大小 |
| 0x20 | `0x0040c158` | app base hint(**假設待驗**) |
| 0x24 | `0x00400000` | image base |

---

## 3. 逆向方法

### 3.1 STC 呼叫慣例(已由指令序列推導)

| 暫存器 | 角色 | 證據 |
|---|---|---|
| `ESP` | 資料堆疊(第二層以下) | `53` push ebx 存舊 TOS |
| `EBX` | **TOS cache**(堆疊頂端) | `53 bb 40 00 00 00` = push ebx; mov ebx,0x40 → 推入字面值 |
| `EBP` | **回傳堆疊指標**,向下成長 | 序言 `83 ed 04 8f 45 00` = sub ebp,4; pop [ebp] |
| `EDI` | 資料區基底 | `8b 9f 70 41 00 00` = mov ebx,[edi+0x4170];`89 9f 84 95 01 00` = mov [edi+0x19584],ebx。**指向 image base 或 user area 尚待確認** |

word 版面:

```
addr+0 : dword = addr+4          ← code field,指向自己的下一個位元組
addr+4 : 83 ED 04 8F 45 00       ← 序言(把 x86 call 壓的返回位址搬到回傳堆疊)
         …本體…
         83 C5 04 FF 65 FC       ← EXIT(add ebp,4; jmp [ebp-4])
```

「dword 值等於自身位址 +4」就是掃 word 邊界的判準,全 image 命中 3663 次。

### 3.2 符號表

`tools/forth_image.py` 已可完整走訪。header record 版面:

```
FF FF FF FF | 00 padding | name chars | count byte | dword seq | dword f2 | dword xt
```

`count` 在名稱**之後**(不是前綴),對齊靠前方 padding。`f2` 欄位語意未確認(**假設待驗**)。

產物:
- `docs/re/symbols.tsv` — seq / xt / 是否命中 code body / 名稱
- `docs/re/words.tsv` — 全部 code body 位址

名稱帶應用層語意,例如 `BIG5-SEARCH` `BIG5-GETWORD` `DICTWIN` `KKPHONE` `VP-JPG-TRANSFORM`
`MARK-START` `MARK-END` `ED-LINE` `FIND-FIRST-FILE`。前綴 `VF-` / `EF-` / `VP-` 疑似對應
view-file / edit-file / view-picture 三個模式(**假設待驗**)。

### 3.3 IDA Pro 的角色

IDA 反組譯的對象是 **`WINCV.IMG`,不是 `wincv.exe`**。反 `wincv.exe` 只會得到 Forth VM,
沒有任何應用邏輯。

作法:
1. 以 flat binary 載入 `WINCV.IMG`,**base 設 0**(image 內部位址就是 image-relative,
   已由「dword == addr+4」全數命中佐證)。
2. IDAPython 讀 `docs/re/words.tsv`,對每個 `addr+4` 下 `MakeFunction`。
3. IDAPython 讀 `docs/re/symbols.tsv`,對 xt 命中 code body 者套用名稱。
4. 把 `[edi+X]` 形式的存取標記成具名資料(需先確認 EDI 語意)。

環境見 `~/.claude/knowledge-base/retro/ida-pro-9.4.md`。該檔記載本機 IDA image 位置,
以及 IDAPython 必須用 `ida-pro-9.4-idapython:py312-v1` 這個 tag(基底 image 是靜默失敗、
exit code 不可信)。

### 3.4 反編當 oracle,不照抄

沿用 `~/.claude/knowledge-base/retro-cht/retro-game-remake/SKILL.md` 的母方法論:
反組譯結果只當**行為真值**用來抽演算法與版面常數,Go 端手寫乾淨模組,
不逐字翻譯 Forth。原版執行檔與資料檔不進 remake 的散布物。

---

## 4. Remake 架構

### 4.1 分層

```
cmd/wincv/                 進入點、命令列參數(原版有 /e 進編輯器)
internal/
  cell/     Screen:cols×rows 的 Cell{Rune, FG, BG, Attr};全形佔兩格
  font/     NE .FON (FNT 2.0) 解析 → 半形 glyph atlas;CJK 點陣字來源
  render/   Ebiten 實作:把 cell buffer blit 上畫面(整數倍縮放,不做濾波)
  input/    鍵盤事件 → WinCV keymap
  vfs/      檔案系統抽象:真實目錄 / 壓縮檔內部 走同一介面
  browser/  M1 檔案列表:游標、標記、排序、欄位、狀態列
  viewer/   M2 文字瀏覽:編碼判讀、ANSI 色碼、搜尋
  archive/  M3 壓縮檔讀取
  encoding/ Big5 / GBK / SJIS / KOR / UTF 互轉
  editor/   M4 PE2 式區塊編輯器
  imgview/  M4 看圖與縮圖
  hexed/    M4 HEX 編輯器
  dict/     M4 英漢字典 + KK 音標(.dat/.idx)
  hash/     M4 MD5 / SFV
```

### 4.2 兩個必須做深的模組(`rulebook/70-deep-modules.md`)

**`cell`** — 整個 UI 的唯一渲染介面。上層只寫字元與屬性,不碰像素。
換字型、換縮放、換 backend 都不影響上層。

**`vfs`** — 「壓縮檔當目錄瀏覽」是 CView 的核心手感。目錄與壓縮檔內部必須是同一個介面,
否則 browser 會被兩套路徑分岔汙染。

### 4.3 壓縮格式:原版 DLL → Go 的對應

原版靠 Windows DLL,remake 要純 Go(避免 CGO 破壞跨平台)。可行性分級:

| 格式 | 原版 | Go 方案 | 風險 |
|---|---|---|---|
| ZIP | `aunzip32.dll` / `bszip.dll` | `archive/zip` | 低 |
| GZ / TAR / BZ2 | `tar32.dll` / `libbz2` | stdlib `compress/*` `archive/tar` | 低 |
| RAR | `unrar.dll` | `github.com/nwaples/rardecode` | 中(RAR5 支援度需實測) |
| 7Z | `7-zip32.dll` | `github.com/bodgit/sevenzip` | 中 |
| LZH | `unlha32.dll` | Go 實作稀少,可能自寫 | 高 |
| ARJ | `unarj32j.dll` | 無成熟 Go 實作,需自寫 | 高 |
| ACE | `unacev2.dll` | 無 Go 實作;格式封閉 | 高,可能落在最後 |
| CAB | `CAB32.DLL` | `github.com/…/cab` 或自寫 MSZIP | 中 |

M3 先做 ZIP/TAR/GZ/BZ2 四種,其餘依 §7 完整性原則逐一補完,不因冷門而砍。

### 4.4 CJK 點陣字形

原版半形用自帶 `.FON`,全形靠 Windows 系統字型。remake 要 pixel 對齊就必須自備 CJK 點陣字。
候選與選型依據見 `~/.claude/knowledge-base/retro-cht/eten-bitmap-font/SKILL.md`
(該檔的結論是老軟體畫面預設用倚天點陣字,不是 TTF rasterize)。

24px 檔位對應倚天 24×24;15/18px 檔位需要 16×16 級 CJK 點陣字。
**實際比對前不預設答案**:Phase 3 要先在 Wine 裝好中文字型、截原版中文畫面,再決定字形來源。

---

## 5. 驗收

`rulebook/65`:驗收一律對 reference 實測,不用內部訊號。測試綠不等於做完。

### 5.1 oracle 管線(已可用)

```
tools/setup-wine-oracle.sh          # 解安裝檔 + 建 Wine prefix
tools/oracle-shot.sh <out.png> <等待秒數> "<xdotool 按鍵序列>"
```

輸出 1024×768 PNG。已實測可取得主畫面。

### 5.2 比對方式

整張 pixel diff 難定位,改用**格點比對**:兩張圖依 cell grid 切格,逐格比對。
先做 `tools/celldiff.py`,輸出「第幾列第幾行不同」而不是「差了幾個像素」。

三級驗收:

1. **版面等價** — 欄位位置、寬度、分隔線、捲軸、狀態列文字位置與原版同格點。
2. **屬性等價** — 每格前景/背景色與原版相同(色盤先從原版截圖抽出成 16 色表)。
3. **像素等價** — glyph 位元完全相同。這一級只在字形來源定案後才要求。

### 5.3 行為等價

- 同一個測試目錄,原版與 remake 的排序結果、欄位格式化(檔名/副檔名分欄、大小、日期)逐字相同。
- 快捷鍵:對照表逐一實測,不靠讀說明檔推斷(說明檔會漏、會過期)。
- 編碼轉換:以原版隨附的對照表當 golden data 做 round-trip 測試。

---

## 6. 里程碑

| 階段 | 內容 | 完成判準 |
|---|---|---|
| **P0** 環境 | Wine oracle 可跑(**已完成**)、中文字型裝進 prefix、IDA 載入 IMG 並套符號 | 能截出中文正常顯示的原版主畫面 |
| **P1** 圖譜 | 掃 call 目標建 word 呼叫圖;定位主迴圈、事件分派、畫面重繪三個入口 | `docs/re/callgraph.md` 標出三個入口的 word 名稱 |
| **P2** 格式 | `.FON`、`eng.txt.dat/.idx`、`keyword_*.cfg`、`ce.ful`、`default.fil`、設定檔 | `docs/formats/*.md` 各附一支可 round-trip 的解析器 |
| **P3** 版面 | 量測主畫面格點:cols×rows、欄位邊界、色盤 | `docs/ui/main-screen.md` + 標註圖 |
| **P4** keymap | 逐鍵實測原版行為 | `docs/ui/keymap.md` |
| **M1** | 檔案瀏覽器主畫面:目錄列表、游標、標記、排序、進出目錄 | 對 P3 截圖格點比對通過 |
| **M2** | 文字瀏覽器:編碼判讀、ANSI 色碼、搜尋 | 同上 + 大檔捲動不卡 |
| **M3** | 壓縮檔當目錄瀏覽 | ZIP/TAR/GZ/BZ2 起步,逐格式補完 |
| **M4** | 編輯器 / HEX / 看圖 / 縮圖 / 轉碼 / 字典 / MD5-SFV | 逐項對原版比對 |
| **M5** | 三平台打包 | 見 `rulebook/82`,每個平台驗**打包產物**在**它自己的環境** |

---

## 7. 硬規則與已踩的雷

### [HARD] 完整性優先於投報

`rulebook/83-retro-completeness-over-roi.md`。冷門功能(ACE 解壓、KOA/PCD 圖檔、
KK 音標)不因使用率低而砍。這類軟體的價值就在保全當年的完整行為。
要砍要先講,並記在 `CONTEXT.md`。

### [HARD] Wine prefix 一定放 `$HOME`

放 `/tmp` 底下 `winex11` driver 會載入失敗,錯誤是
`nodrv_CreateWindow ... no driver could be loaded`,看起來像缺 X server 或缺 i386 函式庫,
但 `xdpyinfo` 正常、`libx11-6:i386` 與 `wine32:i386` 都在。
換成 `$HOME/.wine-wincv` 後直接可跑。(2026-08-20 實測;`rulebook/82` 已記載此雷)

### [HARD] 這台機器 `/tmp` 有大量陳舊 X lock

`/tmp/.X9*-lock` `/tmp/.X1??-lock` 是別的 session 留下的。**不要刪**。
起 Xvfb 一律用 `xvfb-run -a` 自動挑 display,不要指定固定號碼。

### [HARD] docker / 環境紀律

- 編譯與 Python 工具一律走 docker,`docker run` 帶 `--rm --log-opt max-size=10m --log-opt max-file=3`。
- 禁止任何 `docker image/volume/system/builder prune` 與 `docker rmi`。這台機器有其他客戶專案的 image。
- 不動 `~/.cache/`。

### [HARD] 不要把猜測寫成結論

`docs/` 裡的斷言分兩級:**已驗證**(附重跑指令或 offset)與 **假設待驗**。
兩者不可混寫。斷言被推翻時,正文直接改成正確答案,推翻紀錄集中記到
`CONTEXT.md` 的「已被推翻的斷言」表,正文不留檢討敘述(`rulebook/63`)。

### 慢迴圈紀律

Wine 截圖一輪約 20–30 秒。要連續比對多個畫面時,先把按鍵序列一次排好批次跑,
不要「改一個鍵再跑一遍」。跑之前先算「N 個畫面 × 每個約 30 秒 ≈ 多久」並講出來。

---

## 8. 目錄與常用指令

```
CLAUDE.md                本檔
CONTEXT.md               glossary + 已被推翻的斷言表
wincv052a.exe            原版安裝檔(唯一真相,其餘皆可由它重建)
original/app/            解出的原版素材(不進版控)
original/ref-shots/      原版 oracle 截圖
tools/
  forth_image.py         WINCV.IMG 解析(header / symbols / words)
  setup-wine-oracle.sh   解安裝檔 + 建 Wine prefix
  oracle-shot.sh         跑原版並截圖
  xwd2png.py             XWD → PNG(本機無 ImageMagick,ffmpeg 不吃 xwd)
docs/
  re/                    逆向產出(symbols.tsv / words.tsv / callgraph.md)
  formats/               資料檔格式規格
  ui/                    版面與 keymap 規格
cmd/ internal/           Go 實作
```

```bash
# 重建原版素材與 Wine prefix
tools/setup-wine-oracle.sh

# 重抽符號表
python3 tools/forth_image.py symbols original/app/WINCV.IMG > docs/re/symbols.tsv
python3 tools/forth_image.py words   original/app/WINCV.IMG > docs/re/words.tsv

# 截原版畫面
tools/oracle-shot.sh original/ref-shots/main.png 18
tools/oracle-shot.sh original/ref-shots/view.png 18 "Down Down Return"
```

---

## 9. 待驗假設清單

動手用到哪一條,先驗那一條,驗完把它搬進 §1 並註明證據。

| # | 假設 | 怎麼驗 |
|---|---|---|
| A1 | `EDI` 是 image base(而非 user area 指標) | 在 IDA 裡取幾個 `[edi+X]`,對照 X 是否落在 image 的資料區間 |
| A2 | header record 的 `f2` 欄位是 vocabulary / hash link | 統計 `f2` 值的分布;看同名不同 vocabulary 的 word 是否 `f2` 不同 |
| A3 | image header 0x04 是 magic / checksum | 改一個 byte 再跑,看 kernel 是否拒載 |
| A4 | `VF-` / `EF-` / `VP-` 前綴對應 view-file / edit-file / view-picture | 反組譯任一個該前綴的 word,看它碰的資料與畫面 |
| A5 | 主畫面是固定格點的自繪 cell grid(非 Win32 控制項) | 用 Wine 的 spy 或改視窗大小截圖,看文字是否只落在整格位置 |
| A6 | 全形中文由系統字型繪、非自帶字型 | prefix 裝上中文字型後截圖,對比未裝字型的亂碼畫面 |
| A7 | 原版設定存在 `%windir%\wincv.cfg` | `WinCVins.bat` 有 `>> %windir%\wincv.cfg` 的註解行;跑一次原版看檔案是否生成 |

---

## 10. 相關規則與知識庫

命中時先 Read 再動手:

- `~/.claude/knowledge-base/retro-cht/retro-game-remake/SKILL.md` — 逆向 + 乾淨重寫母方法論
- `~/.claude/knowledge-base/retro-cht/eten-bitmap-font/SKILL.md` — 倚天點陣字與 CJK 字形來源
- `~/.claude/knowledge-base/retro-cht/cjk-package-encoding/SKILL.md` — 交付包的檔名與文字編碼
- `~/.claude/knowledge-base/retro/ida-pro-9.4.md` — 本機 IDA 環境與 IDAPython image tag
- `~/.claude/rulebook/62-static-provenance-trace.md` — 「這個值從哪來」靜態反追溯源
- `~/.claude/rulebook/63-truth-in-code-not-stale-markers.md` — 寫進既有文件時的紀律
- `~/.claude/rulebook/65-verify-against-reference-not-internal-signals.md` — 驗收對 reference
- `~/.claude/rulebook/70-deep-modules.md` — 介面設計
- `~/.claude/rulebook/82-cross-platform-port-verification.md` — 跨平台打包驗證
- `~/.claude/rulebook/83-retro-completeness-over-roi.md` — 完整性優先
