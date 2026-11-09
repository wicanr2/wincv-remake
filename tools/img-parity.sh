#!/usr/bin/env bash
# Go 版與 Rust 版的圖檔解碼對照:每張圖的像素(非預乘 RGBA8)逐一比較。
#
#   tools/img-parity.sh <語料目錄> <清單檔>
#
# 清單檔每行一個相對於語料目錄的路徑。語料目錄唯讀掛進容器。
# 結果在 .cache/parity/img/:go.tsv、rust.tsv 與兩邊的像素檔,比對摘要印在 stdout。
#
# REF=1 時另外用 Pillow 11.3.0(libjpeg-turbo、libwebp)解清單裡的 JPEG 與 WebP 當第三方
# 真值,印出 Go、Rust 各自與它的差距。這一步要開網路裝 Pillow(容器用完即刪)。
set -euo pipefail
REPO=$(cd "$(dirname "$0")/.." && pwd)
CORPUS=$(cd "${1:?語料目錄}" && pwd)
LIST=$(cd "$(dirname "${2:?清單檔}")" && pwd)/$(basename "$2")
OUT=$REPO/.cache/parity/img
rm -rf "$OUT/go" "$OUT/rust" "$OUT/ref"
mkdir -p "$OUT/go" "$OUT/rust" "$OUT/ref"
: > "$OUT/ref.tsv"
common=(--rm -i --log-opt max-size=10m --log-opt max-file=3 --network none --cpus 2 --memory 4g --pids-limit 512
    -u "$(id -u):$(id -g)" -e HOME=/tmp -v "$REPO":/src -v "$CORPUS":/corpus:ro)
docker run "${common[@]}" -w /src -v "$REPO/.cache/gomod":/gomod -v "$REPO/.cache/gobuild":/gobuild \
    -e GOFLAGS=-buildvcs=false -e GOCACHE=/gobuild -e GOMODCACHE=/gomod -e GOPROXY=off \
    wincv-build:1 sh -c 'go build -o /tmp/imgsum ./tools/imgsum && sed "s|^|/corpus/|" | /tmp/imgsum /src/.cache/parity/img/go' \
    < "$LIST" > "$OUT/go.tsv"
docker run "${common[@]}" -w /src/rust -v "$REPO/.cache/cargo-home":/cargo-home -e CARGO_HOME=/cargo-home \
    -v "$REPO/.cache/cargo-target":/cargo-target -e CARGO_TARGET_DIR=/cargo-target \
    wincv-rust:1 sh -c 'cargo build -q --release -p wincv-oracle && sed "s|^|/corpus/|" | /cargo-target/release/imgsum /src/.cache/parity/img/rust' \
    < "$LIST" > "$OUT/rust.tsv"
if [ -n "${REF:-}" ]; then
    grep -n -iE '\.(jpe?g|webp)$' "$LIST" | awk '{i = index($0, ":"); print (substr($0, 1, i - 1) - 1) "\t" substr($0, i + 1)}' |
    docker run --rm -i --log-opt max-size=10m --log-opt max-file=3 --cpus 2 --memory 2g --pids-limit 256 -e HOME=/tmp \
        -v "$CORPUS":/corpus:ro -v "$REPO/tools":/t:ro -v "$OUT/ref":/ref python:3.12-slim sh -c \
        "pip install -q --target /tmp/pil pillow==11.3.0 >/dev/null 2>&1 && PYTHONPATH=/tmp/pil python3 /t/img-ref-pillow.py /ref; chown -R $(id -u):$(id -g) /ref" \
        > "$OUT/ref.tsv"
fi
python3 -I - "$OUT" "$LIST" <<'PY'
import sys, collections, os
out, lst = sys.argv[1], sys.argv[2]
names = open(lst, encoding='utf-8', errors='surrogateescape').read().split('\n')
def load(f):
    return {l.split('\t')[0]: l.rstrip('\n').split('\t') for l in open(f, encoding='utf-8', errors='replace')}
g, r = load(out + '/go.tsv'), load(out + '/rust.tsv')
cat = collections.Counter(); worst = collections.defaultdict(list)
for i in g:
    a, b = g[i], r.get(i, [i, 'ERR', '(沒有輸出)'])
    ext = names[int(i)].lower().rsplit('.', 1)[-1]
    if a[1] == 'ERR' and b[1] == 'ERR': cat[(ext, '兩邊都失敗')] += 1; continue
    if a[1] == 'ERR': cat[(ext, 'Go 失敗、Rust 解出')] += 1; worst['go-err'].append((names[int(i)], a[2][:80])); continue
    if b[1] == 'ERR': cat[(ext, 'Rust 失敗、Go 解出')] += 1; worst['rs-err'].append((names[int(i)], b[2][:80])); continue
    if a[2:4] != b[2:4]: cat[(ext, '尺寸不同')] += 1; worst['size'].append((names[int(i)], a[2:4], b[2:4])); continue
    if a[4] == b[4]: cat[(ext, '逐像素相同')] += 1; continue
    pa = open(f'{out}/go/{i}.rgba', 'rb').read(); pb = open(f'{out}/rust/{i}.rgba', 'rb').read()
    # 比「疊在黑底上的結果」:先把 RGB 乘上 alpha。全透明像素的 RGB 畫面上看不到,
    # 兩邊的解碼器對它的取值不同(預乘再轉回來會變 0),不算差異。
    def pre(p):
        q = bytearray(p)
        for k in range(0, len(q), 4):
            a = q[k + 3]
            if a != 255:
                q[k] = q[k] * a // 255; q[k + 1] = q[k + 1] * a // 255; q[k + 2] = q[k + 2] * a // 255
        return q
    pa, pb = pre(pa), pre(pb)
    if pa == pb: cat[(ext, '畫面相同(只差透明像素)')] += 1; continue
    diffs = [abs(x - y) for x, y in zip(pa, pb)]
    mx = max(diffs); mean = sum(diffs) / len(diffs)
    k = '差 ≤2' if mx <= 2 else ('平均差 <1' if mean < 1 else '差較大')
    cat[(ext, k)] += 1
    worst[k].append((names[int(i)], mx, round(mean, 3)))
for (ext, k), n in sorted(cat.items()): print(f'{ext:5} {k:16} {n}')
for k, v in worst.items():
    if k in ('差 ≤2',): continue
    print(f'--- {k}(前 8 個)')
    for x in sorted(v, key=lambda t: -t[1] if isinstance(t[1], int) else 0)[:8]: print('  ', *x)
# 第三方參考(REF=1):Go、Rust 各自與 Pillow 的差距。
def pre(p):
    q = bytearray(p)
    for k in range(3, len(q), 4):
        a = q[k]
        if a != 255:
            q[k - 3] = q[k - 3] * a // 255; q[k - 2] = q[k - 2] * a // 255; q[k - 1] = q[k - 1] * a // 255
    return q
ref = collections.defaultdict(list)
for l in open(out + '/ref.tsv'):
    f = l.rstrip('\n').split('\t')
    if len(f) < 3 or f[1] == 'ERR': continue
    i = f[0]; ext = names[int(i)].lower().rsplit('.', 1)[-1].replace('jpeg', 'jpg')
    rb = pre(open(f'{out}/ref/{i}.rgba', 'rb').read())
    row = []
    for side in ('go', 'rust'):
        p = f'{out}/{side}/{i}.rgba'
        x = open(p, 'rb').read() if os.path.exists(p) else b''
        if len(x) != len(rb): row = None; break
        d = [abs(u - v) for u, v in zip(pre(x), rb)]
        row.append((sum(d) / len(d), max(d)))
    if row: ref[ext].append(row)
for ext, rows in sorted(ref.items()):
    n = len(rows)
    gm, rm = sum(r[0][0] for r in rows) / n, sum(r[1][0] for r in rows) / n
    gx, rx = sum(r[0][1] for r in rows) / n, sum(r[1][1] for r in rows) / n
    print(f'參考 {ext}: {n} 張;與 Pillow 的平均差 Go {gm:.3f} / Rust {rm:.3f};平均最大差 Go {gx:.1f} / Rust {rx:.1f};'
          f'完全相同 Go {sum(1 for r in rows if r[0][1] == 0)} / Rust {sum(1 for r in rows if r[1][1] == 0)};'
          f'Rust 較接近 {sum(1 for r in rows if r[1][0] < r[0][0])} 張')
PY
