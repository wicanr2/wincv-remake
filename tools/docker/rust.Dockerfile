# WinCV Remake 的 Rust 建置環境。
#
# 基底鎖在 rust:1.99.1-slim-bookworm。交叉編譯的目標逐步加:
# 試作階段先要 Linux 本機與 wasm,Windows 用 mingw 的 gnu 目標。
# macOS 沿用 tools/docker/osxcross.Dockerfile 的 SDK,Android 之後再加。
FROM rust:1.99.1-slim-bookworm

# 這台機器連不上 deb.debian.org(Fastly 的位址一律 Network is unreachable),
# 套件來源改用 mirrors.kernel.org 的 HTTPS。它沒有鏡像 debian-security,
# 所以只留 bookworm 與 bookworm-updates:這個映像只拿來建置,不隨產物散布。
RUN printf 'Types: deb\nURIs: https://mirrors.kernel.org/debian\nSuites: bookworm bookworm-updates\nComponents: main\nSigned-By: /usr/share/keyrings/debian-archive-keyring.gpg\n' \
        > /etc/apt/sources.list.d/debian.sources \
    && apt-get update -qq && apt-get install -y -qq --no-install-recommends \
        gcc-mingw-w64-x86-64 pkg-config \
    && rm -rf /var/lib/apt/lists/* \
    && rustup target add x86_64-pc-windows-gnu wasm32-unknown-unknown \
    && rustup component add clippy rustfmt

ENV CARGO_TERM_COLOR=never
WORKDIR /src/rust
