package updater

import (
	"archive/tar"
	"bufio"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
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
	modulePath  = "github.com/liza-mas/liza"
	commandPath = modulePath + "/cmd/liza"

	channelStable = "stable"
	channelMain   = "main"

	channelEnvName = "LIZA_UPDATE_CHANNEL"
	checkEnvName   = "LIZA_CHECK_UPDATE"
	skipEnvName    = "LIZA_SKIP_AUTO_UPDATE"
	updatePrefs    = "update.json"
)

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
	LookupLatest   func(context.Context) (string, error)
	LookupMain     func(context.Context) (string, error)
	Install        func(context.Context, candidate, string, io.Writer, io.Writer) error
	InstallTarget  func() (string, error)
	CheckDisabled  func() bool
	DisableCheck   func() error
	Reexec         func(string, []string, []string) error
}

type moduleInfo struct {
	Version string `json:"Version"`
	Origin  struct {
		Hash string `json:"Hash"`
	} `json:"Origin"`
}

type candidate struct {
	Channel string
	Current string
	Latest  string
	Ref     string
}

func MaybeUpdateAndReexec(ctx context.Context, cfg Config) error {
	cfg = withDefaults(cfg)
	if shouldSkip(cfg) {
		return nil
	}

	lookupCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	next, err := lookupCandidate(lookupCtx, cfg)
	if err != nil {
		fmt.Fprintf(cfg.Stderr, "liza: update check failed: %v\n", err)
		return nil
	}
	if next.Ref == "" {
		return nil
	}

	input := bufio.NewReader(cfg.Stdin)
	if !confirmUpdate(input, cfg.Stdout, next) {
		proposeDisableCheck(input, cfg.Stdout, cfg.DisableCheck)
		return nil
	}

	target, err := cfg.InstallTarget()
	if err != nil {
		return fmt.Errorf("find liza install target: %w", err)
	}
	fmt.Fprintln(cfg.Stdout, installPlan(next, target))
	if err := cfg.Install(ctx, next, target, cfg.Stdout, cfg.Stderr); err != nil {
		return fmt.Errorf("update install: %w", err)
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
		return candidate{}, fmt.Errorf("invalid update channel %q (want stable or main)", updateChannel(cfg))
	}
}

func LatestModuleVersion(ctx context.Context) (string, error) {
	out, err := runOutput(ctx, "go", "list", "-m", "-json", modulePath+"@latest")
	if err != nil {
		return "", err
	}
	return parseLatestVersion(out)
}

func MainCommit(ctx context.Context) (string, error) {
	out, err := runOutput(ctx, "go", "list", "-m", "-json", modulePath+"@main")
	if err != nil {
		return "", err
	}
	return parseMainCommit(out)
}

func Install(ctx context.Context, next candidate, target string, stdout, stderr io.Writer) error {
	switch next.Channel {
	case channelStable:
		return installReleaseBinary(ctx, next.Ref, target, stdout)
	case channelMain:
		return installFromSource(ctx, next.Ref, target, stdout, stderr)
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

func installReleaseBinary(ctx context.Context, version, target string, stdout io.Writer) error {
	url := releaseArchiveURL(version, runtime.GOOS, runtime.GOARCH)
	fmt.Fprintf(stdout, "Downloading %s\n", url)

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
	return installBinaryFromTarGz(resp.Body, target)
}

func releaseArchiveURL(version, goos, goarch string) string {
	versionBare := strings.TrimPrefix(version, "v")
	archive := fmt.Sprintf("liza-%s-%s-%s.tar.gz", versionBare, goos, goarch)
	baseURL := strings.TrimRight(os.Getenv("LIZA_RELEASE_BASE_URL"), "/")
	if baseURL == "" {
		baseURL = "https://github.com/liza-mas/liza/releases/download"
	}
	return fmt.Sprintf("%s/%s/%s", baseURL, version, archive)
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
		if header.Typeflag != tar.TypeReg || filepath.Base(header.Name) != "liza" {
			continue
		}
		return replaceBinary(target, tr, header.FileInfo().Mode())
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

func installFromSource(ctx context.Context, commit, target string, stdout, stderr io.Writer) error {
	tmpDir, err := os.MkdirTemp("", "liza-source-update-*")
	if err != nil {
		return fmt.Errorf("create source checkout: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	repoDir := filepath.Join(tmpDir, "liza")
	if err := runStreaming(ctx, stdout, stderr, "git", "clone", "--depth", "1", "--branch", "main", "https://github.com/liza-mas/liza.git", repoDir); err != nil {
		return fmt.Errorf("clone liza source: %w", err)
	}
	if err := runStreaming(ctx, stdout, stderr, "git", "-C", repoDir, "checkout", commit); err != nil {
		return fmt.Errorf("checkout liza source %s: %w", commit, err)
	}
	installDir := filepath.Dir(target)
	if err := runStreaming(ctx, stdout, stderr, "make", "-C", repoDir, "install", "INSTALL_DIR="+installDir); err != nil {
		return fmt.Errorf("build liza source: %w", err)
	}
	return nil
}

func SyscallExec(path string, args []string, env []string) error {
	return syscall.Exec(path, args, env)
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
	if !checkUpdateEnvEnabled(cfg) {
		return true
	}
	if !cfg.IsInteractive() {
		return true
	}
	return false
}

func checkUpdateFlag(args []string) (bool, bool) {
	for _, arg := range args[1:] {
		if arg == "--check-update" || arg == "--check-update=true" {
			return true, true
		}
		if arg == "--check-update=false" {
			return false, true
		}
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
	CheckUpdate *bool `json:"check_update,omitempty"`
}

func updatePrefsPath() (string, error) {
	dir, err := paths.GlobalLizaDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, updatePrefs), nil
}

func UpdateChecksDisabled() bool {
	path, err := updatePrefsPath()
	if err != nil {
		return false
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	var prefs preferences
	if err := json.Unmarshal(data, &prefs); err != nil {
		return false
	}
	return prefs.CheckUpdate != nil && !*prefs.CheckUpdate
}

func DisableUpdateChecks() error {
	path, err := updatePrefsPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create global liza config dir: %w", err)
	}
	disabled := false
	data, err := json.MarshalIndent(preferences{CheckUpdate: &disabled}, "", "  ")
	if err != nil {
		return fmt.Errorf("encode update preferences: %w", err)
	}
	data = append(data, '\n')
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("write update preferences: %w", err)
	}
	return nil
}

func updateChannel(cfg Config) string {
	for _, arg := range cfg.Args[1:] {
		if strings.HasPrefix(arg, "--update-channel=") {
			return strings.ToLower(strings.TrimSpace(strings.TrimPrefix(arg, "--update-channel=")))
		}
	}
	for i, arg := range cfg.Args[1:] {
		if arg == "--update-channel" && i+2 < len(cfg.Args) {
			return strings.ToLower(strings.TrimSpace(cfg.Args[i+2]))
		}
	}
	if cfg.Channel != "" {
		return strings.ToLower(strings.TrimSpace(cfg.Channel))
	}
	if envChannel := envValue(cfg.Env, channelEnvName); envChannel != "" {
		return strings.ToLower(strings.TrimSpace(envChannel))
	}
	return channelStable
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
	if cfg.LookupLatest == nil {
		cfg.LookupLatest = LatestModuleVersion
	}
	if cfg.LookupMain == nil {
		cfg.LookupMain = MainCommit
	}
	if cfg.Install == nil {
		cfg.Install = Install
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

func runStreaming(ctx context.Context, stdout, stderr io.Writer, name string, args ...string) error {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Stdout = stdout
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
