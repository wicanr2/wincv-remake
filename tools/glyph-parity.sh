#!/usr/bin/env bash
# Go 版與 Rust 版的字型對照:
#   1. 點陣字模(ttf):同一份字型、同一串字、同一個字級,字模逐位元比對。
#   2. 向量外框(vecfont):同一份字型與預設字型組,外框逐段比對(26.6 定點整數)。
#
#   tools/glyph-parity.sh
#
# 主機的 /usr/share/fonts 唯讀掛進兩邊的容器(路徑相同)。字串涵蓋 ASCII、拉丁擴充、希臘、
# 西里爾、框線、繁簡中文、日文假名、韓文與符號。結果在 .cache/parity/glyph/。
set -euo pipefail
REPO=$(cd "$(dirname "$0")/.." && pwd)
OUT=$REPO/.cache/parity/glyph
mkdir -p "$OUT"
python3 -I - "$OUT/chars.txt" <<'PY'
import sys
s = ''.join(chr(c) for c in range(0x21, 0x7f))
s += ''.join(chr(c) for c in range(0xa1, 0x180))                      # 拉丁擴充
s += ''.join(chr(c) for c in range(0x391, 0x3ca)) + 'άέήίόύώΐΰ'       # 希臘(含聲調)
s += ''.join(chr(c) for c in range(0x410, 0x450))                     # 西里爾
s += ''.join(chr(c) for c in range(0x2500, 0x2580, 3))                # 框線
s += ''.join(chr(c) for c in range(0x2190, 0x21a0)) + '①②③★☆♠♥←↑→↓√∞≠≤≥'
s += bytes(range(0xa4, 0xa6)).hex() and ''
big5 = bytearray()
for hi in range(0xa4, 0xc6, 3):
    for lo in list(range(0x40, 0x7f, 5)) + list(range(0xa1, 0xff, 5)):
        big5 += bytes([hi, lo])
s += big5.decode('big5', 'ignore')                                    # 繁體常用字抽樣
s += '简体中文汉字为这个们来对说时国后会还发经现开问东边产给车门'       # 簡體
s += ''.join(chr(c) for c in range(0x3041, 0x3097, 2)) + ''.join(chr(c) for c in range(0x30a1, 0x30f7, 2))
s += ''.join(chr(c) for c in range(0xac00, 0xd7a3, 397))              # 韓文音節抽樣
s += '，。！？「」『』（）《》～、：；…—'
open(sys.argv[1], 'w', encoding='utf-8').write(s)
print(len(s), '個字', file=sys.stderr)
PY
"$REPO/tools/cargo.sh" build -q --release -p wincv-oracle
F=/usr/share/fonts
CASES=(
  "opentype/noto/NotoSansCJK-Regular.ttc 8 16 16 0 0"
  "opentype/noto/NotoSansCJK-Regular.ttc 8 16 16 0 1"
  "opentype/noto/NotoSansCJK-Regular.ttc 12 24 25 0 1"
  "truetype/dejavu/DejaVuSansMono.ttf 8 16 16 0 0"
  "truetype/dejavu/DejaVuSansMono.ttf 10 20 19 0 1"
  "truetype/dejavu/DejaVuSansMono.ttf 8 16 15 1 0"
  "truetype/dejavu/DejaVuSans.ttf 8 16 16 0 0"
  "truetype/liberation/LiberationMono-Regular.ttf 8 16 16 0 1"
  "truetype/liberation/LiberationMono-Regular.ttf 12 24 24 1 0"
  "truetype/arphic/uming.ttc 8 16 16 0 0"
  "truetype/wqy/wqy-zenhei.ttc 8 16 16 0 1"
  "truetype/noto/NotoMono-Regular.ttf 10 20 18 1 0"
)
common=(--rm -i --log-opt max-size=10m --log-opt max-file=3 --network none --cpus 2 --memory 2g --pids-limit 256
    -u "$(id -u):$(id -g)" -e HOME=/tmp -v "$REPO":/src -v "$F":/fonts:ro)
fail=0
for c in "${CASES[@]}"; do
    set -- $c
    [ -f "$F/$1" ] || { echo "跳過(沒有字型) $1"; continue; }
    tag=$(echo "$c" | tr ' /' '__')
    docker run "${common[@]}" -w /src -v "$REPO/.cache/gomod":/gomod -v "$REPO/.cache/gobuild":/gobuild \
        -e GOFLAGS=-buildvcs=false -e GOCACHE=/gobuild -e GOMODCACHE=/gomod -e GOPROXY=off \
        wincv-build:1 sh -c 'go run ./tools/glyphsum "$@"' sh "/fonts/$1" "$2" "$3" "$4" "$5" "$6" < "$OUT/chars.txt" > "$OUT/$tag.go" 2>&1 || true
    docker run "${common[@]}" -w /src/rust -v "$REPO/.cache/cargo-target":/cargo-target \
        wincv-rust:1 /cargo-target/release/glyphsum "/fonts/$1" "$2" "$3" "$4" "$5" "$6" < "$OUT/chars.txt" > "$OUT/$tag.rust" 2>&1 || true
    # 某一邊載不起來字型時,輸出只有一行錯誤訊息。
    for side in go rust; do
        if ! grep -q $'^[UC]+' "$OUT/$tag.$side"; then
            printf '%-60s %s 載不起來:%s\n' "$c" "$side" "$(head -1 "$OUT/$tag.$side")"
            continue 2
        fi
    done
    total=$(wc -l < "$OUT/$tag.go")
    nil=$(grep -c $'\t-$' "$OUT/$tag.go" || true)
    if cmp -s "$OUT/$tag.go" "$OUT/$tag.rust"; then
        printf '%-60s 相同:%d 個字(字型沒有的 %d 個)\n' "$c" "$total" "$nil"
    else
        diff=$(paste "$OUT/$tag.go" "$OUT/$tag.rust" | awk -F'\t' '{n=NF/2; a=""; b=""; for(i=1;i<=n;i++)a=a $i "\t"; for(i=n+1;i<=NF;i++)b=b $i "\t"; if(a!=b)c++} END{print c+0}')
        printf '%-60s 不同:%d / %d 個字\n' "$c" "$diff" "$total"
        fail=1
    fi
done

# 向量外框。「default」是 vecfont 的預設字型組(內嵌 + 系統字型),系統字型目錄
# 以原路徑掛進去,兩邊看到同一份候選清單。
VEC=(
  "opentype/noto/NotoSansCJK-Regular.ttc"
  "truetype/dejavu/DejaVuSans.ttf"
  "truetype/dejavu/DejaVuSansMono.ttf"
  "truetype/liberation/LiberationSerif-Regular.ttf"
  "truetype/noto/NotoMono-Regular.ttf"
  "default"
)
for v in "${VEC[@]}"; do
    arg=()
    if [ "$v" != default ]; then
        [ -f "$F/$v" ] || { echo "跳過(沒有字型) $v"; continue; }
        arg=("/fonts/$v")
    fi
    tag=vec_$(echo "$v" | tr ' /' '__')
    docker run "${common[@]}" -v "$F":/usr/share/fonts:ro -w /src -v "$REPO/.cache/gomod":/gomod -v "$REPO/.cache/gobuild":/gobuild \
        -e GOFLAGS=-buildvcs=false -e GOCACHE=/gobuild -e GOMODCACHE=/gomod -e GOPROXY=off \
        wincv-build:1 sh -c 'go run ./tools/vecsum "$@"' sh "${arg[@]}" < "$OUT/chars.txt" > "$OUT/$tag.go" 2>&1 || true
    docker run "${common[@]}" -v "$F":/usr/share/fonts:ro -w /src/rust -v "$REPO/.cache/cargo-target":/cargo-target \
        wincv-rust:1 /cargo-target/release/vecsum "${arg[@]}" < "$OUT/chars.txt" > "$OUT/$tag.rust" 2>&1 || true
    total=$(wc -l < "$OUT/$tag.go")
    nil=$(grep -c $'\t-$' "$OUT/$tag.go" || true)
    if cmp -s "$OUT/$tag.go" "$OUT/$tag.rust"; then
        printf '%-60s 外框相同:%d 個字(沒有的 %d 個)\n' "vec $v" "$total" "$nil"
    else
        diff=$(diff "$OUT/$tag.go" "$OUT/$tag.rust" | grep -c '^<' || true)
        printf '%-60s 外框不同:%d / %d 個字\n' "vec $v" "$diff" "$total"
        fail=1
    fi
done
exit $fail
