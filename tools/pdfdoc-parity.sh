#!/usr/bin/env bash
# PDF 取文字、書籤、圖片、畫整頁:Go 版 internal/pdfdoc 對 Rust 版 wincv-pdfdoc(hayro),
# 再拿 poppler 與 LibreOffice 當第三方真值。
#
#   tools/pdfdoc-parity.sh                       # 預設語料 ~/cht,每個專案最多抽 20 個
#   CORPUS=/some/dir PER=10 PAGES=20 LO=0 tools/pdfdoc-parity.sh
#   FROM=lo tools/pdfdoc-parity.sh             # 只重跑 LibreOffice 之後的步驟
#
# 1. 清單:internal/pdf/testdata 全收,再從語料依第一層目錄分層抽樣(固定亂數種子),
#    內容相同的檔案只留第一個。
# 2. Go 與 Rust 的 pdfsum 各跑一次(前 PAGES 頁;第 1 頁以 96 dpi 畫成 PNG)。
# 3. poppler(minidocks/poppler):pdfinfo -box、pdftotext -layout、pdfimages -list、
#    pdftoppm 第 1 頁(-cropbox:與檢視器一樣只畫 CropBox,hayro 也是)。LibreOffice 第 1 頁轉 PNG(LO=0 跳過,約 5 秒一個檔)。
# 4. tools/inkdiff 比頁面圖,Python 分層統計。結果在 .cache/parity/pdfdoc/,
#    摘要是 report.txt,每個檔案一列在 files.tsv。
#
# 所有容器都不開網路、唯讀掛語料,只寫 .cache/parity/pdfdoc/。
set -euo pipefail
REPO=$(cd "$(dirname "$0")/.." && pwd)
CORPUS=${CORPUS:-$HOME/cht}
PER=${PER:-20}
PAGES=${PAGES:-20}
LO=${LO:-1}
# FROM=lo:沿用上一次的清單、pdfsum 與 poppler 輸出,從 LibreOffice 起重跑;FROM=ink:再跳過 LibreOffice。
FROM=${FROM:-all}
TMO=${TMO:-120}
POP_IMG=${POP_IMG:-minidocks/poppler:latest}
LO_IMG=${LO_IMG:-linuxserver/libreoffice:latest}
OUT=$REPO/.cache/parity/pdfdoc
[ -d "$CORPUS" ] || { echo "沒有語料目錄 $CORPUS" >&2; exit 2; }
[ -d /usr/share/fonts ] || { echo "沒有 /usr/share/fonts" >&2; exit 2; }
case $FROM in
    all) rm -rf "$OUT/go" "$OUT/rust" "$OUT/pop" "$OUT/lo" "$OUT/ink" ;;
    lo) rm -rf "$OUT/lo" "$OUT/ink" ;;
    ink) rm -rf "$OUT/ink" ;;
    *) echo "FROM 只能是 all、lo、ink" >&2; exit 2 ;;
esac
mkdir -p "$OUT/go" "$OUT/rust" "$OUT/pop" "$OUT/lo" "$OUT/ink"
common=(--rm -i --log-opt max-size=10m --log-opt max-file=3 --network none --cpus 2 --memory 3g --pids-limit 256
    -u "$(id -u):$(id -g)" -e HOME=/tmp -v "$CORPUS":/corpus:ro -v "$OUT":/out -v /usr/share/fonts:/usr/share/fonts:ro)

# 語料裡的 repo 路徑(testdata 要全收)。repo 不在語料底下時另外掛進去。
case "$REPO/" in
    "$CORPUS"/*) TD=/corpus/${REPO#"$CORPUS"/}/internal/pdf/testdata; extra=() ;;
    *) TD=/td; extra=(-v "$REPO/internal/pdf/testdata":/td:ro) ;;
esac

if [ "$FROM" = all ]; then
# 1. 清單
docker run "${common[@]}" "${extra[@]+"${extra[@]}"}" python:3.12-slim python3 -I - "$PER" "$TD" <<'PY'
import hashlib, os, random, sys
per, td = int(sys.argv[1]), sys.argv[2]
skip = {'.git', '.cache', 'node_modules', 'target', 'dist', 'dist-all'}
seen, out = set(), []
def digest(p):
    h = hashlib.sha256()
    with open(p, 'rb') as f:
        for b in iter(lambda: f.read(1 << 20), b''):
            h.update(b)
    return h.digest()
for f in sorted(os.listdir(td)):
    if f.endswith('.pdf'):
        p = os.path.join(td, f)
        seen.add(digest(p)); out.append(p)
groups = {}
for root, dirs, files in os.walk('/corpus'):
    dirs[:] = sorted(d for d in dirs if d not in skip)
    for f in sorted(files):
        p = os.path.join(root, f)
        if not f.lower().endswith('.pdf') or not os.path.isfile(p):
            continue
        if os.path.getsize(p) > 80 << 20:
            continue
        h = digest(p)
        if h not in seen:
            seen.add(h)
            groups.setdefault(p.split('/')[2], []).append(p)
rng = random.Random(20261110)
for g in sorted(groups):
    ps = groups[g]
    out += sorted(rng.sample(ps, min(per, len(ps))))
open('/out/list.txt', 'w', encoding='utf-8').write(''.join(p + '\n' for p in out))
print(f'{len(out)} 個 PDF(testdata 之外內容不重複的共 {sum(map(len, groups.values()))} 個,{len(groups)} 個專案)', file=sys.stderr)
PY

# 2. 建置與兩邊的 pdfsum
"$REPO/tools/go.sh" build -o /src/.cache/parity/pdfdoc/gopdfsum ./tools/pdfsum
"$REPO/tools/go.sh" build -o /src/.cache/parity/pdfdoc/inkdiff ./tools/inkdiff
"$REPO/tools/cargo.sh" build -q --release -p wincv-pdfdoc --example pdfsum
cp "$REPO/.cache/cargo-target/release/examples/pdfsum" "$OUT/rspdfsum"

# 每個檔案一個子行程、各自限時;一個檔案卡住不拖累其他檔案。
loop='i=0; while IFS= read -r f; do mkdir -p "$D/$i"; S=$(date +%s%N);
  timeout '"$TMO"' "$BIN" -pages '"$PAGES"' -shot "$D/$i" -shotpages 1 "$f" > "$D/$i.txt" 2> "$D/$i.err"; echo "$i $? $(( ($(date +%s%N) - S) / 1000000 ))" >> "$D/status";
  i=$((i+1)); done < /out/list.txt'
docker run "${common[@]}" "${extra[@]+"${extra[@]}"}" -e D=/out/go -e BIN=/out/gopdfsum wincv-build:1 sh -c "$loop"
docker run "${common[@]}" "${extra[@]+"${extra[@]}"}" -e D=/out/rust -e BIN=/out/rspdfsum wincv-rust:1 sh -c "$loop"
echo "Go / Rust 的 pdfsum 跑完" >&2

# 3. poppler 與 LibreOffice
docker run "${common[@]}" "${extra[@]+"${extra[@]}"}" --entrypoint sh "$POP_IMG" -c '
i=0; while IFS= read -r f; do
  timeout '"$TMO"' pdfinfo -box -f 1 -l '"$PAGES"' "$f" > /out/pop/$i.info 2>/dev/null; echo "$i $?" >> /out/pop/status
  timeout '"$TMO"' pdftotext -layout -enc UTF-8 -f 1 -l '"$PAGES"' "$f" /out/pop/$i.txt 2>/dev/null
  timeout '"$TMO"' pdfimages -list -f 1 -l '"$PAGES"' "$f" > /out/pop/$i.img 2>/dev/null
  timeout '"$TMO"' pdftoppm -cropbox -f 1 -l 1 -r 96 -png -singlefile "$f" /out/pop/$i 2>/dev/null
  i=$((i+1)); done < /out/list.txt'
echo "poppler 跑完" >&2
fi

if [ "$LO" != 0 ] && [ "$FROM" != ink ]; then
    # [雷] 逾時只送 SIGTERM 的話,oosplash 會留下來空等(0% CPU),之後每個檔案都卡滿
    # 逾時。所以要 -k 補 SIGKILL,每個檔案結束後再把殘留的 soffice 行程清掉。
    # 這些行程都在這個一次性容器裡,清的是自己起的。
    docker run "${common[@]}" "${extra[@]+"${extra[@]}"}" -e HOME=/out/lo --entrypoint sh "$LO_IMG" -c '
    mkdir -p /out/lo/in /out/lo/png; i=0; while IFS= read -r f; do
      ln -sf "$f" /out/lo/in/$i.pdf
      timeout -k 5 '"$TMO"' /usr/bin/soffice --headless -env:UserInstallation=file:///out/lo/.lo \
        --convert-to png --outdir /out/lo/png /out/lo/in/$i.pdf >/dev/null 2>&1
      echo "$i $?" >> /out/lo/status
      pkill -9 -f oosplash 2>/dev/null; pkill -9 -f soffice.bin 2>/dev/null
      i=$((i+1)); done < /out/list.txt'
    echo "LibreOffice 跑完" >&2
fi

# 4. 頁面圖的墨水分布:Go、Rust 各自對 poppler 與 LibreOffice,以及 Go 對 Rust。
docker run "${common[@]}" wincv-build:1 sh -c '
n=$(wc -l < /out/list.txt); i=0; while [ $i -lt $n ]; do
  g=/out/go/$i/1.png; r=/out/rust/$i/1.png; p=/out/pop/$i.png; l=/out/lo/png/$i.png
  for pair in "go-pop $g $p" "rs-pop $r $p" "go-lo $g $l" "rs-lo $r $l" "go-rs $g $r"; do
    set -- $pair
    if [ -s "$2" ] && [ -s "$3" ]; then /out/inkdiff "$3" "$2" > /out/ink/$i.$1 2>&1 || true; fi
  done
  i=$((i+1)); done'

# 5. 分層統計
docker run "${common[@]}" python:3.12-slim python3 -I - <<'PY' | tee "$OUT/report.txt"
import collections, os, re

names = open('/out/list.txt', encoding='utf-8').read().splitlines()

def unesc(s):
    return re.sub(r'\\(.)', lambda m: {'t': '\t', 'n': '\n', 'r': '\r'}.get(m.group(1), m.group(1)), s)

def status(d):
    st = {}
    for l in open(f'/out/{d}/status', encoding='utf-8'):
        i, code, ms = l.split()
        st[int(i)] = (int(code), int(ms))
    return st

def parse(path):
    r = dict(pages=None, sizes={}, marks=[], lines=collections.defaultdict(list), imgs=collections.defaultdict(dict), errors=[])
    if not os.path.exists(path):
        return r
    for l in open(path, encoding='utf-8', errors='replace'):
        f = l.rstrip('\n').split('\t')
        k = f[0]
        if k == 'pages':
            r['pages'] = int(f[1])
        elif k == 'size':
            r['sizes'][int(f[1])] = (f[2], f[3])
        elif k == 'bookmark':
            r['marks'].append((int(f[1]), int(f[2]), unesc(f[3])))
        elif k == 'line':
            r['lines'][int(f[1])].append((float(f[2]), int(f[3]), unesc(f[4])))
        elif k == 'image':
            r['imgs'][int(f[1])][unesc(f[2])] = tuple(f[3:])
        elif k == 'error':
            r['errors'].append('\t'.join(f[1:]))
    return r

def nows(s):
    return re.sub(r'\s+', '', s)

def pop_pages(i):
    p = f'/out/pop/{i}.txt'
    if not os.path.exists(p):
        return None
    return open(p, encoding='utf-8', errors='replace').read().split('\f')

def pop_boxes(i):
    out = {}
    p = f'/out/pop/{i}.info'
    if os.path.exists(p):
        for l in open(p, encoding='utf-8', errors='replace'):
            m = re.match(r'Page\s+(\d+) MediaBox:\s+([-\d.]+)\s+([-\d.]+)\s+([-\d.]+)\s+([-\d.]+)', l)
            if m:
                x0, y0, x1, y1 = map(float, m.groups()[1:])
                out[int(m.group(1))] = ('%.2f' % abs(x1 - x0), '%.2f' % abs(y1 - y0))
    return out

def cropped(i):
    """第 1 頁的 CropBox 比 MediaBox 小。Go 版與 LibreOffice 畫 MediaBox,hayro 與 pdftoppm -cropbox 畫 CropBox。"""
    p = f'/out/pop/{i}.info'
    if not os.path.exists(p):
        return False
    t = open(p, encoding='utf-8', errors='replace').read()
    m = re.search(r'Page\s+1 MediaBox:(.*)', t); c = re.search(r'Page\s+1 CropBox:(.*)', t)
    return bool(m and c) and [round(float(x)) for x in m.group(1).split()] != [round(float(x)) for x in c.group(1).split()]

def pop_images(i):
    c = collections.Counter()
    p = f'/out/pop/{i}.img'
    if os.path.exists(p):
        for l in open(p, encoding='utf-8', errors='replace').read().splitlines()[2:]:
            f = l.split()
            if len(f) > 11 and f[2] in ("image", "stencil"):
                c[(int(f[0]), f[10])] += 1
    per = collections.Counter(pg for pg, _ in c)
    return per

def f1(a, b):
    ca, cb = collections.Counter(a), collections.Counter(b)
    inter = sum((ca & cb).values())
    if not a and not b:
        return 1.0
    if not a or not b:
        return 0.0
    p, r = inter / len(a), inter / len(b)
    return 0 if p + r == 0 else 2 * p * r / (p + r)

def ink(i, pair):
    p = f'/out/ink/{i}.{pair}'
    if not os.path.exists(p):
        return None
    t = open(p, encoding='utf-8', errors='replace').read()
    m = re.search(r'平均密度差\s+([\d.]+)', t); c = re.search(r'相關係數\s+([-\d.]+|NaN)', t)
    if not m or not c:
        return None
    try:
        return float(m.group(1)), float(c.group(1))
    except ValueError:
        return float(m.group(1)), 0.0

gst, rst = status('go'), status('rust')
S = collections.Counter()
ex = collections.defaultdict(list)
def note(cat, msg, n=4):
    S[cat] += 1
    if len(ex[cat]) < n:
        ex[cat].append(msg)

rows = []
gms, rms = [], []
for i, name in enumerate(names):
    g, r = parse(f'/out/go/{i}.txt'), parse(f'/out/rust/{i}.txt')
    short = name.replace('/corpus/', '')
    gc, gt = gst.get(i, (None, 0)); rc, rt = rst.get(i, (None, 0))
    if gc == 124: note('Go 逾時', short)
    if rc == 124: note('Rust 逾時', short)
    gms.append(gt); rms.append(rt)
    gopen = [e for e in g['errors'] if e.startswith('open')]
    ropen = [e for e in r['errors'] if e.startswith('open')]
    if g['pages'] is None or r['pages'] is None:
        if g['pages'] is None and r['pages'] is None:
            note('兩邊都打不開', f'{short}: Go「{gopen[:1]}」 Rust「{ropen[:1]}」', 20)
        elif g['pages'] is None:
            note('只有 Go 打不開', f'{short}: {gopen[:1] or g["errors"][:1] or ["(沒有輸出,code %s)" % gc]}', 20)
        else:
            note('只有 Rust 打不開', f'{short}: {ropen[:1] or r["errors"][:1] or ["(沒有輸出,code %s)" % rc]}', 20)
        rows.append((i, short, 'open', '', ''))
        continue
    S['兩邊都打得開'] += 1
    # 段落 panic 與錯誤
    for side, d in (('Go', g), ('Rust', r)):
        for e in d['errors']:
            sec = e.split('\t')[0]
            note(f'{side} 段落錯誤:{sec}', f'{short}: {e[:160]}')
    # 頁數與頁面大小
    pb = pop_boxes(i)
    if g['pages'] == r['pages']:
        S['頁數相同'] += 1
    else:
        note('頁數不同', f'{short}: Go {g["pages"]} / Rust {r["pages"]}', 10)
    sz_diff = [p for p in g['sizes'] if p in r['sizes'] and g['sizes'][p] != r['sizes'][p]]
    if sz_diff:
        p = sz_diff[0]
        who = 'Go' if pb.get(p) == g['sizes'][p] else 'Rust' if pb.get(p) == r['sizes'][p] else '都不是'
        note('頁面大小不同', f'{short} 第 {p} 頁: Go {g["sizes"][p]} / Rust {r["sizes"][p]} / poppler MediaBox {pb.get(p)}(與 poppler 相同:{who})', 10)
    else:
        S['頁面大小相同(檔)'] += 1
    # 書籤
    gm, rm = g['marks'], r['marks']
    if gm == rm:
        S['書籤相同(檔)'] += 1
        if gm: S['書籤相同且非空(檔)'] += 1
    else:
        if not gm and any(e.startswith('outline') for e in g['errors']):
            note('書籤:Go panic', f'{short}: Rust {len(rm)} 筆')
        elif len(gm) != len(rm):
            note('書籤:筆數不同', f'{short}: Go {len(gm)} / Rust {len(rm)}')
        elif [m[2] for m in gm] != [m[2] for m in rm]:
            k = next(j for j in range(len(gm)) if gm[j][2] != rm[j][2])
            note('書籤:標題不同', f'{short}: Go {gm[k][2]!r} / Rust {rm[k][2]!r}')
        else:
            k = next(j for j in range(len(gm)) if gm[j] != rm[j])
            note('書籤:頁碼或層級不同', f'{short}: Go {gm[k]} / Rust {rm[k]}')
    # 文字
    pp = pop_pages(i)
    npg = min(g['pages'], r['pages'], max(list(g['sizes']) + [0]))
    for p in range(1, npg + 1):
        gl, rl = g['lines'].get(p, []), r['lines'].get(p, [])
        gs, rs = nows(''.join(t for _, _, t in gl)), nows(''.join(t for _, _, t in rl))
        ps = nows(pp[p - 1]) if pp and p - 1 < len(pp) else None
        S['頁(文字比對)'] += 1
        if not gs and not rs:
            S['頁:兩邊都沒有文字'] += 1
            if ps: S['頁:兩邊都沒有文字但 poppler 有'] += 1
            continue
        if gs != rs:
            if ps is None:
                who = '無 poppler'
            else:
                fg, fr = f1(gs, ps), f1(rs, ps)
                who = 'Rust 較近' if fr > fg + 1e-9 else 'Go 較近' if fg > fr + 1e-9 else '一樣近'
            cat = '只有 Rust 有文字' if not gs else '只有 Go 有文字' if not rs else '內容不同'
            j = next((k for k in range(min(len(gs), len(rs))) if gs[k] != rs[k]), min(len(gs), len(rs)))
            note(f'頁:{cat}({who})', f'{short} 第 {p} 頁 @{j}: Go …{gs[max(0,j-8):j+24]!r} / Rust …{rs[max(0,j-8):j+24]!r} / poppler F1 Go {f1(gs, ps or ""):.3f} Rust {f1(rs, ps or ""):.3f}')
            continue
        S['頁:內容相同'] += 1
        gk, rk = [nows(t) for _, _, t in gl], [nows(t) for _, _, t in rl]
        if gk != rk:
            popl = set(nows(x) for x in (pp[p - 1].splitlines() if ps else []) if x.strip())
            hg, hr = sum(x in popl for x in gk), sum(x in popl for x in rk)
            who = 'Rust 較近' if hr > hg else 'Go 較近' if hg > hr else '一樣近'
            j = next((k for k in range(min(len(gk), len(rk))) if gk[k] != rk[k]), 0)
            note(f'頁:行的切法不同({who})', f'{short} 第 {p} 頁 {len(gl)}/{len(rl)} 列,第 {j} 列: Go {gl[j][2][:50] if j < len(gl) else None!r} / Rust {rl[j][2][:50] if j < len(rl) else None!r}')
            continue
        S['頁:行的切法相同'] += 1
        # 空白(詞間空格)
        if [t for _, _, t in gl] != [t for _, _, t in rl]:
            ptok = collections.Counter(pp[p - 1].split()) if ps else collections.Counter()
            tg = collections.Counter(w for _, _, t in gl for w in t.split())
            tr = collections.Counter(w for _, _, t in rl for w in t.split())
            hg, hr = sum((tg & ptok).values()), sum((tr & ptok).values())
            who = 'Rust 較近' if hr > hg else 'Go 較近' if hg > hr else '一樣近'
            k = next(k for k in range(len(gl)) if gl[k][2] != rl[k][2])
            note(f'頁:詞間空白不同({who})', f'{short} 第 {p} 頁: Go {gl[k][2][:60]!r} / Rust {rl[k][2][:60]!r}')
        else:
            S['頁:逐字相同(含空白)'] += 1
        # 座標
        dy = max(abs(a[0] - b[0]) for a, b in zip(gl, rl))
        S['頁:Y 差 ≤0.01' if dy <= 0.01 else '頁:Y 差 ≤0.5' if dy <= 0.5 else '頁:Y 差 >0.5'] += 1
        if dy > 0.5:
            note('座標:Y 差 >0.5', f'{short} 第 {p} 頁 最大 {dy:.2f}', 6)
        bad = sum(a[1] != b[1] for a, b in zip(gl, rl))
        S['頁:縮排全同' if bad == 0 else '頁:縮排有不同'] += 1
        if bad:
            k = next(k for k in range(len(gl)) if gl[k][1] != rl[k][1])
            note('座標:縮排不同', f'{short} 第 {p} 頁 {bad}/{len(gl)} 列, 例 {gl[k][2][:30]!r} Go {gl[k][1]} / Rust {rl[k][1]}', 6)
    # 圖片
    pim = pop_images(i)
    for p in range(1, npg + 1):
        gi, ri = g['imgs'].get(p, {}), r['imgs'].get(p, {})
        if not gi and not ri:
            continue
        S['頁(有圖)'] += 1
        pc = pim.get(p, 0)
        if set(gi) != set(ri):
            who = 'Rust 與 poppler 張數相同' if len(ri) == pc else 'Go 與 poppler 張數相同' if len(gi) == pc else '都不同於 poppler'
            note(f'圖:名單不同({who})', f'{short} 第 {p} 頁: Go {sorted(gi)[:4]}({len(gi)}) / Rust {sorted(ri)[:4]}({len(ri)}) / poppler {pc}')
        for n in sorted(set(gi) & set(ri)):
            a, b = gi[n], ri[n]
            S['圖(兩邊都有)'] += 1
            if a[1] == b[1]:
                S['圖:位元組相同'] += 1
            elif a[3] == b[3]:
                S['圖:位元組不同、像素相同'] += 1
            elif a[2] == b[2] and a[4] != '-' and b[4] != '-':
                ma = list(map(float, a[4].split(','))); mb = list(map(float, b[4].split(',')))
                d = max(abs(x - y) for x, y in zip(ma, mb))
                cat = '圖:尺寸同、平均色差 ≤2' if d <= 2 else '圖:尺寸同、平均色差 >2'
                note(cat, f'{short} 第 {p} 頁 {n}: Go {a[4]} / Rust {b[4]}')
            else:
                note('圖:尺寸不同或解不開', f'{short} 第 {p} 頁 {n}: Go {a[2]} / Rust {b[2]}')
    # 頁面圖
    vals = {k: ink(i, k) for k in ('go-pop', 'rs-pop', 'go-lo', 'rs-lo', 'go-rs')}
    crop = cropped(i)
    rows.append((i, short, 'crop' if crop else 'ok', vals, (gt, rt)))
    if crop:
        note('頁面圖:第 1 頁 CropBox 小於 MediaBox(不列入下面的比較)', f'{short}: Go 對 poppler {vals["go-pop"]} Rust 對 poppler {vals["rs-pop"]}', 10)
        continue
    for ref in ('pop', 'lo'):
        a, b = vals[f'go-{ref}'], vals[f'rs-{ref}']
        if a and b:
            S[f'頁面圖({ref})'] += 1
            if b[0] + 0.002 < a[0]: note(f'頁面圖({ref}):Rust 較近', f'{short}: Go {a[0]:.4f}/{a[1]:.3f} Rust {b[0]:.4f}/{b[1]:.3f}', 6)
            elif a[0] + 0.002 < b[0]: note(f'頁面圖({ref}):Go 較近', f'{short}: Go {a[0]:.4f}/{a[1]:.3f} Rust {b[0]:.4f}/{b[1]:.3f}', 6)
            else: S[f'頁面圖({ref}):相當(平均差差距 ≤0.002)'] += 1

print(f'檔案 {len(names)} 個')
for k in sorted(S):
    print(f'{S[k]:6d}  {k}')
print()
for k in sorted(ex):
    print(f'## {k}')
    for m in ex[k]:
        print(f'   {m}')
print()
for ref, label in (('pop', 'poppler'), ('lo', 'LibreOffice')):
    for side in ('go', 'rs'):
        v = [x[3][f'{side}-{ref}'] for x in rows if x[2] == 'ok' and x[3].get(f'{side}-{ref}')]
        if v:
            print(f'{side} 對 {label}:{len(v)} 張,平均密度差 {sum(a for a, _ in v) / len(v):.4f},相關係數平均 {sum(c for _, c in v) / len(v):.4f}')
print(f'pdfsum 總時間:Go {sum(gms) / 1000:.1f} 秒,Rust {sum(rms) / 1000:.1f} 秒')
with open('/out/files.tsv', 'w', encoding='utf-8') as f:
    for x in rows:
        f.write('\t'.join(map(str, x)) + '\n')
PY
