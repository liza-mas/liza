package updater

import (
	"archive/tar"
	"bufio"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"

	"github.com/liza-mas/liza/internal/brand"
	"github.com/liza-mas/liza/internal/paths"
	"golang.org/x/mod/semver"
)

const (
	canonicalRepo = "liza-mas/liza"

	channelStable = "stable"
	channelMain   = "main"

	channelEnvSuffix = "UPDATE_CHANNEL"
	checkEnvSuffix   = "CHECK_UPDATE"
	skipEnvSuffix    = "SKIP_AUTO_UPDATE"
	updatePrefs      = "update.json"
)

// FatalError represents a fatal CLI input/config error that should exit
// before command execution, as opposed to non-fatal update failures.
type FatalError struct {
	Err error
}

func (e *FatalError) Error() string {
	return e.Err.Error()
}

func (e *FatalError) Unwrap() error {
	return e.Err
}

type Config struct {
	CurrentVersion string
	CurrentCommit  string
	Args           []string
	Env            []string
	Stdin          io.Reader
	Stdout         io.Writer
	Stderr         io.Writer
	IsInteractive  func() bool
	CheckUpdate    bool
	Channel        string
	InstallTimeout time.Duration
	LookupLatest   func(context.Context) (string, error)
	LookupMain     func(context.Context) (string, error)
	IsMainAhead    func(context.Context, string, string) (bool, error)
	Install        func(context.Context, candidate, string, io.Writer, io.Writer) error
	InstallTarget  func() (string, error)
	CheckDisabled  func() bool
	DisableCheck   func() error
	Reexec         func(string, []string, []string) error
	// ReleaseBaseURL overrides the archive download base for this run.
	// When empty, archive and checksum URLs use the compiled brand defaults.
	ReleaseBaseURL string
	// VerifyInstall runs post-install verification (e.g., binary version check).
	// If nil, verification is skipped.
	VerifyInstall func(context.Context, string, io.Writer) error
}

type moduleInfo struct {
	Version string `json:"Version"`
	Origin  struct {
		Hash string `json:"Hash"`
	} `json:"Origin"`
}

type releaseInfo struct {
	TagName string `json:"tag_name"`
}

type commitInfo struct {
	SHA string `json:"sha"`
}

type compareInfo struct {
	AheadBy  int `json:"ahead_by"`
	BehindBy int `json:"behind_by"`
}

type candidate struct {
	Channel string
	Current string
	Latest  string
	Ref     string
}

func MaybeUpdateAndReexec(ctx context.Context, cfg Config) error {
	cfg = withDefaults(cfg)
	if err := validateExplicitUpdateFlags(cfg.Args); err != nil {
		return err
	}
	if err := persistExplicitUpdatePreferences(cfg); err != nil {
		return err
	}
	if UpdateSettingsOnly(cfg.Args) {
		return nil
	}
	if shouldSkip(cfg) {
		return nil
	}

	// Validate update channel if explicitly set via flag
	channel := updateChannel(cfg)
	if channel != channelStable && channel != channelMain {
		return &FatalError{fmt.Errorf("invalid update channel %q (valid values: stable, main)", channel)}
	}

	lookupCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	next, err := lookupCandidate(lookupCtx, cfg)
	if err != nil {
		// FatalError represents invalid CLI input/config that should exit
		var fatalErr *FatalError
		if errors.As(err, &fatalErr) {
			return err
		}
		// Non-fatal lookup failures: log to stderr and continue
		fmt.Fprintf(cfg.Stderr, "%s: update check failed: %v\n", cliPrefix(), err)
		return nil
	}
	if next.Ref == "" {
		return nil
	}

	input := bufio.NewReader(cfg.Stdin)
	if !confirmUpdate(input, cfg.Stderr, next) {
		proposeDisableCheck(input, cfg.Stderr, cfg.DisableCheck)
		return nil
	}

	target, err := cfg.InstallTarget()
	if err != nil {
		fmt.Fprintf(cfg.Stderr, "%s: find install target failed: %v\n", cliPrefix(), err)
		return nil
	}
	fmt.Fprintln(cfg.Stderr, installPlan(next, target))

	// Use a separate bounded context for install operations
	installCtx, cancel := context.WithTimeout(ctx, cfg.InstallTimeout)
	defer cancel()

	rollback, err := prepareInstallRollback(target)
	if err != nil {
		fmt.Fprintf(cfg.Stderr, "%s: prepare update rollback failed: %v\n", cliPrefix(), err)
		return nil
	}
	defer rollback.cleanup()

	if err := cfg.Install(installCtx, next, target, cfg.Stderr, cfg.Stderr); err != nil {
		if restoreErr := rollback.restore(target); restoreErr != nil {
			fmt.Fprintf(cfg.Stderr, "%s: restore previous install failed: %v\n", cliPrefix(), restoreErr)
		}
		fmt.Fprintf(cfg.Stderr, "%s: update install failed: %v\n", cliPrefix(), err)
		return nil
	}

	// Post-install verification if configured
	if cfg.VerifyInstall != nil {
		verifyCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
		defer cancel()
		if err := cfg.VerifyInstall(verifyCtx, target, cfg.Stderr); err != nil {
			fmt.Fprintf(cfg.Stderr, "%s: post-install verification failed: %v\n", cliPrefix(), err)
			if restoreErr := rollback.restore(target); restoreErr != nil {
				fmt.Fprintf(cfg.Stderr, "%s: restore previous install failed: %v\n", cliPrefix(), restoreErr)
			}
			fmt.Fprintf(cfg.Stderr, "%s: falling through to original command without reexec\n", cliPrefix())
			return nil
		}
	}

	args := reexecArgs(cfg.Args)
	env := setEnv(cfg.Env, brand.EnvName(skipEnvSuffix), "1")
	return cfg.Reexec(target, args, env)
}

func lookupCandidate(ctx context.Context, cfg Config) (candidate, error) {
	switch updateChannel(cfg) {
	case channelStable:
		current := normalizeVersion(cfg.CurrentVersion)
		if !semver.IsValid(current) {
			return candidate{}, nil
		}
		latest, err := cfg.LookupLatest(ctx)
		if err != nil {
			return candidate{}, err
		}
		if !isNewerVersion(current, latest) {
			return candidate{}, nil
		}
		return candidate{
			Channel: channelStable,
			Current: current,
			Latest:  normalizeVersion(latest),
			Ref:     normalizeVersion(latest),
		}, nil
	case channelMain:
		current := normalizeCommit(cfg.CurrentCommit)
		if current == "" {
			return candidate{}, nil
		}
		latest, err := cfg.LookupMain(ctx)
		if err != nil {
			return candidate{}, err
		}
		latest = normalizeCommit(latest)
		if latest == "" || sameCommit(current, latest) {
			return candidate{}, nil
		}
		ahead, err := cfg.IsMainAhead(ctx, current, latest)
		if err != nil {
			return candidate{}, err
		}
		if !ahead {
			return candidate{}, nil
		}
		return candidate{
			Channel: channelMain,
			Current: shortCommit(current),
			Latest:  shortCommit(latest),
			Ref:     latest,
		}, nil
	default:
		return candidate{}, &FatalError{fmt.Errorf("invalid update channel %q (want stable or main)", updateChannel(cfg))}
	}
}

func LatestModuleVersion(ctx context.Context) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, githubLatestReleaseURL(), nil)
	if err != nil {
		return "", err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("lookup latest release: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("lookup latest release: HTTP %s", resp.Status)
	}
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("read latest release response: %w", err)
	}
	return parseLatestReleaseVersion(data)
}

func MainCommit(ctx context.Context) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, githubMainCommitURL(), nil)
	if err != nil {
		return "", err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("lookup main commit: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("lookup main commit: HTTP %s", resp.Status)
	}
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("read main commit response: %w", err)
	}
	return parseMainCommit(data)
}

func MainCommitAhead(ctx context.Context, current, latest string) (bool, error) {
	current = normalizeCommit(current)
	latest = normalizeCommit(latest)
	if current == "" || latest == "" || sameCommit(current, latest) {
		return false, nil
	}

	url := githubCompareURL(current, latest)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return false, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return false, fmt.Errorf("compare main commits: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return false, nil
	}
	if resp.StatusCode != http.StatusOK {
		return false, fmt.Errorf("compare main commits: HTTP %s", resp.Status)
	}
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return false, fmt.Errorf("read compare response: %w", err)
	}
	var info compareInfo
	if err := json.Unmarshal(data, &info); err != nil {
		return false, fmt.Errorf("parse compare response: %w", err)
	}
	return mainCommitAheadFromCompare(info), nil
}

func mainCommitAheadFromCompare(info compareInfo) bool {
	return info.AheadBy > 0 && info.BehindBy == 0
}

func Install(ctx context.Context, next candidate, target string, stdout, stderr io.Writer) error {
	return InstallWithBaseURL(ctx, next, target, stdout, stderr, "")
}

// InstallWithBaseURL is the actual implementation that accepts a releaseBaseURL parameter.
// This allows test injection while maintaining a safe default in production.
func InstallWithBaseURL(ctx context.Context, next candidate, target string, stdout, stderr io.Writer, releaseBaseURL string) error {
	switch next.Channel {
	case channelStable:
		return installReleaseBinary(ctx, next.Ref, target, stderr, releaseBaseURL)
	case channelMain:
		return installFromSource(ctx, next.Ref, target, stderr)
	default:
		return fmt.Errorf("invalid update channel %q", next.Channel)
	}
}

func InstallTarget() (string, error) {
	path, err := os.Executable()
	if err != nil {
		if found, lookErr := exec.LookPath(binaryName()); lookErr == nil {
			return found, nil
		}
		return "", err
	}
	return path, nil
}

func installPlan(next candidate, target string) string {
	switch next.Channel {
	case channelStable:
		return fmt.Sprintf("Installing release binary %s to %s", next.Ref, target)
	case channelMain:
		return fmt.Sprintf("Building source commit %s to %s", next.Ref, target)
	default:
		return fmt.Sprintf("Installing %s to %s", next.Ref, target)
	}
}

func installReleaseBinary(ctx context.Context, version, target string, stderr io.Writer, releaseBaseURL string) error {
	return installReleaseBinaryWithChecksumBase(ctx, version, target, stderr, releaseBaseURL, "")
}

func installReleaseBinaryWithChecksumBase(ctx context.Context, version, target string, stderr io.Writer, releaseBaseURL, checksumBaseURL string) error {
	url := releaseArchiveURL(version, runtime.GOOS, runtime.GOARCH, releaseBaseURL)
	fmt.Fprintf(stderr, "Downloading %s\n", url)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("create release request: %w", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("download release archive: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download release archive: HTTP %s", resp.Status)
	}

	// Read the entire archive into memory for checksum verification
	archiveData, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read release archive: %w", err)
	}

	// Download and verify checksums
	checksumsURL := checksumURLWithBase(version, checksumBaseURL)
	checksums, err := downloadChecksums(ctx, checksumsURL)
	if err != nil {
		return fmt.Errorf("download checksums: %w", err)
	}

	versionBare := strings.TrimPrefix(version, "v")
	archiveName := releaseArchiveName(versionBare, runtime.GOOS, runtime.GOARCH)
	expectedChecksum, ok := checksums[archiveName]
	if !ok {
		return fmt.Errorf("checksum not found for %s", archiveName)
	}

	if err := verifyChecksum(archiveData, expectedChecksum); err != nil {
		return fmt.Errorf("verify checksum: %w", err)
	}

	return installBinaryFromTarGz(bytes.NewReader(archiveData), target)
}

func releaseArchiveURL(version, goos, goarch string, releaseBaseURL string) string {
	versionBare := strings.TrimPrefix(version, "v")
	archive := releaseArchiveName(versionBare, goos, goarch)
	baseURL := strings.TrimRight(releaseBaseURL, "/")
	if baseURL == "" {
		baseURL = brand.ReleaseBaseURL
		if baseURL == "" {
			baseURL = "https://github.com/liza-mas/liza/releases/download"
		}
	}
	return fmt.Sprintf("%s/%s/%s", baseURL, version, archive)
}

func releaseArchiveName(versionBare, goos, goarch string) string {
	prefix := brand.ArchivePrefix
	if prefix == "" {
		prefix = brand.BinaryName
	}
	if prefix == "" {
		prefix = "liza"
	}
	return fmt.Sprintf("%s-%s-%s-%s.tar.gz", prefix, versionBare, goos, goarch)
}

func checksumURL(version string) string {
	return checksumURLWithBase(version, "")
}

func checksumURLWithBase(version, checksumBaseURL string) string {
	// Fetch checksums from the configured checksum base, independent from any
	// archive mirror override.
	baseURL := strings.TrimRight(checksumBaseURL, "/")
	if baseURL == "" {
		baseURL = brand.ChecksumBaseURL
		if baseURL == "" {
			baseURL = "https://github.com/liza-mas/liza/releases/download"
		}
	}
	return fmt.Sprintf("%s/%s/checksums.txt", baseURL, version)
}

func downloadChecksums(ctx context.Context, url string) (map[string]string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("create checksum request: %w", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("download checksums: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("download checksums: HTTP %s", resp.Status)
	}

	checksums := make(map[string]string)
	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		// GoReleaser format: "<sha256>  <filename>" or "<sha256> *<filename>"
		parts := strings.Fields(line)
		if len(parts) < 2 {
			continue
		}
		checksum := parts[0]
		filename := strings.TrimPrefix(parts[1], "*")
		checksums[filename] = checksum
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read checksums: %w", err)
	}
	return checksums, nil
}

func verifyChecksum(data []byte, expectedChecksum string) error {
	hash := sha256.Sum256(data)
	actual := hex.EncodeToString(hash[:])
	if actual != expectedChecksum {
		return fmt.Errorf("checksum mismatch: got %s, want %s", actual, expectedChecksum)
	}
	return nil
}

func installBinaryFromTarGz(r io.Reader, target string) error {
	gz, err := gzip.NewReader(r)
	if err != nil {
		return fmt.Errorf("read release gzip: %w", err)
	}
	defer gz.Close()

	tr := tar.NewReader(gz)
	for {
		header, err := tr.Next()
		if err == io.EOF {
			return fmt.Errorf("release archive does not contain %s binary", expectedArchiveBinaryName())
		}
		if err != nil {
			return fmt.Errorf("read release tar: %w", err)
		}

		// Only accept regular files named exactly like the branded binary.
		if filepath.Base(header.Name) != expectedArchiveBinaryName() {
			continue
		}

		// Reject dangerous entry types
		switch header.Typeflag {
		case tar.TypeReg:
			// Regular file is allowed
		case '\x00':
			// Legacy regular file marker is allowed
		default:
			// Reject symlinks, hardlinks, directories, devices, etc.
			return fmt.Errorf("reject dangerous tar entry type %d for %s", header.Typeflag, header.Name)
		}

		// Reject path traversal attempts
		if strings.Contains(header.Name, "..") {
			return fmt.Errorf("reject path traversal in tar entry: %s", header.Name)
		}

		// Strip dangerous permission bits (setuid, setgid, sticky)
		mode := header.FileInfo().Mode()
		mode &^= 0o7000 // Clear setuid (0o4000), setgid (0o2000), sticky (0o1000)

		return replaceBinary(target, tr, mode)
	}
}

func expectedArchiveBinaryName() string {
	if brand.BinaryName != "" {
		return brand.BinaryName
	}
	return "liza"
}

func replaceBinary(target string, r io.Reader, mode os.FileMode) error {
	dir := filepath.Dir(target)
	tmp, err := os.CreateTemp(dir, ".liza-update-*")
	if err != nil {
		return fmt.Errorf("create temporary binary: %w", err)
	}
	tmpPath := tmp.Name()
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.Remove(tmpPath)
		}
	}()

	if _, err := io.Copy(tmp, r); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write temporary binary: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close temporary binary: %w", err)
	}
	if mode == 0 {
		mode = 0o755
	}
	if err := os.Chmod(tmpPath, mode|0o111); err != nil {
		return fmt.Errorf("chmod temporary binary: %w", err)
	}
	if err := os.Rename(tmpPath, target); err != nil {
		return fmt.Errorf("replace %s: %w", target, err)
	}
	cleanup = false
	return nil
}

type installRollback struct {
	backupPath string
}

func prepareInstallRollback(target string) (installRollback, error) {
	info, err := os.Stat(target)
	if err != nil {
		if os.IsNotExist(err) {
			return installRollback{}, nil
		}
		return installRollback{}, fmt.Errorf("stat current binary: %w", err)
	}
	if info.IsDir() {
		return installRollback{}, fmt.Errorf("current binary target is a directory: %s", target)
	}

	src, err := os.Open(target)
	if err != nil {
		return installRollback{}, fmt.Errorf("open current binary: %w", err)
	}
	defer src.Close()

	tmp, err := os.CreateTemp(filepath.Dir(target), ".liza-update-backup-*")
	if err != nil {
		return installRollback{}, fmt.Errorf("create backup binary: %w", err)
	}
	backupPath := tmp.Name()
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.Remove(backupPath)
		}
	}()

	if _, err := io.Copy(tmp, src); err != nil {
		_ = tmp.Close()
		return installRollback{}, fmt.Errorf("copy current binary to backup: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return installRollback{}, fmt.Errorf("close backup binary: %w", err)
	}
	if err := os.Chmod(backupPath, info.Mode().Perm()); err != nil {
		return installRollback{}, fmt.Errorf("chmod backup binary: %w", err)
	}

	cleanup = false
	return installRollback{backupPath: backupPath}, nil
}

func (r *installRollback) restore(target string) error {
	if r.backupPath == "" {
		return nil
	}
	if err := os.Rename(r.backupPath, target); err != nil {
		return fmt.Errorf("restore %s from backup: %w", target, err)
	}
	r.backupPath = ""
	return nil
}

func (r *installRollback) cleanup() {
	if r.backupPath == "" {
		return
	}
	_ = os.Remove(r.backupPath)
	r.backupPath = ""
}

func installFromSource(ctx context.Context, commit, target string, stderr io.Writer) error {
	tmpDir, err := os.MkdirTemp("", binaryName()+"-source-update-*")
	if err != nil {
		return fmt.Errorf("create source checkout: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	repoDir := filepath.Join(tmpDir, sourceCheckoutDirName())
	// Clone with depth 1 for main branch
	if err := runStreaming(ctx, stderr, "git", "clone", "--depth", "1", "--branch", "main", sourceCloneURL(), repoDir); err != nil {
		return fmt.Errorf("clone %s source: %w", nameLower(), err)
	}
	// Fetch the exact commit we need (may not be in the shallow clone)
	fetchErr := runStreaming(ctx, stderr, "git", "-C", repoDir, "fetch", "--depth", "1", "origin", commit)
	if fetchErr != nil {
		// Fallback: try a deeper fetch if shallow exact fetch failed
		fmt.Fprintf(stderr, "Shallow fetch failed for commit %s, attempting deeper fetch...\n", commit)
		if err := runStreaming(ctx, stderr, "git", "-C", repoDir, "fetch", "origin", commit); err != nil {
			return fmt.Errorf("fetch commit %s (shallow and deep fetch both failed): %w (original shallow error: %v)", commit, err, fetchErr)
		}
	}
	// Checkout the fetched commit
	if err := runStreaming(ctx, stderr, "git", "-C", repoDir, "checkout", "--detach", "FETCH_HEAD"); err != nil {
		return fmt.Errorf("checkout commit %s: %w", commit, err)
	}
	installDir := filepath.Dir(target)
	makeArgs := sourceInstallMakeArgs(repoDir, installDir)
	if err := runStreaming(ctx, stderr, "make", makeArgs...); err != nil {
		return fmt.Errorf("build %s source: %w", nameLower(), err)
	}
	return nil
}

func SyscallExec(path string, args []string, env []string) error {
	return syscall.Exec(path, args, env)
}

// VerifyInstall runs a post-install verification by executing the installed binary
// with a version command to ensure it's functional.
func VerifyInstall(ctx context.Context, target string, stderr io.Writer) error {
	cmd := exec.CommandContext(ctx, target, "version")
	cmd.Stdout = io.Discard
	cmd.Stderr = stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("version check failed: %w", err)
	}
	return nil
}

func parseLatestVersion(data []byte) (string, error) {
	var info moduleInfo
	if err := json.Unmarshal(data, &info); err != nil {
		return "", fmt.Errorf("parse go list output: %w", err)
	}
	version := normalizeVersion(info.Version)
	if !semver.IsValid(version) {
		return "", fmt.Errorf("latest module version is not semantic: %q", info.Version)
	}
	return version, nil
}

func parseLatestReleaseVersion(data []byte) (string, error) {
	var info releaseInfo
	if err := json.Unmarshal(data, &info); err != nil {
		return "", fmt.Errorf("parse latest release response: %w", err)
	}
	version := normalizeVersion(info.TagName)
	if !semver.IsValid(version) {
		return "", fmt.Errorf("latest release tag is not semantic: %q", info.TagName)
	}
	return version, nil
}

func parseMainCommit(data []byte) (string, error) {
	var commit commitInfo
	if err := json.Unmarshal(data, &commit); err == nil && commit.SHA != "" {
		sha := normalizeCommit(commit.SHA)
		if sha == "" {
			return "", fmt.Errorf("main commit response did not include sha")
		}
		return sha, nil
	}

	var info moduleInfo
	if err := json.Unmarshal(data, &info); err != nil {
		return "", fmt.Errorf("parse main commit response: %w", err)
	}
	sha := normalizeCommit(info.Origin.Hash)
	if sha == "" {
		return "", fmt.Errorf("main module response did not include origin hash")
	}
	return sha, nil
}

func isNewerVersion(current, latest string) bool {
	current = normalizeVersion(current)
	latest = normalizeVersion(latest)
	if !semver.IsValid(current) || !semver.IsValid(latest) {
		return false
	}
	return semver.Compare(latest, current) > 0
}

func normalizeVersion(version string) string {
	version = strings.TrimSpace(version)
	if version == "" || version == "dev" || version == "unknown" {
		return ""
	}
	if !strings.HasPrefix(version, "v") {
		version = "v" + version
	}
	return version
}

func normalizeCommit(commit string) string {
	commit = strings.ToLower(strings.TrimSpace(commit))
	if commit == "" || commit == "unknown" {
		return ""
	}
	return commit
}

func sameCommit(current, latest string) bool {
	return strings.HasPrefix(latest, current) || strings.HasPrefix(current, latest)
}

func shortCommit(commit string) string {
	if len(commit) <= 12 {
		return commit
	}
	return commit[:12]
}

func confirmUpdate(stdin *bufio.Reader, stdout io.Writer, next candidate) bool {
	fmt.Fprintf(stdout, "%s %s update is available (%s -> %s). Update now and rerun this command? [y/N] ", nameTitle(), next.Channel, next.Current, next.Latest)
	line, err := stdin.ReadString('\n')
	if err != nil && len(line) == 0 {
		fmt.Fprintln(stdout)
		return false
	}
	answer := strings.ToLower(strings.TrimSpace(line))
	return answer == "y" || answer == "yes"
}

func shouldSkip(cfg Config) bool {
	if envValueForSuffix(cfg, skipEnvSuffix) != "" {
		return true
	}
	if enabled, ok := checkUpdateFlag(cfg.Args); ok {
		return !enabled || !cfg.IsInteractive()
	}
	if cfg.CheckUpdate {
		return !cfg.IsInteractive()
	}
	if cfg.CheckDisabled != nil && cfg.CheckDisabled() {
		return true
	}
	if prefs := readUpdatePreferences(); prefs.CheckUpdate != nil {
		return !*prefs.CheckUpdate || !cfg.IsInteractive()
	}
	if !checkUpdateEnvEnabled(cfg) {
		return true
	}
	if !cfg.IsInteractive() {
		return true
	}
	return false
}

func checkUpdateFlag(args []string) (bool, bool) {
	flags, err := parseUpdateFlags(args)
	if err != nil {
		return false, false
	}
	if flags.checkUpdate != nil {
		return *flags.checkUpdate, true
	}
	return false, false
}

func checkUpdateEnvEnabled(cfg Config) bool {
	switch strings.ToLower(strings.TrimSpace(envValueForSuffix(cfg, checkEnvSuffix))) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}

func proposeDisableCheck(stdin *bufio.Reader, stdout io.Writer, disable func() error) {
	fmt.Fprint(stdout, "Update skipped. Disable update checks for future runs? [y/N] ")
	line, err := stdin.ReadString('\n')
	if err != nil && len(line) == 0 {
		fmt.Fprintln(stdout)
		return
	}
	answer := strings.ToLower(strings.TrimSpace(line))
	if answer == "y" || answer == "yes" {
		if err := disable(); err != nil {
			fmt.Fprintf(stdout, "Could not disable update checks: %v\n", err)
			return
		}
		fmt.Fprintln(stdout, "Update checks disabled.")
	}
}

type preferences struct {
	CheckUpdate *bool  `json:"check_update,omitempty"`
	Channel     string `json:"channel,omitempty"`
}

type parsedUpdateFlags struct {
	checkUpdate  *bool
	channel      string
	settingsOnly bool
}

func updatePrefsPath() (string, error) {
	dir, err := paths.GlobalLizaDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, updatePrefs), nil
}

func UpdateChecksDisabled() bool {
	prefs := readUpdatePreferences()
	return prefs.CheckUpdate != nil && !*prefs.CheckUpdate
}

func readUpdatePreferences() preferences {
	path, err := updatePrefsPath()
	if err != nil {
		return preferences{}
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return preferences{}
	}
	var prefs preferences
	if err := json.Unmarshal(data, &prefs); err != nil {
		return preferences{}
	}
	prefs.Channel = strings.ToLower(strings.TrimSpace(prefs.Channel))
	return prefs
}

func DisableUpdateChecks() error {
	prefs := readUpdatePreferences()
	disabled := false
	prefs.CheckUpdate = &disabled
	return writeUpdatePreferences(prefs)
}

func persistExplicitUpdatePreferences(cfg Config) error {
	var changed bool
	prefs := readUpdatePreferences()

	if enabled, ok := checkUpdateFlag(cfg.Args); ok {
		prefs.CheckUpdate = &enabled
		changed = true
	}
	if channel, ok := updateChannelFlag(cfg.Args); ok {
		if err := validateChannel(channel); err != nil {
			return err
		}
		prefs.Channel = channel
		changed = true
	}
	if !changed {
		return nil
	}
	if err := writeUpdatePreferences(prefs); err != nil {
		return &FatalError{fmt.Errorf("write update preferences: %w", err)}
	}
	return nil
}

func validateExplicitUpdateFlags(args []string) error {
	_, err := parseUpdateFlags(args)
	return err
}

func UpdateSettingsOnly(args []string) bool {
	flags, err := parseUpdateFlags(args)
	return err == nil && flags.settingsOnly
}

func parseUpdateFlags(args []string) (parsedUpdateFlags, error) {
	var flags parsedUpdateFlags
	changed := false
	settingsOnly := true
	for i := 1; i < len(args); i++ {
		arg := args[i]
		switch {
		case arg == "--":
			flags.settingsOnly = changed && settingsOnly && i == len(args)-1
			return flags, nil
		case arg == "--check-update":
			flags.checkUpdate = boolPtr(true)
		case strings.HasPrefix(arg, "--check-update="):
			value := strings.TrimPrefix(arg, "--check-update=")
			if value != "true" && value != "false" {
				return flags, &FatalError{fmt.Errorf("invalid --check-update value %q (valid values: true, false)", value)}
			}
			flags.checkUpdate = boolPtr(value == "true")
		case arg == "--update-channel":
			if i+1 >= len(args) || args[i+1] == "--" {
				return flags, &FatalError{fmt.Errorf("--update-channel requires a value (valid values: stable, main)")}
			}
			channel := strings.ToLower(strings.TrimSpace(args[i+1]))
			flags.channel = channel
			if err := validateChannel(channel); err != nil {
				return flags, err
			}
			i++
		case strings.HasPrefix(arg, "--update-channel="):
			channel := strings.ToLower(strings.TrimSpace(strings.TrimPrefix(arg, "--update-channel=")))
			flags.channel = channel
			if err := validateChannel(channel); err != nil {
				return flags, err
			}
		default:
			settingsOnly = false
			continue
		}
		changed = true
	}
	flags.settingsOnly = changed && settingsOnly
	return flags, nil
}

func boolPtr(value bool) *bool {
	return &value
}

func SavedUpdateSettingsSummary() string {
	prefs := readUpdatePreferences()
	checkUpdate := "unset"
	if prefs.CheckUpdate != nil {
		checkUpdate = fmt.Sprintf("%t", *prefs.CheckUpdate)
	}
	channel := prefs.Channel
	if channel == "" {
		channel = channelStable
	}
	return fmt.Sprintf("Update settings saved: check_update=%s, channel=%s\n", checkUpdate, channel)
}

func writeUpdatePreferences(prefs preferences) error {
	path, err := updatePrefsPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create global %s config dir: %w", brand.NameTitle, err)
	}
	prefs.Channel = strings.ToLower(strings.TrimSpace(prefs.Channel))
	data, err := json.MarshalIndent(prefs, "", "  ")
	if err != nil {
		return fmt.Errorf("encode update preferences: %w", err)
	}
	data = append(data, '\n')
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("write update preferences: %w", err)
	}
	return nil
}

func validateChannel(channel string) error {
	channel = strings.ToLower(strings.TrimSpace(channel))
	if channel != channelStable && channel != channelMain {
		return &FatalError{fmt.Errorf("invalid update channel %q (valid values: stable, main)", channel)}
	}
	return nil
}

func updateChannel(cfg Config) string {
	if channel, ok := updateChannelFlag(cfg.Args); ok {
		return channel
	}
	if cfg.Channel != "" {
		return strings.ToLower(strings.TrimSpace(cfg.Channel))
	}
	if envChannel := envValueForSuffix(cfg, channelEnvSuffix); envChannel != "" {
		return strings.ToLower(strings.TrimSpace(envChannel))
	}
	if prefs := readUpdatePreferences(); prefs.Channel != "" {
		return prefs.Channel
	}
	return channelStable
}

func updateChannelFlag(args []string) (string, bool) {
	flags, err := parseUpdateFlags(args)
	if err != nil {
		return "", false
	}
	if flags.channel != "" {
		return flags.channel, true
	}
	return "", false
}

func reexecArgs(args []string) []string {
	if len(args) == 0 {
		return []string{binaryName()}
	}
	out := make([]string, 0, len(args))
	out = append(out, binaryName())
	out = append(out, args[1:]...)
	return out
}

func withDefaults(cfg Config) Config {
	if cfg.Args == nil {
		cfg.Args = os.Args
	}
	if cfg.Env == nil {
		cfg.Env = os.Environ()
	}
	if cfg.Stdin == nil {
		cfg.Stdin = os.Stdin
	}
	if cfg.Stdout == nil {
		cfg.Stdout = os.Stdout
	}
	if cfg.Stderr == nil {
		cfg.Stderr = os.Stderr
	}
	if cfg.IsInteractive == nil {
		cfg.IsInteractive = func() bool { return false }
	}
	if cfg.InstallTimeout == 0 {
		cfg.InstallTimeout = 10 * time.Minute
	}
	if cfg.LookupLatest == nil {
		cfg.LookupLatest = LatestModuleVersion
	}
	if cfg.LookupMain == nil {
		cfg.LookupMain = MainCommit
	}
	if cfg.IsMainAhead == nil {
		cfg.IsMainAhead = MainCommitAhead
	}
	if cfg.Install == nil {
		// Use the configured ReleaseBaseURL for test injection; empty string uses the brand default.
		baseURL := cfg.ReleaseBaseURL
		cfg.Install = func(ctx context.Context, next candidate, target string, stdout, stderr io.Writer) error {
			return InstallWithBaseURL(ctx, next, target, stdout, stderr, baseURL)
		}
	}
	if cfg.InstallTarget == nil {
		cfg.InstallTarget = InstallTarget
	}
	if cfg.CheckDisabled == nil {
		cfg.CheckDisabled = UpdateChecksDisabled
	}
	if cfg.DisableCheck == nil {
		cfg.DisableCheck = DisableUpdateChecks
	}
	if cfg.Reexec == nil {
		cfg.Reexec = SyscallExec
	}
	if cfg.VerifyInstall == nil {
		cfg.VerifyInstall = VerifyInstall
	}
	return cfg
}

var runStreaming = func(ctx context.Context, stderr io.Writer, name string, args ...string) error {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Stdout = stderr
	cmd.Stderr = stderr
	return cmd.Run()
}

func setEnv(env []string, key, value string) []string {
	prefix := key + "="
	out := make([]string, 0, len(env)+1)
	replaced := false
	for _, item := range env {
		if strings.HasPrefix(item, prefix) {
			out = append(out, prefix+value)
			replaced = true
			continue
		}
		out = append(out, item)
	}
	if !replaced {
		out = append(out, prefix+value)
	}
	return out
}

func envValueForSuffix(cfg Config, suffix string) string {
	lookup := brand.RuntimeValues().LookupEnv(func(key string) string {
		return envValue(cfg.Env, key)
	}, suffix)
	if lookup.Warning != "" && cfg.Stderr != nil {
		fmt.Fprintf(cfg.Stderr, "Warning: %s\n", lookup.Warning)
	}
	return lookup.Value
}

func envValue(env []string, key string) string {
	prefix := key + "="
	for _, item := range env {
		if strings.HasPrefix(item, prefix) {
			return strings.TrimPrefix(item, prefix)
		}
	}
	return ""
}

func cliPrefix() string {
	return nameLower()
}

func binaryName() string {
	if brand.BinaryName != "" {
		return brand.BinaryName
	}
	return "liza"
}

func nameLower() string {
	if brand.NameLower != "" {
		return brand.NameLower
	}
	return "liza"
}

func nameTitle() string {
	if brand.NameTitle != "" {
		return brand.NameTitle
	}
	return "Liza"
}

func releaseRepo() string {
	if brand.ReleaseRepo != "" {
		return brand.ReleaseRepo
	}
	return canonicalRepo
}

func sourceRepo() string {
	if brand.Repo != "" {
		return brand.Repo
	}
	return canonicalRepo
}

func githubLatestReleaseURL() string {
	return fmt.Sprintf("https://api.github.com/repos/%s/releases/latest", releaseRepo())
}

func githubMainCommitURL() string {
	return fmt.Sprintf("https://api.github.com/repos/%s/commits/main", sourceRepo())
}

func githubCompareURL(current, latest string) string {
	return fmt.Sprintf("https://api.github.com/repos/%s/compare/%s...%s", sourceRepo(), current, latest)
}

func sourceCloneURL() string {
	return fmt.Sprintf("https://github.com/%s.git", sourceRepo())
}

func sourceCheckoutDirName() string {
	name := nameLower()
	if name == "" {
		return "liza"
	}
	return name
}

func sourceInstallMakeArgs(repoDir, installDir string) []string {
	values := brand.RuntimeValues()
	return []string{
		"-C", repoDir,
		"install",
		"INSTALL_DIR=" + installDir,
		"BRAND_NAME_TITLE=" + values.NameTitle,
		"BRAND_NAME_LOWER=" + values.NameLower,
		"BRAND_NAME_UPPER=" + values.NameUpper,
		"BRAND_REPO=" + values.Repo,
		"BRAND_BINARY_NAME=" + values.BinaryName,
		"BRAND_GLOBAL_DIRNAME=" + values.GlobalDirName,
		"BRAND_PROJECT_DIRNAME=" + values.ProjectDirName,
		"BRAND_ENV_PREFIX=" + values.EnvPrefix,
		"BRAND_ARCHIVE_PREFIX=" + values.ArchivePrefix,
		"BRAND_RELEASE_REPO=" + values.ReleaseRepo,
		"BRAND_RELEASE_BASE_URL=" + values.ReleaseBaseURL,
		"BRAND_CHECKSUM_BASE_URL=" + values.ChecksumBaseURL,
	}
}
