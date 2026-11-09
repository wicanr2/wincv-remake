# 在容器裡跑 Rust 版視窗(配合主機的 Xvfb)用的執行環境。
#
# winit 的 X11 後端執行時以 dlopen 載入 libxkbcommon-x11,
# wincv-build:1 沒有它。套件來源理由同 rust.Dockerfile。
FROM wincv-build:1
RUN printf 'Types: deb\nURIs: https://mirrors.kernel.org/debian\nSuites: bookworm bookworm-updates\nComponents: main\nSigned-By: /usr/share/keyrings/debian-archive-keyring.gpg\n' \
        > /etc/apt/sources.list.d/debian.sources \
    && apt-get update -qq && apt-get install -y -qq --no-install-recommends libxkbcommon-x11-0 \
    && rm -rf /var/lib/apt/lists/*
