# 語法上色設定(`keyword.cfg` / `keyword_*.cfg`)

## `keyword.cfg`:副檔名 → 設定檔

```
f       keyword_f.cfg
fs      .

cpp     keyword_c.cfg
c       .
h       .
rc      .
```

值為 `.` 表示**沿用上一行指到的設定檔**。上面這段讓 `.c` `.h` `.rc`
共用 `keyword_c.cfg`。空行忽略。

## `keyword_*.cfg`:一種語言的設定

```
<設定鍵><空白><值>        ← 檔頭
=====[任意說明文字]        ← 分隔線
<關鍵字><空白><顏色名>     ← 之後每行一個
```

**分隔線不只一條。** `keyword_csharp.cfg` 有 7 條、`keyword_f.cfg` 有 12 條,
`=====` 後面的文字是關鍵字的分類標題(`===== Block word set`、
`========= c# keywords`)。只有第一條的意義是「檔頭到此結束」,
其餘純粹是分類。

`keyword_f.cfg` 的第一行就是 `=====Block word set` —— 它**沒有檔頭**。

### 檔頭的設定鍵

| 鍵 | 意義 |
|---|---|
| `QuoteColor` | 字串的顏色 |
| `NumberColor` | 數字的顏色 |
| `LineCommentStart` | 行註解的起始字串。可以是空值(表示沒有行註解) |
| `OnlyBeginLineComment` | `true` 時行註解只有在行首(可有前置空白)才算 |
| `KeywordDelimiter` | 斷詞字元,用引號包住 |

`keyword_csharp.cfg` 與 `keyword_java.cfg` 的檔頭放的是一份**顏色名清單**
(`black black` / `ltorange ltorange` …),那是給使用者參考用的,不是設定。
這兩個檔因此沒有 `QuoteColor` 與 `NumberColor` —— 原版在這兩種語言下
就是不上字串與數字的色。

### 顏色名

值是 WinCV 自己的 29 個具名顏色(見 `internal/cell` 與 CLAUDE.md §1)。
不在清單裡的名字會被忽略。

### 跨行註解

**設定檔裡沒有這一項。** 但 image 的符號表有 `COMMENTSTATE`、
`END-COMMENT$`、`"CHECK-END-COMMENT`,表示原版有跨行註解的狀態機。
既然沒有設定可讀,推測是依語言寫死的。remake 目前在 `LineCommentStart`
為 `//` 時套用 C 家族的 `/* */`(CLAUDE.md 假設 A12)。

## 怎麼驗

`internal/syntax` 的測試直接讀 `original/app/keyword.cfg` 那一整組,
確認 `.c .h .cpp .rc .java .xml .ini .iss .txt .bat` 都有對應設定、
`.h` 與 `.c` 指到**同一個** Config 物件(`.` 的沿用機制)、
C 的關鍵字表超過 100 個。
