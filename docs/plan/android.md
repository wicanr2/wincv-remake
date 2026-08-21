# Android 版評估與規劃

WinCV Remake 目前跑 Linux / Windows / macOS。這份文件評估搬上 Android 的
可行性、要改什麼、以及分幾步做。

結論先講:**核心可以直接用,介面要重做一遍**。程式碼裡與 Android 衝突的
不是演算法而是兩件事——鍵盤與檔案系統。

## 已實測的部分

下面每一列都是跑過的結果,不是推測。重跑指令附在後面。

| 項目 | 結果 |
|---|---|
| `internal/...` 全部 33 個套件 | `GOOS=android GOARCH=arm64 CGO_ENABLED=0` **編得過** |
| `cmd/celldump`(headless 全 app) | 同上,**編得過** |
| `cmd/wincv`(Ebiten) | 編不過。`GOOS=android` 時 Ebiten 會 import `golang.org/x/mobile/app` → `mobileinit`,那些檔案需要 cgo,關掉 cgo 就被 build constraint 排除 |
| Ebiten v2.6.6 的 Android 支援 | **有**。相依樹裡有 `golang.org/x/mobile v0.0.0-20230922142353-e2f452493d57` |

```bash
docker run --rm -v $PWD:/src -w /src -e CGO_ENABLED=0 wincv-build:1 \
    sh -c 'GOOS=android GOARCH=arm64 go build ./internal/... ./cmd/celldump'
```

意思是:**壓縮檔解碼、圖檔解碼、字型解析、格點渲染、markdown 排版、
編碼轉換、字典、瀏覽器邏輯這一整套已經是可攜的**,一行都不用改。
`cmd/celldump` 編得過這件事特別有價值 —— 它會跑完整的 app 層
(選單、模式切換、按鍵分派),等於整個互動流程都通過了跨平台編譯。

## 兩個真正的問題

### 一、介面是鍵盤驅動的

整套操作繞著按鍵轉:`F1` 選單、`Alt-D` 磁碟窗格、`Alt-P` 預視、
`Ctrl-+` 字級、編輯器的 `Alt-A` 到 `Alt-Z`。Android 沒有實體鍵盤,
軟鍵盤又會吃掉半個畫面。

這不是「加一排按鈕」就能解決的,因為原版的手感建立在
「一個鍵一個動作、不用離開清單」。把它換成觸控要重新想:

- **清單**:上下滑捲動、點一下移游標、點兩下進入、長按標記。
- **功能鍵**:畫面底部一條可捲動的功能列,內容隨模式換
  (瀏覽時是 拷貝/移動/更名/刪除,檢視時是 尋找/編碼/中英文)。
  這條列取代 `F1` 選單當主要入口,選單保留給不常用的。
- **修飾鍵**:不做。`Alt-X` 這種組合在觸控上沒有等價物,
  該做的是把那些動作放進功能列,不是模擬一個 Alt 鍵。
- **文字輸入**:第一期只做唯讀,所以只有「尋找」需要輸入。
  叫系統軟鍵盤,Ebiten 在 Android 上收得到字元事件 —— **這一條還沒實測**。

架構上幫得上忙的是 `internal/keys`:它已經是「與後端無關的按鍵表示」,
觸控事件可以翻成同一組 `keys.Key` 再餵給 `app.HandleKey`,
不必動 app 那一層的任何分派邏輯。

### 二、Android 的檔案系統

Android 10 起是 scoped storage:app 不能拿 `os.ReadDir("/sdcard")` 亂走,
要透過 Storage Access Framework(SAF)由使用者授權目錄。

好消息是 `vfs.FS` 介面只有三個方法:

```go
type FS interface {
    ReadDir(dir string) ([]Entry, error)
    Open(name string) (io.ReadCloser, error)
    Label(dir string) string
}
```

壓縮檔瀏覽已經走這個介面,所以「SAF 當成另一種 FS」在設計上是成立的 ——
這正是當初把 vfs 做成 deep module 的回報。

壞消息是**寫入沒有走這個介面**。實際盤點(排除測試檔):

| 套件 | 直接呼叫 os 的寫入 |
|---|---|
| `app` | 30 |
| `fileop`(拷貝/移動/刪除/更名) | 14 |
| `archive`(解壓縮到目錄) | 12 |
| `search` | 8 |
| `note`(dir.doc 註解) | 4 |
| `checksum` / `dict` | 各 1 |

所以 Android 版要嘛把 `vfs.FS` 擴充成含寫入的介面(影響面大但一次做完),
要嘛第一版只做唯讀。**建議第一版唯讀** —— 理由在下面的分期。

## 其他要處理的

| 項目 | 現況 | Android 要怎麼辦 |
|---|---|---|
| 字型 | 執行檔旁邊放 `cvga.fon`、`original/eten/STDFONT.15` | 那兩份是第三方版權物,不能打包進 APK。要做一次性的匯入流程,存進 app 私有目錄 |
| `vfs.Drives()` | 讀 `/proc/mounts` + `/media` `/Volumes` | Android 上那些讀得到但沒有意義(scoped storage)。改成列 SAF 授權過的根目錄 |
| `vfs.DiskUsage()` | `syscall.Statfs` | Android 是 Linux,statfs 可用;但 scoped storage 路徑上的數字未必是使用者預期的那顆儲存 |
| `launch.Open` / `Run` | `xdg-open` / 直接執行 | Android 不能任意執行程式。`Open` 對應到 Intent(`ACTION_VIEW` + MIME),`Run` 直接拿掉 |
| 字級與倍率 | `Ctrl-+` / `Alt-+` | 手機螢幕密度差異大,啟動時依 DPI 選預設倍率;保留雙指縮放 |
| 磁碟窗格 | 左側 10 欄 | 手機畫面窄,改成抽屜式(從左側滑出) |

## 建議分期

**第一期:唯讀瀏覽器**(已定案 —— 使用者 2026-08-21 指示「唯讀即可」)

只做「看」:目錄瀏覽、文字檢視、markdown、看圖、縮圖、壓縮檔瀏覽、
16 進位、字典查詢。這一期不碰寫入,所以 `vfs.FS` 不用改介面,
只要多一個 SAF 實作。前面實測過的「`internal/...` 全部編得過」
涵蓋的正好就是這一期需要的全部邏輯。

驗收:用 `cmd/celldump` 的同一批畫面在 Android 上截圖比對 ——
渲染器是同一份,所以格點應該逐格相同。

**第二期:觸控介面成形**

底部功能列、手勢、抽屜式磁碟窗格、軟鍵盤輸入。這一期的產出應該包含
一份 `docs/ui/touch-map.md`,把每個原版按鍵對到觸控動作,
與 `docs/ui/keymap.md` 並列。

**第三期:寫入**

**目前不做。** 使用者 2026-08-21 指示唯讀即可,所以這一期沒有排程,
留在這裡是為了記下「如果哪天要做,要動的是什麼」:
`vfs.FS` 擴充成含寫入,`fileop` / `note` / `archive` 解壓改走它。
這一期會動到桌面版的程式碼,所以要有桌面版的回歸測試護著才動。

## 觸控功能列(已實作)

第二期的功能列先做出來了,桌面版加 `-touch` 就看得到,
截圖在 README。做成真的程式碼而不是示意圖,草案與實作之間才不會有落差。

```bash
tools/go.sh run ./cmd/celldump -app <目錄> -cols 44 -rows 62 -touch -o out.png
```

兩個設計決定寫在 `internal/app/touch.go`:

- **功能列隨模式換內容**。做成固定的一組鍵會退化成一個小鍵盤,
  那是把桌面介面硬搬過來,不是移植。
- **修飾鍵不放上來**。`Alt-X` 在觸控上沒有等價物,
  該做的是把動作本身放進功能列,不是模擬一個 Alt 鍵。
- 導覽列的**格位**固定、標籤隨模式換。位置固定是為了讓拇指記得住,
  但讀文件時擺一個「標記」按鈕是按了沒反應的按鈕,那比位置變動更糟。

## 建置路線

Ebiten 的 Android 產物是 AAR,由 `ebitenmobile bind` 產生,再放進一個
Android Studio 專案。需要 Android NDK 與 gomobile,兩者在 Linux 上
都裝得起來 —— 不需要 Mac(與 macOS 版不同)。

沿用專案既有的紀律:編譯一律走 docker,做成 `tools/build-android.sh`,
與 `tools/build-all.sh` 並列。

## 建置環境(已做出來)

`tools/docker/android.Dockerfile` + `tools/build-android.sh`,兩步:
`ebitenmobile bind` 產 AAR,`gradle assembleRelease` 產 APK。
image 約 5.5 GB,tag 固定 `wincv-android:1`。

**這台機器有其他專案的 Android image 與模擬器 image,一律不動。**

組這個工具鏈踩到的三個坑:

1. `gomobile@latest` 要 Go >= 1.25,但 ebiten v2.6.6 要配 Go 1.22。
   把 gomobile / gobind 釘在 ebiten v2.6.6 自己相依的那一版。
2. 釘了還是不夠 —— **`ebitenmobile bind` 內部會跑 `gomobile init`,
   而那一步寫死 `go install gobind@latest`**,繞不過釘選。
   解法是再裝一份新的 Go 放在 PATH 前面,舊的留給 ebitenmobile 自己
   (它是已經編好的 binary,不受影響)。
3. Debian 套件庫的 gradle 是 4.x,吃不動 AGP 8,要抓官方 binary。

## 字型:APK 裡沒有原版字型

原版的 `cvga.fon` 與倚天字庫是第三方版權物,**不打包進 APK**。
所以 `internal/render.Rasterizer.Half` 從具體型別改成介面
(`HalfSource`),多一個 `internal/ttf.HalfFont` 用系統 TrueType
現場產半形字模 —— 對光柵器來說兩者是同一件事。

Android 的系統字型在 `/system/fonts`,讀得到但不能改。優先挑等寬的
(`DroidSansMono`),半形字模拿等寬字產出來的格點最整齊。

使用者把 `cvga.fon` 放進 `<根目錄>/wincv/` 就會換成原版點陣字,
啟動時的訊息列會講現在用的是哪一種 —— 不講的話他會以為畫面本來就長這樣。

## 還沒驗證的假設

寫進來是為了不要被當成已知事實。動到哪一條先驗那一條。

| # | 假設 | 怎麼驗 |
|---|---|---|
| ~~N1~~ | ~~`ebitenmobile bind` 跑得起來~~ | **已驗證**,見上面的建置環境 |
| N2 | Ebiten 在 Android 上收得到軟鍵盤的字元事件 | 最小 app 叫出軟鍵盤,印出收到的事件 |
| N3 | SAF 的目錄樹可以包成 `vfs.FS`(每次 ReadDir 都要 JNI 往返,效能未知) | 用一個幾千個檔案的目錄量 ReadDir 的耗時 |
| N4 | 格點在手機 DPI 下讀得下去(8×16 的格子在 6 吋 400dpi 螢幕上很小) | 用 celldump 產同尺寸 PNG,在實機上看 |
