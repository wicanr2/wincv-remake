#!/usr/bin/env bash
# 在模擬器上實跑 APK,截圖並收 logcat。驗的是行為,不是格式
# (格式與內容由 tools/verify-apk.sh 驗)。
#
# 用法:
#   tools/run-android-emulator.sh [輸出目錄]
#
# ── 邊界(寫在這裡,不是寫在對話裡)────────────────────────────────
# 這支借用**別的專案**建立的模擬器 image。規則:
#   * 只 `docker run --rm`,容器用完即毀。
#   * 不 build、不 commit、不 tag、不 rmi、不 prune 那個 image。
#   * 不寫入 image 內的 AVD 以外的任何地方;AVD 的變更只落在
#     容器可寫層,隨 --rm 一起消失。
#   * CPU 只拿一部分(這台 14 核,取 4),因為上面同時有別人的工作。
# 換自己的 image 就設 EMU_IMAGE / EMU_AVD。
# ─────────────────────────────────────────────────────────────

set -euo pipefail
cd "$(dirname "$0")/.."

EMU_IMAGE=${EMU_IMAGE:-wolong-android-emulator:20260820}
EMU_AVD=${EMU_AVD:-wolong}
EMU_CPUS=${EMU_CPUS:-4}
APK=${APK:-dist-all/wincv-android.apk}
OUT=${1:-docs/ui}
PKG=tw.lcy.wincv

[ -f "$APK" ] || { echo "找不到 $APK,先跑 tools/build-android.sh 或 tools/release.sh" >&2; exit 1; }
[ -e /dev/kvm ] || { echo "沒有 /dev/kvm。沒有硬體加速的話 x86_64 模擬器慢到不實用" >&2; exit 1; }

mkdir -p "$OUT"
# 給模擬器一點東西可以瀏覽 —— 空目錄看起來像程式壞了,分不出
# 「讀不到檔案」和「畫不出畫面」。
SEED=$(mktemp -d)
trap 'rm -rf "$SEED"' EXIT
cp README.md CLAUDE.md whatsnew.txt "$SEED"/ 2>/dev/null || true
cp docs/ui/celldemo.png "$SEED"/ 2>/dev/null || true
mkdir -p "$SEED/sub"
printf '子目錄裡的檔案\n' > "$SEED/sub/inner.txt"   # 空目錄會讓 adb push 中途噴 EOF
printf '這是一個 Big5 測試檔\n' | iconv -f UTF-8 -t BIG5 > "$SEED/big5.txt" 2>/dev/null || true
(cd "$SEED" && zip -q test.zip README.md whatsnew.txt 2>/dev/null) || true

echo "==> 起容器($EMU_IMAGE,${EMU_CPUS} 核,--rm)"
docker run --rm \
    --log-opt max-size=10m --log-opt max-file=3 \
    --device /dev/kvm \
    --cpus "$EMU_CPUS" \
    -v "$PWD/$APK:/work/app.apk:ro" \
    -v "$SEED:/work/seed:ro" \
    -v "$PWD/tools/android-emulator-inner.sh:/work/inner.sh:ro" \
    -v "$PWD/$OUT:/out" \
    -e AVD="$EMU_AVD" -e PKG="$PKG" \
    "$EMU_IMAGE" bash /work/inner.sh

missing=0
for f in android-run-5s.png android-run-10s.png android-run-20s.png android-logcat.txt; do
    [ -s "$OUT/$f" ] || { echo "缺少或空的產物: $OUT/$f" >&2; missing=1; }
done
[ "$missing" = 0 ] || exit 1
echo "==> 輸出在 $OUT/android-run-*.png 與 $OUT/android-logcat.txt"
