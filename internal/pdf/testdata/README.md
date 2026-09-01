# 測試用的 PDF

兩份都是 LibreOffice 產生的。重建方式:

```bash
tools/office-oracle.sh pdf rich.doc        # → rich.pdf(中英混排、書籤、超連結)
tools/office-oracle.sh pdf twocol.fodt     # → twocol.pdf(雙欄內文)
```

`evenodd.pdf` 是手寫的,不是產生的:兩個同心方框各畫一次,左邊用 `f*`
(奇偶)、右邊用 `f`(非零繞組)。檔案未壓縮,用文字編輯器打開就看得到
內容資料流。對照組是 LibreOffice 對同一份檔案的算繪:

```bash
tools/office-oracle.sh png evenodd.pdf
tools/go.sh run ./tools/pdfshot -o mine.png internal/pdf/testdata/evenodd.pdf
tools/go.sh run ./tools/inkdiff mine.png evenodd.png
```

`rich.pdf` 的字型是子集化的 CID 字型(Identity-H 編碼 + ToUnicode),
也就是中文 PDF 最常見的形態;沒有走 ToUnicode 的話整頁會是亂碼。
`twocol.fodt` 的原始檔一起放在這裡,內文是可預測的重複句子,
分欄偵測要對就得把同一句話讀成連續的一列。
