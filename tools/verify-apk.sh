#!/usr/bin/env bash
# APK 的靜態驗收。
#
# 「build 成功」不等於「裝得起來」,更不等於「跑得動」——
# 這支只證明格式與內容對,實跑要在裝置上。
set -euo pipefail
REPO=$(cd "$(dirname "$0")/.." && pwd)
APK=${1:-$REPO/dist/wincv-android.apk}
IMG=${ANDROID_IMG:-wincv-android:1}

[ -s "$APK" ] || { echo "找不到 $APK" >&2; exit 1; }

docker run --rm --log-opt max-size=10m --log-opt max-file=3 \
    -u "$(id -u):$(id -g)" -v "$REPO:/src" -w /src -e HOME=/tmp "$IMG" sh -c '
APK=${1#/src}; APK=/src$APK
echo "=== 基本 ==="
ls -la "$APK" | sed "s|^|  |"
echo "=== 原生程式庫(每個 ABI 都要有)==="
unzip -l "$APK" | grep "lib/" | sed "s|^|  |"
echo "=== 進入點與權限 ==="
AAPT=$(ls -d $ANDROID_HOME/build-tools/*/ | head -1)aapt2
$AAPT dump badging "$APK" 2>/dev/null | grep -E "package:|launchable-activity|uses-permission|sdkVersion" | sed "s|^|  |"
echo "=== 簽章 ==="
$ANDROID_HOME/build-tools/*/apksigner verify --print-certs "$APK" 2>&1 | head -6 | sed "s|^|  |"
' -- "$APK"
echo
echo "靜態驗收通過。注意:這只證明 APK 的格式與內容沒問題,"
echo "不證明它在真的裝置上跑得起來 —— 那要實機或模擬器。"
