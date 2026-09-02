#!/usr/bin/env bash
# 靜態驗收 dist/ 的桌面三平台產物。APK 由 tools/verify-apk.sh 驗。
#
# 「在我的機器上好好的」是跨平台打包最貴的 bug(rulebook/82)。
# 沒有 Mac 也沒有 Windows 機器時,至少要把這些能靜態驗的項目驗掉:
#
#   每個檔案的格式對不對(ELF / PE / Mach-O fat)
#   macOS arm64 有沒有 LC_CODE_SIGNATURE —— 沒有的話 Apple Silicon
#     會直接 Killed: 9,而在 Linux 上完全看不出來
#   有沒有連到系統以外的動態庫 —— 連到編譯機才有的路徑,到使用者手上就開不起來
set -euo pipefail

REPO=$(cd "$(dirname "$0")/.." && pwd)
DIST=$REPO/dist
OSX_IMG=${OSX_IMG:-wincv-osxcross-go:1}
# 空的話由 image 自己說(osxcross-conf)。工具前綴帶 SDK 的次版號
# (SDK 15.5 → darwin24.5,不是 darwin24),寫死的話換一次 SDK 就整批
# 工具找不到,而 clang 只會轉述成 "unable to execute command"。
OSX_TARGET=${OSX_TARGET:-}
fail=0

check() {
    if [ "$1" = ok ]; then
        echo "  ✓ $2"
    else
        echo "  ✗ $2"
        fail=1
    fi
}

echo "=== 檔案格式 ==="
for f in wincv-linux-amd64 wincv-windows-amd64.exe wincv-darwin-universal; do
    if [ ! -f "$DIST/$f" ]; then
        check bad "$f 不存在"
        continue
    fi
    t=$(file -b "$DIST/$f")
    case $f in
        *linux*)   echo "$t" | grep -q "^ELF 64-bit" && check ok "$f: $t" || check bad "$f: $t" ;;
        *.exe)     echo "$t" | grep -q "PE32+"       && check ok "$f: $t" || check bad "$f: $t" ;;
        *darwin*)  echo "$t" | grep -qi "Mach-O universal" && check ok "$f: $t" || check bad "$f: $t" ;;
    esac
done

if [ -f "$DIST/wincv-darwin-universal" ]; then
    echo "=== macOS 逐弧 ==="
    docker run --rm --log-opt max-size=10m --log-opt max-file=3 \
        -u "$(id -u):$(id -g)" -e HOME=/tmp -e OSX_TARGET="$OSX_TARGET" \
        -v "$DIST":/dist "$OSX_IMG" bash -c "
        export PATH=/osxcross/bin:\$PATH
        T=\${OSX_TARGET:-\$(osxcross-conf | sed -n 's/^export OSXCROSS_TARGET=//p')}
        \$T-lipo -info /dist/wincv-darwin-universal 2>/dev/null || x86_64-apple-\$T-lipo -info /dist/wincv-darwin-universal
        for a in arm64 x86_64; do
            x86_64-apple-\$T-lipo -thin \$a /dist/wincv-darwin-universal -output /tmp/\$a
            echo \"--- \$a ---\"
            if x86_64-apple-\$T-otool -l /tmp/\$a | grep -q LC_CODE_SIGNATURE; then
                echo '  有 LC_CODE_SIGNATURE'
            else
                echo '  沒有 LC_CODE_SIGNATURE'
                [ \$a = arm64 ] && echo '  !! Apple Silicon 會 Killed: 9'
            fi
            x86_64-apple-\$T-otool -l /tmp/\$a | grep -m1 minos || true
            echo '  非系統動態庫:'
            x86_64-apple-\$T-otool -L /tmp/\$a | tail -n +2 | awk '{print \$1}' \
                | grep -vE '^(/usr/lib/|/System/Library/)' || echo '    (無)'
        done"
fi

echo
if [ $fail -eq 0 ]; then
    echo "靜態驗收通過。注意:這只證明產物的**格式**沒問題,"
    echo "不證明它在 Windows 或 macOS 上跑得起來 —— 那要在目標平台實跑。"
else
    echo "有項目沒過。"
    exit 1
fi
