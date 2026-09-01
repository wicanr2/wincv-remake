# 測試用的 PDF

兩份都是 LibreOffice 產生的。重建方式:

```bash
tools/office-oracle.sh pdf rich.doc        # → rich.pdf(中英混排、書籤、超連結)
tools/office-oracle.sh pdf twocol.fodt     # → twocol.pdf(雙欄內文)
```

`rich.pdf` 的字型是子集化的 CID 字型(Identity-H 編碼 + ToUnicode),
也就是中文 PDF 最常見的形態;沒有走 ToUnicode 的話整頁會是亂碼。
`twocol.fodt` 的原始檔一起放在這裡,內文是可預測的重複句子,
分欄偵測要對就得把同一句話讀成連續的一列。
