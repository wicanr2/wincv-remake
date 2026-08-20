# 主畫面版面(原版實測)

量測對象是 Wine 底下跑的原版 `wincv.exe`,視窗 client area **792 × 506**。
重跑方式:

```bash
tools/oracle-measure.sh docs/ui/oracle-window.png 18
```

這支腳本會問 X 拿視窗幾何、只截視窗本體(不含外框與標題列),
並印出 Wine 選了哪些字型。

## 字型:原版同時要兩種尺寸

Wine 的 font trace:

```
select_font Chosen: L"cvga Regular" (L"C:\wincv\wincv.fon")
get_fontsig pix_h 15 charset 255   ← OEM_CHARSET,半形
get_fontsig pix_h 16 charset 136   ← CHINESEBIG5,全形
select_font Chosen: L"System Regular" (Z:\usr\share\wine\fonts\cvgasys.fon)
```

半形要 15 px、全形要 16 px。`cvgasys.fon` 是 Wine 給**選單列**用的 System 字型,
與檔案清單無關。

`cvga.fon` 自己的 `dfPixHeight` 是 15、`dfExternalLeading` 是 **0** ——
多出來的那一列不是字型要的,是程式拿全形字的高度當列高。

## 格點

以樣板比對(拿真實的 `cvga` 字模去截圖上找完全吻合的位置)量出來:

| 項目 | 值 |
|---|---|
| 格子大小 | **8 × 16** px(字模 8 × 15,靠上對齊,下方留一列) |
| 欄起點 | x = 34,間距 8,最右一格 x = 770 → **93 欄** |
| 捲軸 | x = 775..791(17 px) |
| 前景 / 背景範例 | 檔名列 `#C0C0C0` on `#000000` |

列的位置(y 為該列第一條掃描線):

| y | 內容 |
|---|---|
| 0..19 | 選單列(Win32 控制項,比例字型) |
| 22..38 | 工具列(Win32,含圖示) |
| 40 | 路徑列 `C:\wincv\*.*` + 位置 + 標記統計(1 列) |
| 56, 72, …, 328 | 檔案清單(**18 列**,間距 16) |
| 344..345 | 2 px 分隔 |
| 346, 362 | 狀態列(2 列) |
| 378 起 | 預視窗格(8 列 × 16 = 128 px) |

檔案清單的第一欄(x = 34)是游標與標記的指示欄,檔名從 x = 42 開始。

## 重製版對應

`internal/render` 的 `LineGap = 1` 就是上表那一列:`CellH = 字身高 + 1`。
`cmd/celldump -app -cols 93 -rows 21` 畫出來的格子與原版同尺寸。

尚未做的部分:選單列與工具列(重製版用 `F1` 自繪選單取代)、
左側磁碟欄、預視窗格、捲軸。這些差異記在 README 的對照表。

## 這份量測推翻過的說法

早期的結論是「oracle 截圖只能當版面與配色真值,不能當字模真值」,
理由寫成「Wine 用自己的 `cvgasys.fon`(16 px)替換掉 `cvga`(15 px)」。
兩點都不成立:Wine 確實載入了 app 的 `cvga`,而當初樣板比對之所以零命中,
是因為 `tools/xwd2png.py` 當時把每像素寫死 4 bytes,容器裡的 Xvfb 卻是 24 bpp,
通道以 3 為週期輪轉。轉檔工具修好之後,每個字元都剛好命中一次。
