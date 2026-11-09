# Rust 版的 macOS 交叉編譯工具鏈:在 wincv-osxcross-go:1 上加 Rust。
#
# [HARD] 與 osxcross.Dockerfile 相同:這個 image 含 Apple 的 macOS SDK,
#        不要 push、不要轉給別人。
#
# Rust 版本與 rust.Dockerfile 相同(1.99.1)。連結器指向 osxcross 的
# clang wrapper,前綴的 darwin 版本跟著 SDK 走(15.5 → darwin24.5)。
#
# 建置:docker build -t wincv-osxcross-rust:1 -f tools/docker/osxcross-rust.Dockerfile tools/docker
FROM wincv-osxcross-go:1
ENV RUSTUP_HOME=/usr/local/rustup CARGO_HOME=/usr/local/cargo \
    PATH=/usr/local/cargo/bin:$PATH
ADD https://static.rust-lang.org/rustup/dist/x86_64-unknown-linux-gnu/rustup-init /tmp/rustup-init
RUN chmod +x /tmp/rustup-init \
    && /tmp/rustup-init -y --no-modify-path --profile minimal --default-toolchain 1.99.1 \
        --target x86_64-apple-darwin --target aarch64-apple-darwin \
    && rm /tmp/rustup-init \
    && chmod -R a+rX /usr/local/rustup /usr/local/cargo
ENV CARGO_TARGET_X86_64_APPLE_DARWIN_LINKER=x86_64-apple-darwin24.5-clang \
    CARGO_TARGET_AARCH64_APPLE_DARWIN_LINKER=aarch64-apple-darwin24.5-clang \
    CC_x86_64_apple_darwin=x86_64-apple-darwin24.5-clang \
    CC_aarch64_apple_darwin=aarch64-apple-darwin24.5-clang \
    CARGO_TERM_COLOR=never
WORKDIR /src/rust
