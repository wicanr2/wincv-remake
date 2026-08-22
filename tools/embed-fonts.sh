#!/usr/bin/env bash
# 把要嵌進執行檔的字型放進 internal/bundled/assets/。
#
#   tools/embed-fonts.sh base   原版的 .FON + 倚天字庫
#   tools/embed-fonts.sh full   再加上 Noto 的 Unicode 後備
#   tools/embed-fonts.sh clean  清掉
#
# 為什麼要有這支而不是各自準備:build-all.sh 與 build-full.sh 都要嵌字型,
# 差別只在多不多一組後備。兩邊各寫一份清單,總有一天會對不上 ——
# 而症狀是「某個平台的產物缺字」,那要跑起來才看得到。
set -euo pipefail

REPO=$(cd "$(dirname "$0")/.." && pwd)
ASSETS=$REPO/internal/bundled/assets
MODE=${1:-base}

if [ "$MODE" = clean ]; then
    rm -rf "$ASSETS"
    exit 0
fi

# 原版的半形點陣字與倚天的全形字庫。
# 半形三個字級都要,不然 Ctrl-+ 只有一級可用;全形只有 15 點那一份,
# 其餘字級由 render.ScaleCJK 縮放。
FONTS=(
    "$REPO/original/app/cvga.fon"
    "$REPO/original/app/CVGA1018.FON"
    "$REPO/original/app/cvga1224.FON"
    "$REPO/original/eten/STDFONT.15"
    "$REPO/original/eten/SPCFONT.15"
    "$REPO/original/eten/SPCFSUPP.15"
)

# Unicode 後備字型(只有 full 模式)。倚天只有 Big5 的字,而網頁、EPUB、
# UTF-8 檔案隨時會碰到簡體、日文假名、韓文、西里爾、阿拉伯、泰文與各種符號。
#
# 檔名是 fb-<順序>-<用途>,internal/bundled 靠 `fb-` 前綴認出它們、
# 靠數字決定誰先補到某個字(見 bundled_fonts.go)。
#
# 這幾份是 SIL Open Font License 1.1。不打包 unifont —— 它是 GPL-2+,
# 會傳染到整個產物,而這個 repo 走 BSD。
FALLBACKS=(
    "10-cjk.ttc:/usr/share/fonts/opentype/noto/NotoSansCJK-Regular.ttc"
    "20-symbols.ttf:/usr/share/fonts/truetype/noto/NotoSansSymbols-Regular.ttf"
    "21-symbols2.ttf:/usr/share/fonts/truetype/noto/NotoSansSymbols2-Regular.ttf"
    "22-math.ttf:/usr/share/fonts/truetype/noto/NotoSansMath-Regular.ttf"
    "30-thai.ttf:/usr/share/fonts/truetype/noto/NotoSansThai-Regular.ttf"
    "31-arabic.ttf:/usr/share/fonts/truetype/noto/NotoSansArabic-Regular.ttf"
    "32-hebrew.ttf:/usr/share/fonts/truetype/noto/NotoSansHebrew-Regular.ttf"
    "33-devanagari.ttf:/usr/share/fonts/truetype/noto/NotoSansDevanagari-Regular.ttf"
)

for f in "${FONTS[@]}"; do
    [ -s "$f" ] || {
        echo "缺字型:$f" >&2
        echo "半形的跑 tools/setup-wine-oracle.sh,倚天的跑 tools/setup-eten.sh" >&2
        exit 1
    }
done

rm -rf "$ASSETS"; mkdir -p "$ASSETS"
for f in "${FONTS[@]}"; do cp "$f" "$ASSETS/$(basename "$f")"; done

fbn=0
if [ "$MODE" = full ]; then
    # 後備字型是**可選的**:這台機器沒裝就跳過,產物仍然可用。
    # 缺一份就整個失敗會讓腳本綁死在某一台機器的字型清單上。
    for entry in "${FALLBACKS[@]}"; do
        name=${entry%%:*}; src=${entry#*:}
        if [ -s "$src" ]; then
            cp "$src" "$ASSETS/fb-$name"
            fbn=$((fbn + 1))
        else
            echo "   (跳過後備字型 $name:$src 不在)" >&2
        fi
    done
fi

echo "==> 內嵌素材 $(du -sh "$ASSETS" | cut -f1),$(ls "$ASSETS" | wc -l) 個檔(含 $fbn 份 Unicode 後備)"
