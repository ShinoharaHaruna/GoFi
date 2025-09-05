#!/bin/bash
# 如果任何命令失败，脚本将立即退出
# Exit immediately if a command exits with a non-zero status.
set -e

# 获取基础版本号和 Git commit hash
# Get the base version and Git commit hash
BASE_VERSION=$(cat VERSION)
GIT_COMMIT=$(git rev-parse --short HEAD)
FULL_VERSION="${BASE_VERSION}-${GIT_COMMIT}"

# 设置 Go 编译环境为 Linux amd64
# Set up the Go build environment for Linux amd64
export CGO_ENABLED=0
export GOOS=linux
export GOARCH=amd64

# 创建 build 目录（如果不存在）
# Create build directory if it doesn't exist
mkdir -p build

echo "正在编译 GoFi (版本: ${FULL_VERSION})..."
# echo "Building GoFi (version: ${FULL_VERSION})..."

# 编译 Go 应用程序，并将版本信息注入
# Build the Go application and inject version info
go build -ldflags "-X main.version=${FULL_VERSION}" -o build/GoFi ./cmd/gofi

echo "编译完成. 可执行文件在 build/GoFi"
# echo "Build complete. The executable is in build/GoFi"
