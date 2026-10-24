package updater

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"syscall"
	"time"

	"github.com/liza-mas/liza/internal/brand"
	"github.com/liza-mas/liza/internal/termutil"
	"golang.org/x/mod/semver"
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

func SyscallExec(path string, args []string, env []string) error {
	return syscall.Exec(path, args, env)
}

func confirmUpdate(stdin *bufio.Reader, stdout io.Writer, next candidate) bool {
	fmt.Fprintf(stdout, "%s %s update is available (%s -> %s). Update now and rerun this command? [y/N] ", nameTitle(), next.Channel, next.Current, next.Latest)
	answer, err := termutil.ReadSingleKey(stdin)
	if err != nil {
		fmt.Fprintln(stdout)
		return false
	}
	if answer != "y" {
		fmt.Fprintln(stdout) // Print newline for clean terminal output
		return false
	}
	return true
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

func proposeDisableCheck(stdin *bufio.Reader, stdout io.Writer, disable func() error) {
	fmt.Fprint(stdout, "Update skipped. Disable update checks for future runs? [y/N] ")
	answer, err := termutil.ReadSingleKey(stdin)
	if err != nil {
		fmt.Fprintln(stdout)
		return
	}
	if answer == "y" {
		if err := disable(); err != nil {
			fmt.Fprintf(stdout, "Could not disable update checks: %v\n", err)
			return
		}
		fmt.Fprintln(stdout, "Update checks disabled.")
	} else {
		fmt.Fprintln(stdout) // Print newline for clean terminal output
	}
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
