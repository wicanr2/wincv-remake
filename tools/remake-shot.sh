#!/usr/bin/env bash
# 把 remake 的**真實視窗**跑起來截圖,不是只跑 headless 光柵器。
#
# 為什麼要這一支:cmd/celldump 驗的是「畫得對不對」,這一支驗的是
# 「Ebiten 這條路徑能不能真的開起來」—— 兩者會壞在不同地方
# (rulebook/82:驗實際打包產物在它自己的執行環境)。
set -euo pipefail

REPO=$(cd "$(dirname "$0")/.." && pwd)
OUT=${1:-$REPO/docs/ui/ebiten-window.png}
DIR=${2:-/src/original/app}
COLS=${3:-74}
ROWS=${4:-22}

# 容器內用主機的 UID/GID 跑,不然產出的檔案屬於 root,主機這邊改不動也刪不掉。
# HOME 要指到容器內寫得進去的地方:go 會想寫 $HOME/.config/go/env,
# 而 root 的家目錄對這個 UID 是唯讀的。
docker run --rm --log-opt max-size=10m --log-opt max-file=3 \
    -u "$(id -u):$(id -g)" -e HOME=/tmp \
    -v "$REPO":/src -w /src \
    -e GOFLAGS=-buildvcs=false -e GOCACHE=/tmp/gocache -e GOMODCACHE=/tmp/gomod \
    -e LIBGL_ALWAYS_SOFTWARE=1 \
    wincv-build:1 sh -c "
go build -o /tmp/wincv ./cmd/wincv
cat > /tmp/run.sh <<EOS
/tmp/wincv -cols $COLS -rows $ROWS -scale 1 $DIR > /tmp/app.log 2>&1 &
sleep 10
xwd -root -silent > /tmp/shot.xwd
EOS
xvfb-run -a -s '-screen 0 800x600x24' sh /tmp/run.sh
if [ -s /tmp/app.log ]; then echo '--- app 輸出 ---'; cat /tmp/app.log; fi
cp /tmp/shot.xwd /src/docs/ui/.shot.xwd
"

python3 "$REPO/tools/xwd2png.py" "$REPO/docs/ui/.shot.xwd" "$OUT"
rm -f "$REPO/docs/ui/.shot.xwd"
echo "$OUT"
