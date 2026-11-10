#!/usr/bin/env bash
# 重新產生 rust/THIRD-PARTY-LICENSES.txt。改了 rust/Cargo.lock 之後要跑一次。
set -euo pipefail
REPO=$(cd "$(dirname "$0")/.." && pwd)
TMP="$REPO/.cache/rust-licenses"
mkdir -p "$TMP"
"$REPO/tools/cargo.sh" tree -p wincv -e normal,build --prefix none --format '{p}' --target all > "$TMP/tree.txt"
"$REPO/tools/cargo.sh" metadata --format-version 1 > "$TMP/meta.json"
# 讀 cargo 下載的原始碼:CARGO_HOME 掛在 .cache/cargo-home,metadata 裡的路徑是容器內的 /cargo-home。
timeout 120 docker run --rm --network none --log-opt max-size=10m --log-opt max-file=3 \
    --memory 512m --cpus 1 --pids-limit 64 -u "$(id -u):$(id -g)" \
    -v "$REPO":/src -v "$REPO/.cache/cargo-home":/cargo-home:ro -w /src python:3.12-slim \
    python3 -I /src/tools/rust-licenses.py /src/.cache/rust-licenses/tree.txt \
        /src/.cache/rust-licenses/meta.json /src/rust/THIRD-PARTY-LICENSES.txt
