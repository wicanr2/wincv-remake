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
# public / full / all。分開的理由:公開版的 zip 一旦上傳,再打包一次會
# 因為時間戳不同而換掉 sha256,與 release 上那份對不起來。
WHAT=${1:-all}

command -v zip >/dev/null || { echo "需要 zip 指令" >&2; exit 1; }

# 平台:<dist-all 裡的檔名>:<zip 裡的執行檔名>:<平台代號>
PLATFORMS=(
    "wincv-linux-amd64:wincv:linux-amd64"
    "wincv-windows-amd64.exe:wincv.exe:windows-amd64"
    "wincv-darwin-universal:wincv:macos-universal"
    "wincv-android.apk:wincv.apk:android"
)

# 完整版(字型嵌在執行檔裡,tools/build-full.sh 產生)。有才打包。
FULL=(
    "wincv-linux-amd64-full:wincv:linux-amd64"
    "wincv-windows-amd64-full.exe:wincv.exe:windows-amd64"
    "wincv-darwin-universal-full:wincv:macos-universal"
    "wincv-android-full.apk:wincv.apk:android"
)

cd "$OUT"

if [ "$WHAT" != full ]; then
    rm -f ./wincv-remake-*[!l].zip SHA256SUMS-zip
fi
if [ "$WHAT" = full ]; then
    rm -f ./*-full.zip SHA256SUMS-zip-full
fi

[ "$WHAT" = full ] || for entry in "${PLATFORMS[@]}"; do
    src=${entry%%:*}; rest=${entry#*:}
    exe=${rest%%:*}; plat=${rest#*:}
    [ -s "$src" ] || { echo "缺少 $src" >&2; exit 1; }

    dir="wincv-remake-$plat"
    rm -rf "$dir"; mkdir -p "$dir"
    cp "$src" "$dir/$exe"
    # APK 不必可執行,其餘要。zip 會保住這個位元。
    case "$plat" in android) chmod 644 "$dir/$exe" ;; *) chmod 755 "$dir/$exe" ;; esac
    cp "$REPO/LICENSE" "$REPO/NOTICE" "$dir/"
    # [雷] zip 裡的檔名用 ASCII。`zip` 不會設 UTF-8 旗標(旗標位元 0x800),
    # 而沒有那個旗標時 Windows 的內建解壓縮會拿系統編碼(繁中是 Big5)
    # 去解 UTF-8 的位元組 —— 「讀我.txt」變成一串亂碼。
    # 檔名是 ASCII 就完全繞開這件事,內容仍然是中文。
    "$REPO/tools/readme-for.sh" "$plat" "$exe" > "$dir/README.txt"
    # Windows 版換成 CRLF:雖然新版記事本讀得懂 LF,舊的會擠成一行。
    if [ "$plat" = "windows-amd64" ]; then
        sed -i 's/$/\r/' "$dir/README.txt"
    fi

    zip -qr "wincv-remake-$TAG-$plat.zip" "$dir"
    rm -rf "$dir"
    printf "  %-40s %10s\n" "wincv-remake-$TAG-$plat.zip" \
        "$(stat -c%s "wincv-remake-$TAG-$plat.zip")"
done

# 完整版:同樣的結構,檔名多一個 -full。
#
# **這幾個不要上傳到 release,也不要轉給別人。** 裡面嵌著原版的 .FON
# 與倚天字庫,那是第三方版權物。要給別人的是上面不帶 -full 的版本 ——
# 它沒有字型也跑得起來(半形改用系統字型現場產,見 cmd/wincv 的
# ttfLevels),只是畫面不是原版的點陣字。
fulln=0
[ "$WHAT" = public ] || for entry in "${FULL[@]}"; do
    src=${entry%%:*}; rest=${entry#*:}
    exe=${rest%%:*}; plat=${rest#*:}
    [ -s "$src" ] || continue

    dir="wincv-remake-$plat-full"
    rm -rf "$dir"; mkdir -p "$dir"
    cp "$src" "$dir/$exe"
    case "$plat" in android) chmod 644 "$dir/$exe" ;; *) chmod 755 "$dir/$exe" ;; esac
    cp "$REPO/LICENSE" "$REPO/NOTICE" "$dir/"
    { "$REPO/tools/readme-for.sh" "$plat" "$exe"
      cat <<'EOF'

這一份是完整版
--------------

與同一個 release 的公開版差別只有一件事:多嵌了一組 Noto 的 Unicode
後備字型(SIL Open Font License),所以簡體、日文假名、韓文、西里爾、
阿拉伯、泰文與各種符號都不必靠系統字型也畫得出來。

原版的半形 .FON 與倚天的全形字庫兩邊都有,畫面完全一樣。
EOF
    } > "$dir/README.txt"
    [ "$plat" = "windows-amd64" ] && sed -i 's/$/\r/' "$dir/README.txt"

    zip -qr "wincv-remake-$TAG-$plat-full.zip" "$dir"
    rm -rf "$dir"
    printf "  %-40s %10s  (完整版,不對外)\n" "wincv-remake-$TAG-$plat-full.zip" \
        "$(stat -c%s "wincv-remake-$TAG-$plat-full.zip")"
    fulln=$((fulln + 1))
done

# 兩份校驗檔分開:SHA256SUMS-zip 是要上傳的那一份,只列公開版。
if [ "$WHAT" != full ]; then
    sha256sum $(ls ./wincv-remake-*.zip | grep -v -- "-full") > SHA256SUMS-zip
    echo
    echo "校驗:cd dist-all && sha256sum -c SHA256SUMS-zip"
fi
if [ "$fulln" -gt 0 ]; then
    sha256sum ./*-full.zip > SHA256SUMS-zip-full
    echo "校驗(完整版):cd dist-all && sha256sum -c SHA256SUMS-zip-full"
    echo "注意:$fulln 個 -full.zip 只留本機。完整版與公開版嵌的字型相同,"
    echo "      差別是完整版多一組 Noto 的 Unicode 後備(SIL OFL)。"
fi
