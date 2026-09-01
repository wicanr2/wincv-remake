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

### 第二期 — 頁面渲染

把 content stream 解出來畫成點陣圖,貼到格點上。純 Go 沒有堪用的 PDF 渲染器,
接 C 函式庫會破掉四平台交叉編譯,所以要自己寫:content stream 解譯器
(路徑、填色、變換矩陣、影像)+ 內嵌字型(TrueType / CFF / Type1)光柵化。
光柵器用 `golang.org/x/image/vector`,TrueType 用 `x/image/font/sfnt`,
CFF 與 Type1 要自己解。不加新相依。

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
