#!/bin/bash
# Installation script
# Downloads and installs the latest release, or builds from a branch

set -e

# Color output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

# Configuration
BRAND_NAME_LOWER="${BRAND_NAME_LOWER:-liza}"
derive_brand_upper() {
    printf '%s' "$1" | tr '[:lower:]-' '[:upper:]_'
}
derive_brand_title() {
    printf '%s' "$1" | awk -F- 'BEGIN{OFS=" "} {for (i=1;i<=NF;i++) $i=toupper(substr($i,1,1)) substr($i,2); print}'
}
BRAND_NAME_UPPER="${BRAND_NAME_UPPER:-$(derive_brand_upper "$BRAND_NAME_LOWER")}"
BRAND_NAME_TITLE="${BRAND_NAME_TITLE:-$(derive_brand_title "$BRAND_NAME_LOWER")}"
BRAND_REPO="${BRAND_REPO:-liza-mas/liza}"
BRAND_INSTALL_REPO="${BRAND_INSTALL_REPO:-$BRAND_REPO}"
REPO="$BRAND_INSTALL_REPO"
BINARY_NAME="${BRAND_BINARY_NAME:-$BRAND_NAME_LOWER}"
SOURCE_DIR_NAME="${BRAND_SOURCE_DIR_NAME:-$BRAND_NAME_LOWER}"
BRAND_GLOBAL_DIRNAME="${BRAND_GLOBAL_DIRNAME:-.${BRAND_NAME_LOWER}}"
BRAND_PROJECT_DIRNAME="${BRAND_PROJECT_DIRNAME:-.${BRAND_NAME_LOWER}}"
BRAND_ENV_PREFIX="${BRAND_ENV_PREFIX:-$BRAND_NAME_UPPER}"
BRAND_ARCHIVE_PREFIX="${BRAND_ARCHIVE_PREFIX:-$BINARY_NAME}"
BRAND_RELEASE_REPO="${BRAND_RELEASE_REPO:-$BRAND_REPO}"
BRAND_RELEASE_BASE_URL="${BRAND_RELEASE_BASE_URL:-https://github.com/${BRAND_RELEASE_REPO}/releases/download}"
BRAND_CHECKSUM_BASE_URL="${BRAND_CHECKSUM_BASE_URL:-$BRAND_RELEASE_BASE_URL}"
if [ -z "${INSTALL_DIR:-}" ]; then
    INSTALL_DIR="$HOME/.local/bin"
    if ! echo "$PATH" | tr ':' '\n' | grep -qxF "$INSTALL_DIR"; then
        echo -e "${YELLOW}Note: $INSTALL_DIR is not in your PATH${NC}"
        echo "Add to your shell profile:  export PATH=\"\$HOME/.local/bin:\$PATH\""
        echo ""
    fi
fi

# Detect OS and architecture
detect_platform() {
    local os
    local arch

    # Detect OS
    case "$(uname -s)" in
        Linux*)     os="linux";;
        Darwin*)    os="darwin";;
        MINGW*|MSYS*|CYGWIN*)
            echo -e "${RED}Error: Native Windows is not supported.${NC}"
            echo "Run ${BINARY_NAME} under WSL2 and run this installer from the WSL2 shell."
            exit 1
            ;;
        *)
            echo -e "${RED}Error: Unsupported operating system: $(uname -s)${NC}"
            exit 1
            ;;
    esac

    # Detect architecture
    case "$(uname -m)" in
        x86_64|amd64)   arch="amd64";;
        arm64|aarch64)  arch="arm64";;
        *)
            echo -e "${RED}Error: Unsupported architecture: $(uname -m)${NC}"
            exit 1
            ;;
    esac

    echo "${os}-${arch}"
}

# Get the latest release version
get_latest_version() {
    local version
    version=$(curl -fsSL "https://api.github.com/repos/${REPO}/releases/latest" | grep '"tag_name"' | sed -E 's/.*"([^"]+)".*/\1/')

    if [ -z "$version" ]; then
        echo -e "${RED}Error: Could not determine latest version${NC}" >&2
        return 1
    fi

    echo "$version"
}

# Remove old binaries from /usr/local/bin if installing elsewhere
cleanup_old_binaries() {
    if [ "$INSTALL_DIR" != "/usr/local/bin" ]; then
        for old_bin in "$BINARY_NAME"; do
            old_path="/usr/local/bin/$old_bin"
            if [ -f "$old_path" ]; then
                echo -e "${YELLOW}Removing old $old_bin from /usr/local/bin...${NC}"
                if [ -w "/usr/local/bin" ]; then
                    rm -f "$old_path"
                else
                    sudo rm -f "$old_path"
                fi
            fi
        done
    fi
}

# Download and install
install_release() {
    local platform=$1
    local version=$2
    local tmp_dir
    local version_bare="${version#v}"

    echo -e "${GREEN}Installing ${BINARY_NAME}...${NC}"
    echo "  Version: ${version}"
    echo "  Platform: ${platform}"
    echo "  Install directory: ${INSTALL_DIR}"
    echo ""

    # Create temporary directory
    tmp_dir=$(mktemp -d)
    trap "rm -rf ${tmp_dir}" EXIT

    # Download archive (goreleaser produces tar.gz for linux/darwin)
    local archive_name="${BRAND_ARCHIVE_PREFIX}-${version_bare}-${platform}.tar.gz"
    local download_url="${BRAND_RELEASE_BASE_URL%/}/${version}/${archive_name}"
    echo "Downloading from ${download_url}..."

    if ! curl -fsSL "${download_url}" -o "${tmp_dir}/${archive_name}"; then
        echo -e "${RED}Error: Failed to download archive${NC}"
        echo "URL: ${download_url}"
        exit 1
    fi

    # Extract
    echo "Extracting..."
    tar -xzf "${tmp_dir}/${archive_name}" -C "${tmp_dir}"

    # Make executable
    chmod +x "${tmp_dir}/${BINARY_NAME}"

    # Verify the binary works
    echo "Verifying binary..."
    if ! "${tmp_dir}/${BINARY_NAME}" version >/dev/null 2>&1; then
        echo -e "${YELLOW}Warning: Could not verify binary${NC}"
    fi

    # Install
    echo "Installing to ${INSTALL_DIR}..."

    # Create install directory if it doesn't exist
    if [ ! -d "$INSTALL_DIR" ]; then
        echo "Creating ${INSTALL_DIR}..."
        mkdir -p "$INSTALL_DIR" 2>/dev/null || sudo mkdir -p "$INSTALL_DIR"
    fi

    # Check if we need sudo
    if [ -w "$INSTALL_DIR" ]; then
        mv "${tmp_dir}/${BINARY_NAME}" "${INSTALL_DIR}/${BINARY_NAME}"
    else
        echo "Note: Sudo access required to install to ${INSTALL_DIR}"
        sudo mv "${tmp_dir}/${BINARY_NAME}" "${INSTALL_DIR}/${BINARY_NAME}"
    fi

    cleanup_old_binaries

    echo -e "${GREEN}✓ Installation complete!${NC}"
    echo ""
    echo "Run '${BINARY_NAME} version' to verify installation"
    echo "Run '${BINARY_NAME} help' to get started"
}

# Build from source and install
install_from_source() {
    local branch=$1

    echo -e "${GREEN}Installing ${BINARY_NAME} from branch '${branch}'...${NC}"
    echo "  Install directory: ${INSTALL_DIR}"
    echo ""

    # Check build dependencies
    if ! command -v go >/dev/null 2>&1; then
        echo -e "${RED}Error: go is required to build from source but not installed${NC}"
        echo "Install Go from https://go.dev/dl/"
        exit 1
    fi
    if ! command -v make >/dev/null 2>&1; then
        echo -e "${RED}Error: make is required to build from source but not installed${NC}"
        exit 1
    fi
    if ! command -v git >/dev/null 2>&1; then
        echo -e "${RED}Error: git is required to build from source but not installed${NC}"
        exit 1
    fi

    # Create temporary directory
    local tmp_dir
    tmp_dir=$(mktemp -d)
    trap "rm -rf ${tmp_dir}" EXIT

    # Clone
    echo "Cloning branch '${branch}'..."
    if ! git clone --depth 1 --branch "$branch" -- "https://github.com/${REPO}.git" "${tmp_dir}/${SOURCE_DIR_NAME}"; then
        echo -e "${RED}Error: Failed to clone branch '${branch}'${NC}"
        exit 1
    fi

    # Build and install via Makefile
    echo "Building from source..."
    if ! make -C "${tmp_dir}/${SOURCE_DIR_NAME}" install INSTALL_DIR="$INSTALL_DIR" \
        BRAND_NAME_TITLE="$BRAND_NAME_TITLE" \
        BRAND_NAME_LOWER="$BRAND_NAME_LOWER" \
        BRAND_NAME_UPPER="$BRAND_NAME_UPPER" \
        BRAND_REPO="$BRAND_REPO" \
        BRAND_BINARY_NAME="$BINARY_NAME" \
        BRAND_GLOBAL_DIRNAME="$BRAND_GLOBAL_DIRNAME" \
        BRAND_PROJECT_DIRNAME="$BRAND_PROJECT_DIRNAME" \
        BRAND_ENV_PREFIX="$BRAND_ENV_PREFIX" \
        BRAND_ARCHIVE_PREFIX="$BRAND_ARCHIVE_PREFIX" \
        BRAND_RELEASE_REPO="$BRAND_RELEASE_REPO" \
        BRAND_RELEASE_BASE_URL="$BRAND_RELEASE_BASE_URL" \
        BRAND_CHECKSUM_BASE_URL="$BRAND_CHECKSUM_BASE_URL"; then
        echo -e "${RED}Error: Build failed${NC}"
        exit 1
    fi

    cleanup_old_binaries

    echo ""
    echo -e "${GREEN}✓ Installation complete!${NC}"
    echo ""
    "${INSTALL_DIR}/${BINARY_NAME}" version 2>/dev/null || true
    echo "Run '${BINARY_NAME} help' to get started"
}

# Main
main() {
    echo ""
    echo "${BRAND_NAME_TITLE} Installer"
    printf '%*s\n' "${#BRAND_NAME_TITLE}" '' | tr ' ' '='
    echo ""

    local branch="${BRANCH:-}"
    if [ -n "$branch" ]; then
        install_from_source "$branch"
        return
    fi

    # Check dependencies
    if ! command -v curl >/dev/null 2>&1; then
        echo -e "${RED}Error: curl is required but not installed${NC}"
        exit 1
    fi

    # Detect platform
    local platform
    platform=$(detect_platform)

    # Get latest version (or use VERSION env var if set)
    local version="${VERSION:-}"
    if [ -z "$version" ]; then
        version=$(get_latest_version) || exit 1
    fi

    # Install
    install_release "$platform" "$version"
}

# Show help
if [ "$1" = "-h" ] || [ "$1" = "--help" ]; then
    echo "${BRAND_NAME_TITLE} Installation Script"
    echo ""
    echo "Usage: $0 [OPTIONS]"
    echo ""
    echo "Options:"
    echo "  -h, --help          Show this help message"
    echo ""
    echo "Environment variables:"
    echo "  VERSION             Specific version to install (default: latest release)"
    echo "  BRANCH              Build and install from a git branch (e.g. main). Requires Go and make."
    echo "  INSTALL_DIR         Installation directory (default: ~/.local/bin)"
    echo ""
    echo "Examples:"
    echo "  # Install latest release"
    echo "  curl -fsSL https://raw.githubusercontent.com/${REPO}/main/install.sh | bash"
    echo ""
    echo "  # Install specific version"
    echo "  curl -fsSL https://raw.githubusercontent.com/${REPO}/main/install.sh | VERSION=v1.0.0 bash"
    echo ""
    echo "  # Install from main branch (build from source)"
    echo "  curl -fsSL https://raw.githubusercontent.com/${REPO}/main/install.sh | BRANCH=main bash"
    echo ""
    echo "  # Install to custom directory"
    echo "  curl -fsSL https://raw.githubusercontent.com/${REPO}/main/install.sh | INSTALL_DIR=~/.local/bin bash"
    exit 0
fi

main
