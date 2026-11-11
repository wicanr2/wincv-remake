#!/usr/bin/env bash
# 重拍 README 的截圖(docs/ui/shot-*.png 等)。用 Rust 版的 celldump:與視窗程式是同一份
# app 與光柵器,畫出來的像素就是視窗裡看到的那一批。
#
#   tools/readme-shots.sh            # 全部重拍
#   ONLY=pdf tools/readme-shots.sh   # 只拍名稱符合的(bash 正規式)
#
# 需要:original/(tools/setup-wine-oracle.sh 解出的原版素材)、original/eten/(倚天字庫)。
# 倚天沒有的字(簡體、假名)由系統字型補,所以 shot-i18n 會隨主機裝了哪些字型而不同。
#
# -keys 是「游標從 .. 往下按幾次 Down 再 Enter」:在那些目錄裡增刪檔案就要重數,數錯不會
# 有錯誤訊息,只會拍到隔壁那個檔。拍完要看過每一張。
set -euo pipefail
REPO=$(cd "$(dirname "$0")/.." && pwd)
UI=/src/docs/ui
TMP=.cache/readme-shots
mkdir -p "$REPO/$TMP"

"$REPO/tools/cargo.sh" build -q --release -p wincv-celldump

# 容器裡跑 celldump(與 gopher 示範站)。原版素材與字型唯讀,只有 docs/ui 與暫存目錄可寫。
run() {
    timeout 300 docker run --rm --init --log-opt max-size=10m --log-opt max-file=3 --network none \
        --cpus 2 --memory 2g --pids-limit 256 -u "$(id -u):$(id -g)" -e HOME=/tmp -e WINCV_HOME=/src \
        -v "$REPO":/src:ro -v "$REPO/docs/ui":/src/docs/ui -v "$REPO/$TMP":/src/$TMP \
        -v "$REPO/.cache/cargo-target":/cargo-target:ro -v /usr/share/fonts:/usr/share/fonts:ro \
        -w /src wincv-rust-run:1 "$@"
}
FONTS=(-half /src/original/app/cvga.fon -eten-std /src/original/eten/STDFONT.15 -eten-spc /src/original/eten/SPCFONT.15)
dump() { run /cargo-target/release/celldump "${FONTS[@]}" "$@"; }
want() { [[ -z "${ONLY:-}" || $1 =~ $ONLY ]]; }
downs() { local s=""; for _ in $(seq "$1"); do s+="Down,"; done; echo "$s"; }
ups() { local s=""; for _ in $(seq "$1"); do s+="Up,"; done; echo "$s"; }
rights() { local s=""; for _ in $(seq "$1"); do s+="Right,"; done; echo "$s"; }

APP=/src/original/app
# 原版目錄的清單:whatsnew.txt 是倒數第 9 個(後面是 wincv.exe、WinCV.* 與 WinCVins.*),End 再 Up 8 次。
if want main; then dump -app $APP -cols 93 -rows 21 -o $UI/shot-main.png; fi
if want viewer; then
    dump -app $APP -cols 93 -rows 21 -keys "End,$(ups 8)Enter" -o $UI/shot-viewer.png
    dump -app $APP -cols 93 -rows 24 -keys "End,$(ups 8)Enter,$(downs 14)" -o $UI/shot-viewer-bar.png
fi
if want menu; then dump -app $APP -cols 93 -rows 29 -keys "F9" -o $UI/shot-menu.png; fi

# 四種語言:展開「7 設定」選單(語言就在裡面)。
if want i18n; then
    for l in zh-Hant zh-Hans en ja; do
        dump -app $APP -cols 100 -rows 17 -keys "F9,$(rights 6)" -lang $l -o /src/$TMP/i18n-$l.png
    done
    run python3 -I /src/tools/png-grid.py 2 $UI/shot-i18n.png \
        /src/$TMP/i18n-zh-Hant.png /src/$TMP/i18n-zh-Hans.png /src/$TMP/i18n-en.png /src/$TMP/i18n-ja.png
fi

# gopher:示範站與 celldump 在同一個容器裡,不連外。
if want gopher; then
    run sh -c "python3 -I /src/tools/gopher-demo.py 7070 & sleep 1;
        /cargo-target/release/celldump ${FONTS[*]} -app /src/docs/demo -cols 92 -rows 25 \
            -gopher gopher://127.0.0.1:7070/ -o $UI/gopher-menu.png"
fi

# markdown 檢視:新增詩詞及洛神圖文示範後，showcase.md 是 .. 之後的第八項。
if want markdown; then dump -app /src/docs/demo -cols 104 -rows 56 -keys "$(downs 8)Enter" -o $UI/showcase-markdown.png; fi

# Office 與 PDF:docs/demo/office 的清單見該目錄的 README。
D=/src/docs/demo/office
if want xlsx; then dump -app $D -cols 100 -rows 30 -keys "$(downs 2)Enter" -o $UI/shot-xlsx.png; fi
if want docx; then dump -app $D -cols 100 -rows 34 -keys "$(downs 5)Enter" -o $UI/shot-docx.png; fi
if want pdf; then
    dump -app $D -cols 100 -rows 34 -keys "$(downs 8)Enter" -o $UI/shot-pdf-text.png
    dump -app $D -cols 100 -rows 40 -keys "$(downs 8)Enter,V" -o $UI/shot-pdf-page.png
fi
if want pptx; then dump -app $D -cols 100 -rows 30 -keys "$(downs 9)Enter" -o $UI/shot-pptx.png; fi
echo "拍好了。逐張看過再 commit:docs/ui/"
