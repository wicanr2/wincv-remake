# 其他設定與資料檔

## `default.fil` — 檔區(書籤)

每行 `. "<路徑>"<空白><說明>`:

```
. "a:\"                     A:\ (軟碟)
. "c:\windows\"             Windows 目錄
. "c:\zip\"                 Zip 壓縮檔區
```

對應 image 內的字串「檔區的路徑 (如: C:\)」「檔區的名稱 (如: 備份一區)」
「確定要刪除此檔區的定義嗎?」。說明文字是 Big5。

## `dir.doc` — 目錄註解

每行 `<檔名><空白><註解>`:

```
fastkey.com  加快鍵盤速度 for dos
file_id.diz  WinCV介紹
whatsnew.txt 新增功能、版本演變
WinCV.txt    主說明檔
```

檔案列表的最右欄會顯示這個註解。註解檔的檔名可以在設定裡改
(image 字串:「設定 預設註解檔檔名」「註解檔檔名(不含路徑):」),
也有「檔案列表不列出dir.doc?」這個選項。

## `ce.ful` — 全形符號表

編輯器插入特殊符號用的對照表。每列是一組符號,以 `.` 結尾:

```
兙兛兞兝兡兣嗧瓩糎碁  ○●◤︽◥  ▏㎜.
１２３４５６７８９０  ⊙◎《  》  ▎㎝.
ㄅㄆㄇㄈㄉㄊㄋㄌㄍㄎㄏㄐㄑㄒㄓㄔㄕㄖㄗ.
```

內容 Big5,涵蓋單位符號、數字、注音、希臘字母、框線字元等。

## `file_id.diz` — 軟體簡介

BBS 時代的慣例檔案,純 Big5 文字,列出軟體的功能清單。
本專案用它當「原版有哪些功能」的清單來源。

## `CV.bat` / `CE.bat` / `WinCVins.bat`

安裝時產生的批次檔。`CV.bat` 開檢視、`CE.bat` 加 `/e` 開編輯器:

```bat
c:\wincv\WinCV.exe %1 %2 %3 %4 %5 %6 %7 %8 %9
c:\wincv\WinCV.exe /e %1 %2 %3 %4 %5 %6 %7 %8 %9
```

這證明**原版吃檔名參數**,而且有 `/e` 這個開關。
oracle 截圖要開特定檔案時走這條路,比用方向鍵把游標移過去可靠。

`WinCVins.bat` 裡有一行被註解掉的
`rem echo WinCVdir %1 ... >> %windir%\wincv.cfg`,
指向設定檔放在 `%windir%\wincv.cfg`(CLAUDE.md 假設 A7,還沒實測)。

## `big52gbk.txt` / `big52kor.txt` / `big52sjis.txt` / `gbk2big5.txt` / `kor2big5.txt` / `sjis.txt` / `big52pinyin.txt`

轉碼對照表。`big52gbk` `big52kor` `big52sjis` 三個檔的大小都是 69,865 bytes,
但內容不同 —— 同一套索引結構、不同的目標碼。

remake 目前不用這些表:`golang.org/x/text` 的 codec 已經涵蓋這幾種轉換,
而且經過 round-trip 測試。留著是為了日後比對「原版的轉換結果跟標準 codec
有沒有出入」——老軟體的對照表常有自訂的補充字。
