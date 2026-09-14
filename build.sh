#!/bin/bash
set -e

PROJECT_NAME="fuzhan"
PLATFORMS=(
    "windows/amd64"
    "linux/amd64"
)

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

# 默认值
VERSION=$(date +%Y.%m.%d)
TARGET=""
PLATFORM_FILTER=""
PACK=false

# 平台别名映射
# w: Windows, l: Linux, m: macOS (darwin)
get_platforms_by_alias() {
    case "$1" in
        w) echo "windows/amd64";;
        l) echo "linux/amd64";;
    esac
}

# 解析参数
while [[ $# -gt 0 ]]; do
    case "$1" in
        -v|--version)
            if [[ -z "$2" || "$2" == -* ]]; then
                echo "Error: -v requires a version argument"
                exit 1
            fi
            VERSION="$2"
            shift 2
            ;;
        -v*)
            VERSION="${1:2}"
            shift
            ;;
        -p|--pack)
            PACK=true
            shift
            ;;
        ui)
            TARGET="$1"
            shift
            ;;
        server)
            TARGET="$1"
            shift
            # 检查下一个参数是否为平台别名
            if [[ -n "$1" && "$1" =~ ^[wlm]$ ]]; then
                PLATFORM_FILTER=$(get_platforms_by_alias "$1")
                shift
            fi
            ;;
        *)
            echo "Usage: $0 [-v version] [-p] [ui|server [w|l]]"
            echo "  -v, --version <version>: Specify version (default: current date)"
            echo "  -p, --pack: Create zip packages (default: only build binary)"
            echo "  ui: build frontend only"
            echo "  server [w|l|m]: build backend (default: all platforms)"
            echo "    w: Windows x64"
            echo "    l: Linux x64"
            echo "    m: macOS (已移除)"
            exit 1
            ;;
    esac
done

# 确定要构建的平台列表
if [[ -n "$PLATFORM_FILTER" ]]; then
    PLATFORMS=($PLATFORM_FILTER)
fi

build_ui() {
    echo "=== Build Frontend ==="
    cd "$SCRIPT_DIR/ui"
    # build:frontend = 主应用 + scalar（已存在则自动跳过，避免每次全量重建）
    yarn build:frontend
    cd "$SCRIPT_DIR"
    echo "Frontend built to: $SCRIPT_DIR/server/web (scalar: $SCRIPT_DIR/server/scalar)"
}

build_server() {
    echo "=== Build Backend ($VERSION) ==="
    rm -rf "$SCRIPT_DIR/bin"
    mkdir -p bin
    cd "$SCRIPT_DIR/server"
    for PLATFORM in "${PLATFORMS[@]}"; do
        OS="${PLATFORM%/*}"
        ARCH="${PLATFORM#*/}"
        echo -n "Building $OS $ARCH... "

        # 统一的可执行文件名
        EXE_NAME="fuzhan"
        if [ "$OS" = "windows" ]; then
            EXE_NAME="$EXE_NAME.exe"
        fi

        # 临时构建到统一名称
        TEMP_OUTPUT="$SCRIPT_DIR/bin/$EXE_NAME"

        if GOOS="$OS" GOARCH="$ARCH" go build -ldflags "-s -w" -o "$TEMP_OUTPUT" .; then
            if [ "$PACK" = true ]; then
                # 创建 zip 包
                ZIP_NAME="$PROJECT_NAME-$VERSION-$OS-$ARCH.zip"
                cd "$SCRIPT_DIR/bin"
                zip "$ZIP_NAME" "$EXE_NAME"
                rm "$EXE_NAME"
                echo "OK ($ZIP_NAME)"
                cd "$SCRIPT_DIR/server"
            else
                echo "OK ($EXE_NAME)"
            fi
        else
            echo "FAIL"
            cd "$SCRIPT_DIR"
            exit 1
        fi
    done
    cd "$SCRIPT_DIR"
    echo ""
    echo "=== Done ==="
    if [ "$PACK" = true ]; then
        ls -lh bin/*.zip
    else
        ls -lh bin/
    fi
}

case "$TARGET" in
    ui)
        build_ui
        ;;
    server)
        build_server
        ;;
    "")
        build_ui
        echo ""
        build_server
        ;;
esac