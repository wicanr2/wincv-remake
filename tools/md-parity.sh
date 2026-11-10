#!/usr/bin/env bash
# Go 版與 Rust 版的 markdown 排版對照:同一份檔案、同一個寬度,Parse 的區塊與
# Layout 的每一列(Piece 的文字、顏色、底線、href 與圖的格位)逐位元比對。
#
#   tools/md-parity.sh
#
# 對象是 repo 內所有 *.md(排除 .cache/、rust/target、node_modules、.git),
# 加上這裡產生的一份邊角案例與一份不是 UTF-8 的檔案,再加上 mdsum 的 -synth
# (手組的區塊:Pre、自訂清單記號、表格、各種尺寸的圖)。每份各跑 cols=40、80、120。
# 結果在 .cache/parity/md/。
set -euo pipefail
REPO=$(cd "$(dirname "$0")/.." && pwd)
OUT=$REPO/.cache/parity/md
WIDTHS=40,80,120
rm -rf "$OUT/go" "$OUT/rust"
mkdir -p "$OUT/go" "$OUT/rust"

# 邊角案例:跳脫多位元組字、巢狀連結、沒收尾的圍籬與反引號、CRLF、全形空白、
# 表格格子裡的 tab、溢位的清單序號、接在清單後面的 setext 底線。
printf '%s\r\n' '# 標題 C# ##' 'CRLF 的段落\中\*星號\* `沒收尾' '' > "$OUT/edge.md"
cat >> "$OUT/edge.md" <<'MD'
[![徽章](b.svg)](https://ci/x "標題") 後面 [壞連結](沒收尾 與 [x]
**粗 *斜* 還是粗** ~~刪~~ ~~沒收尾 _底線_ __雙底線__ ***三個*** ``雙`反`引號``

　全形空白開頭的段落　　中間　也有
- 項目一
	- tab 縮排的項目
99999999999999999999. 溢位的序號
3) 括號序號
---

- 清單後的 setext
===
> 引言一
> > 巢狀引言 **粗**
>
![獨立的圖](a.png "標題")

![圖](a.png) 後面有字

| 名稱	有 tab | 對齊 |   置中 |
|:---|---:|:---:|
| 中文字很長很長很長很長很長很長很長很長很長很長很長 | 1 | x |
| 只有一格 |
|||
不是表格的列

~~~python
	tab 開頭的碼	中間也有 tab
中文	後面的 tab
```
沒收尾的圍籬會吃到檔尾
MD
printf 'abc\xff\xfe def\n' > "$OUT/bad.md"

cd "$REPO"
mapfile -t FILES < <(find . \( -path ./.cache -o -path ./rust/target -o -name node_modules -o -path ./.git \) -prune \
    -o -type f -name '*.md' -print | sed 's|^\./||' | LC_ALL=C sort)
FILES+=(".cache/parity/md/edge.md" ".cache/parity/md/bad.md" "-synth")

"$REPO/tools/cargo.sh" build -q --release -p wincv-oracle --bin mdsum

common=(--rm --log-opt max-size=10m --log-opt max-file=3 --network none --cpus 2 --memory 2g --pids-limit 256
    -u "$(id -u):$(id -g)" -e HOME=/tmp -v "$REPO":/src:ro -w /src)
docker run "${common[@]}" -v "$REPO/.cache/gomod":/gomod -v "$REPO/.cache/gobuild":/gobuild \
    -e GOFLAGS=-buildvcs=false -e GOCACHE=/gobuild -e GOMODCACHE=/gomod -e GOPROXY=off \
    wincv-build:1 go run ./tools/mdsum "$WIDTHS" "${FILES[@]}" > "$OUT/all.go"
docker run "${common[@]}" -v "$REPO/.cache/cargo-target":/cargo-target:ro \
    wincv-rust:1 /cargo-target/release/mdsum "$WIDTHS" "${FILES[@]}" > "$OUT/all.rust"

# 依檔案與寬度切開:n.blocks 是區塊,n.c<寬度> 是那個寬度的排版結果。
split() {
    awk -v d="$2" -F'\t' '
        $1 == "F" { n++; if (f) close(f); f = d "/" n ".blocks" }
        $1 == "C" { if (f) close(f); f = d "/" n ".c" $2 }
        { print > f }' "$1"
}
split "$OUT/all.go" "$OUT/go"
split "$OUT/all.rust" "$OUT/rust"

fail=0 same=0 total=0
IFS=, read -ra WS <<< "$WIDTHS"
for i in "${!FILES[@]}"; do
    n=$((i + 1))
    for part in blocks "${WS[@]/#/c}"; do
        # 略過的輸入(E 列)沒有排版結果,兩邊都沒有那個檔就不算。
        [ -f "$OUT/go/$n.$part" ] || [ -f "$OUT/rust/$n.$part" ] || continue
        total=$((total + 1))
        if [ -f "$OUT/go/$n.$part" ] && cmp -s "$OUT/go/$n.$part" "$OUT/rust/$n.$part"; then
            same=$((same + 1))
        else
            printf '不同:%-50s %s\n' "${FILES[$i]}" "$part"
            fail=1
        fi
    done
done
skipped=$(grep -c $'^E\t' "$OUT/all.go" || true)
printf '%d 份輸入 × (區塊 + %d 種寬度):%d / %d 相同(略過的輸入 %d 份)\n' \
    "${#FILES[@]}" "${#WS[@]}" "$same" "$total" "$skipped"
cmp -s "$OUT/all.go" "$OUT/all.rust" && echo "整份輸出逐位元相同($(wc -c < "$OUT/all.go") 位元組)"
exit $fail
