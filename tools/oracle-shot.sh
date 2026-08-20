#!/usr/bin/env bash
# 用 Wine + Xvfb 跑原版 WinCV 並截圖,當作 remake 的 pixel 對照 oracle。
#
# [HARD] WINEPREFIX 一定要放在 $HOME 底下。放 /tmp 會讓 winex11 driver 載入失敗,
#        症狀是 "nodrv_CreateWindow ... no driver could be loaded",看起來像缺 X
#        或缺 i386 函式庫,實際上 X 與函式庫都正常 (2026-08-20 實測)。
#
# 用法:
#   tools/oracle-shot.sh                       # 開主畫面,20 秒後截圖
#   tools/oracle-shot.sh out.png 30 "Down Down Return"
#                                              # 截圖檔、等待秒數、xdotool 按鍵序列
set -euo pipefail

OUT=${1:-original/ref-shots/main.png}
WAIT=${2:-20}
KEYS=${3:-}

PREFIX=${WINCV_PREFIX:-$HOME/.wine-wincv}
APPDIR=$PREFIX/drive_c/wincv
REPO=$(cd "$(dirname "$0")/.." && pwd)
OUT=$(cd "$(dirname "$OUT")" 2>/dev/null && pwd)/$(basename "$OUT") || OUT=$REPO/$OUT

if [ ! -f "$APPDIR/wincv.exe" ] && [ ! -f "$APPDIR/WinCV.exe" ]; then
    echo "找不到 $APPDIR/WinCV.exe,先跑 tools/setup-wine-oracle.sh" >&2
    exit 1
fi

TMP=$(mktemp -d)
trap 'rm -rf "$TMP"' EXIT

cat > "$TMP/run.sh" <<EOS
cd "$APPDIR"
wine wincv.exe >/dev/null 2>&1 &
WPID=\$!
sleep $WAIT
if [ -n "$KEYS" ]; then
    for k in $KEYS; do xdotool key --clearmodifiers "\$k"; sleep 0.4; done
    sleep 1
fi
xwd -root -silent > "$TMP/shot.xwd"
kill \$WPID 2>/dev/null || true
wineserver -k 2>/dev/null || true
EOS

WINEPREFIX=$PREFIX WINEDEBUG=-all \
    xvfb-run -a -s "-screen 0 1024x768x24" bash "$TMP/run.sh"

python3 "$REPO/tools/xwd2png.py" "$TMP/shot.xwd" "$OUT"
echo "$OUT"
