#!/usr/bin/env bash
# Go 版與 Rust 版的 celldump 逐像素比對,跑多個場景。
#
#   tools/rust-parity.sh
#   ONLY='^app-' tools/rust-parity.sh     # 只跑名稱符合的場景
#
# 兩邊用同一組原版字型(original/app/cvga.fon 與 original/eten/*.15),關掉
# 後備字型;名稱以 fb- 開頭的場景才開,而且指定同一份字型(自動找的後備鏈兩邊
# 不同:Go 版讀不了某些 .ttc)。場景有示範畫面與幾個檔案列表;檔案列表的時間依容器的時區
# (兩邊都沒設 TZ,即 UTC)。結果寫在 .cache/parity/,差異以 cell 格點列出。
set -euo pipefail
REPO=$(cd "$(dirname "$0")/.." && pwd)
OUT=.cache/parity
mkdir -p "$REPO/$OUT"
for f in original/app/cvga.fon original/eten/STDFONT.15 original/eten/SPCFONT.15; do
    [ -f "$REPO/$f" ] || { echo "缺少 $f(先跑 tools/setup-wine-oracle.sh 與 tools/setup-eten.sh)" >&2; exit 2; }
done
# 檔案列表比對用的目錄:tools/parity-fixture.py 以固定內容與時間戳產生。
if [ ! -f "$REPO/$OUT/fixture/book/book.epub" ]; then
    docker run --rm --log-opt max-size=10m --log-opt max-file=3 --network none --memory 256m --cpus 1 \
        --pids-limit 64 -u "$(id -u):$(id -g)" -v "$REPO/$OUT":/p -v "$REPO/tools":/t:ro python:3.12-slim \
        python3 -I /t/parity-fixture.py /p/fixture
fi
# 文件模式(Office、PDF)用 repo 內的測試檔,一個檔一個目錄,按鍵序列才不受排序影響。
if [ ! -f "$REPO/$OUT/fixture/doc/xlsx/sheet.xlsx" ]; then
    for f in officedoc/testdata/rich.docx officedoc/testdata/deck.pptx officedoc/testdata/sheet.xlsx \
             doc97/testdata/rich.doc doc97/testdata/notes.rtf pdf/testdata/rich.pdf pdf/testdata/twocol.pdf; do
        ext=${f##*.}
        mkdir -p "$REPO/$OUT/fixture/doc/$ext"
        cp "$REPO/internal/$f" "$REPO/$OUT/fixture/doc/$ext/"
    done
    find "$REPO/$OUT/fixture/doc" -exec touch -d 2001-09-09T01:46:40Z {} +
fi
E24="-eten24-std /src/original/eten/STD.24M -eten24-spc /src/original/eten/SPCFONT.24"
FB_CJK=/usr/share/fonts/opentype/noto/NotoSansCJK-Regular.ttc
FB_DV=/usr/share/fonts/truetype/dejavu/DejaVuSans.ttf
FONTS=(-half /src/original/app/cvga.fon -eten-std /src/original/eten/STDFONT.15 -eten-spc /src/original/eten/SPCFONT.15)

# 場景:名稱|celldump 的額外參數[|已知差異的格數]
#
# 已知差異的格數留給「Go 版有已知 bug、Rust 版是對的」與刻意不同的地方;格數吻合就不算失敗。
# 目前沒有:Go 版 Big5 編碼器的 HKSCS 問題已在 v.0.53.4 修正。
SCENES=(
    "demo|"
    "dir-docs|-dir /src/docs"
    "dir-testdata|-dir /src/testdata/lzh"
    "dir-root|-dir /src"
    "dir-fixture|-dir /src/$OUT/fixture"
    "dir-fixture-small|-dir /src/$OUT/fixture -cols 60 -rows 12"
    "file-big5|-file /src/original/app/WinCV.txt"
    "file-big5-whatsnew|-file /src/original/app/whatsnew.txt"
    "file-utf8|-file /src/CLAUDE.md"
    "file-ansi|-file /src/$OUT/fixture/ansi.ans"
    "file-hex-fon|-file /src/original/app/cvga.fon"
    "file-hex-dll|-file /src/original/app/unrar.dll"
    "arch-zip|-dir /src/$OUT/fixture/arch/big5.zip!"
    "arch-zip-sub|-dir /src/$OUT/fixture/arch/big5.zip!docs"
    "arch-tgz|-dir /src/$OUT/fixture/arch/dot.tar.gz!"
    "arch-lha|-dir /src/testdata/lzh/dosbox-sample.lha!DOSBox"
    "arch-rar|-dir /src/internal/archive/testdata/sample.rar!"
    "arch-7z|-dir /src/internal/archive/testdata/sample.7z!"
    "arch-arj|-dir /src/testdata/arj/m1.arj!"
    "arch-cab|-dir /src/testdata/cab/mszip.cab!"
    "arch-ace|-dir /src/testdata/ace/blocked.ace!winappdbg-winappdbg_v1.6"
    "arch-arc|-dir /src/testdata/arc/t.arc!"
    "arch-z|-dir /src/testdata/z/real16.Z!"
    "edit-c|-edit /src/$OUT/fixture/edit/sample.c"
    "edit-pas|-edit /src/$OUT/fixture/edit/sample.pas"
    "edit-big5|-edit /src/original/app/whatsnew.txt"
    "edit-small|-edit /src/$OUT/fixture/edit/sample.c -cols 30 -rows 6"
    "img-png|-file /src/docs/ui/shot-main.png"
    "img-ico|-file /src/original/app/WinCV.ico"
    "img-alpha-png|-file /src/android/app/src/main/res/mipmap-xxxhdpi/ic_launcher.png"
    "img-alpha-tga|-file /src/$OUT/fixture/img/grad.tga"
    "img-pgm|-file /src/$OUT/fixture/img/gray.pgm"
    "thumbs-ui|-thumbs /src/docs/ui"
    "thumbs-small|-thumbs /src/$OUT/fixture/img -cols 40 -rows 12"
    # app 層(選單列、模式切換、按鍵分派)。
    "app-main|-app /src/$OUT/fixture"
    "app-menu|-app /src/$OUT/fixture -keys F9"
    "app-menu-setup|-app /src/$OUT/fixture -keys Alt-7"
    "app-menu-sub|-app /src/$OUT/fixture -keys Alt-7,End,Enter"
    "app-view|-app /src/original/app -keys End,Up,Up,Enter"
    "app-hex|-app /src/original/app -keys Home,Down,Down,Down,Enter,H"
    "app-arch|-app /src/$OUT/fixture/arch -keys Down,Enter"
    "app-sort|-app /src/$OUT/fixture -keys S,S,Down,Space,Down,Space"
    "app-prompt|-app /src/$OUT/fixture -keys Down,R"
    "app-find-kind|-app /src/$OUT/fixture -keys W"
    "app-preview|-app /src/$OUT/fixture -keys Alt-P,Down,Down"
    # 「關於」畫面寫著用什麼做的:Go 版是「Go + Ebiten」,Rust 版是「Rust」,差那 10 格。
    "app-about|-app /src/$OUT/fixture -keys Alt-8,Down,Enter|10"
    "app-small|-app /src/$OUT/fixture -keys F9 -cols 40 -rows 12"
    "app-lang-en|-app /src/$OUT/fixture -keys F9,Right,Right -lang en"
    "app-lang-ja|-app /src/$OUT/fixture -keys F9,Right,Right -lang ja"
    "app-lang-hans|-app /src/$OUT/fixture -keys F9,Right,Right -lang zh-Hans"
    "app-touch|-app /src/$OUT/fixture -touch"
    # markdown 檢視(含內嵌圖)與 F1 說明。
    "app-md|-app /src/$OUT/fixture/md -keys Down,Down,Enter"
    "app-md-scroll|-app /src/$OUT/fixture/md -keys Down,Down,Enter,PgDn"
    "app-md-pic|-app /src/$OUT/fixture/md -keys Down,Down,Enter,PgDn,Enter"
    "app-md-narrow|-app /src/$OUT/fixture/md -keys Down,Down,Enter -cols 40 -rows 30"
    "app-help|-app /src/$OUT/fixture -keys F1"
    "app-help-end|-app /src/$OUT/fixture -keys F1,End"
    "app-book-toc|-app /src/$OUT/fixture/book -keys Down,Enter"
    "app-book-ch1|-app /src/$OUT/fixture/book -keys Down,Enter,Enter"
    "app-book-ch2|-app /src/$OUT/fixture/book -keys Down,Enter,Down,Enter"
    "app-book-narrow|-app /src/$OUT/fixture/book -keys Down,Enter,Enter -cols 40 -rows 20"
    "app-book-back|-app /src/$OUT/fixture/book -keys Down,Enter,Enter,End,Up,Enter"
    "app-doc-docx|-app /src/$OUT/fixture/doc/docx -keys Down,Enter"
    "app-doc-docx-end|-app /src/$OUT/fixture/doc/docx -keys Down,Enter,End"
    "app-doc-doc|-app /src/$OUT/fixture/doc/doc -keys Down,Enter"
    "app-doc-rtf|-app /src/$OUT/fixture/doc/rtf -keys Down,Enter"
    "app-doc-pptx|-app /src/$OUT/fixture/doc/pptx -keys Down,Enter"
    "app-doc-pptx-list|-app /src/$OUT/fixture/doc/pptx -keys Down,Enter,End,Up,Enter"
    "app-doc-xlsx|-app /src/$OUT/fixture/doc/xlsx -keys Down,Enter"
    "app-pdf|-app /src/$OUT/fixture/doc/pdf -keys Down,Down,Enter"
    "app-pdf-p2|-app /src/$OUT/fixture/doc/pdf -keys Down,Down,Enter,End,Enter"
    "app-pdf-toc|-app /src/$OUT/fixture/doc/pdf -keys Down,Down,Enter,End,Up,Enter"
    "app-pdf-rich|-app /src/$OUT/fixture/doc/pdf -keys Down,Enter"
    # 字級與全形來源(fontset):10×18 與 12×24 是倚天縮放;12×24 另有原生 24 點字庫。
    "lvl-1018|-half /src/original/app/CVGA1018.FON -file /src/original/app/whatsnew.txt"
    "lvl-1224|-half /src/original/app/cvga1224.FON -file /src/original/app/whatsnew.txt"
    "lvl-1224-24|-half /src/original/app/cvga1224.FON $E24 -file /src/original/app/whatsnew.txt"
    # 後備字型:Big5 以外的字。非原生字級的全形字改走向量字型(反鋸齒),倚天縮放墊後。
    "fb-cjk|-fallback $FB_CJK -file /src/$OUT/fixture/unicode.txt"
    "fb-dejavu|-fallback $FB_DV -file /src/$OUT/fixture/unicode.txt"
    "fb-cjk-1018|-half /src/original/app/CVGA1018.FON -fallback $FB_CJK -file /src/$OUT/fixture/unicode.txt"
    "fb-dejavu-1224|-half /src/original/app/cvga1224.FON -fallback $FB_DV -file /src/$OUT/fixture/unicode.txt"
    "fb-cjk-1224-24|-half /src/original/app/cvga1224.FON $E24 -fallback $FB_CJK -file /src/$OUT/fixture/unicode.txt"
    "fb-bitmap-1018|-half /src/original/app/CVGA1018.FON -bitmap-cjk -fallback $FB_CJK -file /src/$OUT/fixture/unicode.txt"
)
# 不放 GBK 的檔案:文字解碼以 Rust 版(WHATWG)為準,與 Go 版不同是預期的。

"$REPO/tools/cargo.sh" build -q --release -p wincv-celldump
fail=0
for sc in "${SCENES[@]}"; do
    IFS='|' read -r name args known <<< "$sc"
    # ONLY=正規式 只跑名稱符合的場景(例如 ONLY='^app-')。
    [[ -n "${ONLY:-}" && ! $name =~ $ONLY ]] && continue
    nofb=-no-fallback
    case $name in fb-*) nofb= ;; esac
    # 差異以格點回報,格子大小跟著半形字型走(字身高 + 1 列列距)。
    cell="8 16"
    case $args in *CVGA1018*) cell="10 19" ;; *cvga1224*) cell="12 25" ;; esac
    # 兩個容器的工作目錄不同(/src 與 /src/rust),素材目錄用 WINCV_HOME 指定,
    # 自動找的字庫(例如 12×24 的 STD.24M)兩邊才會是同一份。
    export WINCV_HOME=/src
    # shellcheck disable=SC2086
    "$REPO/tools/go.sh" run ./cmd/celldump "${FONTS[@]}" $nofb $args -o "/src/$OUT/$name.go.png" >/dev/null
    # shellcheck disable=SC2086
    "$REPO/tools/cargo.sh" run -q --release -p wincv-celldump --bin celldump -- "${FONTS[@]}" $nofb $args -o "/src/$OUT/$name.rust.png" >/dev/null
    # shellcheck disable=SC2086
    res=$("$REPO/tools/cargo.sh" run -q --release -p wincv-celldump --bin pngdiff -- \
        "/src/$OUT/$name.go.png" "/src/$OUT/$name.rust.png" $cell || true)
    cells=$(sed -n 's/^不同: .*,\([0-9]*\) 格$/\1/p' <<< "$res")
    if [ -z "$cells" ]; then
        printf '%-20s %s\n' "$name" "$(head -1 <<< "$res")"
    elif [ "$cells" = "${known:-}" ]; then
        printf '%-20s 已知差異 %s 格\n' "$name" "$cells"
    else
        printf '%-20s %s\n' "$name" "$res" | head -12
        fail=1
    fi
done
exit $fail
