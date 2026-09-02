# macOS 交叉編譯工具鏈。只給這個專案用,tag 一律 wincv-osxcross-go:*。
#
# [HARD] 這台機器有其他客戶專案的 image,任何 prune / rmi 一律禁止。
# [HARD] **這個 image 不要 push、不要轉給別人。** 裡面含 Apple 的 macOS SDK,
#        它的授權只允許在 Apple 硬體上使用 —— 自用交叉編譯是一回事,
#        散布含 SDK 的 image 是另一回事。
#
# 為什麼要自己組:原本借用別的專案的 osxcross image,那個 tag 被重新
# 建過就整條 macOS 建置線斷掉(而且斷的時候只說 pull access denied,
# 看起來像網路問題)。共用別人的 tag 等於把自己的可重現性交給別人。
#
# 缺的到底是什麼:clang 本來就是交叉編譯器,少的是 macOS SDK(標頭與
# framework 符號)、Mach-O 的連結器與工具(cctools-port 的 ld64/lipo/otool),
# 以及把 -target / -isysroot 包好的 wrapper。osxcross 就是這三樣的組合包。
# 所以編譯階段幾乎不會出事,出事的全在連結與工具鏈那一層。
#
# 建置:tools/build-osxcross.sh

FROM crazymax/osxcross:15.5-debian AS osxcross

# [雷] 底座不能用 golang:1.22-bookworm。osxcross 的工具是在 glibc 2.38 以上
# 建的,bookworm 只有 2.36,結果是 osxcross-conf / clang 一跑就說
# `version GLIBC_2.38 not found` —— image 建得成功,直到真的要編才爆。
# 所以底座用 trixie,Go 自己裝,版本釘成與 Dockerfile.build 相同的 1.22.12。
FROM debian:trixie-slim

ARG GO_VERSION=1.22.12

RUN apt-get update -qq && apt-get install -y -qq --no-install-recommends \
        clang lld llvm \
        libssl-dev liblzma-dev libxml2-dev zlib1g-dev \
        python3 pkg-config file xz-utils curl ca-certificates git \
    && rm -rf /var/lib/apt/lists/*

# Go 的版本跟 Dockerfile.build 一致 —— 三個桌面平台由同一版編譯器產出,
# 才不會有「只有 macOS 那份行為不一樣」這種查不出來的差異。
RUN curl -fsSL "https://go.dev/dl/go${GO_VERSION}.linux-amd64.tar.gz" \
        | tar -C /usr/local -xz \
    && /usr/local/go/bin/go version

# crazymax/osxcross 是 scratch image,沒有 shell,設計就是給 COPY --from 用。
COPY --from=osxcross /osxcross /osxcross

# [雷] ld64 連結時要載 /osxcross/lib 底下的 libxar 與 libtapi,那不在動態
# 載入器的搜尋路徑裡。缺了的話 clang 只會轉述成 "unable to execute command",
# 看起來像編譯器壞掉。用 ldconfig 而不是 LD_LIBRARY_PATH —— 後者會在
# 建置系統生出來的子 shell 裡掉。
RUN echo /osxcross/lib > /etc/ld.so.conf.d/osxcross.conf && ldconfig

ENV PATH="/osxcross/bin:/usr/local/go/bin:${PATH}" \
    GOFLAGS=-buildvcs=false
WORKDIR /src
