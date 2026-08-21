# Android 版評估與規劃

WinCV Remake 目前跑 Linux / Windows / macOS。這份文件評估搬上 Android 的
可行性、要改什麼、以及分幾步做。

結論先講:**核心可以直接用,介面要重做一遍**。程式碼裡與 Android 衝突的
不是演算法而是兩件事——鍵盤與檔案系統。

## 已實測的部分

下面每一列都是跑過的結果,不是推測。重跑指令附在後面。

| 項目 | 結果 |
|---|---|
| `internal/...` 底下每一個套件 | `GOOS=android GOARCH=arm64 CGO_ENABLED=0` **編得過** |
| `cmd/celldump`(headless 全 app) | 同上,**編得過** |
| `cmd/wincv`(Ebiten) | 編不過。`GOOS=android` 時 Ebiten 會 import `github.com/ebitengine/gomobile/app` → `mobileinit`,那些檔案需要 cgo,關掉 cgo 就被 build constraint 排除 |
| Ebiten v2.8.8 的 Android 支援 | **有**。相依樹裡有 `github.com/ebitengine/gomobile v0.0.0-20240911145611-4856209ac325` |

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

Android 10 起是 scoped storage:app 預設不能拿 `os.ReadDir("/sdcard")` 亂走。

**但這個專案是私人 sideload,不上架**,所以走的是「所有檔案存取權」
(`MANAGE_EXTERNAL_STORAGE`)這條路:使用者在系統設定裡開一次,
`vfs.OS` 就照常用,一行都不用改。模擬器上實測過 —— 授權後
`/storage/emulated/0` 的目錄樹讀得出來。Storage Access Framework(SAF)
是上架 Play 才必須的替代路徑,那時 `vfs.FS` 會多一個實作。

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
| `vfs.Drives()` | 讀 `/proc/mounts` + `/media` `/Volumes` | Android 上那些讀得到但沒有意義。改成列 `/storage` 底下實際掛上的儲存 |
| `vfs.DiskUsage()` | `syscall.Statfs` | 已實測可用(狀態列顯示 `剩餘: 5,129MB / 5,939MB`)|
| `launch.Open` / `Run` | `xdg-open` / 直接執行 | Android 不能任意執行程式。`Open` 對應到 Intent(`ACTION_VIEW` + MIME),`Run` 直接拿掉 |
| 字級與倍率 | `Ctrl-+` / `Alt-+` | 手機螢幕密度差異大,啟動時依 DPI 選預設倍率;保留雙指縮放 |
| 磁碟窗格 | 左側 10 欄 | 手機畫面窄,改成抽屜式(從左側滑出) |

## 建議分期

**第一期:唯讀瀏覽器**(已定案 —— 使用者 2026-08-21 指示「唯讀即可」)

只做「看」:目錄瀏覽、文字檢視、markdown、看圖、縮圖、壓縮檔瀏覽、
16 進位、字典查詢。這一期不碰寫入,所以 `vfs.FS` 不用改介面;
配合「所有檔案存取權」連新實作都不用,`vfs.OS` 直接可用。
前面實測過的「`internal/...` 全部編得過」涵蓋的正好就是這一期需要的全部邏輯。

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

## 現況:在模擬器上跑起來了

```
dist/wincv-android.apk   34 MB   minSdk 21 / targetSdk 34
  lib/arm64-v8a/libgojni.so     17.2 MB
  lib/armeabi-v7a/libgojni.so   16.2 MB
  lib/x86/libgojni.so           16.5 MB
  lib/x86_64/libgojni.so        17.8 MB
```

`tools/build-android.sh` 產出,`tools/verify-apk.sh` 驗格式與內容,
`tools/run-android-emulator.sh` 驗行為。

實跑環境:Android 14 / API 34 / x86_64,Pixel 5 profile(1080×2340,440 dpi)。
截圖 `docs/ui/android-run-5s.png`。實測到的:

| 項目 | 結果 |
|---|---|
| 行程存活 | 啟動後 5 / 10 / 20 秒都在,沒有 panic |
| 目錄瀏覽 | 路徑列 `/storage/emulated/0/*.*`,13 個項目,目錄用綠色、`<DIR>`、日期時間都對 |
| 檔案系統權限 | 授予「所有檔案存取權」後讀得到共用儲存(不是退到 app 私有目錄)|
| `vfs.DiskUsage()` | 狀態列 `剩餘: 5,129MB / 5,939MB`,`syscall.Statfs` 在 Android 上可用 |
| 半形字模 | 用 `/system/fonts/DroidSansMono.ttf` 現場產,訊息列有講出來 |
| CJK | 畫得出來(標記 / 剩餘 / 拷貝 / 移動 / 磁碟 / 預視)|
| 觸控功能列 | 底部兩列都在,標籤隨模式換 |
| 格點 | `外部 392x777 dp, 格 49x48, 倍率 1, 畫布 392x768 px` —— 畫布寬剛好等於外部寬,沒有溢出。格子 8×16,與原版量出來的一樣 |

格點那一列的數字是程式自己印進 logcat 的(tag `GoLog`),不是從截圖上量的。
截圖看不出「右邊那一欄是被螢幕切掉,還是這個欄寬本來就會截斷長檔名」——
`docs/ui/android-grid-reference.png` 是桌面版在同一組格點下畫的同一個畫面,
兩張的欄位邊界一致。

**還沒實測的:觸控輸入本身。** 截圖證明「畫得出來、讀得到」,
不證明「點下去會動」—— 那要送觸控事件再比對前後畫面。

## 建置環境

`tools/docker/android.Dockerfile` + `tools/build-android.sh`,兩步:
`ebitenmobile bind` 產 AAR,`gradle assembleRelease` 產 APK。
image 約 5.5 GB,tag 固定 `wincv-android:1`。

**這台機器有其他專案的 Android image 與模擬器 image,一律不動。**

組這個工具鏈踩到的三個坑:

1. `gomobile@latest` 要的 Go 版本比 ebiten 相依的那一版新。
   把 gomobile / gobind 釘在 ebiten 自己相依的那一版(`go.mod` 裡的
   `github.com/ebitengine/gomobile`)。
2. 釘了還是不夠 —— **`ebitenmobile bind` 內部會跑 `gomobile init`,
   而那一步寫死 `go install gobind@latest`**,繞不過釘選。
   解法是再裝一份新的 Go 放在 PATH 前面,舊的留給 ebitenmobile 自己
   (它是已經編好的 binary,不受影響)。
3. Debian 套件庫的 gradle 是 4.x,吃不動 AGP 8,要抓官方 binary。
4. **gobind 把 Go 的 doc comment 原樣抄進產生的 Java**,而 javac 預設用
   平台的 charset 讀原始碼(容器裡是 US-ASCII)。本專案的註解是繁體中文,
   於是每個中文字都變成 `unmappable character`,一次噴 222 個錯。
   治標是把 `mobile` 套件的註解改成英文,但那是讓工具鏈決定文件語言;
   治本是 `JAVA_TOOL_OPTIONS=-Dfile.encoding=UTF-8`。
5. **32 位元 ABI(armeabi-v7a)會抓出 64 位元機器上看不見的錯**。
   實測抓到 `internal/archive/ace` 的 `rel = (rel - pos) & 0xFFFFFFFF`:
   那是平台的 `int`,在 64 位元上「剛好對」,在 32 位元上連編都編不過;
   就算編得過,那個遮罩在 32 位元上是**沒有作用的空操作** ——
   同一份程式碼在兩種機器上會解出不同的位元組,而兩邊都不會報錯。
   改成固定寬度的 `uint32` / `uint16` 之後,對 acefile 的 269 個成員
   重驗過 CRC-32 全數通過。

## 執行期的三個約束(Ebiten 的 Android view 決定的)

這三條都不是選擇,是 `ebitenmobile` 產生的 Java 那一層寫死的行為。
不照著做的症狀離原因都很遠,所以寫在這裡。

**一、`go.Seq.setContext()` 要自己呼叫。** gomobile bind 產生的原生層需要一個
Android `Context` 才能問系統拿東西,而這一步不會自動發生。沒做的話 Ebiten
拿不到 `DisplayMetrics`,`deviceScale` 就是 0;`EbitenView.onLayout` 用
「像素 ÷ deviceScale」算版面尺寸,得到 `+Inf`;Ebiten 再拿它乘上 0 去配置
畫布,得到 `NaN`,轉成 int 是 `INT64_MIN`,最後死在「NewImage 的寬必須是
正數」。整條鏈上沒有一步提到 Context。做法:`MainActivity.onCreate` 第一件事
就 `Seq.setContext(getApplicationContext())`。

**二、Activity 被重建一次就等於 app 結束。** `EbitenSurfaceView` 用一個
**static** 旗標記「這個行程建過幾次 GL surface」,第二次就判定 context 遺失,
`Log.e("Go", "The application was killed due to context loss")` 之後直接
`Runtime.getRuntime().exit(0)`。轉向、螢幕密度、深色模式、語言都會觸發重建。
做法:`configChanges` 把它們全部列進去,由 Activity 自己吃下來。
v2.8.8 沒有別條路 —— `StrictContextRestoration` 這個選項在 `toUIRunOptions`
裡從來沒被填進去(原始碼註解:"not used so far (#3098)"),從
`mobile.SetGameWithOptions` 打不開。

**三、`Layout` 不可以把收到的尺寸原樣傳回去。** Ebiten 規定它回正數,回不出來
就 panic。輸入本身可能是壞的(見第一條),所以要先過濾 ——
`internal/app.SaneLayout` 做這件事,`internal/app/layout_test.go` 用真的
出現過的那組值盯著它。要注意這一步只管好 `Layout` 這個介面的契約:
Ebiten 另外會拿**原始**的外部尺寸去配置畫布,那條路徑看不到回傳值。

## 在模擬器上實跑

```bash
tools/build-android.sh                                    # 產 dist/wincv-android.apk
APK=dist/wincv-android.apk tools/run-android-emulator.sh   # 裝進模擬器、截圖、收 logcat
```

產出在 `docs/ui/android-run-{5,10,20}s.png` 與 `docs/ui/android-logcat.txt`
(log 不進版控)。腳本借用這台機器上**別的專案**建立的模擬器 image,
只 `docker run --rm`,不 build / commit / tag / rmi,CPU 只拿 4 核。
換自己的 image 設 `EMU_IMAGE` / `EMU_AVD`。

兩件事寫進腳本本體而不是只寫在說明裡:

- **結尾驗產物**。截圖或 logcat 缺一個就 `exit 1`。這一段最容易出的錯是
  「容器裡的腳本根本沒跑,但 `docker run` 回 0」——
  `-i` 沒帶、或背景行程把 heredoc 從 stdin 吃掉,兩種都長成這樣。
  內層腳本因此掛成檔案跑,不走 stdin。
- **裝完等 15 秒再啟動**。剛裝完的那幾秒系統在跑安裝廣播,
  Activity 有機會被重建(見上面第二條)。不等的話量到的是模擬器的忙碌程度,
  不是 app 的行為。行程真的沒起來會重試一次並印出來,不會靜靜地當成成功。

另外:模擬器的 `/sdcard` 不讓 shell 使用者建檔(目錄建得出來、檔案回 EPERM,
`adb push` 甚至會回報成功但檔案不在)。腳本因此先 `adb root` ——
`google_apis` 的 image 可以,有 Play 商店的 image 不行,那時就只能瀏覽
裝置本來就有的目錄。

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
| ~~N1~~ | ~~`ebitenmobile bind` 跑得起來~~ | **已驗證**,APK 建得出來也驗得過 |
| ~~N5~~ | ~~app 跑得起來(畫得出畫面、讀得到檔案)~~ | **已驗證**,見上面的實測表。實機仍未試 |
| N2 | Ebiten 在 Android 上收得到軟鍵盤的字元事件 | 最小 app 叫出軟鍵盤,印出收到的事件。**目前 `mobile/mobile.go` 的 `Update` 只讀觸控,完全沒讀鍵盤** |
| N3 | SAF 的目錄樹可以包成 `vfs.FS`(每次 ReadDir 都要 JNI 往返,效能未知) | 用一個幾千個檔案的目錄量 ReadDir 的耗時。**只有上架 Play 才會用到** —— 私人 sideload 走「所有檔案存取權」,`vfs.OS` 已實測可用 |
| N4 | 格點在手機 DPI 下讀得下去(8×16 的格子在 6 吋 400dpi 螢幕上很小) | 440 dpi 的模擬器上讀得下去(格子被放大到約 22×44 實體像素)。實機的可讀性與手指點擊精度仍未試 |
