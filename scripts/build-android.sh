#!/usr/bin/env bash
# 在本机编译安卓 arm64（Termux）服务端并打包到 build/android/omniHub/，网页端用 web/dist 里已构建好的。
# 用法：scripts/build-android.sh；部署到手机见 packaging/android/README.txt。
set -euo pipefail
cd "$(dirname "$0")/.."
out=build/android/omniHub
mkdir -p "$out"
ldflags="-s -w -X omnihub/internal/buildinfo.Revision=$(git describe --tags --always --dirty) -X omnihub/internal/buildinfo.BuildTime=$(date -u +%Y-%m-%dT%H:%M:%SZ)"
CGO_ENABLED=0 GOOS=android GOARCH=arm64 nice -n 19 go build -p 2 -trimpath -ldflags "$ldflags" -o "$out/omnihub-server" ./cmd/server
cp packaging/android/omnihub.sh packaging/android/README.txt "$out/"
echo "已生成 $out"
