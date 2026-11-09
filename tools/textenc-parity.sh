#!/usr/bin/env bash
# Go 版 textenc(x/text)與 Rust 版(encoding_rs)的對照:判讀結果與 8 種
# 編碼的解碼結果逐檔比 sha256。
#
#   tools/textenc-parity.sh
#
# 語料:original/app/ 的原版檔案、docs/ 與 internal/ 的文字檔,以及
# .cache/parity/textenc/fuzz/ 的隨機資料(gen.py 以固定種子產生)。
set -euo pipefail
REPO=$(cd "$(dirname "$0")/.." && pwd)
OUT=$REPO/.cache/parity/textenc
mkdir -p "$OUT"
cd "$REPO"
{ find original/app -maxdepth 1 -type f; find docs internal -type f \( -name '*.md' -o -name '*.txt' -o -name '*.json' \);
  find .cache/parity/textenc/fuzz -type f; } 2>/dev/null | sort > "$OUT/files.txt"
mapfile -t files < <(sed "s|^|/src/|" "$OUT/files.txt")
"$REPO/tools/go.sh" run ./tools/textencsum "${files[@]}" > "$OUT/go.tsv"
"$REPO/tools/cargo.sh" run -q --release -p wincv-oracle --bin textencsum -- "${files[@]}" > "$OUT/rust.tsv"
echo "檔案 ${#files[@]} 個"
if cmp -s "$OUT/go.tsv" "$OUT/rust.tsv"; then echo "判讀與解碼全部相同"; exit 0; fi
python3 - "$OUT/go.tsv" "$OUT/rust.tsv" <<'PY'
import sys, collections
g = {l.split('\t')[0]: l.rstrip('\n').split('\t') for l in open(sys.argv[1])}
r = {l.split('\t')[0]: l.rstrip('\n').split('\t') for l in open(sys.argv[2])}
det = [(p, g[p][1], r[p][1]) for p in g if g[p][1] != r[p][1]]
print(f"判讀不同 {len(det)} 個")
for d in det[:10]:
    print("  ", *d)
# 解碼不同的分三類:
#   多解出字:字元數相同,Rust 的 U+FFFD 比較少(Go 不認得、Rust 認得的碼位)
#   粒度不同:字元數不同(一段壞位元組換成幾個 U+FFFD 的規則不同)
#   其他:字元數與 U+FFFD 都相同但內容不同(兩邊對到不同的字,最需要注意)
kind = collections.Counter()
other = []
for p in g:
    real = '/fuzz/' not in p
    for a, b in zip(g[p][2].split(), r[p][2].split()):
        if a == b:
            continue
        enc = a.split('=')[0]
        _, gn, gb = a.split('=')[1].split(':')
        _, rn, rb = b.split('=')[1].split(':')
        if gn == rn and int(rb) < int(gb):
            k = '多解出字'
        elif gn != rn:
            k = '粒度不同'
        else:
            k = '其他'
            other.append((p, enc))
        kind[('真實檔案' if real else '隨機資料', enc, k)] += 1
for k, v in sorted(kind.items()):
    print(f"  {k[0]} {k[1]:9} {k[2]}: {v}")
for o in other[:10]:
    print("  其他:", *o)
PY
exit 1
