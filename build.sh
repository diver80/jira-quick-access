#!/usr/bin/env bash
# ==============================================================================
# Jira Quick Access - Multi-Platform Build Script (OSX / Windows / Linux)
# ==============================================================================

set -e

APP_NAME="jira-quick-access"
APP_DISPLAY_NAME="Jira Quick Access"
BUNDLE_ID="com.avono.jira-quick-access"
VERSION="${VERSION:-1.0.0}"
BUILD_TIME=$(date -u +"%Y-%m-%dT%H:%M:%SZ")
LDFLAGS="-s -w -X main.version=${VERSION} -X main.buildTime=${BUILD_TIME}"
DIST_DIR="$(pwd)/dist"

# Terminal Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
BLUE='\033[0;34m'
CYAN='\033[0;36m'
YELLOW='\033[1;33m'
BOLD='\033[1m'
NC='\033[0m' # No Color

print_banner() {
    echo -e "${CYAN}${BOLD}"
    echo "================================================================"
    echo "   🚀 Jira Quick Access - Multi-Platform Build Script"
    echo "   Version: ${VERSION}  |  Engine: gogpu/ui"
    echo "================================================================"
    echo -e "${NC}"
}

clean() {
    echo -e "${YELLOW}🧹 Cleaning dist directory...${NC}"
    rm -rf "${DIST_DIR}"
    mkdir -p "${DIST_DIR}"
}

build_osx() {
    echo -e "${BLUE}🍎 Building for macOS (Darwin)...${NC}"
    
    # 1. ARM64 (Apple Silicon: M1/M2/M3/M4)
    echo -e "   -> Compiling darwin/arm64..."
    mkdir -p "${DIST_DIR}/osx/arm64"
    GOOS=darwin GOARCH=arm64 go build -ldflags="${LDFLAGS}" -o "${DIST_DIR}/osx/arm64/${APP_NAME}" .
    
    # 2. AMD64 (Intel Mac)
    echo -e "   -> Compiling darwin/amd64..."
    mkdir -p "${DIST_DIR}/osx/amd64"
    GOOS=darwin GOARCH=amd64 go build -ldflags="${LDFLAGS}" -o "${DIST_DIR}/osx/amd64/${APP_NAME}" .
    
    # 3. Create macOS .app Bundle
    APP_BUNDLE="${DIST_DIR}/osx/${APP_DISPLAY_NAME}.app"
    echo -e "   -> Creating macOS App Bundle at ${APP_BUNDLE}..."
    rm -rf "${APP_BUNDLE}"
    mkdir -p "${APP_BUNDLE}/Contents/MacOS"
    mkdir -p "${APP_BUNDLE}/Contents/Resources"
    
    HOST_ARCH=$(uname -m)
    if [ "${HOST_ARCH}" = "arm64" ]; then
        cp "${DIST_DIR}/osx/arm64/${APP_NAME}" "${APP_BUNDLE}/Contents/MacOS/${APP_NAME}"
    else
        cp "${DIST_DIR}/osx/amd64/${APP_NAME}" "${APP_BUNDLE}/Contents/MacOS/${APP_NAME}"
    fi
    chmod +x "${APP_BUNDLE}/Contents/MacOS/${APP_NAME}"
    
    cat <<EOF > "${APP_BUNDLE}/Contents/Info.plist"
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
    <key>CFBundleDevelopmentRegion</key>
    <string>en</string>
    <key>CFBundleExecutable</key>
    <string>${APP_NAME}</string>
    <key>CFBundleIdentifier</key>
    <string>${BUNDLE_ID}</string>
    <key>CFBundleInfoDictionaryVersion</key>
    <string>6.0</string>
    <key>CFBundleName</key>
    <string>${APP_DISPLAY_NAME}</string>
    <key>CFBundlePackageType</key>
    <string>APPL</string>
    <key>CFBundleShortVersionString</key>
    <string>${VERSION}</string>
    <key>CFBundleVersion</key>
    <string>${VERSION}</string>
    <key>NSHighResolutionCapable</key>
    <true/>
    <key>LSUIElement</key>
    <true/>
</dict>
</plist>
EOF

    cd "${DIST_DIR}/osx"
    tar -czf "JiraQuickAccess-macOS-arm64.tar.gz" -C "${DIST_DIR}/osx" "${APP_DISPLAY_NAME}.app"
    cd - > /dev/null

    echo -e "${GREEN}✓ macOS build completed!${NC}"
}

build_win() {
    echo -e "${BLUE}🪟 Building for Windows...${NC}"
    
    mkdir -p "${DIST_DIR}/win"
    CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -ldflags="${LDFLAGS}" -o "${DIST_DIR}/win/${APP_NAME}-windows-amd64.exe" .
    CGO_ENABLED=0 GOOS=windows GOARCH=arm64 go build -ldflags="${LDFLAGS}" -o "${DIST_DIR}/win/${APP_NAME}-windows-arm64.exe" .
    
    if command -v zip >/dev/null 2>&1; then
        cd "${DIST_DIR}/win"
        zip -q "${APP_NAME}-windows-amd64.zip" "${APP_NAME}-windows-amd64.exe"
        cd - > /dev/null
    fi

    echo -e "${GREEN}✓ Windows build completed!${NC}"
}

build_lin() {
    echo -e "${BLUE}🐧 Building for Linux...${NC}"
    
    mkdir -p "${DIST_DIR}/lin"
    CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags="${LDFLAGS}" -o "${DIST_DIR}/lin/${APP_NAME}-linux-amd64" .
    chmod +x "${DIST_DIR}/lin/${APP_NAME}-linux-amd64"
    
    CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -ldflags="${LDFLAGS}" -o "${DIST_DIR}/lin/${APP_NAME}-linux-arm64" .
    chmod +x "${DIST_DIR}/lin/${APP_NAME}-linux-arm64"

    cd "${DIST_DIR}/lin"
    tar -czf "${APP_NAME}-linux-amd64.tar.gz" "${APP_NAME}-linux-amd64"
    tar -czf "${APP_NAME}-linux-arm64.tar.gz" "${APP_NAME}-linux-arm64"
    cd - > /dev/null

    echo -e "${GREEN}✓ Linux build completed!${NC}"
}

print_summary() {
    echo ""
    echo -e "${GREEN}${BOLD}🎉 Build Successful! Release artifacts generated in ${DIST_DIR}:${NC}"
    find "${DIST_DIR}" -type f -maxdepth 3 | sort | while read -r file; do
        size=$(ls -lh "$file" | awk '{print $5}')
        rel_path="${file#$DIST_DIR/}"
        echo -e "   📦 ${CYAN}${rel_path}${NC} (${size})"
    done
    echo ""
}

print_usage() {
    echo "Usage: ./build.sh [target]"
    echo ""
    echo "Targets:"
    echo "  all        Build for all platforms (osx, win, lin) [Default]"
    echo "  osx, mac   Build for macOS (ARM64, AMD64 & .app bundle)"
    echo "  win        Build for Windows (x86_64 & ARM64 .exe)"
    echo "  lin, linux Build for Linux (x86_64 & ARM64 ELF)"
    echo "  clean      Clean previous build artifacts"
    echo "  help       Show this help message"
    echo ""
}

print_banner
TARGET="${1:-all}"

case "$TARGET" in
    osx|mac|darwin)
        mkdir -p "${DIST_DIR}"
        build_osx
        print_summary
        ;;
    win|windows)
        mkdir -p "${DIST_DIR}"
        build_win
        print_summary
        ;;
    lin|linux)
        mkdir -p "${DIST_DIR}"
        build_lin
        print_summary
        ;;
    all)
        clean
        build_osx
        build_win
        build_lin
        print_summary
        ;;
    clean)
        clean
        echo -e "${GREEN}✓ Dist directory cleaned.${NC}"
        ;;
    help|--help|-h)
        print_usage
        ;;
    *)
        echo -e "${RED}Unknown target: ${TARGET}${NC}"
        print_usage
        exit 1
        ;;
esac
