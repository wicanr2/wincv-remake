#!/usr/bin/env bash
# Go 版與 Rust 版的 celldump 逐像素比對,跑多個場景。
#
#   tools/rust-parity.sh
#
# 兩邊用同一組原版字型(original/app/cvga.fon 與 original/eten/*.15),關掉
# 後備字型。場景有示範畫面與幾個檔案列表;檔案列表的時間依容器的時區
# (兩邊都沒設 TZ,即 UTC)。結果寫在 .cache/parity/,差異以 cell 格點列出。
set -euo pipefail
REPO=$(cd "$(dirname "$0")/.." && pwd)
OUT=.cache/parity
mkdir -p "$REPO/$OUT"
for f in original/app/cvga.fon original/eten/STDFONT.15 original/eten/SPCFONT.15; do
    [ -f "$REPO/$f" ] || { echo "缺少 $f(先跑 tools/setup-wine-oracle.sh 與 tools/setup-eten.sh)" >&2; exit 2; }
done
# 檔案列表比對用的目錄:tools/parity-fixture.py 以固定內容與時間戳產生。
if [ ! -d "$REPO/$OUT/fixture" ]; then
    docker run --rm --log-opt max-size=10m --log-opt max-file=3 --network none --memory 256m --cpus 1 \
        --pids-limit 64 -u "$(id -u):$(id -g)" -v "$REPO/$OUT":/p -v "$REPO/tools":/t:ro python:3.12-slim \
        python3 -I /t/parity-fixture.py /p/fixture
fi
FONTS=(-half /src/original/app/cvga.fon -eten-std /src/original/eten/STDFONT.15 -eten-spc /src/original/eten/SPCFONT.15)

# 場景:名稱|celldump 的額外參數[|已知差異的格數]
#
# 已知差異的格數留給「Go 版有已知 bug、Rust 版是對的」的情況;格數吻合就不算失敗。
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
)
# 不放 GBK 的檔案:文字解碼以 Rust 版(WHATWG)為準,與 Go 版不同是預期的。

"$REPO/tools/cargo.sh" build -q --release -p wincv-celldump
fail=0
for sc in "${SCENES[@]}"; do
    IFS='|' read -r name args known <<< "$sc"
    # shellcheck disable=SC2086
    "$REPO/tools/go.sh" run ./cmd/celldump "${FONTS[@]}" -no-fallback $args -o "/src/$OUT/$name.go.png" >/dev/null
    # shellcheck disable=SC2086
    "$REPO/tools/cargo.sh" run -q --release -p wincv-celldump --bin celldump -- "${FONTS[@]}" $args -o "/src/$OUT/$name.rust.png" >/dev/null
    res=$("$REPO/tools/cargo.sh" run -q --release -p wincv-celldump --bin pngdiff -- \
        "/src/$OUT/$name.go.png" "/src/$OUT/$name.rust.png" 8 16 || true)
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
