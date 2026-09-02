#!/usr/bin/env bash
# 桌面三平台打包。產物一律放 dist/。Android 在 tools/build-android.sh。
#
#   linux/amd64    wincv-build:1 原生編
#   windows/amd64  同一個 image 的 mingw-w64 交叉編
#   darwin universal (arm64 + x86_64)  osxcross image 交叉編後 lipo
#
# Ebiten 需要 cgo,所以三個平台都不能用 CGO_ENABLED=0。
# macOS 的工具鏈與踩雷清單見 ~/.claude/skills/osxcross-macos-cross-build/SKILL.md。
set -euo pipefail

REPO=$(cd "$(dirname "$0")/.." && pwd)
DIST=$REPO/dist
BUILD_IMG=${BUILD_IMG:-wincv-build:1}
OSX_IMG=${OSX_IMG:-wincv-osxcross-go:1}
# 空的話由 image 自己說(osxcross-conf)。工具前綴帶 SDK 的次版號
# (SDK 15.5 → darwin24.5,不是 darwin24),寫死的話換一次 SDK 就整批
# 工具找不到,而 clang 只會轉述成 "unable to execute command"。
OSX_TARGET=${OSX_TARGET:-}

# 容器內用主機的 UID/GID 跑,不然產出的檔案屬於 root,主機這邊改不動也刪不掉。
# HOME 要指到容器內寫得進去的地方:go 會想寫 $HOME/.config/go/env,
# 而 root 的家目錄對這個 UID 是唯讀的。

# 這台機器是共用的,上面同時有別的專案在跑。WINCV_CPUS / WINCV_MEM 設了就
# 把容器限制在那個額度內 —— 負載高的時候用得上,平常不設就是不限制。
#
#   WINCV_CPUS=2 WINCV_MEM=6g tools/build-all.sh
LIMITS=""
[ -n "${WINCV_CPUS:-}" ] && LIMITS="$LIMITS --cpus $WINCV_CPUS"
[ -n "${WINCV_MEM:-}" ] && LIMITS="$LIMITS --memory $WINCV_MEM"
DOCKER="docker run --rm --log-opt max-size=10m --log-opt max-file=3
        $LIMITS
        -u $(id -u):$(id -g) -e HOME=/tmp
        -v $REPO:/src -w /src
        -e GOFLAGS=-buildvcs=false -e GOCACHE=/tmp/gocache -e GOMODCACHE=/tmp/gomod"

# 公開版也嵌原版的 .FON 與倚天字庫:解開就是與原版逐像素對齊的畫面,
# 不必自己準備。授權與出處見 NOTICE。
# 完整版(tools/build-full.sh)另外多嵌一組 Unicode 後備。
"$REPO/tools/embed-fonts.sh" base
cleanup() { "$REPO/tools/embed-fonts.sh" clean; }
trap cleanup EXIT

mkdir -p "$DIST"

# 每一步都要確認產物真的生出來。
#
# 光看回傳值不夠:docker run 包了好幾層 shell,中間一層吞掉非零回傳值時
# 整個腳本會若無其事地跑完,而 dist/ 裡少一個檔 —— 這種靜默失敗比直接
# 報錯難發現得多(實際踩過一次)。
expect() {
    if [ ! -s "$DIST/$1" ]; then
        echo "!! $1 沒有產生出來" >&2
        exit 1
    fi
    echo "   $1  $(stat -c%s "$DIST/$1" | numfmt --to=iec 2>/dev/null || stat -c%s "$DIST/$1") bytes"
}

echo "=== linux/amd64 ==="
$DOCKER "$BUILD_IMG" sh -c '
    CGO_ENABLED=1 go build -tags fonts -trimpath -ldflags "-s -w" -o /src/dist/wincv-linux-amd64 ./cmd/wincv'
expect wincv-linux-amd64

echo "=== windows/amd64 ==="
# -H windowsgui 讓它不要開一個主控台視窗。
$DOCKER "$BUILD_IMG" sh -c '
    CGO_ENABLED=1 GOOS=windows GOARCH=amd64 CC=x86_64-w64-mingw32-gcc \
    go build -tags fonts -trimpath -ldflags "-s -w -H windowsgui" -o /src/dist/wincv-windows-amd64.exe ./cmd/wincv'
expect wincv-windows-amd64.exe

echo "=== darwin universal ==="
# [雷] 架構名有**兩套**:Go 的 GOARCH 用 amd64,osxcross 的工具前綴用 x86_64。
# 混用會得到 "unsupported GOOS/GOARCH pair darwin/x86_64",而且因為它印在
# go build 的輸出裡、不影響回傳值,整個腳本會若無其事地繼續跑完。
$DOCKER -e OSX_TARGET="$OSX_TARGET" "$OSX_IMG" bash -c "
    set -e
    export PATH=/osxcross/bin:\$PATH
    T=\${OSX_TARGET:-\$(osxcross-conf | sed -n 's/^export OSXCROSS_TARGET=//p')}
    build() {   # \$1=GOARCH  \$2=工具鏈前綴
        CGO_ENABLED=1 GOOS=darwin GOARCH=\$1 \
            CC=\$2-apple-\$T-clang CXX=\$2-apple-\$T-clang++ \
            go build -tags fonts -trimpath -ldflags '-s -w' -o /src/dist/wincv-darwin-\$1 ./cmd/wincv
    }
    build arm64 arm64
    build amd64 x86_64
    x86_64-apple-\$T-lipo -create \
        /src/dist/wincv-darwin-arm64 /src/dist/wincv-darwin-amd64 \
        -output /src/dist/wincv-darwin-universal
    rm -f /src/dist/wincv-darwin-arm64 /src/dist/wincv-darwin-amd64"
expect wincv-darwin-universal

echo
ls -lh "$DIST"
echo
echo "驗收:tools/verify-dist.sh"
