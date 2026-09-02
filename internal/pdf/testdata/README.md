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

`inline.pdf` 也是手寫的:一張 4×4 的紅藍棋盤用 `BI`/`ID`/`EI` 直接寫在
內容資料流裡,放大貼在頁面上,旁邊描一個框當定位參考。用棋盤而不是
單色塊,是因為「貼歪了、上下顛倒了」在單色塊上看不出來。

`rich.pdf` 的字型是子集化的 CID 字型(Identity-H 編碼 + ToUnicode),
也就是中文 PDF 最常見的形態;沒有走 ToUnicode 的話整頁會是亂碼。
`twocol.fodt` 的原始檔一起放在這裡,內文是可預測的重複句子,
分欄偵測要對就得把同一句話讀成連續的一列。

`shading.pdf` 是手寫的,四塊各盯一件事:三角形裁剪的軸向漸層(`sh` 的形狀
完全來自裁剪路徑)、放射漸層當填色圖樣、接合函式(綠→黃→綠)、取樣函式
(紅→藍→綠→黃)。後兩塊的顏色順序是刻意挑的 —— 兩端相同、或中間繞一圈,
兩種都不可能由「兩個端點線性內插」生出來,所以函式沒讀對就一定紅。
對照組是 poppler:

```bash
docker run --rm --network none -u "$(id -u):$(id -g)" \
    -v "$PWD/.cache/probe":/w -w /w --entrypoint /usr/bin/pdftoppm \
    minidocks/poppler:latest -r 96 -f 1 -l 1 -png -singlefile shading.pdf shading-poppler
```

`shading-unsupported.pdf` 放的是**故意不支援**的兩種:網格漸層
(ShadingType 4)與 PostScript 計算函式(FunctionType 4)。它該畫出一張全白
的頁面 —— 看不懂就不畫,留白比塗一片猜的顏色好。poppler 畫得出來,
所以這一份不做算繪比對,只驗「整頁沒有墨水」。

`seac.pdf` 裡的 Type1 字型是自己寫的,不是從別處拿來的:三個字形都是方塊
(`A` 是 50..450 × 0..600、`acute` 是 50..250 × 700..800),第三個 `Aacute`
用 `seac` 把前兩個拼起來,重音再往右 300、往上 100。用方塊而不是真的字母,
是因為「重音有沒有偏一點」在真字母上看不出來,在方塊上差一格都看得到。
eexec 區段用十六進位寫,整份字型都是可讀的 ASCII,但字形本身是加密的,
打開 PDF 看不到 —— 產生它的 `tools/mkseac.py` 就是這份 fixture 的規格:

```bash
python3 tools/mkseac.py internal/pdf/testdata/seac.pdf
```

對照組是 poppler(相關係數 0.9997)。

另外用系統上真的有用 seac 的字型(`/usr/share/fonts/X11/Type1/c0632bt_.pfb`,
Bitstream Charter)也對過一次,相關係數 0.9994 —— 那份字型是第三方的,
所以只在本機驗,不放進 repo。

`softmask.pdf` 是手寫的,三塊各盯一件事:常數透明度(紅色以 `ca 0.5` 畫在
白底上,結果要是淡紅不是灰)、亮度軟遮罩(純紅色透過一道黑到白的漸層)、
透明群組(兩個互相重疊的不透明方塊,整組以 `ca 0.5` 合成)。

第三塊是關鍵:沒有群組語意的話,重疊處會被塗兩次而變深;有群組語意的話,
整組先算完再套透明度,重疊處與非重疊處一樣。對照組是 poppler,每一點都在
1/255 以內。
