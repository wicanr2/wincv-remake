#!/usr/bin/env bash
# 在建置容器裡跑 go。module cache 與 build cache 掛到 repo 底下的 .cache/,
# 不然每次都要重新下載相依套件(一次要一分多鐘)。
set -euo pipefail
REPO=$(cd "$(dirname "$0")/.." && pwd)
IMG=${BUILD_IMG:-wincv-build:1}
mkdir -p "$REPO/.cache/gomod" "$REPO/.cache/gobuild"
# WINCV_* 的環境變數要帶進容器:有幾個測試靠它們指語料位置或輸出路徑
# (WINCV_ACE_CORPUS、WINCV_RENDER_OUT)。不帶的話那些測試會安靜地跳過,
# 而「跳過」與「通過」在輸出上長得一樣。
envs=()
while IFS='=' read -r name _; do
    case "$name" in WINCV_*) envs+=(-e "$name") ;; esac
done < <(env)

exec docker run --rm --log-opt max-size=10m --log-opt max-file=3 \
    -v "$REPO":/src -w /src \
    -v "$REPO/.cache/gomod":/gomod -v "$REPO/.cache/gobuild":/gobuild \
    -v /usr/share/fonts:/usr/share/fonts:ro \
    -e GOFLAGS=-buildvcs=false -e GOCACHE=/gobuild -e GOMODCACHE=/gomod \
    "${envs[@]+"${envs[@]}"}" \
    "$IMG" go "$@"
