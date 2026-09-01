# 測試用的 .doc

兩份都是產生的,不是別人的文件。重建方式(需要 `linuxserver/libreoffice` 映像):

```bash
tools/office-oracle.sh 'doc:MS Word 97' rich.html     # → rich.doc
tools/office-oracle.sh 'doc:MS Word 97' notes.rtf     # → notes.doc
tools/office-oracle.sh txt rich.doc                   # → 驗收用的純文字對照
```

`rich.html` 涵蓋標題、粗體斜體、有序清單、表格、超連結與中英混排;
`notes.rtf` 涵蓋註腳。兩份的原始檔就放在這裡。
