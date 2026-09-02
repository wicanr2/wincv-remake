# README 截圖用的示範文件

`README.md` 裡 Word / PowerPoint / Excel / PDF 那四張截圖用的就是這個目錄。
內容是本專案自己寫的,不是別人的文件。

原始檔三份,其餘都是 LibreOffice 從它們轉出來的:

| 原始檔 | 轉出 |
|---|---|
| `wincv.html` | `wincv.doc` → `wincv.docx`、`wincv.rtf`、`wincv.pdf` |
| `wincv.fodp` | `wincv.pptx` |
| `formats.csv` | `formats.xlsx` |

重建:

```bash
cd docs/demo/office
../../../tools/office-oracle.sh 'doc:MS Word 97' "$PWD/wincv.html"
../../../tools/office-oracle.sh 'docx:MS Word 2007 XML' "$PWD/wincv.doc"
../../../tools/office-oracle.sh rtf "$PWD/wincv.doc"
../../../tools/office-oracle.sh pdf "$PWD/wincv.html"
../../../tools/office-oracle.sh 'pptx:Impress MS PowerPoint 2007 XML' "$PWD/wincv.fodp"
../../../tools/office-oracle.sh xlsx "$PWD/formats.csv"
rm -rf .lo .cache          # LibreOffice 會在這裡留下使用者設定
```

重拍截圖。`-keys` 是「游標從 `..` 往下按幾次 `Down` 再 `Enter`」,所以
**在這個目錄裡增刪檔案就要重數** —— 數錯不會有錯誤訊息,只會拍到隔壁那個檔。
先跑一次不帶 `-keys` 的看清單:

```bash
tools/go.sh run ./cmd/celldump -app /src/docs/demo/office -cols 100 -rows 22 -o /src/.cache/list.png
```

目前的順序是 `..` / formats.csv / formats.xlsx / README.md / wincv.doc /
wincv.docx / wincv.fodp / wincv.html / wincv.pdf / wincv.pptx / wincv.rtf:

```bash
D=/src/docs/demo/office
# formats.xlsx:2 個 Down
tools/go.sh run ./cmd/celldump -app $D -cols 100 -rows 30 \
    -keys "Down,Down,Enter" -o /src/docs/ui/shot-xlsx.png
# wincv.docx:5 個
tools/go.sh run ./cmd/celldump -app $D -cols 100 -rows 34 \
    -keys "Down,Down,Down,Down,Down,Enter" -o /src/docs/ui/shot-docx.png
# wincv.pdf:8 個。加 V 就是整頁算繪
tools/go.sh run ./cmd/celldump -app $D -cols 100 -rows 34 \
    -keys "Down,Down,Down,Down,Down,Down,Down,Down,Enter" -o /src/docs/ui/shot-pdf-text.png
tools/go.sh run ./cmd/celldump -app $D -cols 100 -rows 40 \
    -keys "Down,Down,Down,Down,Down,Down,Down,Down,Enter,V" -o /src/docs/ui/shot-pdf-page.png
# wincv.pptx:9 個
tools/go.sh run ./cmd/celldump -app $D -cols 100 -rows 30 \
    -keys "Down,Down,Down,Down,Down,Down,Down,Down,Down,Enter" -o /src/docs/ui/shot-pptx.png
```

`wincv.pdf` 另外當 `internal/pdf` 的回歸樣本:它裡面有一份子集化的 TrueType,
對照表只涵蓋 56 個字形裡的兩個數字,其餘的字是用字碼直接定址的。
`TestRenderUsesEmbeddedFonts` 盯著那份字型不能被換成系統字型。
