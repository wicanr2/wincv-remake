#!/usr/bin/env bash
# 把四個平台的產物整理進 dist-all/,附 SHA-256 與一份對照表。
#
# 為什麼要有這支而不是手動搬:發布物必須對得上**某一個 commit**。
# 手動搬最容易出的錯是「桌面版是上一版建的、APK 是這一版建的」——
# 檔案都在、大小也合理,但它們來自不同的原始碼,而這件事事後查不出來。
# 所以這支會拒絕在有未提交變更時執行,並把 commit 寫進 MANIFEST。
#
# 用法:
#   tools/release.sh              # 重建四個平台並整理進 dist-all/
#   tools/release.sh --no-build   # 只整理(產物必須已經是這個 commit 建的)
set -euo pipefail

REPO=$(cd "$(dirname "$0")/.." && pwd)
OUT=$REPO/dist-all
BUILD=1
[ "${1:-}" = "--no-build" ] && BUILD=0

cd "$REPO"
if [ -n "$(git status --porcelain)" ]; then
    echo "工作區有未提交的變更。發布物要對得上一個 commit,先提交再跑。" >&2
    git status --short >&2
    exit 1
fi
COMMIT=$(git rev-parse HEAD)
SHORT=$(git rev-parse --short HEAD)

if [ "$BUILD" = 1 ]; then
    echo "== 桌面三平台 =="
    "$REPO/tools/build-all.sh"
    echo "== Android =="
    "$REPO/tools/build-android.sh"
fi

rm -rf "$OUT"; mkdir -p "$OUT"
for f in wincv-linux-amd64 wincv-windows-amd64.exe wincv-darwin-universal wincv-android.apk; do
    [ -s "$REPO/dist/$f" ] || { echo "缺少 dist/$f" >&2; exit 1; }
    cp "$REPO/dist/$f" "$OUT/$f"
done

# 產物比 commit 舊的話,它是上一版建的 —— 那正是這支腳本要擋的錯。
CT=$(git log -1 --format=%ct)
for f in "$OUT"/*; do
    if [ "$(stat -c %Y "$f")" -lt "$CT" ]; then
        echo "$(basename "$f") 比 HEAD 還舊,是上一版建的。重跑不要加 --no-build。" >&2
        exit 1
    fi
done

( cd "$OUT" && sha256sum * > SHA256SUMS )

echo
echo "== 打包 =="
"$REPO/tools/package.sh"

# MANIFEST 描述的是**發布出去的東西**,所以要在打包之後才產生 ——
# 列裸執行檔的檔名會讓下載的人對不上 release 頁面上的 zip。
cat > "$OUT/MANIFEST.txt" <<EOF
WinCV Remake — 產物對照表

commit   $COMMIT
建置日期 $(date -u +%Y-%m-%d)（UTC）

$(cd "$OUT" && ls -la --time-style=+ ./*.zip 2>/dev/null | grep -v -- "-full" | awk '{printf "  %-46s %12s\n", substr($NF, 3), $5}')

檔案                                             平台
  wincv-remake-*-linux-amd64.zip                 Linux x86-64
  wincv-remake-*-windows-amd64.zip               Windows x86-64
  wincv-remake-*-macos-universal.zip             macOS（arm64 + x86_64 universal）
  wincv-remake-*-android.zip                     Android（四種 ABI，minSdk 21）

每個 zip 解開是一個目錄，裡面是執行檔加上 LICENSE、NOTICE 與 README.txt。

這四個產物**不含**原版的字型與資料檔——那些是第三方版權物，由使用者自備。
沒有它們也跑得起來：半形字改用系統的等寬字型現場產，尺寸照原版的
8×15 / 10×18 / 12×24，版面與按鍵行為完全一樣，只有字形不同。

校驗：sha256sum -c SHA256SUMS-zip
EOF

echo
echo "dist-all/（commit $SHORT）:"
ls -la --time-style=+ "$OUT" | tail -n +2 | awk '{printf "  %-46s %12s\n", $NF, $5}'
