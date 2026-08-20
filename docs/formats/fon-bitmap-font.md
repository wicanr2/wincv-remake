# `.FON` 點陣字型(NE / FNT 2.0)

WinCV 隨附四個半形點陣字型,以 `AddFontResource` 註冊後用 face 名指定。

| 檔案 | face | 字格 | ascent | 字元範圍 |
|---|---|---|---|---|
| `cvga.fon` | `cvga` | 8 × 15 | 11 | 0x00–0xFF |
| `CVGA1018.FON` | `cvga1018` | 10 × 18 | 16 | 0x00–0xFF |
| `cvga1224.FON` | `cvga1224` | 12 × 24 | 20 | 0x00–0xFF |
| `WinCV.fon` | `cvga` | 8 × 15 | 11 | 0x00–0xFF |

`WinCV.fon` 與 `cvga.fon` **位元組完全相同**(md5 `5ebe59a6…`),
只是安裝時放了兩個名字。

全部定寬、涵蓋整個單位元組空間、charset 欄位為 `0xFF`(OEM)。
**沒有全形字** —— 原版的中文是 Windows GDI 用系統字型畫的
(image 裡指名「新細明體」),所以「原版的中文字形」隨使用者的 Windows 而異。

## 容器:NE resource

檔案是 16-bit NE 執行檔,字型放在 `RT_FONT`(type 8)資源裡。

```
0x3C            WORD  → NE header 位移
NE+0x24         WORD  → resource table 相對 NE header 的位移
resource table:
  +0x00         WORD  alignment shift(資源位移要左移這麼多位)
  之後重複:
    WORD  type id(最高位元是旗標,要遮掉;8 = RT_FONT,0 = 結束)
    WORD  這個 type 有幾筆
    6 bytes 保留
    每筆 12 bytes:WORD 位移(<< shift)、WORD 長度(<< shift)、8 bytes 其他
```

## FNT 2.0 檔頭(相對資源起點)

```
0x00  WORD   dfVersion        0x0200
0x02  DWORD  dfSize
0x06  60B    dfCopyright
0x42  WORD   dfType
0x44  WORD   dfPoints
0x46  WORD   dfVertRes
0x48  WORD   dfHorizRes
0x4A  WORD   dfAscent
0x4C  WORD   dfInternalLeading
0x4E  WORD   dfExternalLeading
0x50  BYTE   dfItalic
0x51  BYTE   dfUnderline
0x52  BYTE   dfStrikeOut
0x53  WORD   dfWeight
0x55  BYTE   dfCharSet
0x56  WORD   dfPixWidth        0 = 變寬
0x58  WORD   dfPixHeight
0x5A  BYTE   dfPitchAndFamily
0x5B  WORD   dfAvgWidth
0x5D  WORD   dfMaxWidth
0x5F  BYTE   dfFirstChar
0x60  BYTE   dfLastChar
0x61  BYTE   dfDefaultChar
0x62  BYTE   dfBreakChar
0x63  WORD   dfWidthBytes
0x65  DWORD  dfDevice
0x69  DWORD  dfFace            → face 名字串的位移(相對資源起點)
0x6D  DWORD  dfBitsPointer
0x71  DWORD  dfBitsOffset
0x75  BYTE   dfReserved
0x76  dfCharTable:(dfLastChar − dfFirstChar + 2) 筆
        WORD  這個字的寬度
        WORD  字模資料的位移(相對資源起點)
```

**多出來的那一筆**是「哨兵」,用來算最後一個字的資料長度。

## 字模是 column-major

這是最容易寫錯的地方。**先存第 0 欄的全部列,再存第 1 欄**,
每 8 列一個 byte、MSB 在上。寬度超過 8 的字因此分成多個 8-pixel 欄組,
每組 `dfPixHeight` 個 byte。

```
byte(col_group, y) 位於  資料起點 + col_group * dfPixHeight + y
第 x 欄的位元       = byte(x/8, y) & (0x80 >> (x % 8))
```

一般點陣字型是 row-major,照那個寫會得到轉置過的字 ——
而轉置過的字**看起來還是有東西**,不會空白,所以很容易以為解對了。

## 怎麼驗

`cvga` 的 `'A'` 應該長這樣(8×15):

```
........
........
........
....#...
...###..
..##.##.
.##...##
.##...##
.#######
.##...##
.##...##
.##...##
........
........
........
```

兩支獨立實作互為對照:`tools/fnt.py`(探索用)與 `internal/fnt`(執行期用),
`internal/fnt/fnt_test.go` 的 `TestGlyphA` 把上面那個字模寫死在測試裡。

```sh
python3 tools/fnt.py info  original/app/cvga.fon
python3 tools/fnt.py glyph original/app/cvga.fon 0x41
python3 tools/fnt.py atlas original/app/cvga.fon /tmp/atlas.pbm
```
