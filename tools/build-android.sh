#!/usr/bin/env bash
# 產 Android 的 AAR 與 APK。
#
# 兩步:
#   1. ebitenmobile bind  → mobile 套件包成 AAR(含 arm64/arm/x86_64 的 .so)
#   2. gradle assembleRelease → APK(debug 簽章,私人 sideload 用)
#
# [HARD] 這台機器有其他客戶專案的 image 與 android 工具鏈。
#        只用自己的 wincv-android:1,任何 prune / rmi 一律禁止。
set -euo pipefail

REPO=$(cd "$(dirname "$0")/.." && pwd)
IMG=${ANDROID_IMG:-wincv-android:1}
DIST=$REPO/dist
JAVAPKG=tw.lcy.wincv

mkdir -p "$DIST" "$REPO/android/app/libs"

DOCKER="docker run --rm --log-opt max-size=10m --log-opt max-file=3
        -u $(id -u):$(id -g)
        -v $REPO:/src -w /src
        -e GOFLAGS=-buildvcs=false
        -e GOCACHE=/tmp/gocache -e GOMODCACHE=/tmp/gomod
        -e GRADLE_USER_HOME=/tmp/gradle
        -e HOME=/tmp
        -e JAVA_TOOL_OPTIONS=-Dfile.encoding=UTF-8"

# [雷] JAVA_TOOL_OPTIONS 那一行不能拿掉。
#
# gobind 會把 Go 的 doc comment 原樣抄進產生的 Java 檔,而 javac 預設用
# 平台的 charset 讀原始碼 —— 在這個容器裡是 US-ASCII。本專案的註解是
# 繁體中文,於是每個中文字都變成 "unmappable character",一次噴 222 個錯。
#
# 治標的作法是把 mobile 套件的註解改成英文,但那是讓工具鏈決定文件語言。
# 治本是告訴 javac 原始碼是 UTF-8。

echo "== 1/2 ebitenmobile bind =="
$DOCKER "$IMG" ebitenmobile bind \
    -target android -androidapi 21 \
    -javapkg "$JAVAPKG" \
    -o /src/android/app/libs/wincv.aar \
    ./mobile

[ -s "$REPO/android/app/libs/wincv.aar" ] || { echo "AAR 沒產出來" >&2; exit 1; }
echo "   $(du -h "$REPO/android/app/libs/wincv.aar" | cut -f1)  android/app/libs/wincv.aar"

echo "== 2/2 gradle assembleRelease =="
$DOCKER -w /src/android "$IMG" gradle --no-daemon assembleRelease

APK=$REPO/android/app/build/outputs/apk/release/app-release.apk
[ -s "$APK" ] || { echo "APK 沒產出來" >&2; exit 1; }
cp "$APK" "$DIST/wincv-android.apk"
echo "   $(du -h "$DIST/wincv-android.apk" | cut -f1)  dist/wincv-android.apk"

echo
echo "驗收:tools/verify-apk.sh"
