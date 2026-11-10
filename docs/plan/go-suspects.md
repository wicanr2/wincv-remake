# Go 版的可疑行為

搬到 Rust 時逐行對照 Go 原始碼,順手發現的問題。除非另外註明,Rust 版照抄 Go 的行為,維持兩邊輸出相同;
要修就兩邊一起修,並各補一條測試。markdown 的 10 項已經兩邊一起修好(見 `rust-rewrite.md` 的 3a),不再列。

「證據」一欄:**實測**是做過重現;**讀碼**是只看程式碼推得,沒有跑過。

## 文件模式

| # | 模組 | 行為 | 影響 | 證據 | Rust 現況 |
|---|---|---|---|---|---|
| D1 | pptx | `<a:br><a:rPr/></a:br>` 遇上 `HTMLAutoClose`:br 一打開就關閉,後面的 `</a:br>` 把外層元素一路關掉。這是 PowerPoint 常見的換行寫法 | 投影片換行之後的內容全部遺失,畫面顯示「(這一張沒有文字)」 | 實測 `gen/pptx-br-children.pptx` | 已修,兩邊相同:`ooxml` 的解碼器不設 AutoClose,保留非嚴格模式與 HTML 實體。測試 `TestLineBreakWithRunProps`、`TestBrWithChildrenKeepsStructure` / `line_break_with_run_props`、`br_with_children_keeps_structure` |
| D2 | app | Office 文件的狀態列尾端多出「— doc:」:`drawBrowseStatus` 特別處理了 `pdf:` 與 `epub:`,沒處理 `doc:`,`hostOf` 把 `doc:` 當成主機名 | 狀態列多一段無意義的字 | 實測 `app-doc-*` 場景 | 兩邊已修:Office 文件比照 PDF 只給標題(`TestDocStatusBarShowsTitleOnly` / `doc_status_bar_shows_title_only`) |
| D3 | pdfdoc | 書籤遇到具名目的地時 nil pointer panic(pdfcpu 的 `Names["Dests"]` 是 nil,`Outline` 沒有 recover) | 開這份 PDF 時程式直接結束 | 實測 Vorbis_I_spec.pdf | 已修,兩邊相同:Go 改成自己查目錄的 `/Dests` 字典與 `/Names` 的 `/Dests` 名稱樹(與 Rust 同一套規則),`Outline` 另有 recover。Vorbis_I_spec.pdf 兩邊都是 101 筆書籤、頁碼逐筆相同。測試 `TestNamedDestinations` |
| D4 | pdfdoc | 整頁抽圖失敗:不支援的濾鏡或色彩空間讓 Reader 是 nil,`io.ReadAll` panic;或 `ReadValidateAndOptimize` 的版本驗證失敗 | 218 頁的圖全部拿不到 | 實測 | 不同:Rust 用 hayro |
| D5 | pdfdoc | 圖片名稱取全文件第一次登記的資源名,受 map 迭代順序影響;同名的不同物件互相覆蓋;沒畫出來的圖也被列出 | 圖片清單不穩定、會少圖 | 實測 San5(poppler 16 張、Go 2 張)、MIC4680 | 不同 |
| D6 | pdfdoc | seac 組合字 Aacute 解成「¨」 | 重音字錯 | 實測 `seac.pdf` | 不同:Rust 給「Á」 |
| D7 | pdfdoc | 畫整頁不看 CropBox,畫整張 MediaBox | 26 份 PDF 的頁面圖比閱讀器看到的大 | 實測 | 不同:Rust 畫 CropBox |
| D8 | pdfdoc | AS4C1M16F5.pdf 整份取不出文字,畫出來是白頁 | 整份看不到 | 實測 | 不同:Rust 與 poppler 正常 |
| D9 | pdfdoc | 同一列內字的順序錯(「Intle」「Rertiggering」) | 文字錯亂 | 實測 Intel 文件 | 不同:Rust 正確 |
| D10 | pdfdoc | CMYK JPEG 轉 PNG 色偏較大 | 圖片偏色 | 實測(魔眼殺機攻略) | 不同:Rust 較接近 poppler |
| D11 | pdfdoc | hex 字串的書籤標題多過一次 `Unescape`;無效的 UTF-16 標題變空字串被略過;超過 32 MB 的圖被截斷成損壞的檔案 | 書籤或圖片錯 | 讀碼 | 未查 |
| D12 | doc97 | `parseCLX`:Prc 的 cb 等於 -3 時無窮迴圈,小於 -3 可能讀負索引而 panic | 程式卡住或結束 | 讀碼 | 已修,兩邊相同:3+cb 不是正數時回報 piece table 截斷。測試 `TestCLXNegativePrcSize` / `clx_negative_prc_size` |
| D13 | doc97 | `readStyles`:cbStd 為 1 而且剛好在表尾時讀出界而 panic | 程式結束 | 讀碼 | 已修,兩邊相同:cbStd 小於 2 的樣式不論在哪裡都當成識別碼未知,不讀下一筆的長度欄位或表外。測試 `TestStyleTooShortForSti` / `style_too_short_for_sti` |
| D14 | doc97 | FC 照「解出來的第幾個字」算,遇到代理對之後字元屬性錯位兩個位元組 | 樣式套錯字 | 讀碼 | 照抄 |
| D15 | rtf | `\bin99999999999999999999` 讓 `p.i+n` 溢位,切片 panic | 程式結束 | 實測 | 已修,兩邊相同:長度用「剩下多少位元組」比,不做加法。測試 `TestBinLengthOverflow` / `bin_length_overflow` |
| D16 | docx | `nextNum`:start=0 的清單永遠停在 0;start 為負的清單數到 0 之後跳回起始值 | 編號錯 | 實測 | 照抄 |
| D17 | xlsx | 從不讀 core properties,標題一律是檔名 | 與 docx、pptx 不一致 | 讀碼 | 照抄 |
| D18 | xlsx | NaN 套上日期格式時 `int(NaN)` 依平台而異(amd64 得到 1607-09-20) | 顯示一個不存在的日期 | 實測 | 不同:顯示 `NaN` |
| D19 | pptx | 備忘稿只留 `Spans`,備忘稿裡的表格變成空的引言區塊 | 表格消失 | 讀碼 | 照抄 |
| D20 | ooxml | `RelsByType` 走訪 Go map,同型別的關聯有兩筆時選哪一筆不固定 | 結果不穩定 | 讀碼 | 不同:依文件順序 |
| D21 | docx | `fieldHyperlink` 先 ToUpper 再依位元組切 9 個位元組,遇到 `ı` 切在字中間 | 超連結欄位解錯 | 讀碼 | 照抄 |
| D22 | officedoc | 判斷 .doc 是不是 RTF 只看前 8 個位元組,不跳過 BOM 與空白 | 有 BOM 的 RTF(副檔名 .doc)打不開 | 讀碼 | 照抄 |

## 網路瀏覽與 EPUB

| # | 模組 | 行為 | 影響 | 證據 | Rust 現況 |
|---|---|---|---|---|---|
| N1 | gopher | IPv6 主機的位址多包一層方括號 | IPv6 gopher 站連不上 | 讀碼 | 照抄 |
| N2 | gopher | 選單的顯示文字沒有做編碼判讀 | Big5 選單變亂碼 | 實測 | 兩邊已修:整份選單一起判讀(樣本是每一列第一個 tab 之前的部分,含型別字元),判出 Big5 / GBK / Shift_JIS / EUC-KR 才解碼顯示文字;selector 留原始位元組。測試 `TestParseMenuDecodesBig5`、`TestParseMenuDetectsWholeMenu`、`TestParseMenuKeepsUTF8`(Rust `parse_menu_decodes_big5`、`parse_menu_detects_whole_menu`、`parse_menu_keeps_utf8`) |
| N3 | web | 宣告 utf-8 但內容是 Big5 時不退回判讀;iso-8859-1 與 windows-1252 當 ASCII | 網頁亂碼 | 實測 | 兩邊已修:宣告 UTF-8 或 ASCII 時,不合法的位元組算成替代字元再套亂碼門檻,超過就退回判讀;ISO-8859-1 一族照 WHATWG 用 windows-1252 解(兩邊共用同一張 0x80–0x9F 表);宣告 UTF-8 以外的編碼而內容是含非 ASCII 字元的合法 UTF-8 時照 UTF-8。測試 `TestFetchDeclaredCharset`(Rust `fetch_declared_charset`、`cp1252_table`) |
| N4 | web | 只真正跟隨 4 次重導向 | 第 5 次重導向的網址打不開 | 讀碼 | 照抄 |
| N5 | web | `<script/>` 自閉合時,後面的腳本文字外洩到畫面 | 畫面出現程式碼 | 讀碼 | 照抄 |
| N6 | web | `<a href=" ">` 的 href 是空的,仍套上連結樣式 | 點了沒反應的連結 | 讀碼 | 照抄 |
| N7 | web | ErrTooLarge 的訊息沒有過 i18n | 其他語言介面出現中文 | 讀碼 | 照抄 |
| N8 | epub | 程式碼裡的 `[雷]` 註解寫錯原因;真正的觸發條件是帶內容的 `<meta>` | 誤導維護者 | 讀碼 | Rust 的註解已改寫 |
| N9 | epub | 寬鬆模式重試時解進同一個結構,章節重複 | 壞掉的書目錄重複 | 讀碼 | 照抄 |
| N10 | epub | 非 UTF-8 的 OPF 打不開 | 舊的中文電子書打不開 | 實測 | 兩邊已修:Go 設 `CharsetReader`,Rust 的 goxml 照 Go 的 switchToReader 換掉宣告之後的位元組;宣告之後的內容交給 web 的 `DecodeCharset`(與網頁同一套宣告規則,認不得的編碼改用判讀)。有 UTF-16 BOM 的檔案先整份轉成 UTF-8,有 BOM 就不理會宣告。container.xml 與 NCX 走同一條。測試 `TestOpenNonUTF8OPF`(Rust `open_non_utf8_opf`) |
| N11 | epub | 章節 XHTML 沒有做編碼判讀,BOM 會多出一段空段落 | 亂碼或多一段空白 | 實測 | 兩邊已修:章節與 nav.xhtml 先過 web 的 `DecodeMarkup`,依 BOM、XML 宣告、meta charset 的順序決定編碼,都沒有就判讀,再交給 HTML 解析器。測試 `TestChapterEncoding`(Rust `chapter_encoding`) |
| N12 | epub | NCX 只取第一層 | 子章節不在目錄上 | 讀碼 | 照抄 |

## markdown

| # | 行為 | 證據 | Rust 現況 |
|---|---|---|---|
| M1 | Go 的 Loader 可以回 nil 圖與 nil 錯誤 | 讀碼 | Rust 的型別表達不出來,沒有呼叫端這樣用,不改 |
