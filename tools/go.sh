#!/usr/bin/env bash
# 在建置容器裡跑 go。module cache 與 build cache 掛到 repo 底下的 .cache/,
# 不然每次都要重新下載相依套件(一次要一分多鐘)。
set -euo pipefail
REPO=$(cd "$(dirname "$0")/.." && pwd)
IMG=${BUILD_IMG:-wincv-build:1}
mkdir -p "$REPO/.cache/gomod" "$REPO/.cache/gobuild"
exec docker run --rm --log-opt max-size=10m --log-opt max-file=3 \
    -v "$REPO":/src -w /src \
    -v "$REPO/.cache/gomod":/gomod -v "$REPO/.cache/gobuild":/gobuild \
    -e GOFLAGS=-buildvcs=false -e GOCACHE=/gobuild -e GOMODCACHE=/gomod \
    "$IMG" go "$@"
