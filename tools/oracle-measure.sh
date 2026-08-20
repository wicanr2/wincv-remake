#!/usr/bin/env bash
# 量測原版主畫面的格點:視窗幾何 + 只截視窗本體 + Wine 選了哪個字型。
#
# 為什麼不從整張桌面截圖去數格子:桌面截圖包含視窗外框與標題列,
# 邊界要用肉眼估。改成問 X 拿 client area 的實際尺寸,再除以
# .FON 自己宣告的字身大小(8x15,已由 internal/fnt 與 tools/fnt.py
# 互為對照驗過),算出來的欄列數才不依賴 Wine 怎麼畫字。
set -euo pipefail

OUT=${1:-docs/ui/oracle-window.png}
WAIT=${2:-20}
PREFIX=${WINCV_PREFIX:-$HOME/.wine-wincv}
APPDIR=$PREFIX/drive_c/wincv
REPO=$(cd "$(dirname "$0")/.." && pwd)
case "$OUT" in /*) ;; *) OUT=$REPO/$OUT ;; esac

TMP=$(mktemp -d); trap 'rm -rf "$TMP"' EXIT

cat > "$TMP/run.sh" <<EOS2
cd "$APPDIR"
wine wincv.exe >"$TMP/wine.log" 2>&1 &
sleep $WAIT
WIN=\$(xdotool search --name "WinCV" | head -1)
if [ -z "\$WIN" ]; then WIN=\$(xdotool search --onlyvisible --class "" 2>/dev/null | head -1); fi
{
  echo "=== xdotool getwindowname ==="
  xdotool getwindowname "\$WIN" 2>&1 || true
  echo "=== xwininfo ==="
  xwininfo -id "\$WIN" 2>&1 || true
  echo "=== 所有可見視窗 ==="
  for w in \$(xdotool search --onlyvisible --name "." 2>/dev/null); do
     echo "-- \$w \$(xdotool getwindowname \$w 2>/dev/null)"
     xdotool getwindowgeometry \$w 2>/dev/null
  done
} > "$TMP/geom.txt" 2>&1
xwd -id "\$WIN" -silent > "$TMP/win.xwd" 2>/dev/null || xwd -root -silent > "$TMP/win.xwd"
wineserver -k 2>/dev/null || true
EOS2

WINEPREFIX=$PREFIX WINEDEBUG=+font,-all \
    xvfb-run -a -s "-screen 0 1024x768x24" bash "$TMP/run.sh" 2>/dev/null || true

cat "$TMP/geom.txt"
echo "=== Wine 選中的字型 ==="
grep -iE "Chosen|pix_h|FindBestFont|realize" "$TMP/wine.log" 2>/dev/null | sort -u | head -20
python3 "$REPO/tools/xwd2png.py" "$TMP/win.xwd" "$OUT" && echo "截圖: $OUT"
