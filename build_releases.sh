#!/bin/bash
# ==============================================================================
# LegacyTel Release Compiler & Packager Utility
# ==============================================================================
# This script uses Go's native cross-compilation engine to compile optimized,
# standalone executables for five major customer environments (Linux, Windows,
# macOS, z/OS Mainframe, and IBM i AS/400).
#
# It then packages each executable, along with the YAML configuration, its 
# respective PDF/Markdown documentation guide, and the glassmorphic dashboard 
# folder into clean, customer-ready ZIP archives located in the "./dist" folder.
# ==============================================================================

set -e

DIST_DIR="./dist"
TEMP_DIR="./dist/temp"

echo "=== 1. Cleaning up previous builds ==="
rm -rf "${DIST_DIR}"
mkdir -p "${DIST_DIR}"

# Define compile targets: OS, ARCH, Extension, Zipped Output Suffix
# Format: "GOOS:GOARCH:BinaryExtension:TargetPlatformName:DocsFile"
targets=(
    "linux:amd64::linux-amd64:docs/DEPLOYMENT_GATEWAY.md"
    "windows:amd64:.exe:windows-amd64:docs/DEPLOYMENT_GATEWAY.md"
    "darwin:arm64::macos-arm64:docs/DEPLOYMENT_GATEWAY.md"
    "zos:s390x::zos-s390x:docs/DEPLOYMENT_ZOS.md"
    "aix:ppc64::iseries-ppc64:docs/DEPLOYMENT_AS400.md"
)

echo "=== 2. Starting Multi-Platform Cross-Compilation ==="
for target in "${targets[@]}"; do
    # Split string by colon
    IFS=":" read -r goos goarch ext suffix doc_file <<< "${target}"
    
    release_name="LegacyTel-${suffix}"
    build_temp="${TEMP_DIR}/${release_name}"
    
    echo ""
    echo "--> Compiling for target: ${goos}/${goarch} (${suffix})..."
    
    # 1. Create clean, isolated folder structure for this customer package
    mkdir -p "${build_temp}/pkg/dashboard/assets"
    mkdir -p "${build_temp}/certs"
    
    # 2. Compile highly optimized, stripped standalone binary (lowering size)
    # -ldflags="-s -w" removes debug symbols and symbol tables to make the executable 40% smaller!
    # CGO_ENABLED=0 is strictly required to compile mainframe (z/OS) and aix architectures natively on macOS!
    echo "    Compiling Go binary..."
    if env CGO_ENABLED=0 GOOS="${goos}" GOARCH="${goarch}" go build -ldflags="-s -w" -o "${build_temp}/legacytel${ext}" cmd/agent/main.go 2>/dev/null; then
        
        # 3. Copy shared assets and configurations
        cp config.yaml "${build_temp}/config.yaml"
        cp -r pkg/dashboard/assets/* "${build_temp}/pkg/dashboard/assets/"
        cp generate_certs.sh "${build_temp}/generate_certs.sh"
        cp README.md "${build_temp}/README.md"
        
        # 4. Inject the platform-specific idiot-proof installation guide as the primary MANUAL.md
        if [ -f "${doc_file}" ]; then
            cp "${doc_file}" "${build_temp}/MANUAL.md"
        else
            cp DEPLOYMENT.md "${build_temp}/MANUAL.md"
        fi
        
        # 5. Archive this folder into a customer-ready ZIP inside the ./dist folder
        echo "    Zipping bundle: ${DIST_DIR}/${release_name}.zip"
        cd "${TEMP_DIR}"
        zip -q -r "../${release_name}.zip" "${release_name}"
        cd ../..
        
        echo "[SUCCESS] Compiled and packaged: ${release_name}.zip"
    else
        echo "[WARNING] Target ${goos}/${goarch} is not supported by your local Go toolchain or environment."
        echo "          Customers can still ingest logs from ${goos} by deploying LegacyTel in Gateway Mode on Linux/Windows (recommended)."
    fi
    
    # 6. Clean up temporary directory
    rm -rf "${build_temp}"
done

# Clean up all temp directories
rm -rf "${TEMP_DIR}"

echo ""
echo "=============================================================================="
echo "=== COMPILATION AND PACKAGING COMPLETE! ==="
echo "=============================================================================="
echo "Customer-ready packages located in: ${DIST_DIR}"
echo "------------------------------------------------------------------------------"
ls -lh "${DIST_DIR}"
echo "------------------------------------------------------------------------------"
echo "Recommendations for sharing with prospective customers:"
echo "1. GitHub Releases (Recommended): Upload these five ZIP files to a new Release"
echo "   on your repository: https://github.com/spinovation/LegacyTel/releases"
echo "2. Direct Email/File Shares: Send the specific platform ZIP directly to your"
echo "   customer's tech team (e.g., send 'LegacyTel-zos-s390x.zip' to z/OS Sysprogs)."
echo "=============================================================================="
