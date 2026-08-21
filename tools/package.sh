#!/usr/bin/env bash
# 把 dist-all/ 的產物各自壓成一個 zip,附授權與一份說明。
#
# 為什麼要壓而不是直接放執行檔:
#   - 授權與出處要跟著產物走。LICENSE 與 NOTICE 分開放在 release 頁面上,
#     下載的人拿到的只有一個執行檔,而那份執行檔裡有一段是從 BSD 授權的
#     acefile 逐行移植的 —— 著作權聲明必須隨產物散布。
#   - 瀏覽器下載裸執行檔會掉權限位元。zip 裡的 +x 解出來還在。
#   - macOS 與 Linux 的檔名帶著平台後綴很難用;zip 裡改回 `wincv`,
#     解開就能跑。
set -euo pipefail

REPO=$(cd "$(dirname "$0")/.." && pwd)
OUT=$REPO/dist-all
TAG=${TAG:-$(cd "$REPO" && git describe --tags --abbrev=0 2>/dev/null || echo v0.52-remake)}

command -v zip >/dev/null || { echo "需要 zip 指令" >&2; exit 1; }

# 平台:<dist-all 裡的檔名>:<zip 裡的執行檔名>:<平台代號>
PLATFORMS=(
    "wincv-linux-amd64:wincv:linux-amd64"
    "wincv-windows-amd64.exe:wincv.exe:windows-amd64"
    "wincv-darwin-universal:wincv:macos-universal"
    "wincv-android.apk:wincv.apk:android"
)

cd "$OUT"
rm -f ./*.zip SHA256SUMS-zip

for entry in "${PLATFORMS[@]}"; do
    src=${entry%%:*}; rest=${entry#*:}
    exe=${rest%%:*}; plat=${rest#*:}
    [ -s "$src" ] || { echo "缺少 $src" >&2; exit 1; }

    dir="wincv-remake-$plat"
    rm -rf "$dir"; mkdir -p "$dir"
    cp "$src" "$dir/$exe"
    # APK 不必可執行,其餘要。zip 會保住這個位元。
    case "$plat" in android) chmod 644 "$dir/$exe" ;; *) chmod 755 "$dir/$exe" ;; esac
    cp "$REPO/LICENSE" "$REPO/NOTICE" "$dir/"
    "$REPO/tools/readme-for.sh" "$plat" "$exe" > "$dir/讀我.txt"

    zip -qr "wincv-remake-$TAG-$plat.zip" "$dir"
    rm -rf "$dir"
    printf "  %-40s %10s\n" "wincv-remake-$TAG-$plat.zip" \
        "$(stat -c%s "wincv-remake-$TAG-$plat.zip")"
done

sha256sum ./*.zip > SHA256SUMS-zip
echo
echo "校驗:cd dist-all && sha256sum -c SHA256SUMS-zip"
