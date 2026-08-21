# Android 建置工具鏈。只給這個專案用,tag 一律 wincv-android:*。
#
# [HARD] 這台機器有其他客戶專案的 image,任何 prune / rmi 一律禁止。
#
# 為什麼要自己組:ebitenmobile 需要 NDK,產 APK 還要 SDK platform、
# build-tools 與 gradle。官方沒有把這幾樣兜好的現成 image。
FROM golang:1.22-bookworm

ARG NDK=r26d
ARG CMDLINE=11076708
ARG API=34
ARG BUILDTOOLS=34.0.0

ENV ANDROID_HOME=/opt/android-sdk \
    ANDROID_SDK_ROOT=/opt/android-sdk \
    ANDROID_NDK_HOME=/opt/android-ndk \
    GRADLE_USER_HOME=/tmp/gradle

RUN apt-get update && apt-get install -y --no-install-recommends \
        unzip openjdk-17-jdk-headless ca-certificates curl \
    && rm -rf /var/lib/apt/lists/*

# NDK:ebitenmobile 需要它來交叉編 arm64/arm/x86_64
RUN curl -fsSL -o /tmp/ndk.zip \
        "https://dl.google.com/android/repository/android-ndk-${NDK}-linux.zip" \
    && unzip -q /tmp/ndk.zip -d /opt \
    && mv "/opt/android-ndk-${NDK}" "$ANDROID_NDK_HOME" \
    && rm /tmp/ndk.zip

# SDK command-line tools → 再用 sdkmanager 裝 platform 與 build-tools
RUN mkdir -p "$ANDROID_HOME/cmdline-tools" \
    && curl -fsSL -o /tmp/cmd.zip \
        "https://dl.google.com/android/repository/commandlinetools-linux-${CMDLINE}_latest.zip" \
    && unzip -q /tmp/cmd.zip -d "$ANDROID_HOME/cmdline-tools" \
    && mv "$ANDROID_HOME/cmdline-tools/cmdline-tools" "$ANDROID_HOME/cmdline-tools/latest" \
    && rm /tmp/cmd.zip \
    && yes | "$ANDROID_HOME/cmdline-tools/latest/bin/sdkmanager" --licenses >/dev/null \
    && "$ANDROID_HOME/cmdline-tools/latest/bin/sdkmanager" \
        "platforms;android-${API}" "build-tools;${BUILDTOOLS}" "platform-tools" >/dev/null

ENV PATH=$PATH:/opt/android-sdk/cmdline-tools/latest/bin:/opt/android-sdk/platform-tools:/go/bin

# ebitenmobile。版本要跟 go.mod 裡的 ebiten 對齊 ——
# 不對齊的話 bind 產出的 AAR 與程式碼用的是兩套 runtime。
#
# [雷] **不要用 golang.org/x/mobile**。ebiten v2.6 走 x/mobile,而那條路
# 在今天是死路:x/mobile@latest 要 Go >= 1.25,釘回舊版又擋不住
# `ebitenmobile bind` 內部寫死的 `go install gobind@latest`;裝了新 Go
# 之後,舊的 x/tools@v0.13.0 又在 Go 1.25 下編不過(tokeninternal 的
# 負數陣列長度)。兩頭都堵。
#
# ebiten v2.8 起改用自家的 github.com/ebitengine/gomobile,整個問題消失。
# 選 v2.8.8 而不是 v2.9:v2.9 的 go.mod 要求 Go >= 1.25,會連帶把桌面版的
# 建置 image 也拖著升級。v2.8.8 只要 Go 1.22,兩邊都不用動。
ARG EBITEN=v2.8.8
RUN go install github.com/hajimehoshi/ebiten/v2/cmd/ebitenmobile@${EBITEN}

# Gradle。Debian 套件庫的版本太舊(4.x),吃不動 AGP 8,所以抓官方 binary。
ARG GRADLE=8.5
RUN curl -fsSL -o /tmp/gradle.zip \
        "https://services.gradle.org/distributions/gradle-${GRADLE}-bin.zip" \
    && unzip -q /tmp/gradle.zip -d /opt \
    && ln -s "/opt/gradle-${GRADLE}/bin/gradle" /usr/local/bin/gradle \
    && rm /tmp/gradle.zip

ENV PATH=/tmp/go/bin:$PATH

# 以非 root 身分跑時要寫得進這幾個地方。
RUN mkdir -p /tmp/gradle /tmp/gocache /tmp/gomod && chmod -R 777 /tmp/gradle /tmp/gocache /tmp/gomod /go

WORKDIR /src
