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

**seac(用兩個標準字形拼出重音字)**:`é` `ü` `Æ` 這類字在舊的 Type1 字型裡
常常不是獨立的外框,而是一道「拿第 65 號字加上第 194 號字、往右移多少、
往上移多少」的指令。Type1 用 `seac` 運算子,CFF 沒有這個運算子,改用
`endchar` 的四參數形式。兩邊都做了。

兩個字是用 **StandardEncoding 的字碼**指定的,不是字形編號。Type1 靠字形名
查得到;CFF 裡沒有名字,要先把字碼換成標準字串編號(SID)再由 charset 查
字形編號 —— 而 CFF 的前 149 個標準字串就是照 StandardEncoding 的順序排的,
所以不必抄一份 391 個字串的表。

**漸層(`sh` 與漸層圖樣)**:軸向(第 2 型)與放射(第 3 型)兩種,顏色由
PDF 的函式算出來 —— 指數(第 2 型)、接合(第 3 型)、取樣表(第 0 型)三種。
漸層當填色圖樣用時(`/Pattern cs … scn`,PatternType 2),座標系綁在頁面的
預設座標系上,不是綁在畫它的當下的 CTM。

`sh` 沒有自己的形狀,**裁剪就是形狀**,所以它另外記下裁剪路徑本身,
不像其他運算子那樣只取外接矩形(那會把箭頭、圓角、斜角畫成方塊)。
交給光柵器的是一個「逐點問顏色」的函式,所以邊緣照樣有反鋸齒。

還沒做的:

- **拼貼圖樣**(PatternType 1,一小段內容資料流重複鋪滿):那塊填色跳過不畫。
- **網格類漸層**(ShadingType 4–7)與 **PostScript 計算函式**(FunctionType 4):
  看不懂就不畫,留白而不是塗一片猜的顏色。`testdata/shading-unsupported.pdf`
  盯著這個行為。
- **軟遮罩與透明群組**(ExtGState 的 `/SMask`、表單物件的 `/Group`):
  目前把群組內容直接畫在同一張畫布上,群組內重設的透明度會蓋掉群組外的。
- **內嵌影像的 LZW 與 CCITT 濾鏡**:遇到會明確回報,不會畫出錯的東西。

### 實際可及性:真實檔案到底用什麼(2026-09-02 量的)

決定先做哪一項不是照列表順序,是照「真實產生器會不會用到」。
`tools/pdfprobe scan` 掃這台機器上蒐得到的 105 份 PDF(CAD 零件圖、行銷型錄、
arXiv 論文、LibreOffice/Word 產出、系統文件;104 份打得開):

| 種類 | 出現的型別 | 沒出現的型別 |
|---|---|---|
| 漸層 ShadingType | 2 軸向 ×48、3 放射 ×10 | 1、4–7(函式式、三種網格、兩種曲面)|
| 圖樣 PatternType | 2 漸層圖樣 ×4 | **1 拼貼圖樣一次都沒有** |
| 函式 FunctionType | 0 取樣 ×24、2 指數 ×96、3 接合 ×32 | **4 PostScript 計算一次都沒有** |

105 份裡只有 5 份用到漸層或圖樣。這個分布就是實作範圍的依據:做了出現的
那幾種,沒出現的明講不做。

**零結果要先做正對照,不然「沒有」與「量錯了」長得一樣。** 同一批 PDF 的
嵌入字型裡 `seac` 是 0 個(95 份 Type1 共 8,307 個字形、557 份 CFF 共 17,387 個),
但這個零不能拿來當「不必做」的理由:那批檔案裡看起來像重音字的字形名只有
3 種,也就是**根本沒有重音字可數**。改掃系統上的 Type1 字型就出現了 448 個。
`TestSeacUsageInCorpus` 與 `TestSeacUsageInSystemType1` 是這兩次量測,
`TestSeacScanners` 是掃描器自己的正對照。

| 功能 | 真實檔案的用量 | 現況 |
|---|---|---|
| 奇偶填法 `f*` | LibreOffice 大量使用(一份漸層文件 129 次)| 對 LO 的算繪相關係數 **1.0000** |
| 奇偶裁剪 `W*` | 每份 2–3 次 | 裁剪取外接矩形,填法無關,沒有差異 |
| 內嵌影像 `BI`/`ID`/`EI` | LibreOffice 不產,是標準功能 | 對 LO 的算繪相關係數 **0.9998** |
| 漸層 `sh` / 漸層圖樣 | 105 份裡 5 份、共 58 個漸層 | 對 poppler 的算繪相關係數 **0.9999** |
| 拼貼圖樣 PatternType 1 | **105 份裡 0 次** | 不做 |
| ShadingType 4–7 / FunctionType 4 | **105 份裡 0 次** | 不做,留白 |
| Type1 `seac` | 系統的 43 份 Type1 字型、30,442 個字形裡 **448 個**用它(全在 Bitstream Charter);104 份 PDF 的嵌入字型裡 0 個 | 對 poppler 的算繪相關係數 **0.9994** |

### 渲染的驗收

`tools/pdfshot` 畫一頁成 PNG,另一個渲染器畫同一頁當對照,
`tools/inkdiff` 比兩張的墨水密度格點:

```bash
tools/go.sh run ./tools/pdfshot -o /src/.cache/a.png /src/internal/pdf/testdata/twocol.pdf
tools/office-oracle.sh png twocol.pdf                     # LibreOffice 那一張
tools/go.sh run ./tools/inkdiff /src/.cache/a.png /src/.cache/lo.png
```

對照組有兩個。LibreOffice 走的是 PDF **匯入**再重新排版,對版面夠用;
poppler 是獨立的 PDF 渲染器,對「顏色與幾何對不對」準得多,而且它本來就
在這台機器上(`minidocks/poppler`):

```bash
docker run --rm --network none -u "$(id -u):$(id -g)" \
    -v "$PWD/.cache":/w -w /w --entrypoint /usr/bin/pdftoppm \
    minidocks/poppler:latest -r 96 -f 1 -l 1 -png -singlefile in.pdf out
```

不逐像素比:兩個渲染器對反鋸齒與字型微調的處理一定不同,逐像素永遠是紅的。
量到的結果(2026-09-02,32×44 格):

| 檔案 | 對照 | 平均密度差 | 最大單格差 | 相關係數 |
|---|---|---|---|---|
| `twocol.pdf` 雙欄內文 | LibreOffice | 0.0098 | 0.0593 | 0.9952 |
| `rich.pdf` 中英混排 | LibreOffice | 0.0024 | 0.2169 | 0.9464 |
| `twocol.pdf` | poppler | — | — | 0.9930 |
| `rich.pdf` | poppler | — | — | 0.9856 |
| `evenodd.pdf` 奇偶填法 | LibreOffice | — | — | 1.0000 |
| `inline.pdf` 內嵌影像 | LibreOffice | — | — | 0.9998 |
| `shading.pdf` 漸層 | poppler | 0.0008 | 0.0577 | 0.9999 |

前兩份都用檔案裡嵌的字型畫(`Rendered.Substituted` 是空的,有測試盯著)。

**墨水密度對「形狀對不對」是瞎的。** 它量的是每一格有多少墨水,不是輪廓:
把箭頭畫成方塊、把漸層畫成平塗,都可能拿到很高的相關係數。驗形狀要看圖,
驗顏色要用 `tools/pdfprobe px` 沿一條線逐點取樣。

真實檔案上量到的還有(對 poppler,同樣 32×44 格):

| 頁面 | 相關係數 | 還差在哪 |
|---|---|---|
| arXiv 論文首頁(軸向漸層 + 接合函式)| 0.9902 | — |
| arXiv 論文內頁(取樣函式的漸層)| 0.7043 | 圖片沒畫出來,漸層本身逐點與 poppler 差 1/255 以內 |
| 投影片(放射漸層)| 0.9800 | — |
| 行銷報告(亮度軟遮罩)| 0.3657 | 軟遮罩與透明群組沒做:該半透明的圓畫成不透明 |
| 自組的 seac 頁(嵌 Bitstream Charter)| 0.9994 | — |

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
