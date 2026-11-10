#!/usr/bin/env bash
# 建立 Rust 版 preview 的公開發布樹:Linux x86-64、Windows x86-64、macOS universal。
# 只建、只打包,不上傳。上傳是另一個動作(gh release create --prerelease),要人確認過再做。
#
#   tools/rust-release.sh v.0.54.0-preview-20261110
#
# 版本號一定帶 -preview:Go 版仍是正式版,Rust 版的發布一律標成 prerelease。
# 公開產物不內嵌原版、倚天或國喬字型(與 Go 版的 release.sh 相同)。
set -euo pipefail

REPO=$(cd "$(dirname "$0")/.." && pwd)
VERSION=${1:?用法: tools/rust-release.sh v.主版.次版.修訂版-preview-YYYYMMDD}
case "$VERSION" in
    v.[0-9]*.[0-9]*.[0-9]*-preview-[0-9][0-9][0-9][0-9][0-9][0-9][0-9][0-9]) ;;
    *) echo "不合規的版本號:$VERSION(要像 v.0.54.0-preview-20261110)" >&2; exit 2 ;;
esac

cd "$REPO"
[ -z "$(git status --porcelain --untracked-files=no)" ] || {
    echo "工作區有未提交變更;發布物必須對應一個 commit。" >&2
    exit 1
}
COMMIT=$(git rev-parse HEAD)
OUT=$REPO/dist-all/$VERSION/rust
mkdir -p "$OUT"

# 第三方授權清單要對得上這次的 Cargo.lock。
tools/rust-licenses.sh
git diff --quiet rust/THIRD-PARTY-LICENSES.txt || {
    echo "rust/THIRD-PARTY-LICENSES.txt 與 Cargo.lock 不一致,重新產生後先 commit。" >&2
    exit 1
}

T=$REPO/.cache/cargo-target
export WINCV_VERSION=$VERSION

echo "=== linux/amd64 ==="
tools/cargo.sh build -q --release -p wincv
cp "$T/release/wincv" "$OUT/wincv-linux-amd64"

echo "=== windows/amd64 ==="
tools/cargo.sh build -q --release -p wincv --target x86_64-pc-windows-gnu
cp "$T/x86_64-pc-windows-gnu/release/wincv.exe" "$OUT/wincv-windows-amd64.exe"

echo "=== darwin universal ==="
# [HARD] wincv-osxcross-rust:1 含 Apple 的 macOS SDK,不 push、不轉給別人。
timeout 3600 docker run --rm --log-opt max-size=10m --log-opt max-file=3 --network none \
    --cpus "${WINCV_CPUS:-4}" --memory "${WINCV_MEM:-6g}" --pids-limit 1024 \
    -u "$(id -u):$(id -g)" -e HOME=/tmp -e WINCV_VERSION \
    -v "$REPO":/src -w /src/rust \
    -v "$REPO/.cache/cargo-home":/cargo-home -e CARGO_HOME=/cargo-home \
    -v "$T":/cargo-target -e CARGO_TARGET_DIR=/cargo-target \
    wincv-osxcross-rust:1 bash -c '
        set -e
        export PATH=/osxcross/bin:/usr/local/cargo/bin:$PATH
        cargo build -q --offline --release -p wincv --target aarch64-apple-darwin
        cargo build -q --offline --release -p wincv --target x86_64-apple-darwin
        x86_64-apple-darwin24.5-lipo -create \
            /cargo-target/aarch64-apple-darwin/release/wincv \
            /cargo-target/x86_64-apple-darwin/release/wincv \
            -output /src/dist-all/'"$VERSION"'/rust/wincv-darwin-universal'

for f in wincv-linux-amd64 wincv-windows-amd64.exe wincv-darwin-universal; do
    [ -s "$OUT/$f" ] || { echo "!! $f 沒有產生出來" >&2; exit 1; }
done
(cd "$OUT" && sha256sum wincv-linux-amd64 wincv-windows-amd64.exe wincv-darwin-universal > SHA256SUMS)

# --- 打包:每個平台一個 zip,附 LICENSE、NOTICE、第三方授權與讀我 ---
cd "$OUT"
zips=()
for entry in "wincv-linux-amd64:wincv:linux-amd64" "wincv-windows-amd64.exe:wincv.exe:windows-amd64" \
             "wincv-darwin-universal:wincv:macos-universal"; do
    src=${entry%%:*}; rest=${entry#*:}
    exe=${rest%%:*}; plat=${rest#*:}
    dir="wincv-remake-rust-$plat"
    zip="wincv-remake-rust-$VERSION-$plat.zip"
    rm -rf "$dir" "$zip"
    mkdir -p "$dir"
    cp "$src" "$dir/$exe"
    chmod 755 "$dir/$exe"
    cp "$REPO/LICENSE" "$REPO/NOTICE" "$REPO/rust/THIRD-PARTY-LICENSES.txt" "$dir/"
    "$REPO/tools/readme-for.sh" "$plat" "$exe" rust > "$dir/README.txt"
    if [ "$plat" = windows-amd64 ]; then
        sed -i 's/$/\r/' "$dir/README.txt"
    fi
    "$REPO/tools/go.sh" run ./tools/zipdir "/src/dist-all/$VERSION/rust/$zip" "/src/dist-all/$VERSION/rust/$dir"
    rm -rf "$dir"
    zips+=("$zip")
    printf '  %-62s %10s\n' "$zip" "$(stat -c%s "$zip")"
done
sha256sum "${zips[@]}" > SHA256SUMS-zip

cat > MANIFEST.txt <<EOF
WinCV Remake(Rust 版 preview)$VERSION

commit   $COMMIT
建置日期 $(date -u +%Y-%m-%d)(UTC)

公開產物:Linux x86-64、Windows x86-64、macOS universal(arm64 + x86_64)。
未嵌入原版、倚天或國喬字型。每個 zip 附 LICENSE、NOTICE、THIRD-PARTY-LICENSES.txt
與 README.txt。SHA256SUMS-zip 是上傳檔的校驗表。
EOF
echo "發布樹: $OUT"
