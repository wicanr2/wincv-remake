#!/usr/bin/env bash
# 把四個平台的產物整理進 dist-all/,附 SHA-256 與一份對照表。
#
# 為什麼要有這支而不是手動搬:發布物必須對得上**某一個 commit**。
# 手動搬最容易出的錯是「桌面版是上一版建的、APK 是這一版建的」——
# 檔案都在、大小也合理,但它們來自不同的原始碼,而這件事事後查不出來。
# 所以這支會拒絕在有未提交變更時執行,並把 commit 寫進 MANIFEST。
#
# 用法:
#   tools/release.sh              # 重建四個平台並整理進 dist-all/
#   tools/release.sh --no-build   # 只整理(產物必須已經是這個 commit 建的)
set -euo pipefail

REPO=$(cd "$(dirname "$0")/.." && pwd)
OUT=$REPO/dist-all
BUILD=1
[ "${1:-}" = "--no-build" ] && BUILD=0

cd "$REPO"
if [ -n "$(git status --porcelain)" ]; then
    echo "工作區有未提交的變更。發布物要對得上一個 commit,先提交再跑。" >&2
    git status --short >&2
    exit 1
fi
COMMIT=$(git rev-parse HEAD)
SHORT=$(git rev-parse --short HEAD)

if [ "$BUILD" = 1 ]; then
    echo "== 桌面三平台 =="
    "$REPO/tools/build-all.sh"
    echo "== Android =="
    # build-all.sh 已經把 base 素材放好並註冊了清理;Android 這一支
    # 跑在同一輪裡,直接沿用,只要把 build tag 帶進去。
    "$REPO/tools/embed-fonts.sh" base
    TAGS=fonts "$REPO/tools/build-android.sh"
    "$REPO/tools/embed-fonts.sh" clean
fi

# 公開版的四個產物。這支腳本只管這幾個(加上它們的校驗檔與對照表)。
PUBLIC=(wincv-linux-amd64 wincv-windows-amd64.exe wincv-darwin-universal wincv-android.apk)

# [雷] 不要 `rm -rf "$OUT"`。dist-all/ 裡還有完整版(tools/build-full.sh 產的)
# 與使用者自己解開來跑的目錄,那些不是這支腳本的東西。整個刪掉的話,
# 兩支腳本的執行順序就變成一個沒有人會記得的隱藏相依 —— 反過來跑會安靜地
# 少掉四個檔,而 dist-all/ 看起來仍然是滿的。
#
# 「發布物要對得上同一個 commit」這個保證不靠清空目錄,靠的是下面兩件事:
# 先把自己要重出的那幾個刪掉(留著舊的會讓下面的陳舊檢查誤判成新的),
# 再逐一確認產物比 HEAD 新。
mkdir -p "$OUT"
for f in "${PUBLIC[@]}"; do rm -f "$OUT/$f"; done
rm -f "$OUT/SHA256SUMS" "$OUT/MANIFEST.txt"

for f in "${PUBLIC[@]}"; do
    [ -s "$REPO/dist/$f" ] || { echo "缺少 dist/$f" >&2; exit 1; }
    cp "$REPO/dist/$f" "$OUT/$f"
done

# 產物比 commit 舊的話,它是上一版建的 —— 那正是這支腳本要擋的錯。
# 只檢查公開版:完整版有自己的建置流程,它比 HEAD 舊是正常的。
CT=$(git log -1 --format=%ct)
for f in "${PUBLIC[@]}"; do
    if [ "$(stat -c %Y "$OUT/$f")" -lt "$CT" ]; then
        echo "$f 比 HEAD 還舊,是上一版建的。重跑不要加 --no-build。" >&2
        exit 1
    fi
done

# 只列公開版。`sha256sum *` 會連完整版與解開的目錄一起吃進去,
# 而這份校驗檔是要上傳到 release 的。
( cd "$OUT" && sha256sum "${PUBLIC[@]}" > SHA256SUMS )

echo
echo "== 打包 =="
"$REPO/tools/package.sh" public

# MANIFEST 描述的是**發布出去的東西**,所以要在打包之後才產生 ——
# 列裸執行檔的檔名會讓下載的人對不上 release 頁面上的 zip。
cat > "$OUT/MANIFEST.txt" <<EOF
WinCV Remake — 產物對照表

commit   $COMMIT
建置日期 $(date -u +%Y-%m-%d)（UTC）

$(cd "$OUT" && ls -la --time-style=+ ./*.zip 2>/dev/null | grep -v -- "-full" | awk '{printf "  %-46s %12s\n", substr($NF, 3), $5}')

檔案                                             平台
  wincv-remake-*-linux-amd64.zip                 Linux x86-64
  wincv-remake-*-windows-amd64.zip               Windows x86-64
  wincv-remake-*-macos-universal.zip             macOS（arm64 + x86_64 universal）
  wincv-remake-*-android.zip                     Android（四種 ABI，minSdk 21）

每個 zip 解開是一個目錄，裡面是執行檔加上 LICENSE、NOTICE 與 README.txt。

執行檔內嵌原版隨附的半形點陣字型與倚天的全形字庫，解開就有與原版逐像素
對齊的畫面。這兩份字型的權利仍在原權利人手上，BSD 2-Clause 不涵蓋它們，
出處寫在 NOTICE。字典資料不打包。

倚天沒有的字（簡體、日文假名、韓文、西里爾……）會往系統的 TrueType 字型找；
系統上一份都沒有時那些字顯示成缺字方塊，其餘功能不受影響。

校驗：sha256sum -c SHA256SUMS-zip
EOF

echo
echo "dist-all/（commit $SHORT）:"
ls -la --time-style=+ "$OUT" | tail -n +2 | awk '{printf "  %-46s %12s\n", $NF, $5}'
