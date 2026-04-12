# Release Guide

This document describes how to create and publish releases for liza.

## Automated Releases (Recommended)

Releases are automated using GitHub Actions and GoReleaser. Simply push a version tag to trigger a release:

```bash
# Create a new version tag
git tag -a v1.0.0 -m "Release v1.0.0"

# Push the tag to GitHub
git push origin v1.0.0
```

This will automatically:
1. Build binaries for all supported platforms (liza)
2. Create checksums
3. Generate a changelog
4. Create a GitHub release with all artifacts
5. Make binaries available via the installation script

Note: Tests and linting run via CI on push/PR to `main`. The release workflow assumes `main` is green when tagged.

## Manual Releases

If you need to create a release manually:

### 1. Build Release Artifacts

```bash
# Set the version
export VERSION=v1.0.0

# Create release artifacts (runs tests, builds all platforms, creates checksums)
make release

# Optionally create distribution packages (tarballs and zip files)
make package
```

This creates the following in the `dist/` directory:
- Binaries for Linux (amd64, arm64)
- Binaries for macOS (amd64, arm64)
- Binary for Windows (amd64)
- SHA256 checksums file
- Compressed archives (if using `make package`)

### 2. Test the Binaries

```bash
# Test a binary
./dist/liza-darwin-arm64 version
./dist/liza-linux-amd64 version

# Extract and test a package
tar -xzf dist/liza-v1.0.0-linux-amd64.tar.gz
./liza version
```

### 3. Create GitHub Release

1. Go to https://github.com/liza-mas/liza/releases/new
2. Create a new tag (e.g., `v1.0.0`)
3. Write release notes (see format below)
4. Upload all files from `dist/`
5. Publish the release

## Release Notes Format

Use this template for release notes:

````markdown
## Liza v1.0.0

Brief description of the release.

### Features
- New feature 1
- New feature 2

### Bug Fixes
- Fixed bug 1
- Fixed bug 2

### Installation

**Quick install (macOS/Linux):**
```bash
curl -fsSL https://raw.githubusercontent.com/liza-mas/liza/main/install.sh | bash
```

**Manual download:**
Download the appropriate archive for your platform below, extract it, and move the binaries to your PATH.

### Checksums

Download `checksums.txt` and verify with:
```bash
sha256sum -c checksums.txt 2>&1 | grep OK
```
````

## Versioning

This project follows [Semantic Versioning](https://semver.org/):

- **MAJOR** version (v1.0.0 → v2.0.0): Incompatible API changes
- **MINOR** version (v1.0.0 → v1.1.0): New functionality (backwards compatible)
- **PATCH** version (v1.0.0 → v1.0.1): Bug fixes (backwards compatible)

### Pre-release Versions

For beta or release candidate versions, use suffixes:
- `v1.0.0-beta.1`
- `v1.0.0-rc.1`

## Release Checklist

Before creating a release:

- [ ] All tests pass: `make test`
- [ ] Linting passes: `make lint`
- [ ] README is up to date
- [ ] Version number follows semantic versioning
- [ ] All commits are in main branch
- [ ] No uncommitted changes

After creating a release:

- [ ] Test the installation script
- [ ] Verify binaries work on each platform (or use CI)
- [ ] Update any package managers (Homebrew, etc.) if applicable

## Using GoReleaser Locally

To test the release process locally without publishing:

```bash
# Install goreleaser (if not already installed)
brew install goreleaser
# or: go install github.com/goreleaser/goreleaser/v2@latest

# Run a dry-run release (doesn't publish)
goreleaser release --snapshot --clean

# Check the artifacts
ls -la dist/
```

## Installation Script

The installation script (`install.sh`) automatically downloads the latest release:

```bash
# Install latest version
curl -fsSL https://raw.githubusercontent.com/liza-mas/liza/main/install.sh | bash

# Install specific version
curl -fsSL https://raw.githubusercontent.com/liza-mas/liza/main/install.sh | VERSION=v1.0.0 bash

# Install to custom directory
curl -fsSL https://raw.githubusercontent.com/liza-mas/liza/main/install.sh | INSTALL_DIR=~/.local/bin bash
```

## Troubleshooting

### Build fails on a specific platform

Cross-compilation issues are rare with Go, but if you encounter one:

1. Check if CGO is disabled: `CGO_ENABLED=0`
2. Ensure Go version is compatible
3. Check for platform-specific code

### Release workflow fails

1. Check GitHub Actions logs
2. Ensure `GITHUB_TOKEN` has proper permissions
3. Verify `.goreleaser.yaml` is valid: `goreleaser check`

### Installation script fails

1. Verify the release artifacts are uploaded
2. Check that archive names match the expected format
3. Test the URL manually: `curl -fsSL <release-url>`

## Support Matrix

### Platforms

| OS | Architecture | Supported |
|----|--------------|-----------|
| Linux | amd64 | Yes |
| Linux | arm64 | Yes |
| macOS | amd64 (Intel) | Yes |
| macOS | arm64 (Apple Silicon) | Yes |
| Windows | amd64 | Yes |

### Go Versions

- Minimum: Go 1.25
- Recommended: Go 1.25+

## Additional Resources

- [GoReleaser Documentation](https://goreleaser.com)
- [GitHub Actions Documentation](https://docs.github.com/en/actions)
- [Semantic Versioning](https://semver.org/)
