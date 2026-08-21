#!/usr/bin/env bash
# 在模擬器容器內部跑。由 tools/run-android-emulator.sh 掛進去執行。
#
# 為什麼是獨立檔案而不是 heredoc:heredoc 是走 stdin 餵給 `bash -s` 的,
# 而 bash 從管線讀腳本是**邊讀邊執行**。背景起的 emulator 繼承同一個 stdin,
# 會把還沒讀到的腳本文字吃掉 —— 腳本從中間斷掉,而且是 exit 0 的斷法。
# 掛成檔案就沒有這個耦合。

set -uo pipefail
export ANDROID_AVD_HOME=/opt/avd
AVD=${AVD:-wolong}
PKG=${PKG:-tw.lcy.wincv}

# 不論成敗都要留下模擬器自己的 log,否則容器 --rm 掉就什麼都不剩。
finish() {
    cp /tmp/emulator.log /out/android-emulator.log 2>/dev/null || true
    adb emu kill >/dev/null 2>&1 || true
}
trap finish EXIT

echo "==> 開機(headless,swiftshader)"
# stdin 導到 /dev/null:見開頭的說明。
emulator -avd "$AVD" -no-window -no-audio -no-boot-anim \
    -gpu swiftshader_indirect -no-snapshot -wipe-data -accel on \
    </dev/null >/tmp/emulator.log 2>&1 &
EMU_PID=$!

echo "==> 等開機(最多 6 分鐘)"
booted=0
for i in $(seq 1 180); do
    if ! kill -0 "$EMU_PID" 2>/dev/null; then
        echo "模擬器行程已經死了(第 $((i*2)) 秒)"
        tail -40 /tmp/emulator.log
        exit 1
    fi
    if [ "$(adb shell getprop sys.boot_completed 2>/dev/null | tr -d '\r')" = "1" ]; then
        booted=1
        echo "   第 $((i*2)) 秒開機完成"
        break
    fi
    sleep 2
done
if [ "$booted" != 1 ]; then
    echo "開機逾時"
    tail -40 /tmp/emulator.log
    exit 1
fi

# /sdcard 對 shell 使用者不給建檔(目錄建得出來、檔案 EPERM)。
# google_apis 的 image 可以 adb root —— 有 Play 商店的 image 不行,
# 那時就只能瀏覽裝置本來就有的目錄。
adb root >/dev/null 2>&1 || true
adb wait-for-device
echo "==> adb 身分: $(adb shell id -u 2>/dev/null | tr -d '\r')(0 = root)"

echo "==> 裝置: Android $(adb shell getprop ro.build.version.release | tr -d '\r') / API $(adb shell getprop ro.build.version.sdk | tr -d '\r') / $(adb shell getprop ro.product.cpu.abi | tr -d '\r')"

for s in window_animation_scale transition_animation_scale animator_duration_scale; do
    adb shell settings put global "$s" 0 >/dev/null 2>&1 || true
done

# 目錄讓 adb 自己建。先用 `adb shell mkdir` 建好再 push,FUSE 那一層
# 會擋下來(EPERM),而錯誤訊息講的是「建不出檔案」,不是「目錄不對」。
# 直接 push 到 /sdcard 在這個模擬器上會回報成功但檔案不在
# (adb 的 sync 通道對上模擬器的 FUSE)。先推到 /data/local/tmp,
# 再由裝置自己 cp —— 那一步不經過 adb sync。
echo "==> 放測試檔到 /sdcard/Download/wincv-test"
adb shell rm -rf /data/local/tmp/wincv-seed >/dev/null 2>&1 || true
adb push /work/seed /data/local/tmp/wincv-seed 2>&1 | tail -1 | sed 's/^/   /'
adb shell "mkdir -p /sdcard/Download/wincv-test && cp -r /data/local/tmp/wincv-seed/. /sdcard/Download/wincv-test/" 2>&1 | sed 's/^/   /'
adb shell ls -la /sdcard/Download/wincv-test 2>&1 | sed 's/^/   /'

echo "==> 安裝"
adb install -r -g /work/app.apk || { echo "安裝失敗"; exit 1; }

# 「所有檔案存取權」是特殊權限,adb install -g 給不到,要走 appops。
# 不給的話 app 會退到自己的私有目錄(空的),而空清單看起來像畫不出來。
echo "==> 給所有檔案存取權"
adb shell appops set --uid "$PKG" MANAGE_EXTERNAL_STORAGE allow 2>&1 | sed 's/^/   /'
adb shell appops get --uid "$PKG" MANAGE_EXTERNAL_STORAGE 2>&1 | sed 's/^/   /'

# 剛裝完的那幾秒系統在跑一堆安裝廣播(packageinstaller、GMS、MediaProvider),
# Activity 有機會被重建 —— 而 Ebiten 的 Android view 一被重建就 exit(0)。
# 等它安靜下來再啟動,不然量到的是模擬器的忙碌程度,不是 app 的行為。
echo "==> 等安裝後的系統雜訊過去"
sleep 15

launch() {
    adb logcat -c >/dev/null 2>&1 || true
    adb shell am start -n "$PKG/.MainActivity" 2>&1 | sed 's/^/   /'
}

alive() { [ -n "$(adb shell pidof "$PKG" 2>/dev/null | tr -d '\r')" ]; }

echo "==> 啟動 $PKG/.MainActivity"
launch
sleep 8
if ! alive; then
    # 第一次冷啟動被系統雜訊打斷是模擬器的常態。行程重來一次是乾淨的
    # (那個「建過幾次 surface」的旗標是 static,隨行程重置),所以再試一次;
    # 但要講出來,不然會被當成一次就成功。
    echo "   第一次啟動後行程不在,重試一次"
    launch
fi

for t in 5 10 20; do
    sleep "$t"
    if adb exec-out screencap -p > "/out/android-run-${t}s.png" 2>/dev/null; then
        echo "   ${t}s 截圖 $(stat -c%s "/out/android-run-${t}s.png") bytes,行程 $(alive && echo 在 || echo 不在)"
    else
        echo "   ${t}s 截圖失敗"
    fi
done

echo "==> 行程狀態"
adb shell ps -A 2>/dev/null | grep -i wincv | sed 's/^/   /' || echo "   行程不在(可能已崩潰)"

adb logcat -d > /out/android-logcat.txt 2>&1 || true
adb logcat -d -b crash > /out/android-crash.txt 2>&1 || true

echo "==> 與本 app 有關的 log"
grep -iE "wincv|GoLog|ebiten|AndroidRuntime|FATAL|libgojni" /out/android-logcat.txt \
    | head -40 | sed 's/^/   /' || echo "   (沒有)"
