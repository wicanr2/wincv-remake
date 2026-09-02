#!/usr/bin/env bash
# 建「完整版」:把字型打包進執行檔,四個平台都做,輸出到 dist-all/ 的 *-full。
#
# 為什麼要分兩種產物:原版的 `.FON` 與倚天字庫是第三方版權物,
# **不能進對外散布的檔案**。所以 tools/release.sh 建的是不含字型的版本
# (跑起來退到系統 TrueType),這一支建的是含字型的版本,只留在本機。
#
# [HARD] 這裡產出的東西**不要上傳到 release、不要進版控**。
#        dist-all/ 本來就在 .gitignore 裡,但上傳是手動動作,不會有人擋你。
#
# 用法:
#   tools/build-full.sh              # 四個平台
#   tools/build-full.sh desktop      # 只做桌面三個
#   tools/build-full.sh android      # 只做 APK
#
# 兩支腳本共用 dist-all/,但各自只動自己的檔案(這一支只碰 *-full*),
# 所以先跑哪一支都可以。

set -euo pipefail

REPO=$(cd "$(dirname "$0")/.." && pwd)
OUT=$REPO/dist-all
ASSETS=$REPO/internal/bundled/assets
BUILD_IMG=${BUILD_IMG:-wincv-build:1}
OSX_IMG=${OSX_IMG:-wincv-osxcross-go:1}
# 空的話由 image 自己說(osxcross-conf)。工具前綴帶 SDK 的次版號
# (SDK 15.5 → darwin24.5,不是 darwin24),寫死的話換一次 SDK 就整批
# 工具找不到,而 clang 只會轉述成 "unable to execute command"。
OSX_TARGET=${OSX_TARGET:-}
WHAT=${1:-all}

# 字型素材交給 tools/embed-fonts.sh 準備(公開版與完整版共用同一份清單,
# 差別只在 full 多一組 Unicode 後備)。
#
# 素材只在建置期間存在。留著會讓下一次不帶 tag 的建置看起來也有字型,
# 而那是錯覺 —— 沒有 tag 的話那個目錄根本不會被 embed。
cleanup() { "$REPO/tools/embed-fonts.sh" clean; }
trap cleanup EXIT
"$REPO/tools/embed-fonts.sh" full

mkdir -p "$OUT"

# 每一步都要確認產物真的生出來(理由同 build-all.sh:docker 包了好幾層 shell,
# 中間吞掉非零回傳值時整支腳本會若無其事地跑完)。
expect() {
    [ -s "$OUT/$1" ] || { echo "!! $1 沒有產生出來" >&2; exit 1; }
    echo "   $1  $(du -h "$OUT/$1" | cut -f1)"
}

# 容器內用主機的 UID/GID 跑,不然產出的檔案屬於 root,主機這邊改不動也刪不掉。
# HOME 要指到容器內寫得進去的地方:go 會想寫 $HOME/.config/go/env,
# 而 root 的家目錄對這個 UID 是唯讀的。

# 這台機器是共用的,上面同時有別的專案在跑。WINCV_CPUS / WINCV_MEM 設了就
# 把容器限制在那個額度內 —— 負載高的時候用得上,平常不設就是不限制。
#
#   WINCV_CPUS=2 WINCV_MEM=6g tools/build-full.sh
LIMITS=""
[ -n "${WINCV_CPUS:-}" ] && LIMITS="$LIMITS --cpus $WINCV_CPUS"
[ -n "${WINCV_MEM:-}" ] && LIMITS="$LIMITS --memory $WINCV_MEM"
DOCKER="docker run --rm --log-opt max-size=10m --log-opt max-file=3
        $LIMITS
        -u $(id -u):$(id -g) -e HOME=/tmp
        -v $REPO:/src -w /src
        -e GOFLAGS=-buildvcs=false -e GOCACHE=/tmp/gocache -e GOMODCACHE=/tmp/gomod"

if [ "$WHAT" = all ] || [ "$WHAT" = desktop ]; then
    echo "=== linux/amd64(含字型)==="
    $DOCKER "$BUILD_IMG" sh -c '
        CGO_ENABLED=1 go build -tags fonts -trimpath -ldflags "-s -w" \
            -o /src/dist-all/wincv-linux-amd64-full ./cmd/wincv'
    expect wincv-linux-amd64-full

    echo "=== windows/amd64(含字型)==="
    $DOCKER "$BUILD_IMG" sh -c '
        CGO_ENABLED=1 GOOS=windows GOARCH=amd64 CC=x86_64-w64-mingw32-gcc \
        go build -tags fonts -trimpath -ldflags "-s -w -H windowsgui" \
            -o /src/dist-all/wincv-windows-amd64-full.exe ./cmd/wincv'
    expect wincv-windows-amd64-full.exe

    echo "=== darwin universal(含字型)==="
    # [雷] 架構名有兩套:Go 的 GOARCH 用 amd64,osxcross 的工具前綴用 x86_64。
    $DOCKER -e OSX_TARGET="$OSX_TARGET" "$OSX_IMG" bash -c "
        set -e
        export PATH=/osxcross/bin:\$PATH
        T=\${OSX_TARGET:-\$(osxcross-conf | sed -n 's/^export OSXCROSS_TARGET=//p')}
        build() {
            CGO_ENABLED=1 GOOS=darwin GOARCH=\$1 \
                CC=\$2-apple-\$T-clang CXX=\$2-apple-\$T-clang++ \
                go build -tags fonts -trimpath -ldflags '-s -w' \
                -o /src/dist-all/wincv-darwin-\$1-full ./cmd/wincv
        }
        build arm64 arm64
        build amd64 x86_64
        x86_64-apple-\$T-lipo -create \
            /src/dist-all/wincv-darwin-arm64-full /src/dist-all/wincv-darwin-amd64-full \
            -output /src/dist-all/wincv-darwin-universal-full
        rm -f /src/dist-all/wincv-darwin-arm64-full /src/dist-all/wincv-darwin-amd64-full"
    expect wincv-darwin-universal-full
fi

if [ "$WHAT" = all ] || [ "$WHAT" = android ]; then
    echo "=== android(含字型)==="
    TAGS=fonts APK_OUT="$OUT/wincv-android-full.apk" "$REPO/tools/build-android.sh"
    expect wincv-android-full.apk
fi

# 只列檔案。`./*-full*` 會連解開來的目錄一起吃進去,sha256sum 對目錄
# 會印一行錯誤然後繼續 —— 校驗檔少一列,而腳本看起來跑完了。
( cd "$OUT" && find . -maxdepth 1 -type f -name '*-full*' ! -name 'SHA256SUMS-full' \
    -printf '%P\n' | sort | xargs sha256sum > SHA256SUMS-full )

cat > "$OUT/README-full.txt" <<EOF
完整版(含字型)—— 本機保留,不對外散布

  wincv-linux-amd64-full
  wincv-windows-amd64-full.exe
  wincv-darwin-universal-full
  wincv-android-full.apk

與同目錄不帶 -full 的產物差別只有一件事:這些把字型嵌進執行檔,
不必另外放檔案就是點陣像素對齊的畫面。嵌的是:

  原版半形    cvga.fon / CVGA1018.FON / cvga1224.FON
  倚天全形    STDFONT.15 / SPCFONT.15 / SPCFSUPP.15
  Unicode 後備 Noto Sans CJK 與幾份語系子集(fb-*)

前兩組是第三方版權物。**這幾個檔不要上傳到 release,也不要轉給別人。**
要給別人的是不帶 -full 的版本,由對方自己準備字型。
(fb-* 是 SIL Open Font License,那一部分本身可以散布。)

建置:tools/build-full.sh
commit:$(cd "$REPO" && git rev-parse HEAD)
EOF

echo
echo "dist-all/ 的完整版:"
ls -la --time-style=+ "$OUT" | grep -E "full" | awk '{printf "  %-36s %10s\n", $NF, $5}'
echo
echo "校驗:cd dist-all && sha256sum -c SHA256SUMS-full"

# 怎麼確認字型真的進去了(檔案大小差不算證據)
# ------------------------------------------------------------------
# Linux 版:把三條字型路徑都指到不存在的目錄,還畫得出中文就是內建生效。
#
#   WINCV_FONT_DIR=/nope/ WINCV_ETEN_DIR=/nope/ ./dist-all/wincv-linux-amd64-full
#
# Android 版:沒有那個開關,改在原生庫裡找字庫的位元組。
#
#   unzip -p dist-all/wincv-android-full.apk lib/arm64-v8a/libgojni.so > /tmp/f.so
#   unzip -p dist-all/wincv-android.apk      lib/arm64-v8a/libgojni.so > /tmp/p.so
#   docker run --rm --log-opt max-size=10m --log-opt max-file=3 \
#     -v "$PWD:/w" -v /tmp:/t -w /t python:3.12-slim python3 -c '
#   import pathlib
#   fp = pathlib.Path("/w/original/eten/STDFONT.15").read_bytes()[200000:200064]
#   for n in ("p.so","f.so"):
#       print(n, pathlib.Path(n).read_bytes().count(fp))'
#
#   公開版要是 0、完整版要是 1。
#
# [雷] 這一步不要用 grep -a -F -f pattern.bin。字模裡有 0x00,
# grep 會在那裡把 pattern 截斷,於是**兩個 APK 都命中五萬次** ——
# 看起來像「兩邊都有字型」,實際上是查詢本身壞了。
# 回滿跟回空一樣需要先做對照組:公開版必須是 0,那個 0 才讓完整版的 1 有意義。
