#!/usr/bin/env bash
# Go 版與 Rust 版的 celldump 逐像素比對,跑多個場景。
#
#   tools/rust-parity.sh
#
# 兩邊用同一組原版字型(original/app/cvga.fon 與 original/eten/*.15),關掉
# 後備字型。場景有示範畫面與幾個檔案列表;檔案列表的時間依容器的時區
# (兩邊都沒設 TZ,即 UTC)。結果寫在 .cache/parity/,差異以 cell 格點列出。
set -euo pipefail
REPO=$(cd "$(dirname "$0")/.." && pwd)
OUT=.cache/parity
mkdir -p "$REPO/$OUT"
for f in original/app/cvga.fon original/eten/STDFONT.15 original/eten/SPCFONT.15; do
    [ -f "$REPO/$f" ] || { echo "缺少 $f(先跑 tools/setup-wine-oracle.sh 與 tools/setup-eten.sh)" >&2; exit 2; }
done
# 檔案列表比對用的目錄:tools/parity-fixture.py 以固定內容與時間戳產生。
if [ ! -d "$REPO/$OUT/fixture" ]; then
    docker run --rm --log-opt max-size=10m --log-opt max-file=3 --network none --memory 256m --cpus 1 \
        --pids-limit 64 -u "$(id -u):$(id -g)" -v "$REPO/$OUT":/p -v "$REPO/tools":/t:ro python:3.12-slim \
        python3 -I /t/parity-fixture.py /p/fixture
fi
FONTS=(-half /src/original/app/cvga.fon -eten-std /src/original/eten/STDFONT.15 -eten-spc /src/original/eten/SPCFONT.15)

# 場景:名稱|celldump 的額外參數
SCENES=(
    "demo|"
    "dir-docs|-dir /src/docs"
    "dir-testdata|-dir /src/testdata/lzh"
    "dir-root|-dir /src"
    "dir-fixture|-dir /src/$OUT/fixture"
    "dir-fixture-small|-dir /src/$OUT/fixture -cols 60 -rows 12"
)

"$REPO/tools/cargo.sh" build -q --release -p wincv-celldump
fail=0
for sc in "${SCENES[@]}"; do
    name=${sc%%|*}; args=${sc#*|}
    # shellcheck disable=SC2086
    "$REPO/tools/go.sh" run ./cmd/celldump "${FONTS[@]}" -no-fallback $args -o "/src/$OUT/$name.go.png" >/dev/null
    # shellcheck disable=SC2086
    "$REPO/tools/cargo.sh" run -q --release -p wincv-celldump --bin celldump -- "${FONTS[@]}" $args -o "/src/$OUT/$name.rust.png" >/dev/null
    printf '%-20s ' "$name"
    "$REPO/tools/cargo.sh" run -q --release -p wincv-celldump --bin pngdiff -- \
        "/src/$OUT/$name.go.png" "/src/$OUT/$name.rust.png" 8 16 | head -12 || fail=1
done
exit $fail
