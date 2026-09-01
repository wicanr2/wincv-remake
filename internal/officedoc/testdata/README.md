# 測試用的 Office 檔

三份都是 LibreOffice 產生的,不是別人的文件。重建方式:

```bash
tools/office-oracle.sh 'doc:MS Word 97' rich.html                   # → rich.doc
tools/office-oracle.sh 'docx:MS Word 2007 XML' rich.doc             # → rich.docx
tools/office-oracle.sh 'pptx:Impress MS PowerPoint 2007 XML' deck.fodp  # → deck.pptx
tools/office-oracle.sh xlsx sheet.csv                               # → sheet.xlsx
```

驗收對照(同一份檔案轉成純文字):

```bash
tools/office-oracle.sh txt rich.docx
```

原始檔 `rich.html`、`deck.fodp`、`sheet.csv` 一起放在這裡。
