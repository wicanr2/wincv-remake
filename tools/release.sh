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

# 建置完成的印記:記下這批產物是從哪一個 commit 建的。
#
# 為什麼不用檔案時間判斷:原本是拿 dist-all/ 裡檔案的 mtime 跟 HEAD 的
# commit 時間比。但 --no-build 會把 dist/ 重新複製一次,mtime 變成複製的
# 當下,於是那道檢查在 --no-build 這條路上**永遠會過** —— 而 --no-build
# 正是它唯一要防的情境。守門的東西擋不住它要擋的事,就等於沒有。
#
# 時間本來也只是代理指標:「比 HEAD 新」是必要條件,不是充分條件
# (在新 commit 建完再 checkout 回舊的,時間照樣是新的)。印記直接回答
# 「從哪一個 commit 建的」,兩個方向都擋得住。
STAMP=$REPO/dist/BUILT-FROM

if [ "$BUILD" = 1 ]; then
    # 先清掉:建到一半失敗時,留著的是上一輪的印記,而 dist/ 裡是半成品。
    rm -f "$STAMP"
    echo "== 桌面三平台 =="
    "$REPO/tools/build-all.sh"
    echo "== Android =="
    # build-all.sh 已經把 base 素材放好並註冊了清理;Android 這一支
    # 跑在同一輪裡,直接沿用,只要把 build tag 帶進去。
    "$REPO/tools/embed-fonts.sh" base
    TAGS=fonts "$REPO/tools/build-android.sh"
    "$REPO/tools/embed-fonts.sh" clean
    echo "$COMMIT" > "$STAMP"
fi

# 印記只由這支腳本寫。自己跑 build-all.sh / build-android.sh 建出來的 dist/
# 沒有印記,--no-build 會拒絕 —— 那種產物來自哪一個 commit 無從查證,
# 而這支腳本存在的理由就是不讓那種東西進發布。
if [ ! -f "$STAMP" ]; then
    echo "dist/ 沒有建置印記,無從確認它來自哪一個 commit。" >&2
    echo "重跑一次不要加 --no-build。" >&2
    exit 1
fi
BUILT=$(cat "$STAMP")
if [ "$BUILT" != "$COMMIT" ]; then
    echo "dist/ 是 ${BUILT:0:12} 建的,HEAD 是 ${COMMIT:0:12}。" >&2
    echo "發布物要對得上一個 commit,重跑一次不要加 --no-build。" >&2
    exit 1
fi

# 公開版的四個產物。這支腳本只管這幾個(加上它們的校驗檔與對照表)。
PUBLIC=(wincv-linux-amd64 wincv-windows-amd64.exe wincv-darwin-universal wincv-android.apk)

# [雷] 不要 `rm -rf "$OUT"`。dist-all/ 裡還有完整版(tools/build-full.sh 產的)
# 與使用者自己解開來跑的目錄,那些不是這支腳本的東西。整個刪掉的話,
# 兩支腳本的執行順序就變成一個沒有人會記得的隱藏相依 —— 反過來跑會安靜地
# 少掉四個檔,而 dist-all/ 看起來仍然是滿的。
#
# 「發布物要對得上同一個 commit」這個保證由上面的建置印記負責,
# 這裡只負責不要把上一版的檔案留在目錄裡。
mkdir -p "$OUT"
for f in "${PUBLIC[@]}"; do rm -f "$OUT/$f"; done
rm -f "$OUT/SHA256SUMS" "$OUT/MANIFEST.txt"

# -p 保留建置時間。不保留的話 dist-all/ 裡看到的是複製的時間,
# `ls -la` 會讓每一批產物看起來都是「剛剛才建的」。
for f in "${PUBLIC[@]}"; do
    [ -s "$REPO/dist/$f" ] || { echo "缺少 dist/$f" >&2; exit 1; }
    cp -p "$REPO/dist/$f" "$OUT/$f"
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

# 順手把本機自己要跑的那一個也更新掉。
#
# 為什麼放在這裡:dist-all/wincv-linux-amd64-full 是掛在使用者 shell alias
# 上的固定名稱,而它由另一支腳本(build-full.sh)產生 —— 只發布不重建的話,
# 手上跑的會是舊版,而檔案還在、名字也對,看起來完全正常。
# 一分鐘出頭的事,不值得讓「發布過的版本」與「自己在跑的版本」分岔。
#
# WINCV_SKIP_LOCAL=1 可以跳過(只想快點做出發布物的時候)。
if [ "${WINCV_SKIP_LOCAL:-}" != 1 ]; then
    echo
    echo "== 本機完整版(alias 指的那一個)=="
    "$REPO/tools/build-full.sh" linux
fi

echo
echo "dist-all/（commit $SHORT）:"
ls -la --time-style=+ "$OUT" | tail -n +2 | awk '{printf "  %-46s %12s\n", $NF, $5}'
