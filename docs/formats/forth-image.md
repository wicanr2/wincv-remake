# `WINCV.IMG`(Win32Forth v4 STC dictionary image)

WinCV 的應用邏輯全部在這裡。`wincv.exe` 只是 Win32Forth 的 kernel 與載入器。

## 整體版面

```
0x000000            image header(10 個 dword)
0x000000–0x12334c   code space:3663 個 STC word body
0x122794–0x186618   header space:9497 筆 word header(名稱 + xt)
執行期再往上延伸(觀察到 xt 值超過 image 長度,如 0x1f3cc0)
```

## image header

| offset | 值 | 判讀 |
|---|---|---|
| 0x00 | `0x00000000` | — |
| 0x04 | `0x019211d5` | 疑似 magic / checksum(未驗證) |
| 0x08 | `0x0012334c` | code space 結束 |
| 0x0c | `0x00122794` | **header space 起點**(已用來成功走訪 9497 筆) |
| 0x10 / 0x14 | `0x00063e85` / `0x00063e84` | header space 大小 |
| 0x20 | `0x0040c158` | app base hint(未驗證) |
| 0x24 | `0x00400000` | image base |

## word body(code space)

```
addr+0 : dword = addr+4          ← code field,指向自己的下一個位元組
addr+4 : 83 ED 04 8F 45 00       ← 序言
         …本體…
         83 C5 04 FF 65 FC       ← EXIT
```

**「dword 值等於自身位址 +4」就是掃 word 邊界的判準**,全 image 命中 3663 次,
其中 3633 個後面接著標準序言。

## STC 呼叫慣例(由指令序列推導)

| 暫存器 | 角色 | 證據 |
|---|---|---|
| `ESP` | 資料堆疊(第二層以下) | `53` push ebx 存舊 TOS |
| `EBX` | **TOS cache** | `53 BB 40 00 00 00` = push ebx; mov ebx,0x40 → 推入字面值 |
| `EBP` | **回傳堆疊指標**,向下成長 | 序言 `83 ED 04 8F 45 00` = sub ebp,4; pop [ebp] |
| `EDI` | 資料區基底 | `8B 9F 70 41 00 00` = mov ebx,[edi+0x4170] |

序言的意思是「把 x86 `call` 壓在資料堆疊上的返回位址搬到 Forth 的回傳堆疊」;
EXIT 是 `add ebp,4; jmp [ebp-4]`。

## header record(header space)

```
FF FF FF FF | 00 padding | name chars | count byte | dword seq | dword f2 | dword xt
```

**count 在名稱之後**,不是前綴;對齊靠前方的 zero padding。
`seq` 是定義順序流水號,`f2` 語意未確認(疑似 vocabulary / hash link)。

9497 筆、8957 個唯一名稱,其中 3509 筆的 `xt` 直接命中 code body
(其餘是常數、變數、VALUE 這類 xt 落在資料區的 word)。

名稱帶應用層語意,例如 `BIG5-SEARCH`、`DICTWIN`、`KKPHONE`、
`VP-JPG-TRANSFORM`、`MARK-START`、`ED-LINE`、`COMMENTSTATE`。
前綴 `VF-` / `EF-` / `VP-` 疑似對應 view-file / edit-file / view-picture。

## Big5 字串

UI 文字是 Forth 的 counted string。光用「合法 Big5 位元組」當判準的話,
15761 個候選裡絕大多數是 x86 指令碰巧落在合法範圍;
**加上「前一個 byte 等於字串長度」這道檢查後收斂到 1293 個**。

## 工具

```sh
python3 tools/forth_image.py header  original/app/WINCV.IMG
python3 tools/forth_image.py symbols original/app/WINCV.IMG > docs/re/symbols.tsv
python3 tools/forth_image.py words   original/app/WINCV.IMG > docs/re/words.tsv
python3 tools/img_strings.py original/app/WINCV.IMG --min 8 > docs/re/big5-strings.tsv
```

## IDA 的用法

反組譯對象是 **`WINCV.IMG`,不是 `wincv.exe`**。

1. 以 flat binary 載入,**base 設 0**(image 內部位址就是 image-relative,
   已由「dword == addr+4」全數命中佐證)。
2. IDAPython 讀 `docs/re/words.tsv`,對每個 `addr+4` 下 `MakeFunction`。
3. IDAPython 讀 `docs/re/symbols.tsv`,對 xt 命中 code body 者套用名稱。
