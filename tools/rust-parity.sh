#!/usr/bin/env bash
# Go 版與 Rust 版的 celldump 示範畫面逐像素比對。
#
# 兩邊用同一組原版字型(original/app/cvga.fon 與 original/eten/*.15),
# 關掉後備字型。結果寫在 .cache/parity/,差異以 cell 格點列出。
set -euo pipefail
REPO=$(cd "$(dirname "$0")/.." && pwd)
OUT=.cache/parity
mkdir -p "$REPO/$OUT"
for f in original/app/cvga.fon original/eten/STDFONT.15 original/eten/SPCFONT.15; do
    [ -f "$REPO/$f" ] || { echo "缺少 $f(先跑 tools/setup-wine-oracle.sh 與 tools/setup-eten.sh)" >&2; exit 2; }
done
FONTS=(-half /src/original/app/cvga.fon -eten-std /src/original/eten/STDFONT.15 -eten-spc /src/original/eten/SPCFONT.15)

"$REPO/tools/go.sh" run ./cmd/celldump "${FONTS[@]}" -no-fallback -o "/src/$OUT/go.png"
"$REPO/tools/cargo.sh" run -q --release -p wincv-celldump --bin celldump -- "${FONTS[@]}" -o "/src/$OUT/rust.png"
"$REPO/tools/cargo.sh" run -q --release -p wincv-celldump --bin pngdiff -- "/src/$OUT/go.png" "/src/$OUT/rust.png" 8 16
