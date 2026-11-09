#!/usr/bin/env bash
# PDF 頁面渲染的三方對照:LibreOffice(真值)、Go 版 internal/pdf、Rust 候選 hayro。
#
#   tools/pdf-parity.sh [PDF...]      # 不給就用 internal/pdf/testdata/*.pdf
#
# 每份 PDF 的第 1 頁各畫一張(96 dpi),再用 tools/inkdiff 分別比
# Go 對 LibreOffice、hayro 對 LibreOffice 的墨水分布。PDF 先複製到
# .cache/parity/pdf/,LibreOffice 的輸出寫在那裡,不碰 testdata。
set -euo pipefail
REPO=$(cd "$(dirname "$0")/.." && pwd)
OUT=$REPO/.cache/parity/pdf
mkdir -p "$OUT"
[ $# -gt 0 ] || set -- "$REPO"/internal/pdf/testdata/*.pdf
names=()
for f in "$@"; do
    cp "$f" "$OUT/"
    names+=("$(basename "$f" .pdf)")
done

( cd "$OUT" && "$REPO/tools/office-oracle.sh" png "${names[@]/%/.pdf}" >/dev/null 2>&1 ) || true

gopdf=$(printf ' /src/.cache/parity/pdf/%s.pdf' "${names[@]}")
"$REPO/tools/go.sh" build -o /src/.cache/parity/pdf/gopdfshot ./tools/pdfshot
"$REPO/tools/go.sh" build -o /src/.cache/parity/pdf/inkdiff ./tools/inkdiff
"$REPO/tools/cargo.sh" build -q --release -p wincv-oracle --bin pdfshot

run_go() { docker run --rm --log-opt max-size=10m --log-opt max-file=3 --network none --cpus 2 --memory 2g \
    --pids-limit 256 -u "$(id -u):$(id -g)" -e HOME=/tmp -v "$REPO":/src -w /src \
    -v /usr/share/fonts:/usr/share/fonts:ro wincv-build:1 "$@"; }
run_rs() { docker run --rm --log-opt max-size=10m --log-opt max-file=3 --network none --cpus 2 --memory 2g \
    --pids-limit 256 -u "$(id -u):$(id -g)" -e HOME=/tmp -v "$REPO":/src -w /src \
    -v "$REPO/.cache/cargo-target":/cargo-target:ro wincv-rust:1 "$@"; }

printf '%-22s %-30s %-30s\n' "檔案" "Go:平均差/最大差/相關" "hayro:平均差/最大差/相關"
for n in "${names[@]}"; do
    p=/src/.cache/parity/pdf
    run_go $p/gopdfshot -o $p/$n.go.png -page 1 -dpi 96 $p/$n.pdf >/dev/null 2>"$OUT/$n.go.err" || true
    run_rs /cargo-target/release/pdfshot -o $p/$n.rs.png -page 1 -dpi 96 $p/$n.pdf >/dev/null 2>"$OUT/$n.rs.err" || true
    score() {
        [ -s "$OUT/$1" ] && [ -s "$OUT/$n.png" ] || { echo "沒有圖"; return; }
        # 只留「平均差 / 最大差 / 相關係數 / 判定」
        run_go $p/inkdiff $p/$n.png $p/$1 2>&1 | awk '
            /^平均密度差/ {m=$2} /^最大密度差/ {split($2,a,"("); x=a[1]} /^相關係數/ {c=$2}
            /^通過|^不通過|^未通過|^失敗/ {v=$1}
            END {printf "%s / %s / %s %s", m, x, c, v}' 
    }
    printf '%-22s %-30s %-30s\n' "$n" "$(score $n.go.png)" "$(score $n.rs.png)"
done
