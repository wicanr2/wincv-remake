# Office 文件與 PDF 完整閱讀

把 Word、PowerPoint、Excel 加進來,並把 PDF 從「抽得到文字」推到「讀得完整」。

## 範圍

| 格式 | 內容 | 做法 |
|---|---|---|
| `.docx` | WordprocessingML | `internal/ooxml` + `internal/docx` |
| `.doc` | Word 97–2003 二進位 | `internal/cfb` + `internal/doc97` |
| `.rtf` | Rich Text Format | `internal/rtf` |
| `.pptx` | PresentationML,含版面配置繼承、表格、備忘稿、圖片 | `internal/ooxml` + `internal/pptx` |
| `.xlsx` | SpreadsheetML | `internal/ooxml` + `internal/xlsx` |
| `.pdf` | 先把取文字做完整,再做頁面渲染 | `internal/pdfdoc` 擴充 |

`.ppt`(PowerPoint 97–2003 二進位)與 `.xls`(BIFF8)不在這一輪。兩者都是另一套
記錄格式,和 `.doc` 共用 CFB 容器但上層完全不同。

## 為什麼不是「再寫一個檢視器」

現有的文件路徑只有一條:**解析 → `markdown.Block` → `internal/markdown` 排版 →
格點畫面**。`.md` 走 `markdown.Parse`,HTML 與 EPUB 走 `web.ParseHTML`,PDF 走
`pdfdoc` 的 `Pre` 區塊。新格式沿用同一條 —— 每個解析器只負責產出區塊,
斷行、顏色、圖片貼格、捲動、連結導覽全部是既有的。

所以每一包的介面都一樣窄:

```go
Open(path) (*Doc, error)
(*Doc) Parts() []string                  // 章節 / 投影片 / 工作表
(*Doc) Blocks(i int) []markdown.Block
(*Doc) Image(ref string) ([]byte, error)
```

`internal/officedoc` 把五種格式收在這個介面後面,`internal/app` 只認得
`officedoc`,不認得 docx 或 rtf。要加第六種格式時 app 一行都不用改。

## OOXML 的共用底層

`.docx` / `.pptx` / `.xlsx` 都是 OPC 包:ZIP + `[Content_Types].xml` +
每個組件旁邊的 `_rels/<名字>.rels`。三者共用的是**關聯解析**(`r:id` →
組件路徑)與**組件查表**,不是 XML 結構 —— 結構三者完全不同,不共用。

兩個會產生「自洽但錯」結果的地方,寫在 `internal/ooxml` 的註解裡:

1. **關聯目標是相對於「持有關聯的那個組件所在的目錄」**,不是相對於 ZIP 根。
   `ppt/slides/slide1.xml.rels` 裡的 `../media/image1.png` 要接成
   `ppt/media/image1.png`。用根目錄接會全部找不到圖,而找不到圖不會報錯,
   只會少一張。
2. **`mc:AlternateContent` 的 `mc:Choice` 與 `mc:Fallback` 是同一段內容的兩個版本。**
   照單全收會讓文字方塊的內容出現兩次,而兩次都是對的字 —— 看起來像檔案本身
   有重複,不像解析錯誤。

## PDF:兩期

### 第一期 — 取文字做到完整(已完成)

文字的解讀改成自己做(`internal/pdf`):內容資料流的解譯、字型編碼、
CMap 與寬度。物件層(交叉參照表、解密、串流濾鏡)交給 pdfcpu。

- **CID / `ToUnicode`**:中文 PDF 用的是子集化的 CID 字型,字碼是字型
  內部的編號。走 ToUnicode 對照表才變得回文字 —— 同一份測試檔在改之前
  整頁是亂碼,而那是合法的字串,不會有任何錯誤訊息。
- **預先定義的中日韓 CMap**:`ETen-B5-H` 這類編碼的 CMap 檔不在 PDF 裡,
  但它的字碼就是 Big5 的位元組,直接用字集解碼器解得出來。舊的中文 PDF
  幾乎都是這一種,而它們通常沒有 ToUnicode。
- **分欄**:偵測「一條從頁首通到頁尾、沒有字跨過的縱向空白帶」,再用
  「每一欄大部分的列有沒有填滿欄寬」把表格排除掉 —— 表格也有那條空白帶,
  但它要橫著讀。
- **大綱**:`/Outlines` 的書籤變成目錄,含頁碼與階層。
- **加密**:空密碼的檔案打得開(大部分「加密」只是設了權限)。
- **字寬**:核心 14 字型的度量由 pdfcpu 內建的表提供,不必嵌在檔案裡;
  複合字型走 `/W` 與 `/DW`。
- **表單物件**:很多產生器把整頁內容放在 Form XObject 裡,要遞迴進去。
- **失控的檔案**:壞掉的 PDF 會讓物件層無止境地掃下去(40 個位元組的
  垃圾就能讓它 100% CPU 轉個不停)。加一層操作次數上限擋住。

### 第二期 — 頁面渲染(已完成主要路徑)

同一個解譯器換一個輸出裝置:取文字用 `textDevice`,畫頁面用 `rasterDevice`。
文字定位那一段只有一份,兩邊不會分岔。瀏覽模式按 `V` 看整頁。

做好的:路徑建構與填色/描邊(含線帽、接合、虛線)、色彩空間
(Gray / RGB / CMYK / ICCBased / Indexed / Separation 近似)、裁剪(矩形)、
影像(交給 pdfcpu 解成 png/jpg 再依 CTM 貼上)、表單物件遞迴、頁面旋轉,
以及三種嵌入字型格式的字形外框:

| 格式 | 誰解的 |
|---|---|
| TrueType / OpenType(`FontFile2`、`FontFile3` 的 OpenType) | `x/image/font/sfnt` |
| CFF(`FontFile3` 的 Type1C 與 CIDFontType0C) | 自寫:INDEX / DICT / charset / FDSelect + Type2 charstring |
| Type1(`FontFile`) | 自寫:eexec 兩層解密 + Type1 charstring(含 flex 與提示替換) |

三種都解不開時才用系統字型照同樣的位置與字級補畫,並在畫面上說明換過
哪些字型 —— 字形不同使用者看不出來,整段文字消失看得出來。

光柵器用相依裡已經有的 `rasterx`(SVG 那條路本來就在用)。沒有加新相依。

**奇偶填法(`f*`)**:光柵器本身只會非零繞組,所以改用奇偶規則的定義來算 ——
把路徑拆成一圈一圈各自光柵化,再逐像素做互斥或。單一圈的路徑(真實檔案裡
絕大多數的填色)走原本的快路,不多配遮罩。對 LibreOffice 的算繪相關係數
**1.0000**、墨水量完全相同。邊界:自己交叉的單一圈會退化成非零繞組。

**內嵌影像(`BI`/`ID`/`EI`)**:參數字典(縮寫與全名兩種鍵)、AHx / A85 /
Flate / RunLength / DCT 五種濾鏡、1/2/4/8/16 位元的取樣值、索引色、遮罩影像。
資料長度算得出來時就照算的走,不掃描 —— 影像的原始位元組裡可以剛好含 `EI`。
對 LibreOffice 的算繪相關係數 **0.9998**。

還沒做的:

- **漸層與圖樣**:那些區域目前留白。
- **seac**(用兩個標準字形拼出重音字):基底字形照樣畫,重音沒有加上去。
- **內嵌影像的 LZW 與 CCITT 濾鏡**:遇到會明確回報,不會畫出錯的東西。

### 這四項的實際可及性(2026-09-02 量的)

決定先做哪一項不是照列表順序,是照「真實產生器會不會用到」:

| 功能 | LibreOffice 匯出的 PDF | 手寫探針的現況(對 LO 的算繪)|
|---|---|---|
| 奇偶填法 `f*` | **大量使用**(一份漸層文件 129 次) | 修好前 0.9317(洞被填實)→ 修好後 **1.0000** |
| 奇偶裁剪 `W*` | 每份 2–3 次 | 裁剪取外接矩形,填法無關,沒有差異 |
| 內嵌影像 `BI`/`ID`/`EI` | **不產** | 修好前 0.3953(整塊沒畫)→ 修好後 **0.9998** |
| 漸層 `sh` / 圖樣 | **不產**(漸層攤成上百條實心填色) | 0.0000(整片留白) |
| Type1 `seac` | 不適用 | 這台機器上的 Type1 字型(URW C059 等)**3,700 多個字形裡 0 個用它** |

LibreOffice 不產的那幾項仍然是標準功能(Chrome、Illustrator、LaTeX 的 pgf
都會產),所以要用手寫 fixture 驗 —— LibreOffice 的 PDF **匯入**能正確算繪
那些檔案,可以當對照組。

### 渲染的驗收

`tools/pdfshot` 畫一頁成 PNG,LibreOffice 畫同一頁當對照,
`tools/inkdiff` 比兩張的墨水密度格點:

```bash
tools/go.sh run ./tools/pdfshot -o /src/.cache/a.png /src/internal/pdf/testdata/twocol.pdf
tools/office-oracle.sh png twocol.pdf                     # LibreOffice 那一張
tools/go.sh run ./tools/inkdiff /src/.cache/a.png /src/.cache/lo.png
```

不逐像素比:兩個渲染器對反鋸齒與字型微調的處理一定不同,逐像素永遠是紅的。
量到的結果(2026-09-02,32×44 格):

| 檔案 | 平均密度差 | 最大單格差 | 相關係數 | 墨水總量(本專案 / LibreOffice)|
|---|---|---|---|---|
| `twocol.pdf` 雙欄內文 | 0.0098 | 0.0593 | 0.9952 | 0.1199 / 0.1285 |
| `rich.pdf` 中英混排 | 0.0024 | 0.2169 | 0.9464 | 0.0067 / 0.0078 |

兩份都用檔案裡嵌的字型畫(`Rendered.Substituted` 是空的,有測試盯著)。
`rich.pdf` 的最大單格差落在大字級的標題那一格 —— 兩邊的反鋸齒與字型微調
不同,字級越大差越明顯。整體墨水量本專案略低,也是同一個原因。

字型格式本身另外有兩道與畫面無關的驗證,它們比整頁比對更能定位錯誤:

- CFF:拿系統上 OpenType 字型的 `CFF ` 表,同一份資料給自己的解析器與
  `x/image/font/sfnt` 各解一次,比 199 個字形的外接矩形(容差 1.5 個字身單位)。
- Type1:用 testdata 的真檔(LibreOffice 把中日韓子集嵌成 Type1),
  檢查自帶編碼裡的每一個字碼都畫得出外框,且外框落在字身範圍內。

## 驗收

沿用 `rulebook/65`:對外部工具實測,不看內部訊號。

| 包 | 對照 |
|---|---|
| `docx` / `pptx` / `xlsx` | LibreOffice `--convert-to txt` 的文字內容 |
| `doc97` | `antiword` / LibreOffice |
| `rtf` | LibreOffice |
| `pdf` 取文字 | `pdftotext -layout`(poppler) |
| `pdf` 渲染 | `pdftoppm` 出的點陣圖做格點比對 |

外部工具一律在容器裡跑,不裝進主機。committed 的測試樣本自己產生
(用 Go 組出最小的 OOXML / RTF / CFB),不放第三方文件。
