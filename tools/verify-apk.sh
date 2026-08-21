#!/usr/bin/env bash
# APK 的靜態驗收。
#
# 「build 成功」不等於「裝得起來」,更不等於「跑得動」——
# 這支只證明格式與內容對,實跑要在裝置上。
#
# [HARD] 每一項都要真的檢查回傳值。第一版寫成「印一堆資訊,最後固定印
#        『通過』」,結果四項全部失敗它還是說通過 —— 那比沒有驗收更糟。
set -euo pipefail

REPO=$(cd "$(dirname "$0")/.." && pwd)
APK=${1:-$REPO/dist/wincv-android.apk}
IMG=${ANDROID_IMG:-wincv-android:1}

[ -s "$APK" ] || { echo "找不到 $APK" >&2; exit 1; }
# 換算成容器內的路徑
case "$APK" in
    "$REPO"/*) IN=/src${APK#$REPO} ;;
    /*)        echo "APK 要放在 repo 底下(現在是 $APK)" >&2; exit 1 ;;
    *)         IN=/src/$APK ;;
esac

# [雷] 要 -i。沒有 -i 的話 docker 不接 stdin,heredoc 進不去容器,
# 於是 `sh -s` 讀到空的腳本、什麼都不做、回傳 0 ——
# 「沒有輸出 + 退出碼 0」看起來跟成功一模一樣。
docker run --rm -i --log-opt max-size=10m --log-opt max-file=3 \
    -u "$(id -u):$(id -g)" -v "$REPO:/src" -w /src -e HOME=/tmp "$IMG" \
    sh -s "$IN" <<'INNER'
set -eu
APK=$1
fail=0
check() {  # check <說明> <期望的最少筆數> <指令...>
    desc=$1; want=$2; shift 2
    n=$("$@" | wc -l)
    if [ "$n" -lt "$want" ]; then
        echo "  ✗ $desc(拿到 $n 筆,至少要 $want)"
        fail=1
    else
        echo "  ✓ $desc($n 筆)"
    fi
}

BT=$(ls -d "$ANDROID_HOME"/build-tools/*/ | head -1)

echo "=== 原生程式庫 ==="
unzip -l "$APK" | grep "lib/.*\.so" | sed 's|^|    |'
# 四個 ABI 都要有:arm64-v8a / armeabi-v7a / x86 / x86_64
check "ABI 數" 4 sh -c "unzip -l '$APK' | grep -o 'lib/[^/]*/' | sort -u"

echo "=== 進入點與權限 ==="
"$BT"aapt2 dump badging "$APK" 2>/dev/null |
    grep -E "^package:|launchable-activity|uses-permission|sdkVersion" | sed 's|^|    |'
check "有 launcher activity" 1 sh -c "'$BT'aapt2 dump badging '$APK' 2>/dev/null | grep launchable-activity"

echo "=== 簽章 ==="
if "$BT"apksigner verify --print-certs "$APK" >/tmp/sig.txt 2>&1; then
    grep -E "Signer #1 certificate DN|Signer #1 certificate SHA-256" /tmp/sig.txt | sed 's|^|    |'
    echo "  ✓ 簽章驗證通過"
else
    sed 's|^|    |' /tmp/sig.txt | head -5
    echo "  ✗ 簽章驗證失敗"
    fail=1
fi

echo
if [ "$fail" -ne 0 ]; then
    echo "驗收失敗。"
    exit 1
fi
echo "靜態驗收通過。注意:這只證明 APK 的格式與內容沒問題,"
echo "不證明它在真的裝置上跑得起來 —— 那要實機或模擬器。"
INNER
