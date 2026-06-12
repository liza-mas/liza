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

	"github.com/liza-mas/liza/internal/paths"
	"golang.org/x/mod/semver"
)

const (
	modulePath = "github.com/liza-mas/liza"

	channelStable = "stable"
	channelMain   = "main"

	channelEnvName = "LIZA_UPDATE_CHANNEL"
	checkEnvName   = "LIZA_CHECK_UPDATE"
	skipEnvName    = "LIZA_SKIP_AUTO_UPDATE"
	updatePrefs    = "update.json"
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
	Install        func(context.Context, candidate, string, io.Writer, io.Writer) error
	InstallTarget  func() (string, error)
	CheckDisabled  func() bool
	DisableCheck   func() error
	Reexec         func(string, []string, []string) error
	// ReleaseBaseURL allows test override of the release archive base URL.
	// In production, this is always the canonical GitHub releases URL.
	// The checksum URL is always fetched from the canonical GitHub releases URL
	// to maintain trust chain integrity.
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

type candidate struct {
	Channel string
	Current string
	Latest  string
	Ref     string
}

func MaybeUpdateAndReexec(ctx context.Context, cfg Config) error {
	cfg = withDefaults(cfg)
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
		fmt.Fprintf(cfg.Stderr, "liza: update check failed: %v\n", err)
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
		fmt.Fprintf(cfg.Stderr, "liza: find install target failed: %v\n", err)
		return nil
	}
	fmt.Fprintln(cfg.Stderr, installPlan(next, target))

	// Use a separate bounded context for install operations
	installCtx, cancel := context.WithTimeout(ctx, cfg.InstallTimeout)
	defer cancel()

	rollback, err := prepareInstallRollback(target)
	if err != nil {
		fmt.Fprintf(cfg.Stderr, "liza: prepare update rollback failed: %v\n", err)
		return nil
	}
	defer rollback.cleanup()

	if err := cfg.Install(installCtx, next, target, cfg.Stderr, cfg.Stderr); err != nil {
		if restoreErr := rollback.restore(target); restoreErr != nil {
			fmt.Fprintf(cfg.Stderr, "liza: restore previous install failed: %v\n", restoreErr)
		}
		fmt.Fprintf(cfg.Stderr, "liza: update install failed: %v\n", err)
		return nil
	}

	// Post-install verification if configured
	if cfg.VerifyInstall != nil {
		verifyCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
		defer cancel()
		if err := cfg.VerifyInstall(verifyCtx, target, cfg.Stderr); err != nil {
			fmt.Fprintf(cfg.Stderr, "liza: post-install verification failed: %v\n", err)
			if restoreErr := rollback.restore(target); restoreErr != nil {
				fmt.Fprintf(cfg.Stderr, "liza: restore previous install failed: %v\n", restoreErr)
			}
			fmt.Fprintf(cfg.Stderr, "liza: falling through to original command without reexec\n")
			return nil
		}
	}

	args := reexecArgs(cfg.Args)
	env := setEnv(cfg.Env, skipEnvName, "1")
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
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://api.github.com/repos/liza-mas/liza/releases/latest", nil)
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
	out, err := runOutput(ctx, "go", "list", "-m", "-json", modulePath+"@main")
	if err != nil {
		return "", err
	}
	return parseMainCommit(out)
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
		if found, lookErr := exec.LookPath("liza"); lookErr == nil {
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
	archiveName := fmt.Sprintf("liza-%s-%s-%s.tar.gz", versionBare, runtime.GOOS, runtime.GOARCH)
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
	archive := fmt.Sprintf("liza-%s-%s-%s.tar.gz", versionBare, goos, goarch)
	baseURL := strings.TrimRight(releaseBaseURL, "/")
	if baseURL == "" {
		baseURL = "https://github.com/liza-mas/liza/releases/download"
	}
	return fmt.Sprintf("%s/%s/%s", baseURL, version, archive)
}

func checksumURL(version string) string {
	return checksumURLWithBase(version, "")
}

func checksumURLWithBase(version, checksumBaseURL string) string {
	// Always fetch checksums from the canonical GitHub releases URL
	// to maintain trust chain integrity, regardless of any archive mirror.
	baseURL := strings.TrimRight(checksumBaseURL, "/")
	if baseURL == "" {
		baseURL = "https://github.com/liza-mas/liza/releases/download"
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
			return fmt.Errorf("release archive does not contain liza binary")
		}
		if err != nil {
			return fmt.Errorf("read release tar: %w", err)
		}

		// Only accept regular files named exactly "liza" by basename
		if filepath.Base(header.Name) != "liza" {
			continue
		}

		// Reject dangerous entry types
		switch header.Typeflag {
		case tar.TypeReg:
			// Regular file is allowed
		case tar.TypeRegA:
			// Regular file (extended) is allowed
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
	tmpDir, err := os.MkdirTemp("", "liza-source-update-*")
	if err != nil {
		return fmt.Errorf("create source checkout: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	repoDir := filepath.Join(tmpDir, "liza")
	// Clone with depth 1 for main branch
	if err := runStreaming(ctx, stderr, "git", "clone", "--depth", "1", "--branch", "main", "https://github.com/liza-mas/liza.git", repoDir); err != nil {
		return fmt.Errorf("clone liza source: %w", err)
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
	if err := runStreaming(ctx, stderr, "make", "-C", repoDir, "install", "INSTALL_DIR="+installDir); err != nil {
		return fmt.Errorf("build liza source: %w", err)
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
	var info moduleInfo
	if err := json.Unmarshal(data, &info); err != nil {
		return "", fmt.Errorf("parse go list output: %w", err)
	}
	commit := normalizeCommit(info.Origin.Hash)
	if commit == "" {
		return "", fmt.Errorf("main module response did not include origin hash")
	}
	return commit, nil
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
	fmt.Fprintf(stdout, "Liza %s update is available (%s -> %s). Update now and rerun this command? [y/N] ", next.Channel, next.Current, next.Latest)
	line, err := stdin.ReadString('\n')
	if err != nil && len(line) == 0 {
		fmt.Fprintln(stdout)
		return false
	}
	answer := strings.ToLower(strings.TrimSpace(line))
	return answer == "y" || answer == "yes"
}

func shouldSkip(cfg Config) bool {
	if envValue(cfg.Env, skipEnvName) != "" {
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
	// Stop parsing at --
	preDashArgs := args
	for i, arg := range args {
		if arg == "--" {
			preDashArgs = args[:i]
			break
		}
	}

	var enabled *bool
	for i := 1; i < len(preDashArgs); i++ {
		arg := preDashArgs[i]
		if strings.HasPrefix(arg, "--check-update=") {
			value := strings.TrimPrefix(arg, "--check-update=")
			if value == "true" {
				enabled = &[]bool{true}[0]
			} else if value == "false" {
				enabled = &[]bool{false}[0]
			}
		} else if arg == "--check-update" {
			// --check-update without = is treated as true
			enabled = &[]bool{true}[0]
		}
	}
	if enabled != nil {
		return *enabled, true
	}
	return false, false
}

func checkUpdateEnvEnabled(cfg Config) bool {
	switch strings.ToLower(strings.TrimSpace(envValue(cfg.Env, checkEnvName))) {
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

func UpdateSettingsOnly(args []string) bool {
	changed := false
	for i := 1; i < len(args); i++ {
		arg := args[i]
		switch {
		case arg == "--":
			return changed && i == len(args)-1
		case arg == "--check-update" || strings.HasPrefix(arg, "--check-update="):
			changed = true
		case arg == "--update-channel":
			changed = true
			i++
		case strings.HasPrefix(arg, "--update-channel="):
			changed = true
		default:
			return false
		}
	}
	return changed
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
		return fmt.Errorf("create global liza config dir: %w", err)
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
	if envChannel := envValue(cfg.Env, channelEnvName); envChannel != "" {
		return strings.ToLower(strings.TrimSpace(envChannel))
	}
	if prefs := readUpdatePreferences(); prefs.Channel != "" {
		return prefs.Channel
	}
	return channelStable
}

func updateChannelFlag(args []string) (string, bool) {
	// Stop parsing at --
	preDashArgs := args
	for i, arg := range args {
		if arg == "--" {
			preDashArgs = args[:i]
			break
		}
	}

	var channel string
	for i := 1; i < len(preDashArgs); i++ {
		arg := preDashArgs[i]
		if strings.HasPrefix(arg, "--update-channel=") {
			channel = strings.ToLower(strings.TrimSpace(strings.TrimPrefix(arg, "--update-channel=")))
		} else if arg == "--update-channel" && i+1 < len(preDashArgs) {
			channel = strings.ToLower(strings.TrimSpace(preDashArgs[i+1]))
		}
	}
	if channel != "" {
		return channel, true
	}
	return "", false
}

func reexecArgs(args []string) []string {
	if len(args) == 0 {
		return []string{"liza"}
	}
	out := make([]string, 0, len(args))
	out = append(out, "liza")
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
	if cfg.Install == nil {
		// Use the configured ReleaseBaseURL for test injection, empty string uses canonical GitHub
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

func runOutput(ctx context.Context, name string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg != "" {
			return nil, fmt.Errorf("%s %s: %w: %s", name, strings.Join(args, " "), err, msg)
		}
		return nil, fmt.Errorf("%s %s: %w", name, strings.Join(args, " "), err)
	}
	return out, nil
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

func envValue(env []string, key string) string {
	prefix := key + "="
	for _, item := range env {
		if strings.HasPrefix(item, prefix) {
			return strings.TrimPrefix(item, prefix)
		}
	}
	return ""
}
