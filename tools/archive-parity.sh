#!/usr/bin/env bash
# Go 版與 Rust 版的壓縮檔解碼對照:每個檔案比 sha256,並量整段時間。
#
#   tools/archive-parity.sh <語料目錄> <相對於語料目錄的壓縮檔>...
#
# 語料目錄以唯讀掛進容器,壓縮檔不會被複製或修改。兩個容器都限 2 核,
# 計時是打開、列目錄、解出全部檔案重做 ROUNDS 次(預設 5)取最快。
# 結果在 .cache/parity/archive/。
#
# 完整比對(檔名 + 內容)不同時,再只比內容(大小與 sha256 的多重集合):
# 檔名的編碼由 vfs 層決定(RAR 的 Unicode 檔名、OEM 檔名),與解碼器
# 對不對是兩回事,兩種差異分開報。
set -euo pipefail
REPO=$(cd "$(dirname "$0")/.." && pwd)
CORPUS=$(cd "${1:?語料目錄}" && pwd); shift
OUT=$REPO/.cache/parity/archive
mkdir -p "$OUT"
files=()
for f in "$@"; do
    [ -f "$CORPUS/$f" ] || { echo "沒有 $CORPUS/$f" >&2; exit 2; }
    files+=("/corpus/$f")
done
ROUNDS=${ROUNDS:-5}
common=(-e ROUNDS="$ROUNDS" --rm --log-opt max-size=10m --log-opt max-file=3 --network none --cpus 2 --memory 4g
    --pids-limit 512 -u "$(id -u):$(id -g)" -e HOME=/tmp -v "$REPO":/src -v "$CORPUS":/corpus:ro)

docker run "${common[@]}" -w /src \
    -v "$REPO/.cache/gomod":/gomod -v "$REPO/.cache/gobuild":/gobuild \
    -e GOFLAGS=-buildvcs=false -e GOCACHE=/gobuild -e GOMODCACHE=/gomod -e GOPROXY=off \
    wincv-build:1 sh -c 'go build -o /tmp/archsum ./tools/archsum && /tmp/archsum -n "$ROUNDS" "$@"' sh "${files[@]}" \
    > "$OUT/go.tsv" 2> "$OUT/go.err" || true
docker run "${common[@]}" -w /src/rust \
    -v "$REPO/.cache/cargo-home":/cargo-home -e CARGO_HOME=/cargo-home \
    -v "$REPO/.cache/cargo-target":/cargo-target -e CARGO_TARGET_DIR=/cargo-target \
    wincv-rust:1 sh -c 'cargo build -q --release -p wincv-oracle && /cargo-target/release/archsum -n "$ROUNDS" "$@"' sh "${files[@]}" \
    > "$OUT/rust.tsv" 2> "$OUT/rust.err" || true

echo "--- Go";   cat "$OUT/go.err"
echo "--- Rust"; cat "$OUT/rust.err"
echo "--- 比對:Go $(wc -l < "$OUT/go.tsv") 列,Rust $(wc -l < "$OUT/rust.tsv") 列"
if cmp -s "$OUT/go.tsv" "$OUT/rust.tsv"; then echo "相同"; exit 0; fi
cut -f2,3 "$OUT/go.tsv" | sort > "$OUT/go.content"
cut -f2,3 "$OUT/rust.tsv" | sort > "$OUT/rust.content"
if cmp -s "$OUT/go.content" "$OUT/rust.content"; then
    echo "內容相同(大小與 sha256 全部一致),檔名有 $(diff <(cut -f1 "$OUT/go.tsv" | sort) <(cut -f1 "$OUT/rust.tsv" | sort) | grep -c '^<') 個不同"
    exit 0
fi
echo "內容不同:"
diff "$OUT/go.content" "$OUT/rust.content" | head -20
exit 1
