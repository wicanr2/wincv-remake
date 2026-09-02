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

# 容器內用主機的 UID/GID 跑,不然產出的檔案屬於 root,主機這邊改不動也刪不掉。
# HOME 要指到容器內寫得進去的地方:go 會想寫 $HOME/.config/go/env,
# 而 root 的家目錄對這個 UID 是唯讀的。
# 這台機器是共用的。WINCV_CPUS / WINCV_MEM 設了就限制額度,不設就不限制。
limits=()
[ -n "${WINCV_CPUS:-}" ] && limits+=(--cpus "$WINCV_CPUS")
[ -n "${WINCV_MEM:-}" ] && limits+=(--memory "$WINCV_MEM")

exec docker run --rm --log-opt max-size=10m --log-opt max-file=3 \
    "${limits[@]+"${limits[@]}"}" \
    -u "$(id -u):$(id -g)" -e HOME=/tmp \
    -v "$REPO":/src -w /src \
    -v "$REPO/.cache/gomod":/gomod -v "$REPO/.cache/gobuild":/gobuild \
    -v /usr/share/fonts:/usr/share/fonts:ro \
    -e GOFLAGS=-buildvcs=false -e GOCACHE=/gobuild -e GOMODCACHE=/gomod \
    "${envs[@]+"${envs[@]}"}" \
    "$IMG" go "$@"
