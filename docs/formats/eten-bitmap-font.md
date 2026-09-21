# 倚天與國喬點陣字庫

狀態：**CONFORMED**（ETUNPACK 解碼、原生 24×24 選擇與兩種來源的實際
渲染路徑均已驗證）。

remake 的全形字來源。取自倚天中文系統 3.53 的光碟映像
(`~/cht/etan_font/ET353S.iso`,`tools/setup-eten.sh` 負責抽出)。

尺寸與 WinCV 隨附的半形 `.FON` 剛好成對 —— 這不是巧合,是同一個年代的規格:

| WinCV 半形 | 全形格 | 倚天檔案 |
|---|---|---|
| `cvga` 8×15 | 16×15 | `STDFONT.15` + `SPCFONT.15` + `SPCFSUPP.15` |
| `cvga1224` 12×24 | 24×24 | `STD.24M/K/L/R/B/S`（ETUNPACK 壓縮）或國喬相容裸字模 |

## 24×24 的 ETUNPACK 壓縮格式

`STD.24M/K/L/R/B/S` 的檔頭魔術字為 `ETUNPACK V1.00`。解碼器必須：

- 驗證魔術字、目錄與碼流位移都在輸入範圍內；不接受截斷資料或無法走到
  葉節點的位元碼流。
- 自碼流開頭依序讀三棵靜態 Huffman 樹。每個 byte 值的描述為 5-bit
  碼長（不可為零）與該長度的 MSB-first 碼字；輸出第 `i` 個 byte 使用
  第 `i mod 3` 棵樹。
- 解出 `13094 × 72 = 942768` bytes 後，對**每一個字模**依目錄項
  `+0x12`、`+0x13` 的列範圍做 XOR 還原：`g[i+3] ^= g[i]`。
- 對 `STD.24M` 以外的字體同樣嚴格處理；不把來源已損壞的 `STD.24L`
  偽裝成有效字模。預設尋找 `STD.24M`。

解碼結果是無檔頭的 24×24 字模，與下節的 Big5 分區索引相同。

## 國喬 24×24 字形

`workplace/AdafruitGFX-ChineseFont-Addon` 是使用者指定的研究輸入，未偵測
到授權檔，因此不納入 Git 或公開散布包。其 `font/stdfont.24f` 與
`font/spcfont.24` 是與倚天相同的 24×24 裸字模布局，程式可經同一組
`-eten24-std`、`-eten24-spc` 參數載入；這表示格式相容，**不表示兩者
字形或字節內容相同**。

## 15×15 與 24×24 裸字模格式

裸格式,**沒有檔頭**。每字 `stride = ((W+7)/8) * H` bytes,
row-major、MSB 在上、由上而下。16×15 的 stride 是 30。

| 檔案 | 內容 | 字數 |
|---|---|---|
| `STDFONT.15` | 漢字 | 13,094 |
| `SPCFONT.15` | 全形符號與標點 | 408 |
| `SPCFSUPP.15` | 符號補充 | 365 |
| `ASCFONT.15` | ASCII 半形 8×15 | 256 |
| 解開的 `STD.24*` 或國喬 `stdfont.24f` | 漢字 | 13,094 |
| `SPCFONT.24` 或國喬 `spcfont.24` | 全形符號與標點 | 408 |

## Big5 分區索引

線性序號:

```
raw(hi, lo) = (hi - 0xA1) * 157 + (lo - 0x40  if lo < 0x7F  else  lo - 0x62)
```

低位元組有兩段(`0x40`–`0x7E` 與 `0xA1`–`0xFE`),所以不是單純相減。

分區與檔案的對應:

| Big5 範圍 | raw | 碼位數 | 檔案 | 字模數 | 對齊? |
|---|---|---|---|---|---|
| A140–A3BF | 0–407 | 408 | `SPCFONT.15` | 408 | 對齊 |
| A3C0–A43F | 408–470 | 63 | — | — | 空隙 |
| A440–C67E | 471–5871 | 5401 | `STDFONT.15` 前段 | 5401 | 對齊 |
| C6A1–C8FE | 5872–6279 | 408 | `SPCFSUPP.15` | **365** | **對不起來** |
| C940–F9FE | 6280–13972 | 7693 | `STDFONT.15` 後段 | 7693 | 對齊 |

```
if   raw <= 407:            SPCFONT[raw]
elif raw <  471:            缺字
elif raw <= 5871:           STDFONT[raw - 471]
elif raw <  6280:           SPCFSUPP[只計入 Big5 有定義的碼位]
else:                       STDFONT[5401 + (raw - 6280)]
```

## [雷] 符號補充區不能用線性索引

那一區有 408 個碼位,`SPCFSUPP.15` 卻只有 365 個字模 ——
字模是「把有定義的碼位擠在一起」存的,中間有 43 個洞。

線性索引會整批錯位,而**錯位取到的是另一個看起來正常的字**。
實測:`C6E7` 應該是「ゃ」,線性索引取到的是別的字。
這比缺字難發現得多 —— 缺字一眼看得出來,錯字看起來像「有解出來」。

程式以 Big5 解碼器辨識哪些碼位有定義，僅讓有定義的碼位遞增字模索引；
43 個洞則回缺字。`internal/eten/eten_test.go` 的補充符號區測試會同時
驗證可用的日文符號與洞的位置，避免索引再次整批偏移。

已知 `SPCFSUPP.15` 的 index 0–5 是 ①②③④⑤⑥(對應 C6A1–C6A6),
所以基底位置是對的,問題只在中間的洞。

## [雷] 一定要一起載 `SPCFONT.15`

`STDFONT.15` 從 A440(「一」)起,**不含** A140–A3BF 的全形標點。
只載 STDFONT 的話 `，。！？「」『』（）《》～` 會整批變缺字。

## 怎麼驗

先過這一關再往下做,否則整批字會整體偏移(看起來像「有字但都不對」):

- `STDFONT.15` 的 index 0 必須是「一」——一條橫線。
  (它的橫線上方還有一個單像素的起筆點,所以「只有一列墨跡」這個
   斷言會誤殺,要看最寬的那一列。)
- 「中」「猴」「檔」「案」dump 成 ASCII art 必須可辨識。
- `，。！？「」（）` 取得到(這一條在驗有沒有載 `SPCFONT.15`)。

`internal/eten/eten_test.go` 涵蓋以上全部。

## 已符合的驗證收據

- 合成 ETUNPACK 串流測試覆蓋三樹選擇、MSB-first 位元序、截斷拒絕與逐字模
  XOR 還原，沒有使用第三方字形資料。
- 使用者提供的 ETEN 3.53 `FILES/STD.24M`：輸入 SHA-256
  `347ae2655807fc250a18673e6634a363dfba7feee6c3355b886a0810d2d9c030`，解開後
  942,768 bytes，SHA-256
  `65aafa2a59baa73e835dbfebeeaf6c99ae85db52b3bc8e184cbc1d3f7bd364e6`。
- `cmd/celldump` 分別以 ETUNPACK `STD.24M` 和國喬 `stdfont.24f`、原版
  `cvga1224.FON` 成功渲染 80×25 個 12×25 格；兩種輸出雜湊不同，未將其
  誤認成同一套字形。

使用時將使用者自備檔案放進 `WINCV_HOME` 可自動找到 `STD.24M` 與
`SPCFONT.24`，或明確指定：

```
wincv -eten24-std /path/STD.24M -eten24-spc /path/SPCFONT.24
wincv -eten24-std /path/stdfont.24f -eten24-spc /path/spcfont.24
```

第一行會解 ETUNPACK；第二行會直接載入國喬的相容裸字模。兩者都不隨
公開產物散布。

## WinCV 內建字形設定（CONFORMED）

設定選單提供下列 24 點 CJK 字形代號，並將選項寫入既有
`session.json` 的 `cjk24` 欄位：

| 代號 | 漢字區 | 符號區 |
|---|---|---|
| `eten-m` | `STD.24M` | `SPCFONT.24` |
| `eten-k` | `STD.24K` | `SPCFONT.24` |
| `eten-l` | `STD.24L` | `SPCFONT.24` |
| `eten-r` | `STD.24R` | `SPCFONT.24` |
| `eten-b` | `STD.24B` | `SPCFONT.24` |
| `eten-s` | `STD.24S` | `SPCFONT.24` |
| `guoqiao` | `stdfont.24f` | `spcfont.24` |

程式經 `WINCV_HOME` 與既有素材搜尋路徑尋找檔案。使用者選擇後必須先完整
載入、驗證並重建字形鏈；成功才立即套用並寫入 JSON。若檔案不存在、壓縮資料
截斷或格式無效，保留目前字形與原本代號，僅在狀態列報錯。舊 JSON 沒有
`cjk24` 時預設 `eten-m`，維持既有自動尋找 `STD.24M` 的行為。

驗證涵蓋 JSON round-trip、選單代號切換、`WINCV_HOME` 路徑解析，以及以真實
ETUNPACK／國喬素材先成功切換、再以缺失來源確認不覆寫目前字形的視窗層收據。
