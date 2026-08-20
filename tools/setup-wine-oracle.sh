#!/usr/bin/env bash
# 從原版安裝檔解出 WinCV,裝進一個專用的 Wine prefix,供 oracle 截圖使用。
#
# [HARD] prefix 一定放 $HOME (見 tools/oracle-shot.sh 的說明)。
# [HARD] innoextract 走 docker,不裝進系統 (docker run 一律帶 --rm 與 log 上限)。
set -euo pipefail

REPO=$(cd "$(dirname "$0")/.." && pwd)
INSTALLER=${1:-$REPO/wincv052a.exe}
PREFIX=${WINCV_PREFIX:-$HOME/.wine-wincv}
EXTRACT=$REPO/original/app

if [ ! -f "$INSTALLER" ]; then
    echo "找不到安裝檔 $INSTALLER" >&2
    exit 1
fi

if [ ! -d "$EXTRACT" ]; then
    mkdir -p "$REPO/original"
    docker run --rm --log-opt max-size=10m --log-opt max-file=3 \
        -v "$(dirname "$INSTALLER")":/in:ro -v "$REPO/original":/out \
        ubuntu:24.04 bash -c '
            apt-get update -qq >/dev/null 2>&1
            apt-get install -y -qq innoextract >/dev/null 2>&1
            innoextract -e -d /out "/in/'"$(basename "$INSTALLER")"'" >/dev/null
            chmod -R a+rwX /out'
fi

mkdir -p "$PREFIX"
WINEPREFIX=$PREFIX WINEDEBUG=-all wineboot -u >/dev/null 2>&1
mkdir -p "$PREFIX/drive_c/wincv"
cp "$EXTRACT"/* "$PREFIX/drive_c/wincv/"

echo "prefix : $PREFIX"
echo "app    : $PREFIX/drive_c/wincv"
echo "解出的原始素材: $EXTRACT"
