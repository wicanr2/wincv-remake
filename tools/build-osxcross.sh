#!/usr/bin/env bash
# 建 macOS 交叉編譯用的 image。平常不用跑 —— 只有第一次或要換 SDK 版本時。
#
# [HARD] 建完的 image 不要 push。裡面含 Apple 的 macOS SDK。
set -euo pipefail

REPO=$(cd "$(dirname "$0")/.." && pwd)
IMG=${OSX_IMG:-wincv-osxcross-go:1}

docker build -f "$REPO/tools/docker/osxcross.Dockerfile" -t "$IMG" "$REPO/tools/docker"

echo
echo "=== 工具鏈自述(target 前綴由這裡決定,腳本不寫死)==="
docker run --rm --network none --log-opt max-size=10m --log-opt max-file=3 \
    -u "$(id -u):$(id -g)" -e HOME=/tmp "$IMG" osxcross-conf
echo
docker run --rm --network none --log-opt max-size=10m --log-opt max-file=3 \
    -u "$(id -u):$(id -g)" -e HOME=/tmp "$IMG" go version
