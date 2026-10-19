package toolchain

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/liza-mas/liza/internal/semble"
)

// sembleToolID names the one tool whose usefulness depends on a model cache:
// a Semble upgrade can change its default model, and every offline query then
// fails until the new model is downloaded (ADR-0079).
const sembleToolID = "semble"

// errSembleModelUnavailable marks an offline query refused for an uncached model.
var errSembleModelUnavailable = errors.New("model unavailable offline")

// sembleExecutable is the semble binary to run: the file a fresh install
// placed in installDir, which configure may not have put on PATH yet, or else
// whatever PATH resolves. Commands run by name resolve through this process's
// PATH, so the prewarm must be given the absolute path.
func sembleExecutable(tool Tool, installDir, goos string, runner Runner) (string, error) {
	if installDir != "" {
		candidates := []string{filepath.Join(installDir, tool.Binary)}
		if goos == "windows" {
			candidates = nil
			for _, ext := range []string{".exe", ".cmd", ".bat", ".com", ""} {
				candidates = append(candidates, filepath.Join(installDir, tool.Binary+ext))
			}
		}
		for _, candidate := range candidates {
			if regularFileExists(candidate) {
				return candidate, nil
			}
		}
	}
	path, err := runner.LookPath(tool.Binary)
	if err != nil || path == "" {
		return "", fmt.Errorf("%s not found in %s or on PATH", tool.Binary, installDir)
	}
	return path, nil
}

// sembleModelQuery runs Semble on the package-owned validation fixture with
// HF_HUB_OFFLINE set to offline ("1" checks the cache, "0" may download).
func sembleModelQuery(runner Runner, executable, offline string) (CommandOutput, error) {
	dir, args, cleanup, err := semble.ValidationFixtureCommand()
	defer cleanup()
	if err != nil {
		return CommandOutput{}, err
	}
	return runner.Run(Command{Name: executable, Args: args, Dir: dir, Env: map[string]string{"HF_HUB_OFFLINE": offline}})
}

// sembleOfflineCheck reports whether Semble answers offline, distinguishing
// an uncached model from any other failure.
func sembleOfflineCheck(runner Runner, executable string) (CommandOutput, error) {
	output, err := sembleModelQuery(runner, executable, "1")
	if err == nil && output.ExitCode == 0 {
		return output, nil
	}
	if semble.IsModelUnavailableOffline(output.Stderr) {
		return output, errSembleModelUnavailable
	}
	return output, fmt.Errorf("offline query failed (exit %d)", output.ExitCode)
}

// sembleEnsureModel leaves Semble's model cached. A fresh install prewarms
// online, the operator's deliberate download moment, whatever the cache holds.
// An existing install checks offline first, so a ready cache downloads
// nothing, and prewarms only for an uncached model. Either way it checks
// offline again after a prewarm. It reports whether it prewarmed.
func sembleEnsureModel(runner Runner, executable string, fresh bool) (CommandOutput, bool, error) {
	if !fresh {
		output, err := sembleOfflineCheck(runner, executable)
		if !errors.Is(err, errSembleModelUnavailable) {
			return output, false, err
		}
	}
	if output, err := sembleModelQuery(runner, executable, "0"); err != nil || output.ExitCode != 0 {
		return output, true, fmt.Errorf("model prewarm failed (exit %d)", output.ExitCode)
	}
	output, err := sembleOfflineCheck(runner, executable)
	if err != nil {
		return output, true, fmt.Errorf("offline check after prewarm: %w", err)
	}
	return output, true, nil
}

// sembleModelStep finishes a semble install step by leaving its model cached:
// after a fresh install, and for an already-installed binary, so repeating the
// install repairs a model an upgrade left uncached. Other outcomes pass
// through unchanged.
func sembleModelStep(step InstallStep, tool Tool, installDir, goos string, runner Runner) InstallStep {
	var executable string
	fresh := step.Status == InstallInstalled
	switch {
	case fresh:
		path, err := sembleExecutable(tool, installDir, goos, runner)
		if err != nil {
			return sembleSetupFailure(step, installDir, CommandOutput{}, err)
		}
		executable = path
	case step.Status == InstallSkipped:
		path, err := runner.LookPath(tool.Binary)
		if err != nil || path == "" {
			return step // skipped for want of an installer, not installed
		}
		executable = path
	default:
		return step
	}
	output, prewarmed, err := sembleEnsureModel(runner, executable, fresh)
	if err != nil {
		return sembleSetupFailure(step, executable, output, err)
	}
	switch {
	case fresh:
		step.Message = strings.TrimSpace(step.Message + " (model prewarmed)")
	case prewarmed:
		step.Status = InstallInstalled
		step.Message += "; model prewarmed"
		step.Output = output
	default:
		step.Message += "; model ready offline"
	}
	return step
}

// sembleSetupFailure keeps the fact that the binary is installed while
// failing the step, so the aggregate install error reports it.
func sembleSetupFailure(step InstallStep, executable string, output CommandOutput, err error) InstallStep {
	step.Status = InstallFailed
	step.Output = output
	step.Message = fmt.Sprintf("semble installed at %s, but %v; retry: %s", executable, err, semble.ModelRecoveryCommand())
	return step
}
