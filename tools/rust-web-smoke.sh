#!/usr/bin/env bash
# Rust 版的 wasm 在瀏覽器裡實際跑一次並截圖。
#
#   tools/rust-web-smoke.sh            # 輸出 .cache/web/shot.png
#
# 用 fonts 功能內嵌 original/ 的原版字型(網頁上沒有檔案系統),所以產物
# 只能留在本機,不能散布。wasm-bindgen-cli 的版本必須與 Cargo.lock 裡的
# wasm-bindgen 相同,第一次會裝進 .cache/cargo-home/bin/(要開網路)。
# 瀏覽器用 Playwright 映像裡的 headless Chromium,不需要主機的 X。
set -euo pipefail
REPO=$(cd "$(dirname "$0")/.." && pwd)
WEB=$REPO/.cache/web
PW_IMG=${PW_IMG:-mcr.microsoft.com/playwright:v1.56.1-noble}
ver=$(awk '/^name = "wasm-bindgen"$/{getline; gsub(/[^0-9.]/,""); print}' "$REPO/rust/Cargo.lock")
mkdir -p "$WEB"

if ! "$REPO/.cache/cargo-home/bin/wasm-bindgen" --version 2>/dev/null | grep -q " $ver\$"; then
    WINCV_NET=1 "$REPO/tools/cargo.sh" install --locked --root /cargo-home wasm-bindgen-cli --version "$ver"
fi
WINCV_FONT_DIR=/src/original "$REPO/tools/cargo.sh" build --release -p wincv --features fonts --target wasm32-unknown-unknown
docker run --rm --log-opt max-size=10m --log-opt max-file=3 --network none -u "$(id -u):$(id -g)" \
    -v "$REPO/.cache":/c wincv-rust:1 /c/cargo-home/bin/wasm-bindgen --target web --no-typescript \
    --out-dir /c/web /c/cargo-target/wasm32-unknown-unknown/release/wincv.wasm
cat > "$WEB/index.html" <<'HTML'
<!doctype html>
<html lang="zh-Hant"><head><meta charset="utf-8"><title>WinCV Remake</title>
<style>html,body{margin:0;background:#000}canvas{display:block;image-rendering:pixelated}</style></head>
<body><script type="module">
import init from "./wincv.js";
// winit 在網頁上用丟出例外的方式離開 run_app,那不是錯誤。
init().catch(() => {});
</script></body></html>
HTML
docker run --rm --log-opt max-size=10m --log-opt max-file=3 --network none --cpus 2 --memory 2g \
    --pids-limit 512 -u "$(id -u):$(id -g)" -e HOME=/tmp -v "$WEB":/web "$PW_IMG" sh -c '
cd /web && (python3 -m http.server 8765 --bind 127.0.0.1 >/tmp/http.log 2>&1 &) && sleep 1
/ms-playwright/chromium_headless_shell-*/chrome-linux/headless_shell --no-sandbox --disable-gpu \
    --window-size=700,450 --virtual-time-budget=8000 --screenshot=/web/shot.png \
    http://127.0.0.1:8765/index.html >/dev/null 2>&1'
printf 'wasm %d bytes(gzip %d)\n' "$(stat -c%s "$WEB/wincv_bg.wasm")" "$(gzip -9 -c "$WEB/wincv_bg.wasm" | wc -c)"
echo "$WEB/shot.png"
