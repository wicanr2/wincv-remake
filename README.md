# WinCV Remake

把 2011 年的台灣共享軟體 **WinCV 0.52（CView for Windows）** 逆向，
以 Go + Ebiten 重寫成 Linux / Windows / macOS 都能跑的版本。

原作者 Lcc Wizard（林健總）。本專案為私人使用，不對外散布。

---

## 這是什麼

WinCV 是一支「什麼都做一點」的中文工具：檔案與壓縮檔管理、文字瀏覽、
PE2 式區塊編輯器、HEX 編輯、看圖與縮圖、Big5/GB/SJIS/KOR 轉碼、
英漢字典與 KK 音標、MD5 與 SFV 檢驗。它用 Win32Forth 寫成，
畫面是自繪的固定格點介面。

remake 保留那個格點介面與點陣字，不做成現代 GUI。

## 目前能做什麼

| 功能 | 狀態 |
|---|---|
| 檔案列表：游標、標記、排序、進出目錄 | 可用 |
| 壓縮檔當目錄瀏覽 | ZIP / TAR / GZ / BZ2 / RAR / 7z |
| 文字檢視：編碼自動判讀、ANSI 彩色、自動換行、搜尋 | 可用 |
| 16 進位檢視（字元欄以 Big5 判讀） | 可用 |
| 看圖 | JPEG / PNG / GIF / BMP / TIFF / WebP / PCX / TGA / PNM / ICO |
| 縮圖列表 | 可用 |
| PE2 式區塊編輯器 + 語法上色 | 可用 |
| 英漢字典 + KK 音標 | 可用 |
| MD5 / SFV、換行與編碼轉換、去 HTML/ANSI、批次改名 | 函式庫完成，UI 待接 |
| 檔案操作（拷貝／移動／改名／刪除） | 未做 |
| LZH / ARJ / ACE / CAB / .Z / ARC | 未做 |

## 跑起來

需要兩份外部素材：

1. **原版的半形點陣字型**（`cvga.fon`）與設定檔。
   從原版安裝檔取得：

   ```sh
   tools/setup-wine-oracle.sh    # 解出 original/app/
   ```

2. **倚天中文系統的點陣字庫**（全形字）。
   從倚天 3.53 的光碟映像取得：

   ```sh
   tools/setup-eten.sh           # 解出 original/eten/
   ```

兩份都是有版權的第三方素材，不隨程式散布。

```sh
tools/build-all.sh              # 產出 dist/ 三平台執行檔
dist/wincv-linux-amd64 ~/       # 開始瀏覽某個目錄
```

## 按鍵

完整的快捷鍵表在 [`docs/ui/keymap.md`](docs/ui/keymap.md)，
每一條都標了證據等級：取自程式自己的按鍵標籤（附 offset）、
取自說明檔、或還沒查證的推測。

常用的幾個：

```
↑↓ PgUp PgDn Home End   移動
Enter                   進目錄 / 看檔 / 進壓縮檔 / 看圖
BackSpace               上一層
Space                   標記
T / Alt-T / U           標記所有檔案 / 含目錄 / 解除
E                       編輯
5                       縮圖列表
H                       文字 ↔ 16 進位
Esc                     回上一層（永遠不會直接關掉程式）
```

編輯器裡：`Alt-B` 矩形區塊、`Alt-L` 整列、`Alt-Z/M/D/F` 拷貝／移動／刪除／填充、
`Ctrl-C/X/V` 剪貼、`Ctrl-U` 復原、`Ctrl-S` 存檔、`F7` 字典視窗。

## 專案文件

| 檔案 | 內容 |
|---|---|
| [`CLAUDE.md`](CLAUDE.md) | 逆向與重寫的完整規劃、已查證事實、待驗假設 |
| [`CONTEXT.md`](CONTEXT.md) | 統一語言、已被推翻的斷言、決策紀錄、進度 |
| [`docs/formats/`](docs/formats/) | 每一種資料檔的格式規格 |
| [`docs/ui/keymap.md`](docs/ui/keymap.md) | 快捷鍵表（分三級證據） |
| [`docs/re/`](docs/re/) | 逆向產出：符號表、word 位址、Big5 字串表 |

## 工具

```sh
tools/setup-wine-oracle.sh   解原版安裝檔 + 建 Wine prefix
tools/oracle-shot.sh         跑原版並截圖（版面與配色的真值來源）
tools/setup-eten.sh          從倚天光碟抽字庫
tools/forth_image.py         解 WINCV.IMG 的 header / 符號表 / word 位址
tools/img_strings.py         抽 image 內的 Big5 字串
tools/fnt.py                 解 .FON 點陣字型
tools/celldump               把任一畫面渲染成 PNG（不需要顯示器）
tools/remake-shot.sh         把真實 Ebiten 視窗跑在 Xvfb 上截圖
tools/build-all.sh           三平台打包
tools/verify-dist.sh         打包產物的靜態驗收
```

## 開發

一律走 docker：

```sh
docker build -f Dockerfile.build -t wincv-build:1 .
docker run --rm -v "$PWD":/src -w /src wincv-build:1 go test ./internal/...
```

測試盡量用**原版自己的檔案**當 golden data，而不是自己造的樣本 ——
自己造的樣本只驗得到自己的理解。
