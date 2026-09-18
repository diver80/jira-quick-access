#!/usr/bin/env bash
# ==============================================================================
# Jira Quick Access - Multi-Platform Build Script (OSX / Windows / Linux)
# ==============================================================================

set -e

APP_NAME="jira-quick-access"
APP_DISPLAY_NAME="Jira Quick Access"
BUNDLE_ID="com.avono.jira-quick-access"
VERSION="${VERSION:-1.0.5}"
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
    touch "${DIST_DIR}/.metadata_never_index"
}

build_osx() {
    echo -e "${BLUE}🍎 Building for macOS (Darwin Universal 2)...${NC}"
    
    # 0. Ensure icons are present
    if [ ! -f "assets/AppIcon.icns" ]; then
        echo -e "   -> Generating AppIcon.icns from assets/generate_icon.py..."
        python3 assets/generate_icon.py
    fi

    # 1. ARM64 (Apple Silicon: M1/M2/M3/M4)
    echo -e "   -> Compiling darwin/arm64..."
    mkdir -p "${DIST_DIR}/osx/arm64"
    GOOS=darwin GOARCH=arm64 go build -ldflags="${LDFLAGS}" -o "${DIST_DIR}/osx/arm64/${APP_NAME}" .
    
    # 2. AMD64 (Intel Mac)
    echo -e "   -> Compiling darwin/amd64..."
    mkdir -p "${DIST_DIR}/osx/amd64"
    GOOS=darwin GOARCH=amd64 go build -ldflags="${LDFLAGS}" -o "${DIST_DIR}/osx/amd64/${APP_NAME}" .
    
    # 3. Create macOS .app Bundle in .noindex staging directory (prevents Spotlight duplicate indexing)
    STAGING_DIR="${DIST_DIR}/osx.noindex"
    APP_BUNDLE="${STAGING_DIR}/${APP_DISPLAY_NAME}.app"
    echo -e "   -> Creating macOS App Bundle at ${APP_BUNDLE}..."
    rm -rf "${STAGING_DIR}"
    mkdir -p "${APP_BUNDLE}/Contents/MacOS"
    mkdir -p "${APP_BUNDLE}/Contents/Resources"
    
    # Universal 2 Binary (runs natively on Apple Silicon and Intel)
    if command -v lipo >/dev/null 2>&1; then
        echo -e "   -> Creating Universal 2 binary with lipo (arm64 + x86_64)..."
        lipo -create -output "${APP_BUNDLE}/Contents/MacOS/${APP_NAME}" \
            "${DIST_DIR}/osx/arm64/${APP_NAME}" \
            "${DIST_DIR}/osx/amd64/${APP_NAME}"
    else
        HOST_ARCH=$(uname -m)
        if [ "${HOST_ARCH}" = "arm64" ]; then
            cp "${DIST_DIR}/osx/arm64/${APP_NAME}" "${APP_BUNDLE}/Contents/MacOS/${APP_NAME}"
        else
            cp "${DIST_DIR}/osx/amd64/${APP_NAME}" "${APP_BUNDLE}/Contents/MacOS/${APP_NAME}"
        fi
    fi
    chmod +x "${APP_BUNDLE}/Contents/MacOS/${APP_NAME}"
    
    # Copy App Icon into bundle
    if [ -f "assets/AppIcon.icns" ]; then
        echo -e "   -> Installing AppIcon.icns to Resources..."
        cp "assets/AppIcon.icns" "${APP_BUNDLE}/Contents/Resources/AppIcon.icns"
    fi

    cat <<EOF > "${APP_BUNDLE}/Contents/Info.plist"
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
    <key>CFBundleDevelopmentRegion</key>
    <string>en</string>
    <key>CFBundleExecutable</key>
    <string>${APP_NAME}</string>
    <key>CFBundleIconFile</key>
    <string>AppIcon</string>
    <key>CFBundleIconName</key>
    <string>AppIcon</string>
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
    <key>LSMinimumSystemVersion</key>
    <string>11.0</string>
    <key>NSHighResolutionCapable</key>
    <true/>
    <key>LSUIElement</key>
    <true/>
</dict>
</plist>
EOF

    # 4. Create DMG Installer
    if command -v hdiutil >/dev/null 2>&1; then
        echo -e "   -> Building macOS DMG installer (with /Applications drag-and-drop)..."
        DMG_STAGING="${STAGING_DIR}/dmg_staging"
        DMG_FILE="${DIST_DIR}/osx/${APP_DISPLAY_NAME}-v${VERSION}-macOS-Universal.dmg"
        rm -rf "${DMG_STAGING}" "${DMG_FILE}"
        mkdir -p "${DMG_STAGING}"
        cp -R "${APP_BUNDLE}" "${DMG_STAGING}/"
        ln -s /Applications "${DMG_STAGING}/Applications"
        
        hdiutil create -volname "${APP_DISPLAY_NAME}" \
            -srcfolder "${DMG_STAGING}" \
            -ov -format UDZO \
            "${DMG_FILE}" >/dev/null
        rm -rf "${DMG_STAGING}"
    fi

    tar -czf "${DIST_DIR}/osx/JiraQuickAccess-v${VERSION}-macOS-Universal.tar.gz" -C "${STAGING_DIR}" "${APP_DISPLAY_NAME}.app"
    cp "${DIST_DIR}/osx/JiraQuickAccess-v${VERSION}-macOS-Universal.tar.gz" "${DIST_DIR}/osx/JiraQuickAccess-macOS-Universal.tar.gz"

    echo -e "${GREEN}✓ macOS build & DMG completed!${NC}"
}

build_win() {
    echo -e "${BLUE}🪟 Building for Windows (v${VERSION})...${NC}"
    
    mkdir -p "${DIST_DIR}/win"
    CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -ldflags="${LDFLAGS}" -o "${DIST_DIR}/win/${APP_NAME}-v${VERSION}-windows-amd64.exe" .
    CGO_ENABLED=0 GOOS=windows GOARCH=arm64 go build -ldflags="${LDFLAGS}" -o "${DIST_DIR}/win/${APP_NAME}-v${VERSION}-windows-arm64.exe" .
    
    cp "${DIST_DIR}/win/${APP_NAME}-v${VERSION}-windows-amd64.exe" "${DIST_DIR}/win/${APP_NAME}-windows-amd64.exe"
    cp "${DIST_DIR}/win/${APP_NAME}-v${VERSION}-windows-arm64.exe" "${DIST_DIR}/win/${APP_NAME}-windows-arm64.exe"

    if command -v zip >/dev/null 2>&1; then
        cd "${DIST_DIR}/win"
        zip -q "${APP_NAME}-v${VERSION}-windows-amd64.zip" "${APP_NAME}-v${VERSION}-windows-amd64.exe"
        cp "${APP_NAME}-v${VERSION}-windows-amd64.zip" "${APP_NAME}-windows-amd64.zip"
        zip -q "${APP_NAME}-v${VERSION}-windows-arm64.zip" "${APP_NAME}-v${VERSION}-windows-arm64.exe"
        cd - > /dev/null
    fi

    echo -e "${GREEN}✓ Windows build completed!${NC}"
}

build_lin() {
    echo -e "${BLUE}🐧 Building for Linux (v${VERSION})...${NC}"
    
    mkdir -p "${DIST_DIR}/lin"
    CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags="${LDFLAGS}" -o "${DIST_DIR}/lin/${APP_NAME}-v${VERSION}-linux-amd64" .
    chmod +x "${DIST_DIR}/lin/${APP_NAME}-v${VERSION}-linux-amd64"
    
    CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -ldflags="${LDFLAGS}" -o "${DIST_DIR}/lin/${APP_NAME}-v${VERSION}-linux-arm64" .
    chmod +x "${DIST_DIR}/lin/${APP_NAME}-v${VERSION}-linux-arm64"

    cp "${DIST_DIR}/lin/${APP_NAME}-v${VERSION}-linux-amd64" "${DIST_DIR}/lin/${APP_NAME}-linux-amd64"
    cp "${DIST_DIR}/lin/${APP_NAME}-v${VERSION}-linux-arm64" "${DIST_DIR}/lin/${APP_NAME}-linux-arm64"

    cd "${DIST_DIR}/lin"
    tar -czf "${APP_NAME}-v${VERSION}-linux-amd64.tar.gz" "${APP_NAME}-v${VERSION}-linux-amd64"
    cp "${APP_NAME}-v${VERSION}-linux-amd64.tar.gz" "${APP_NAME}-linux-amd64.tar.gz"
    tar -czf "${APP_NAME}-v${VERSION}-linux-arm64.tar.gz" "${APP_NAME}-v${VERSION}-linux-arm64"
    cp "${APP_NAME}-v${VERSION}-linux-arm64.tar.gz" "${APP_NAME}-linux-arm64.tar.gz"
    cd - > /dev/null

    echo -e "${GREEN}✓ Linux build completed!${NC}"
}

install_osx() {
    build_osx
    echo -e "${BLUE}📲 Installing ${APP_DISPLAY_NAME} to /Applications...${NC}"
    APP_BUNDLE="${DIST_DIR}/osx.noindex/${APP_DISPLAY_NAME}.app"
    DEST_APP="/Applications/${APP_DISPLAY_NAME}.app"
    
    # If running, terminate previous instance
    pkill -f "${APP_NAME}" 2>/dev/null || true
    
    rm -rf "${DEST_APP}"
    cp -R "${APP_BUNDLE}" "/Applications/"
    
    # Remove quarantine flag for local build
    xattr -dr com.apple.quarantine "${DEST_APP}" 2>/dev/null || true
    
    # Clean up staging app bundle to ensure Spotlight only indexes /Applications
    rm -rf "${DIST_DIR}/osx.noindex"
    
    echo -e "${GREEN}✓ Successfully installed to ${DEST_APP}!${NC}"
    echo -e "${CYAN}💡 You can now launch it via Spotlight (⌘+Space -> 'Jira Quick Access') or Applications folder.${NC}"
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
    echo "  all          Build for all platforms (osx, win, lin) [Default]"
    echo "  osx, mac     Build for macOS (Universal 2 .app bundle & .dmg installer)"
    echo "  install      Build and install directly to /Applications on this Mac"
    echo "  dmg          Build macOS DMG drag-and-drop installer"
    echo "  win          Build for Windows (x86_64 & ARM64 .exe)"
    echo "  lin, linux   Build for Linux (x86_64 & ARM64 ELF)"
    echo "  clean        Clean previous build artifacts"
    echo "  help         Show this help message"
    echo ""
}

print_banner
TARGET="${1:-all}"

case "$TARGET" in
    install)
        mkdir -p "${DIST_DIR}"
        install_osx
        ;;
    osx|mac|darwin|dmg)
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

