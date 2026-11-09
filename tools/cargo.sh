#!/usr/bin/env bash
# 在 wincv-rust:1 容器裡跑 cargo,工作目錄是 rust/。
#
# 相依套件與建置快取掛到 repo 底下的 .cache/(已在 .gitignore),
# 不然每次都要重新下載與重編。
#
# 預設不開網路:相依套件要先抓過一次。
#   WINCV_NET=1 tools/cargo.sh fetch      # 第一次或改了 Cargo.toml 之後
#   tools/cargo.sh test                   # 平常
set -euo pipefail
REPO=$(cd "$(dirname "$0")/.." && pwd)
IMG=${RUST_IMG:-wincv-rust:1}
mkdir -p "$REPO/.cache/cargo-home" "$REPO/.cache/cargo-target"
net=(--network none)
[ -n "${WINCV_NET:-}" ] && net=()
# WINCV_* 帶進容器(例如 fonts 功能的 WINCV_FONT_DIR),與 tools/go.sh 相同。
envs=()
while IFS='=' read -r name _; do
    case "$name" in WINCV_NET|WINCV_CPUS|WINCV_MEM) ;; WINCV_*) envs+=(-e "$name") ;; esac
done < <(env)
# 系統字型唯讀掛進去(與 tools/go.sh 相同):後備字型的測試與 Go 對照才看得到同一批字型。
limits=(--cpus "${WINCV_CPUS:-4}" --memory "${WINCV_MEM:-6g}" --pids-limit 1024)
exec docker run --rm --log-opt max-size=10m --log-opt max-file=3 \
    "${net[@]+"${net[@]}"}" "${limits[@]}" "${envs[@]+"${envs[@]}"}" \
    -u "$(id -u):$(id -g)" -e HOME=/tmp \
    -v "$REPO":/src -w /src/rust \
    -v /usr/share/fonts:/usr/share/fonts:ro \
    -v "$REPO/.cache/cargo-home":/cargo-home -e CARGO_HOME=/cargo-home \
    -v "$REPO/.cache/cargo-target":/cargo-target -e CARGO_TARGET_DIR=/cargo-target \
    "$IMG" cargo "$@"
