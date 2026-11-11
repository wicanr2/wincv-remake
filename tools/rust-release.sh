#!/usr/bin/env bash
# 建置 Rust 三平台公開封包，不上傳。Rust 是否為預覽版由 GitHub Release 的
# prerelease 屬性表示；tag 一律使用 v.主版.次版.修訂版-YYYYMMDD。
# 主機只處理 git 與 Docker；建置、授權核對、複製及打包全部在容器內。
set -euo pipefail
REPO=$(cd "$(dirname "$0")/.." && pwd)
VERSION=${1:?用法: tools/rust-release.sh v.主版.次版.修訂版-YYYYMMDD}
[[ "$VERSION" =~ ^v\.[0-9]+\.[0-9]+\.[0-9]+-[0-9]{8}$ ]] || {
    echo "不合規的版本號:$VERSION" >&2; exit 2;
}
cd "$REPO"
[ -z "$(git status --porcelain --untracked-files=no)" ] || {
    echo "追蹤中的檔案尚未提交，停止發行。" >&2; exit 1;
}
COMMIT=$(git rev-parse HEAD)
for d in "$REPO" "$REPO/.cache/cargo-home" "$REPO/.cache/cargo-target"; do
    [ -d "$d" ] || { echo "必要目錄不存在:$d" >&2; exit 1; }
done
[ ! -e "$REPO/dist-all/$VERSION" ] || {
    echo "輸出已存在，不覆寫:$REPO/dist-all/$VERSION" >&2; exit 1;
}
run() {
    local img=$1; shift
    timeout 3600 docker run --rm --init --network none \
        --log-opt max-size=10m --log-opt max-file=3 \
        --cpus "${WINCV_CPUS:-4}" --memory "${WINCV_MEM:-6g}" --pids-limit 1024 \
        --user "$(id -u):$(id -g)" -e HOME=/tmp -e WINCV_VERSION="$VERSION" \
        -e WINCV_RELEASE_COMMIT="$COMMIT" -e CARGO_HOME=/cargo-home -e CARGO_TARGET_DIR=/cargo-target \
        -v "$REPO":/src -v "$REPO/.cache/cargo-home":/cargo-home \
        -v "$REPO/.cache/cargo-target":/cargo-target -w /src "$img" "$@"
}
run wincv-build:1 bash -c '
    set -eu
    out=dist-all/$WINCV_VERSION
    test "$(stat -c %u:%g dist-all)" = "$(id -u):$(id -g)"
    mkdir -p "$out/patch" "$out/smoke"
'
run wincv-rust:1 bash -c '
    set -eu
    cd rust
    out=/src/dist-all/$WINCV_VERSION
    cargo tree --offline --locked -p wincv -e normal,build --prefix none --format "{p}" --target all > "$out/smoke/cargo-tree.txt"
    cargo metadata --offline --locked --format-version 1 > "$out/smoke/cargo-metadata.json"
    cargo build --offline --locked --release -p wincv
    cp /cargo-target/release/wincv "$out/patch/wincv-linux-amd64"
    cargo build --offline --locked --release -p wincv --target x86_64-pc-windows-gnu
    cp /cargo-target/x86_64-pc-windows-gnu/release/wincv.exe "$out/patch/wincv-windows-amd64.exe"
    rustc --version > "$out/smoke/rustc.txt"
'
run wincv-osxcross-rust:1 bash -c '
    set -eu
    export PATH=/osxcross/bin:/usr/local/cargo/bin:$PATH
    cd rust
    out=/src/dist-all/$WINCV_VERSION
    cargo build --offline --locked --release -p wincv --target aarch64-apple-darwin
    cargo build --offline --locked --release -p wincv --target x86_64-apple-darwin
    x86_64-apple-darwin24.5-lipo -create \
        /cargo-target/aarch64-apple-darwin/release/wincv \
        /cargo-target/x86_64-apple-darwin/release/wincv -output "$out/patch/wincv-darwin-universal"
    x86_64-apple-darwin24.5-lipo -info "$out/patch/wincv-darwin-universal" > "$out/smoke/macos-architectures.txt"
    for arch in arm64 x86_64; do
        x86_64-apple-darwin24.5-lipo -thin "$arch" "$out/patch/wincv-darwin-universal" -output "/tmp/wincv-$arch"
        x86_64-apple-darwin24.5-otool -l "/tmp/wincv-$arch" > "$out/smoke/macos-$arch-load-commands.txt"
        x86_64-apple-darwin24.5-otool -L "/tmp/wincv-$arch" > "$out/smoke/macos-$arch-libraries.txt"
    done
'
run wincv-build:1 bash -c '
    set -eu
    out=dist-all/$WINCV_VERSION
    python3 tools/rust-licenses.py "$out/smoke/cargo-tree.txt" "$out/smoke/cargo-metadata.json" /tmp/licenses.txt
    cmp rust/THIRD-PARTY-LICENSES.txt /tmp/licenses.txt
    python3 tools/package-rust-release.py "$WINCV_VERSION" "$WINCV_RELEASE_COMMIT"
    python3 tools/verify-rust-release.py "$WINCV_VERSION"
'
echo "發布樹:$REPO/dist-all/$VERSION/patch/"
