#!/usr/bin/env bash
# 建立可公開散布的桌面版發布樹。完整版必須另由 build-full.sh 建在
# dist-all/<版本>/full-local，絕不可進 GitHub Release。
set -euo pipefail

REPO=$(cd "$(dirname "$0")/.." && pwd)
VERSION=${1:?用法: tools/release.sh v.主版.次版.修訂版-YYYYMMDD [--no-build]}
BUILD=1
[ "${2:-}" = --no-build ] && BUILD=0
case "$VERSION" in
    v.[0-9]*.[0-9]*.[0-9]-????????) ;;
    *) echo "不合規的版本號:$VERSION" >&2; exit 2 ;;
esac

cd "$REPO"
if [ -n "${WINCV_RELEASE_COMMIT:-}" ]; then
    COMMIT=$WINCV_RELEASE_COMMIT
else
    [ -z "$(git status --porcelain)" ] || {
        echo "工作區有未提交變更；發布物必須對應一個 commit。" >&2
        exit 1
    }
    COMMIT=$(git rev-parse HEAD)
fi

OUT=$REPO/dist-all/$VERSION/patch
STAMP=$REPO/dist/BUILT-FROM
if [ "$BUILD" = 1 ]; then
    "$REPO/tools/build-all.sh"
    printf '%s\n' "$COMMIT" > "$STAMP"
fi
[ -s "$STAMP" ] || { echo "dist/ 沒有建置印記" >&2; exit 1; }
[ "$(cat "$STAMP")" = "$COMMIT" ] || { echo "dist/ 並非此 commit 的產物" >&2; exit 1; }

mkdir -p "$OUT"
for f in wincv-linux-amd64 wincv-windows-amd64.exe wincv-darwin-universal; do
    [ -s "$REPO/dist/$f" ] || { echo "缺少 dist/$f" >&2; exit 1; }
    cp -p "$REPO/dist/$f" "$OUT/$f"
done
(cd "$OUT" && sha256sum wincv-linux-amd64 wincv-windows-amd64.exe wincv-darwin-universal > SHA256SUMS)
VERSION="$VERSION" OUT="$OUT" "$REPO/tools/package.sh" public

cat > "$OUT/MANIFEST.txt" <<EOF
WinCV Remake $VERSION

commit   $COMMIT
建置日期 $(date -u +%Y-%m-%d)（UTC）

公開產物：Linux x86-64、Windows x86-64、macOS universal（arm64 + x86_64）。
這三份僅含可散布的引擎與文件，未嵌入原版、倚天或國喬字型；使用者可自行
提供具合法取得權的字型。SHA256SUMS-zip 是 GitHub Release 上傳檔的校驗表。
EOF
echo "公開發布樹: $OUT"
