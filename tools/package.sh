#!/usr/bin/env bash
# 將一個版本目錄內的三個桌面產物壓成可攜 zip。
#
# 用法:
#   VERSION=v.0.53.0-20260921 OUT=dist-all/$VERSION/patch tools/package.sh public
#   VERSION=v.0.53.0-20260921 OUT=dist-all/$VERSION/full-local tools/package.sh full-local
#
# public 只可含可散布的引擎；full-local 可能含第三方字型，只能留在本機。
set -euo pipefail

REPO=$(cd "$(dirname "$0")/.." && pwd)
VERSION=${VERSION:?需要 VERSION}
OUT=${OUT:?需要 OUT}
MODE=${1:-public}

case "$VERSION" in
    v.[0-9]*.[0-9]*.[0-9]-????????) ;;
    *) echo "不合規的版本號:$VERSION" >&2; exit 2 ;;
esac
case "$MODE" in public|full-local) ;; *) echo "模式只能是 public 或 full-local" >&2; exit 2 ;; esac
[ -d "$OUT" ] || { echo "找不到輸出目錄:$OUT" >&2; exit 1; }

if [ "$MODE" = public ]; then
    PLATFORMS=(
        "wincv-linux-amd64:wincv:linux-amd64"
        "wincv-windows-amd64.exe:wincv.exe:windows-amd64"
        "wincv-darwin-universal:wincv:macos-universal"
    )
    suffix=""
    sums=SHA256SUMS-zip
else
    PLATFORMS=(
        "wincv-linux-amd64-full:wincv:linux-amd64"
        "wincv-windows-amd64-full.exe:wincv.exe:windows-amd64"
        "wincv-darwin-universal-full:wincv:macos-universal"
    )
    suffix="-full-local"
    sums=SHA256SUMS-zip-full-local
fi

cd "$OUT"
rm -f "$sums"
zips=()
for entry in "${PLATFORMS[@]}"; do
    src=${entry%%:*}; rest=${entry#*:}
    exe=${rest%%:*}; plat=${rest#*:}
    [ -s "$src" ] || { echo "缺少 $OUT/$src" >&2; exit 1; }

    dir="wincv-remake-$plat$suffix"
    zip="wincv-remake-$VERSION-$plat$suffix.zip"
    rm -rf "$dir"
    rm -f "$zip"
    mkdir -p "$dir"
    cp "$src" "$dir/$exe"
    chmod 755 "$dir/$exe"
    cp "$REPO/LICENSE" "$REPO/NOTICE" "$dir/"
    "$REPO/tools/readme-for.sh" "$plat" "$exe" > "$dir/README.txt"
    if [ "$MODE" = full-local ]; then
        cat >> "$dir/README.txt" <<'EOF'

本機完整版
----------

此版本嵌入原版半形點陣字型、倚天 15／24 點字庫及國喬字形。這些素材的
散布權未在本專案確認，僅限本機持有者使用；不得上傳 GitHub Release、
不得轉交或重新散布。
EOF
    fi
    [ "$plat" = windows-amd64 ] && sed -i 's/$/\r/' "$dir/README.txt"
    go run "$REPO/tools/zipdir" "$zip" "$dir"
    rm -rf "$dir"
    zips+=("$zip")
    printf '  %-62s %10s\n' "$zip" "$(stat -c%s "$zip")"
done
sha256sum "${zips[@]}" > "$sums"
echo "校驗: cd $OUT && sha256sum -c $sums"
