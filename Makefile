.PHONY: build test test-fast test-race test-e2e clean install lint check-testhelpers check-embedded release package build-all tidy run coverage help

# Brand variables
BRAND_NAME_LOWER?=liza
BRAND_NAME_UPPER?=$(shell printf '%s' '$(BRAND_NAME_LOWER)' | tr '[:lower:]-' '[:upper:]_')
BRAND_NAME_TITLE?=$(shell printf '%s' '$(BRAND_NAME_LOWER)' | awk -F- 'BEGIN{OFS=" "} {for (i=1;i<=NF;i++) $$i=toupper(substr($$i,1,1)) substr($$i,2); print}')
BRAND_REPO?=liza-mas/liza
BRAND_BINARY_NAME?=$(BRAND_NAME_LOWER)
BRAND_GLOBAL_DIRNAME?=.$(BRAND_NAME_LOWER)
BRAND_PROJECT_DIRNAME?=.$(BRAND_NAME_LOWER)
BRAND_ENV_PREFIX?=$(BRAND_NAME_UPPER)
BRAND_ARCHIVE_PREFIX?=$(BRAND_BINARY_NAME)
BRAND_RELEASE_REPO?=$(BRAND_REPO)
BRAND_RELEASE_BASE_URL?=https://github.com/$(BRAND_RELEASE_REPO)/releases/download
BRAND_CHECKSUM_BASE_URL?=$(BRAND_RELEASE_BASE_URL)

# Binary name
BINARY_NAME?=$(BRAND_BINARY_NAME)

# Build variables
# Derived from git rather than pinned, because a pinned default goes stale and
# then lies: the binary claimed 0.2.0 long after 0.8.0 shipped. Only an exact,
# clean tag produces the semver value — anything else (commits past the tag, a
# dirty tree, no tags, no git) is the non-semver "dev-<sha>". A prerelease-style
# value like "v0.8.0-3-gabc123" would still be valid semver but would sort
# *below* the v0.8.0 release, so the updater would offer to replace a newer
# dev build with an older one. Non-semver keeps the updater's "valid semver
# current version required" gate closed, so a source build is left alone
# instead of being offered a release to overwrite itself with.
VERSION?=$(shell tag=$$(git describe --tags --exact-match 2>/dev/null); if [ -n "$$tag" ] && [ -z "$$(git status --porcelain 2>/dev/null)" ]; then printf '%s' "$$tag"; else printf 'dev-%s' "$$(git rev-parse --short HEAD 2>/dev/null || echo unknown)"; fi)
GIT_COMMIT?=$(shell git rev-parse --short HEAD 2>/dev/null || echo "unknown")
BUILD_DATE?=$(shell date -u +"%Y-%m-%dT%H:%M:%SZ")
LDFLAGS=-ldflags "-X 'github.com/liza-mas/liza/internal/embedded.Version=$(VERSION)' \
		-X 'github.com/liza-mas/liza/internal/embedded.GitCommit=$(GIT_COMMIT)' \
		-X 'github.com/liza-mas/liza/internal/embedded.BuildDate=$(BUILD_DATE)' \
		-X 'github.com/liza-mas/liza/internal/brand.NameLower=$(BRAND_NAME_LOWER)' \
		-X 'github.com/liza-mas/liza/internal/brand.NameUpper=$(BRAND_NAME_UPPER)' \
		-X 'github.com/liza-mas/liza/internal/brand.NameTitle=$(BRAND_NAME_TITLE)' \
		-X 'github.com/liza-mas/liza/internal/brand.Repo=$(BRAND_REPO)' \
		-X 'github.com/liza-mas/liza/internal/brand.BinaryName=$(BRAND_BINARY_NAME)' \
		-X 'github.com/liza-mas/liza/internal/brand.GlobalDirName=$(BRAND_GLOBAL_DIRNAME)' \
		-X 'github.com/liza-mas/liza/internal/brand.ProjectDirName=$(BRAND_PROJECT_DIRNAME)' \
		-X 'github.com/liza-mas/liza/internal/brand.EnvPrefix=$(BRAND_ENV_PREFIX)' \
		-X 'github.com/liza-mas/liza/internal/brand.ArchivePrefix=$(BRAND_ARCHIVE_PREFIX)' \
		-X 'github.com/liza-mas/liza/internal/brand.ReleaseRepo=$(BRAND_RELEASE_REPO)' \
		-X 'github.com/liza-mas/liza/internal/brand.ReleaseBaseURL=$(BRAND_RELEASE_BASE_URL)' \
		-X 'github.com/liza-mas/liza/internal/brand.ChecksumBaseURL=$(BRAND_CHECKSUM_BASE_URL)' \
		-X 'main.Version=$(VERSION)' -X 'main.GitCommit=$(GIT_COMMIT)' -X 'main.BuildDate=$(BUILD_DATE)'"

# Sync embedded files from project root
.PHONY: sync-embedded
sync-embedded:
	@echo "Syncing files to internal/embedded/..."
	@go run ./internal/brandrender/cmd/sync-embedded --repo-root .
	@echo "Files synced successfully"

# Build the binaries
build: sync-embedded
	@echo "Building $(BINARY_NAME) (version=$(VERSION), commit=$(GIT_COMMIT), date=$(BUILD_DATE))"
	@go build $(LDFLAGS) -o $(BINARY_NAME) ./cmd/liza

# Run tests
# IMPORTANT: Always use `make test`, not bare `go test ./...`.
# The sync-embedded step copies contracts/ and skills/ into internal/embedded/ for go:embed.
# claude-settings.json and hooks/ are mastered directly in internal/embedded/.
test: sync-embedded check-testhelpers
	go test ./...

# Run the short test subset for fast local feedback
test-fast: sync-embedded check-testhelpers
	go test -short ./...

# Run the full suite with race instrumentation
test-race: sync-embedded check-testhelpers
	go test -race ./...

# Run e2e tests (full sprint sequence with mock CLI — ~40s)
test-e2e: sync-embedded check-testhelpers
	go test -race -tags e2e -run '^(TestFullSprintSequence|TestTerminalDependencyRecovery)$$' ./internal/integration/ -count=1

# Run tests with a per-invocation, self-cleaning coverage profile
coverage: sync-embedded check-testhelpers
	@set -eu; \
		coverage_file="$$(mktemp "$${TMPDIR:-/tmp}/$(BINARY_NAME)-coverage.XXXXXX")"; \
		trap 'rm -f "$$coverage_file"' EXIT HUP INT TERM; \
		go test -coverprofile="$$coverage_file" ./...; \
		go tool cover -html="$$coverage_file"

# Clean build artifacts
clean:
	rm -f $(BINARY_NAME)
	rm -f $(BINARY_NAME)-*
	rm -f coverage.out
	rm -rf dist
	rm -rf internal/embedded/contracts internal/embedded/skills internal/embedded/support-docs internal/embedded/docs internal/embedded/specs
	go clean

# Install the binaries
# Prefer INSTALL_DIR env var, then ~/.local/bin (same as install.sh)
INSTALL_DIR ?= $(HOME)/.local/bin
SUDO := $(shell test -w $(INSTALL_DIR) && echo "" || echo "sudo")
install: build
	@mkdir -p $(INSTALL_DIR)
	$(SUDO) install -m 755 $(BINARY_NAME) $(INSTALL_DIR)/$(BINARY_NAME)
	@if [ "$(INSTALL_DIR)" != "/usr/local/bin" ] && [ -f /usr/local/bin/$(BINARY_NAME) ]; then \
		echo "Warning: old $(BINARY_NAME) binary found in /usr/local/bin — run 'sudo rm /usr/local/bin/$(BINARY_NAME)' to avoid shadowing"; \
	fi

# Check that testhelpers package is not imported in production code
# This prevents test utilities from leaking into production binaries and
# ensures clear separation between test and production code. Test helpers
# should only be used in *_test.go files.
check-testhelpers:
	@echo "Checking for testhelpers in production code..."
	@matches="$$(grep -rl --include='*.go' --exclude='*_test.go' 'internal/testhelpers' cmd internal plugin || true)"; \
	if [ -n "$$matches" ]; then \
		echo "ERROR: testhelpers package imported in production code:"; \
		printf '%s\n' "$$matches"; \
		echo ""; \
		echo "The testhelpers package should only be imported in test files (*_test.go)."; \
		echo "This ensures test utilities don't leak into production binaries."; \
		exit 1; \
	fi
	@echo "✓ No testhelpers in production code"

# Check that embedded copies match repo master files
check-embedded:
	@echo "Checking embedded artifact consistency..."
	@go test ./internal/embedded/ -run TestArtifactConsistency -count=1
	@echo "✓ Embedded artifacts are consistent with masters"

# Run linters
lint: sync-embedded check-testhelpers check-embedded
	go fmt ./...
	go vet ./...

# Tidy dependencies
tidy:
	go mod tidy

# Run the binary
run: build
	./$(BINARY_NAME)

# Build for multiple platforms
build-all: sync-embedded
	GOOS=linux GOARCH=amd64 go build $(LDFLAGS) -o $(BINARY_NAME)-linux-amd64 ./cmd/liza
	GOOS=darwin GOARCH=amd64 go build $(LDFLAGS) -o $(BINARY_NAME)-darwin-amd64 ./cmd/liza
	GOOS=darwin GOARCH=arm64 go build $(LDFLAGS) -o $(BINARY_NAME)-darwin-arm64 ./cmd/liza
	GOOS=windows GOARCH=amd64 go build $(LDFLAGS) -o $(BINARY_NAME)-windows-amd64.exe ./cmd/liza
	GOOS=windows GOARCH=arm64 go build $(LDFLAGS) -o $(BINARY_NAME)-windows-arm64.exe ./cmd/liza

# Create release artifacts
release: clean lint test-race
	@echo "Building release artifacts..."
	@mkdir -p dist
	@# Build $(BINARY_NAME) for all platforms
	GOOS=linux GOARCH=amd64 go build $(LDFLAGS) -o dist/$(BINARY_NAME)-linux-amd64 ./cmd/liza
	GOOS=linux GOARCH=arm64 go build $(LDFLAGS) -o dist/$(BINARY_NAME)-linux-arm64 ./cmd/liza
	GOOS=darwin GOARCH=amd64 go build $(LDFLAGS) -o dist/$(BINARY_NAME)-darwin-amd64 ./cmd/liza
	GOOS=darwin GOARCH=arm64 go build $(LDFLAGS) -o dist/$(BINARY_NAME)-darwin-arm64 ./cmd/liza
	GOOS=windows GOARCH=amd64 go build $(LDFLAGS) -o dist/$(BINARY_NAME)-windows-amd64.exe ./cmd/liza
	GOOS=windows GOARCH=arm64 go build $(LDFLAGS) -o dist/$(BINARY_NAME)-windows-arm64.exe ./cmd/liza
	@# Create checksums
	@cd dist && sha256sum * > checksums.txt
	@echo "✓ Release artifacts created in dist/"
	@echo ""
	@echo "Artifacts:"
	@ls -lh dist/
	@echo ""
	@echo "Checksums:"
	@cat dist/checksums.txt

# Create distribution packages (tarballs)
package: release
	@echo "Creating distribution packages..."
	@cd dist && \
		tar -czf $(BINARY_NAME)-$(VERSION)-linux-amd64.tar.gz $(BINARY_NAME)-linux-amd64 && \
		tar -czf $(BINARY_NAME)-$(VERSION)-linux-arm64.tar.gz $(BINARY_NAME)-linux-arm64 && \
		tar -czf $(BINARY_NAME)-$(VERSION)-darwin-amd64.tar.gz $(BINARY_NAME)-darwin-amd64 && \
		tar -czf $(BINARY_NAME)-$(VERSION)-darwin-arm64.tar.gz $(BINARY_NAME)-darwin-arm64
	@echo "✓ Distribution packages created"
	@ls -lh dist/*.tar.gz

# Help target
help:
	@echo "Available targets:"
	@echo "  build              - Build $(BINARY_NAME) binary"
	@echo "  test               - Run the full suite without race or coverage instrumentation"
	@echo "  test-fast          - Run the short test subset"
	@echo "  test-race          - Run the full suite with race instrumentation"
	@echo "  test-e2e           - Run e2e full sprint test (~40s, requires -tags e2e)"
	@echo "  coverage           - Run tests with coverage report"
	@echo "  clean              - Clean build artifacts"
	@echo "  install            - Install $(BINARY_NAME) binary"
	@echo "  lint               - Run linters (includes testhelpers check)"
	@echo "  check-testhelpers  - Verify testhelpers not in production code"
	@echo "  check-embedded     - Verify embedded copies match repo masters"
	@echo "  tidy               - Tidy dependencies"
	@echo "  run                - Build and run the $(BINARY_NAME) binary"
	@echo "  build-all          - Build $(BINARY_NAME) for multiple platforms"
	@echo "  release            - Create release artifacts (run tests, build all platforms, create checksums)"
	@echo "  package            - Create distribution packages (tarballs)"
