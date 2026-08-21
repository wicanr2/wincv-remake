# WinCV Remake

把 1999–2011 年的台灣共享軟體 **WinCV 0.52 (CView for Windows)** 逆向並以 Go + Ebiten
乾淨重寫,目標 Linux / Windows / macOS / Android 四平台,介面做到與原版點陣像素對齊,
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
| 點陣字型 | NE FNT 2.0,全部定寬、涵蓋 0x00–0xFF:`cvga` **8×15**、`cvga1018` **10×18**、`cvga1224` **12×24**(`WinCV.fon` 與 `cvga.fon` 同規格)。ascent 11 / 16 / 20 |
| 字型註冊方式 | 程式以 `AddFontResource` 註冊自己的 `.FON`,再用 `CreateFontIndirect` 指名 face。Wine log 實測有 `Chosen: L"cvga Regular" (C:\wincv\wincv.fon)` |
| CJK 字形來源 | 不在隨附字型內。image 裡指名 **`新細明體`** → 原版全形中文由 Windows GDI 用系統字型繪製,字形隨使用者的 Windows 而異 |
| 全形格點 | 半形 8×15 → 全形 **16×15**。倚天 `STDFONT.15` 正好是 16×15、`ASCFONT.15` 正好是 8×15;`cvga1224` 12×24 對應倚天 `STD.24x` 24×24。同一個年代的規格 |
| 配色 | 語法設定檔用 **29 個具名顏色**,不是 16 色。名稱與順序取自 image 0x5692d 的斜線分隔清單(counted string,長度 227):black/dkgray/red/ltred/green/ltgreen/blue/ltblue/yellow/mildyellow/ltyellow/magenta/ltmagenta/cyan/ltcyan/gray/white/ltgray/purple/ltpurple/orange/ltorange/gooseyellow/bluegreen/inkgreen/mildwhite/mildgreen/mildcyan/mildmagenta。`keyword_*.cfg` 就是用這些名字指定語法上色。image 裡其實有 **46 個色彩 word**,那 29 個只是設定檔用得到的子集;檔案清單的副檔名配色用的是不在清單上的 `DIR-*` 系列。**RGB 值也在 image 裡**:每個顏色是一個 Forth word,body 有 0x24 個位元組,第 8-10 個就是 R、G、B(Win32 的 COLORREF 是 `0x00BBGGRR`,小端存放後記憶體順序正好是 R G B);word 的 xt 在標頭裡「名字結尾 +9」的那個 dword。抽取程式 `tools/palette.py`,結果在 `internal/render/raster.go` 的 `DefaultPalette` |
| Big5 字串表 | image 內的 UI 文字是 Forth counted string。加上「前一個 byte 等於長度」這道檢查後,15761 個候選收斂到 1293 個真字串;長度 ≥8 的有 845 筆,含選單、對話框、快捷鍵說明 |
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

已經包成 `tools/ida.sh` + `tools/ida/*.py`:

```bash
tools/ida.sh load_wincv.py /work/out/load.json    # 建庫、種函式邊界、套名字
```

實測結果:種下 2392 個函式邊界後 IDA 認出 **3253 個函式**,套上 **6837 個名字**
(1606 個 xt 落在 image 之外,那些是執行期才配的,跳過)。

**這一段有三個會產生「自洽但錯」結果的坑,都踩過:**

1. `set_segm_addressing(seg, bitness)` 的 bitness 是 **0=16 / 1=32 / 2=64**。
   填 2 會把 32 位元的碼當 64 位元解,而反組譯**看起來仍然是合理的指令**
   (只是暫存器變成 `rdi`/`rax`),不會有任何錯誤訊息。
2. 腳本語法錯誤的症狀是**一行都沒跑**:零輸出、零訊息、沒有 traceback,
   與「IDAPython 在這個環境不能用」長得一模一樣。所以 `tools/ida.sh`
   會先 `ast.parse` 再燒一次 IDA,而每支腳本第一行就寫痕跡檔。
3. **不要 grep 反組譯文字**去找 `[edi+X]`:名字套上去之後位移可能被符號化,
   而且那是二手資料。用 `ida_ua.decode_insn` 讀運算元型別與基底暫存器。
   (同一個查詢,grep 文字找到 1 處,解碼找到 8703 處。)

### 3.4 反編當 oracle,不照抄

沿用 `~/.claude/knowledge-base/retro-cht/retro-game-remake/SKILL.md` 的母方法論:
反組譯結果只當**行為真值**用來抽演算法與版面常數,Go 端手寫乾淨模組,
不逐字翻譯 Forth。原版執行檔與資料檔不進 remake 的散布物。

---

## 4. Remake 架構

### 4.1 分層

```
cmd/
  wincv/      Ebiten 進入點(桌面)
  celldump/   同一份 app,不開視窗直接出 PNG(-app + -keys 可送按鍵)
mobile/       Android 進入點(ebitenmobile bind 的對象)+ 觸控事件翻按鍵
android/      Gradle 專案:MainActivity 與 manifest
internal/
  cell/       Screen:cols×rows 的 Cell{Ch, FG, BG, Wide, Cont};全形佔兩格
  fnt/        NE .FON(FNT 2.0)解析 → 半形字模
  eten/       倚天字庫(STDFONT/SPCFONT)→ 全形字模
  render/     純 CPU 光柵器:cell buffer → image.RGBA(不依賴 Ebiten)
  keys/       與後端無關的按鍵表示 + 文字寫法的解析
  vfs/        檔案系統抽象:真實目錄與壓縮檔內部走同一介面
  browser/    檔案列表:游標、標記、排序、欄位、狀態列、註解
  viewer/     文字檢視:編碼判讀、ANSI 色碼、換行、搜尋
  hexview/    16 進位檢視
  editor/     PE2 式區塊編輯器(矩形/整列區塊、虛擬空白、undo)
  syntax/     keyword_*.cfg 語法上色
  imgview/    看圖   thumbs/ 縮圖列表   imgfmt/ 圖檔解碼
  dict/       英漢字典 + KK 音標(.dat/.idx)
  textenc/    編碼判讀   convert/ 換行與編碼轉換、去 HTML/ANSI
  fileop/     拷貝 / 移動 / 改名 / 刪除 / 比對
  search/     尋找 檔名 / 字串 / 註解      note/ dir.doc 註解讀寫
  checksum/   MD5 / SFV      launch/ 跨平台開啟與執行
  archive/    壓縮檔讀取(見 §4.3)
    lzh/ arj/ cab/ arc/ zcompress/   自寫的解碼器
  app/        把上面全部接起來:模式切換、按鍵分派、選單、輸入列
```

`app` 這一層不依賴 Ebiten,所以整個互動流程可以 headless 測,
`cmd/celldump -app -keys` 也走同一條路徑。


### 4.2 兩個必須做深的模組(`rulebook/70-deep-modules.md`)

**`cell`** — 整個 UI 的唯一渲染介面。上層只寫字元與屬性,不碰像素。
換字型、換縮放、換 backend 都不影響上層。

**`vfs`** — 「壓縮檔當目錄瀏覽」是 CView 的核心手感。目錄與壓縮檔內部必須是同一個介面,
否則 browser 會被兩套路徑分岔汙染。

### 4.3 壓縮格式:原版 DLL → Go 的對應

原版靠 Windows DLL,remake 要純 Go(避免 CGO 破壞跨平台)。可行性分級:

| 格式 | 原版 | remake | 驗證方式 |
|---|---|---|---|
| ZIP | `aunzip32.dll` / `bszip.dll` | `archive/zip` | stdlib |
| GZ / TAR / BZ2 | `tar32.dll` / `libbz2` | stdlib `compress/*` `archive/tar` | stdlib |
| RAR | `unrar.dll` | `nwaples/rardecode/v2` | 該套件 |
| 7Z | `7-zip32.dll` | `bodgit/sevenzip` | 該套件 |
| LZH | `unlha32.dll` | `internal/archive/lzh`(自寫) | 對 p7zip 比 sha256,675 個成員 |
| ARJ | `unarj32j.dll` | `internal/archive/arj`(自寫) | 對 arj 3.10 產生的檔案 |
| CAB | `CAB32.DLL` | `internal/archive/cab`(自寫) | 對 gcab / cabextract |
| .Z | — | `internal/archive/zcompress`(自寫) | 對 ncompress |
| ARC / PAK | 外掛 | `internal/archive/arc`(自寫) | 對 arc 5.21 |
| ACE | `unace.dll` / `unacev2.dll` | `internal/archive/ace`(自寫) | 對 acefile 比 sha256,269 個成員 |

**ACE 在 image 裡沒有演算法可逆向。** 原版自己不解 ACE,是把整包丟給
WinACE 原廠的免費解壓元件,而且支援兩代 API:`unace.dll`(1999,匯出
`ACEOpenArchive` / `ACEReadHeader` / `ACEProcessFile` / `ACECloseArchive` /
`ACESetPassword`)與 `unacev2.dll`(2002,匯出 `ACEInitDll` / `ACEList` /
`ACEExtract` / `ACETest` / `ACEReadArchiveData`)。image 裡對應的是**綁定層**
而不是解碼器 —— Forth 原始檔名 `unace10.f` / `unace20.f`,word 群 `UNACE10`
與 `ACE2-DLL`,連 API 的結構都照建成 word(`ACEOPENARCHIVEDATA`、`ARCNAME`、
`OPENMODE`、`OPENRESULT`、`FLAGS`、`ACE_COMMENT_OK` / `_SMALLBUF` / `_NONE`)。

所以 ACE 和其他格式的差別不是「難」,是**沒有東西可以逆向**:演算法在一個
42 KB 的封閉 DLL 裡,格式也從來沒有公開規格。

**ACE 已經自己實作了**,材料是:

- 規格:Marcel Lemke 1998 年的〈Technical information of the archiver ACE v1.2〉
- 參考實作:[droe/acefile](https://github.com/droe/acefile),BSD 授權的純 Python,
  支援 ACE 1.0/2.0(含 EXE / DELTA / PIC / SOUND 模式與加密)
- 測試資料:[droe/acefile-testdata](https://github.com/droe/acefile-testdata),
  實測可解出 268 個成員,壓縮法分布是 type 2(LZ77)265 個、type 0(stored)3 個
- 額外的 oracle:原版隨附的 `unacev2.dll` 可以在 Wine 底下跑

已實作:stored、ACE 1.0 的 LZ77、ACE 2.0 的 blocked(含 LZ77 / LZ77_DELTA /
LZ77_EXE 三種子模式)。未實作:SOUND 與 PIC 兩種子模式、加密、跨片壓縮檔,
遇到會明確回報。

驗證:269 個成員全部通過標頭裡的 CRC-32,並與 acefile 的輸出逐位元組相同。
committed 的測試樣本只有 8.3 KB(走到 MODE_LZ77);EXE 與 DELTA 由
`TestFullCorpus` 對完整語料驗,設 `WINCV_ACE_CORPUS` 才會跑。

三個實作上的坑:

1. **位元序**:資料先以小端序每 4 個位元組讀成一個 uint32,再從那個
   uint32 裡由高位往低位取位元。當成單純的 MSB-first 位元流讀,
   每 4 個位元組就錯一次序。
2. **Huffman 表的建法依賴一個特定的不穩定 quicksort**。碼的指派順序
   取決於相等寬度的符號最後排成什麼次序,換成 `sort.Slice`
   (即使加 tie-break)會得到一套自洽但與編碼器對不上的碼 ——
   解出來是垃圾而不是錯誤。這段要逐行照抄。
3. **字典跨成員延續**。`reinit` 只重設符號讀取器與距離歷史,
   字典不歸零;壓縮檔後面的成員會回頭引用前面成員的內容。

另外有一條捷徑但沒走:Go 在 Windows 用 `syscall.NewLazyDLL` 就能載入
`unacev2.dll`,不需要 CGO,不破壞跨平台編譯。不走的理由是只有 Windows 版能用
(三平台等價就破了),而且那個 DLL 就是 CVE-2018-20250(WinRAR 路徑穿越)
的主角,原廠約 2005 年後不再維護、沒有修好的版本,隨附這個是 2002 年的組建。

**沒有 oracle 就不寫解碼器。** 這是 §7「完整性優先於投報」的界線:
完整性指的是「不因冷門而砍」,不是「沒驗過也塞一個進去」。
一個沒對照過參考實作的解碼器,會安靜地解出**看起來有內容但其實是錯的**檔案,
那比明講不支援更糟。同樣的判準適用於 CAB 的 LZX / Quantum、
ARC 的方法 4 與 7、LHA 的 `-lh1-`、ACE 的 SOUND / PIC、以及圖檔的 PCD。

自寫的四個解碼器都是先取得參考實作的原始碼或規格再寫,不是憑印象 ——
憑印象寫出來的會是「自洽但錯」的碼,而且**小檔案的測試會剛好通過**
(LHA 的 `nc` 少算一格、CAB 的 MSZIP 跨區塊字典,兩個都只在大檔上現形)。

### 4.4 CJK 點陣字形:倚天字庫(已定案並實作)

原版半形用自帶 `.FON`、全形靠 Windows 系統字型(image 裡指名「新細明體」)。
也就是說**「原版的中文字形」本來就不是單一固定答案**,它隨使用者的 Windows 而異。
remake 需要自備一套,選倚天(ETEN 3.53),依據見
`~/.claude/knowledge-base/retro-cht/eten-bitmap-font/SKILL.md`。

尺寸是決定性的:

| WinCV 半形 | 全形格 | 倚天對應 |
|---|---|---|
| `cvga` 8×15 | 16×15 | `STDFONT.15`(漢字 13094 字)+ `SPCFONT.15`(全形標點 408 字) |
| `cvga1224` 12×24 | 24×24 | `STD.24M/K/L/R/B/S`(六種字體,ETUNPACK 壓縮,尚未支援) |

字庫來源:`~/cht/etan_font/ET353S.iso`(倚天 3.53 光碟)。
`tools/setup-eten.sh` 會抽出需要的檔案放進 `original/eten/`。

實作在 `internal/eten`,Big5 分區索引公式照 kb(已實測驗證)。
`internal/render.CJKSource` 是介面,要換字形來源不動上層。

**[雷] 一定要一起載 `SPCFONT.15`。** `STDFONT.15` 從 A440「一」起,不含 A140–A3BF 的
全形標點;只載 STDFONT 的話 `，。！？「」（）《》` 會整批變缺字。
`internal/eten` 的 `TestPunctuationFromSpc` 就是擋這個。

**[雷] 符號補充區(Big5 C6A1–C8FE)不能用線性索引。** 那一區有 408 個碼位,
`SPCFSUPP.15` 卻只有 365 個字模 —— 字模是「把有定義的碼位擠在一起」存的,中間有洞。
線性索引會整批錯位,而錯位取到的是**另一個看起來正常的字**(實測:C6E7 應為「ゃ」,
線性索引取到的是別的字),比缺字難發現得多。在補齊那張洞表之前,這一區一律當缺字。
其餘三區(符號 408/408、常用 5401/5401、次常用 7693/7693)碼位與字模數剛好對齊,
線性索引成立。

---

## 5. 驗收

`rulebook/65`:驗收一律對 reference 實測,不用內部訊號。測試綠不等於做完。

### 5.1 oracle 管線(已可用)

```
tools/setup-wine-oracle.sh          # 解安裝檔 + 建 Wine prefix
tools/oracle-shot.sh <out.png> <等待秒數> "<xdotool 按鍵序列>"
```

輸出 1024×768 PNG。已實測可取得主畫面與中文選單。

**[重要] oracle 只能當版面與配色的真值,不能當字模真值。**
Wine 會把 app 要的字型換掉:實測它用自己的 `cvgasys.fon`(16 px)去畫,
而不是 `cvga`(15 px),量到的列距 16 就是這麼來的。
字模真值直接來自 `.FON` 檔本身(`internal/fnt` 已解出並與 `tools/fnt.py` 互為對照)。

### 5.2 比對方式

整張 pixel diff 難定位,改用**格點比對**:兩張圖依 cell grid 切格,逐格比對。
先做 `tools/celldiff.py`,輸出「第幾列第幾行不同」而不是「差了幾個像素」。
remake 這一側不需要開視窗:`cmd/celldump` 用與 Ebiten 相同的 `render.Rasterizer`
直接輸出 PNG,所以 headless 畫出來的跟視窗裡看到的是同一份像素。

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
| **M1** ✅ | 檔案瀏覽器主畫面:目錄列表、游標、標記、排序、進出目錄 | `internal/browser`,測試涵蓋排序/標記/捲動邊界 |
| **M2** ✅ | 文字瀏覽器:編碼判讀、ANSI 色碼、換行、搜尋 | `internal/viewer` + `internal/textenc` |
| **M3** ✅ | 壓縮檔當目錄瀏覽 | 12 種格式全支援(六種自寫;各自的未做子模式列在 `archive.Formats` 裡) |
| **M4** ✅ | 編輯器 / HEX / 看圖 / 縮圖 / 轉碼 / 字典 / MD5-SFV / 檔案操作 | 主畫面與編輯器的按鍵表都接完(見 `docs/ui/keymap.md`) |
| **M5** ✅ | 三平台打包 | `tools/build-all.sh` + `tools/verify-dist.sh` |
| **M6** ✅ | Android:APK 建得出來並在模擬器上跑起來 | `tools/build-android.sh` + `tools/verify-apk.sh`(格式)+ `tools/run-android-emulator.sh`(行為);規劃與實測見 `docs/plan/android.md` |

各格式的支援進度是**程式碼裡的表**,不是文件:
`archive.Formats` 與 `imgfmt.Formats`,各有一個測試盯著它別悄悄過期。

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

A1–A13 全部驗完,結論在 §1。新的假設寫進這裡,不要直接寫進 §1。

| # | 假設 | 怎麼驗 |
|---|---|---|
| A14 | 跨行註解的起訖標記是**依語言寫死在程式裡**的。`keyword_*.cfg` 沒有這一項,但 image 的符號表有 `COMMENTSTATE`、`END-COMMENT$`、`"CHECK-END-COMMENT`,表示原版有跨行註解的狀態機。remake 目前在 `LineComment` 是 `//` 時預設 C 家族的 `/* */`(`internal/syntax`) | 進 IDA 看那三個 word 的呼叫端怎麼取得標記;或在原版各開一個含跨行註解的 `.c` / `.pas` / `.asm`,看上色範圍到哪裡 |

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
