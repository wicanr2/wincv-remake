#!/usr/bin/env bash
# SVG 的第三方對照:Go 版(oksvg + 自畫的 <text>)與 Rust 版(resvg)各自與 Chromium 比。
#
#   tools/svg-parity.sh                 # 預設語料 ~/cht,每個專案最多抽 25 個
#   CORPUS=/some/dir PER=40 tools/svg-parity.sh
#
# 兩邊的光柵器不同(反鋸齒的算法不一樣),本來就不可能逐像素相同,所以改問瀏覽器:
# 以 Rust 版的尺寸在 Chromium(playwright 映像裡的 headless_shell)畫同一個檔案,
# 三者疊在黑底上比 RGB 差值。系統字型目錄唯讀掛進三個容器,看到的是同一批字型。
# 結果在 .cache/parity/svg/,每個檔案一列(svgcmp 的輸出)在 cmp.tsv。
set -euo pipefail
REPO=$(cd "$(dirname "$0")/.." && pwd)
CORPUS=${CORPUS:-$HOME/cht}
PER=${PER:-25}
PW_IMG=${PW_IMG:-mcr.microsoft.com/playwright:v1.56.1-noble}
OUT=$REPO/.cache/parity/svg
[ -d "$CORPUS" ] || { echo "沒有語料目錄 $CORPUS" >&2; exit 2; }
[ -d /usr/share/fonts ] || { echo "沒有 /usr/share/fonts" >&2; exit 2; }
rm -rf "$OUT/go" "$OUT/rust" "$OUT/chrome"
mkdir -p "$OUT/go" "$OUT/rust" "$OUT/chrome"
common=(--rm -i --log-opt max-size=10m --log-opt max-file=3 --network none --cpus 2 --memory 2g --pids-limit 256
    -u "$(id -u):$(id -g)" -e HOME=/tmp -v "$CORPUS":/corpus:ro -v /usr/share/fonts:/usr/share/fonts:ro)

# 1. 清單:依第一層目錄分層抽樣(固定亂數種子,可重現),排除建置與快取目錄。
#    內容相同的檔案(各個工作目錄複製的同一張圖)只留第一個。
docker run "${common[@]}" -v "$OUT":/out python:3.12-slim python3 -I - "$PER" <<'PY'
import hashlib, os, random, sys
per = int(sys.argv[1])
seen = set()
skip = {'.git', '.cache', 'node_modules', 'target', 'dist', 'dist-all'}
groups = {}
for root, dirs, files in os.walk('/corpus'):
    dirs[:] = sorted(d for d in dirs if d not in skip)
    for f in sorted(files):
        p = os.path.join(root, f)
        if f.lower().endswith('.svg') and os.path.getsize(p) < 2 << 20:
            h = hashlib.sha256(open(p, 'rb').read()).digest()
            if h not in seen:
                seen.add(h)
                groups.setdefault(p.split('/')[2], []).append(p)
rng = random.Random(20261109)
out = []
for g in sorted(groups):
    ps = groups[g]
    out += sorted(rng.sample(ps, min(per, len(ps))))
open('/out/list.txt', 'w', encoding='utf-8').write(''.join(p + '\n' for p in out))
print(f'{len(out)} 個 SVG(內容不重複的共 {sum(map(len, groups.values()))} 個,{len(groups)} 個專案)', file=sys.stderr)
PY

# 2. Go 與 Rust 各自解(imgsum:每張寫非預乘 RGBA,stdout 一列尺寸)。
docker run "${common[@]}" -v "$REPO":/src -w /src -v "$REPO/.cache/gomod":/gomod -v "$REPO/.cache/gobuild":/gobuild \
    -e GOFLAGS=-buildvcs=false -e GOCACHE=/gobuild -e GOMODCACHE=/gomod -e GOPROXY=off \
    wincv-build:1 go run ./tools/imgsum /src/.cache/parity/svg/go < "$OUT/list.txt" > "$OUT/go.tsv"
"$REPO/tools/cargo.sh" build -q --release -p wincv-oracle
docker run "${common[@]}" -v "$REPO":/src -v "$REPO/.cache/cargo-target":/cargo-target \
    wincv-rust:1 /cargo-target/release/imgsum /src/.cache/parity/svg/rust < "$OUT/list.txt" > "$OUT/rust.tsv"

# 3. Chromium:以 Rust 版的尺寸、黑底截圖。視窗至少 64×64(太小的視窗截圖會被放大),
#    比對時只取左上角那一塊。每個檔案限 20 秒。
#    --init:Python 當 PID 1 不會回收 Chromium 結束後的子行程,殭屍累積到 pids 上限之後
#    每一張都啟動失敗(實測第 96 張起全部沒有截圖)。
docker run "${common[@]}" --init --memory 3g -v "$OUT":/out "$PW_IMG" python3 -I - <<'PY'
import glob, os, subprocess, sys, urllib.parse
shell = glob.glob('/ms-playwright/chromium_headless_shell-*/chrome-linux/headless_shell')[0]
names = open('/out/list.txt', encoding='utf-8').read().splitlines()
ok = 0
for line in open('/out/rust.tsv', encoding='utf-8'):
    f = line.rstrip('\n').split('\t')
    if len(f) < 4 or f[1] == 'ERR':
        continue
    i, w, h = int(f[0]), int(f[2]), int(f[3])
    url = 'file://' + urllib.parse.quote(names[i])
    html = f'/out/chrome/{i}.html'
    open(html, 'w', encoding='utf-8').write(
        '<!doctype html><html><head><meta charset="utf-8"><style>html,body{margin:0;background:#000}'
        f'img{{display:block;width:{w}px;height:{h}px}}</style></head><body><img src="{url}"></body></html>')
    try:
        subprocess.run([shell, '--no-sandbox', '--disable-gpu', '--hide-scrollbars', '--force-device-scale-factor=1',
                        '--allow-file-access-from-files', f'--window-size={max(w, 64)},{max(h, 64)}',
                        f'--screenshot=/out/chrome/{i}.png', 'file://' + html],
                       stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL, timeout=20)
        ok += os.path.exists(f'/out/chrome/{i}.png')
    except subprocess.TimeoutExpired:
        pass
print(f'Chromium 截圖 {ok} 張', file=sys.stderr)
PY

# 4. 比較。
docker run "${common[@]}" -v "$REPO":/src -v "$REPO/.cache/cargo-target":/cargo-target \
    wincv-rust:1 /cargo-target/release/svgcmp /src/.cache/parity/svg > "$OUT/cmp.tsv"
