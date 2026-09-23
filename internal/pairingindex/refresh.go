package pairingindex

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/liza-mas/liza/internal/brand"
	"github.com/liza-mas/liza/internal/filelock"
	"github.com/liza-mas/liza/internal/gitbash"
	"github.com/liza-mas/liza/internal/gitenv"
	"github.com/liza-mas/liza/internal/subprocess"
)

// RefreshCommandName is the hidden CLI subcommand the dispatcher and the
// merge trigger run to refresh repo-root indexes through the coordinator.
const RefreshCommandName = "index-refresh"

// lockFD is the descriptor the index script receives the held refresh lock
// on: exec.Cmd.ExtraFiles[0] becomes fd 3. The script closes it for every
// tool it starts, so only the script itself, which does all the publishing,
// keeps the lock alive past a coordinator crash.
const lockFD = 3

// closeLockFD is appended to every tool the index script starts. A leftover
// daemon that kept the descriptor would hold the lock forever and silently
// stop all later refreshes.
var closeLockFD = fmt.Sprintf(" %d>&-", lockFD)

// RefreshOptions configures one coordinator run.
type RefreshOptions struct {
	RepoRoot string
	// Trigger names what requested the refresh (a hook name or "merge"),
	// recorded in the log.
	Trigger string
	// Echo also receives the script output. Hooks pass stderr so a commit
	// still shows indexing progress; the detached merge trigger passes nil.
	Echo io.Writer
}

// afterRefreshRelease runs between releasing the lock and re-checking the
// request marker. Tests use it to land a request in that window.
var afterRefreshRelease func()

// RunRefresh records a refresh request and, unless another run holds the
// refresh lock, runs the index script until no request is pending.
//
// Every writer of repo-root indexes goes through here, so runs are serialized
// and an older run can no longer publish over a newer one. A requester writes
// the marker before trying the lock. If the lock is busy, it returns at once:
// its failed attempt came before the owner's release, and the owner re-checks
// the marker after releasing, so the request is never lost. A crashed owner
// leaves the marker in place, and the next trigger consumes it.
func RunRefresh(opts RefreshOptions) error {
	repoRoot, scriptPath, installed, err := installedIndexScript(opts.RepoRoot)
	if err != nil || !installed {
		return err
	}
	commonDir, err := gitCommonDir(repoRoot)
	if err != nil {
		return err
	}
	paths := refreshPathsIn(commonDir)
	if err := os.WriteFile(paths.request, nil, 0o644); err != nil {
		return fmt.Errorf("record index refresh request: %w", err)
	}

	lock := filelock.New(paths.lockBase)
	var runErr error
	for {
		held, acquired, err := lock.TryHold("index-refresh " + opts.Trigger)
		if err != nil {
			return errors.Join(runErr, err)
		}
		if !acquired {
			return runErr
		}
		if err := os.Remove(paths.request); err == nil {
			runErr = errors.Join(runErr, runIndexScript(repoRoot, commonDir, scriptPath, paths, held, opts))
		} else if !os.IsNotExist(err) {
			// The marker would still be there after release, so looping
			// would retake the lock and fail again forever, inside a hook.
			consumeErr := fmt.Errorf("consume index refresh request: %w", err)
			return errors.Join(runErr, consumeErr, held.Release())
		}
		if err := held.Release(); err != nil {
			return errors.Join(runErr, err)
		}
		if afterRefreshRelease != nil {
			afterRefreshRelease()
		}
		if _, err := os.Stat(paths.request); os.IsNotExist(err) {
			return runErr
		}
	}
}

// StartRefresh launches a detached coordinator run and returns without
// waiting for it. The MAS merge calls it because wt-merge moves the
// integration branch with update-ref, which fires no Git hook. It does nothing
// when the index script is not installed.
func StartRefresh(repoRoot, trigger string) error {
	repoRoot, _, installed, err := installedIndexScript(repoRoot)
	if err != nil || !installed {
		return err
	}
	binary := indexBinary()
	if binary == "" {
		return errors.New("locate the index refresh coordinator executable")
	}
	// Nil standard streams read from and write to the null device; the
	// coordinator writes its own log.
	cmd := exec.Command(binary, RefreshCommandName, "--trigger", trigger)
	cmd.Dir = repoRoot
	subprocess.SetDetachedProcessGroup(cmd)
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start index refresh: %w", err)
	}
	// A long-lived supervisor must reap the coordinator; a CLI that exits
	// first leaves it to be adopted by init.
	go func() { _ = cmd.Wait() }()
	return nil
}

// installedIndexScript resolves the repository root and the index script
// path, and reports whether the script is installed.
func installedIndexScript(repoRoot string) (absRoot, scriptPath string, installed bool, err error) {
	absRoot, err = filepath.Abs(repoRoot)
	if err != nil {
		return "", "", false, fmt.Errorf("resolve repository root: %w", err)
	}
	hooksDir, err := ResolveEffectiveHooksDir(absRoot)
	if err != nil {
		return "", "", false, err
	}
	scriptPath = filepath.Join(hooksDir, scriptName())
	if _, err := os.Stat(scriptPath); os.IsNotExist(err) {
		return absRoot, scriptPath, false, nil
	}
	return absRoot, scriptPath, true, nil
}

type refreshPaths struct {
	lockBase string
	request  string
	log      string
	staging  string
}

// refreshPathsIn keeps the coordination files in the Git common directory:
// project cleanup never removes it, so the lock file is never recreated under
// a live holder, and Pairing repositories without a runtime directory have one.
func refreshPathsIn(commonDir string) refreshPaths {
	prefix := filepath.Join(commonDir, brand.RuntimeValues().BinaryName+"-index-refresh")
	return refreshPaths{
		lockBase: prefix,
		request:  prefix + ".request",
		log:      prefix + ".log",
		staging:  filepath.Join(commonDir, stagingDirName()),
	}
}

func runIndexScript(repoRoot, commonDir, scriptPath string, paths refreshPaths, held *filelock.Held, opts RefreshOptions) error {
	log, err := os.Create(paths.log)
	if err != nil {
		return fmt.Errorf("open index refresh log: %w", err)
	}
	defer log.Close()
	var out io.Writer = log
	if opts.Echo != nil {
		out = io.MultiWriter(log, opts.Echo)
	}
	fmt.Fprintf(out, "%s: index refresh (trigger %s)\n", brand.RuntimeValues().BinaryName, opts.Trigger)

	// Only the lock holder writes staging, so runs a crash interrupted can go.
	if err := os.RemoveAll(paths.staging); err != nil {
		return fmt.Errorf("remove stale index staging: %w", err)
	}
	if same, err := sameFilesystem(commonDir, repoRoot); err == nil && !same {
		fmt.Fprintf(out, "%s: warning: %s is on a different filesystem from %s; indexes are published by copy, not atomically\n",
			brand.RuntimeValues().BinaryName, commonDir, repoRoot)
	}

	cmd, err := indexScriptCommand(scriptPath)
	if err != nil {
		return err
	}
	cmd.Dir = repoRoot
	cmd.Stdout = out
	cmd.Stderr = out
	if file := held.File(); file != nil {
		cmd.ExtraFiles = []*os.File{file}
	}
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%s failed (trigger %s; see %s): %w", scriptName(), opts.Trigger, paths.log, err)
	}
	return nil
}

// indexScriptCommand runs the script directly on Unix and through Git Bash on
// Windows, which cannot exec an extensionless shell script.
func indexScriptCommand(scriptPath string) (*exec.Cmd, error) {
	if runtime.GOOS != "windows" {
		return exec.Command(scriptPath), nil
	}
	bash, err := gitbash.Resolve()
	if err != nil {
		return nil, fmt.Errorf("resolve shell for %s: %w", scriptName(), err)
	}
	return exec.Command(bash, filepath.ToSlash(scriptPath)), nil
}

func gitCommonDir(repoRoot string) (string, error) {
	output, err := gitenv.Output(repoRoot, "rev-parse", "--git-common-dir")
	if err != nil {
		return "", fmt.Errorf("resolve Git common directory: %w%s", err, outputSuffix(string(output)))
	}
	dir := strings.TrimSpace(string(output))
	if !filepath.IsAbs(dir) {
		dir = filepath.Join(repoRoot, dir)
	}
	return filepath.Clean(dir), nil
}
