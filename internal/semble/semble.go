package semble

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/liza-mas/liza/internal/brand"
	"github.com/liza-mas/liza/internal/envgate"
)

const EnvEnableSemble = "LIZA_ENABLE_SEMBLE"

const (
	sembleExecutableName      = "semble"
	prewarmQuery              = "__liza_semble_prewarm__"
	validationFixtureFileName = "prewarm.py"
	validationFixtureContent  = "def liza_semble_prewarm(): pass\n"
	validationTopK            = 1
	validationContentMode     = "code"
)

const maxDiagnosticBytes = 1024

// SembleValidationTimeout is the default timeout for prewarm and offline
// validation command execution.
const SembleValidationTimeout = 30 * time.Second

var defaultIgnorePatternsTail = []string{
	".worktrees/",
	"stacklit.json",
	"*.scip",
	".env",
	".env.*",
	"*.env",
	"credentials.*",
	"secrets.*",
	"*secret*.*",
	"*.pem",
	"*.key",
	"*.p12",
	"*.pfx",
	"*.jks",
	"*_rsa",
	"*_dsa",
	"*_ecdsa",
	"*_ed25519",
	"*.keystore",
	"*.truststore",
	"config/secrets/",
	"**/secrets/",
	"serviceAccountKey.json",
	"*-credentials.json",
}

// ExecutableLookup resolves an executable name to the path that would be run.
type ExecutableLookup func(name string) (string, error)

// CommandPlanOptions configures Semble command planning for one validation
// fixture directory.
type CommandPlanOptions struct {
	FixtureDir string
	LookPath   ExecutableLookup
	Timeout    time.Duration
	Fixture    FixtureIdentity
}

// PlanResult contains activation state, executable state, fixed command plans,
// and bounded diagnostics from command planning.
type PlanResult struct {
	Enabled           bool
	ExecutablePath    string
	Prewarm           CommandPlan
	OfflineValidation CommandPlan
	Diagnostics       []Diagnostic
}

// CommandPlan describes one fixed Semble validation command without executing
// it.
type CommandPlan struct {
	Enabled        bool
	Name           string
	ExecutablePath string
	Args           []string
	Dir            string
	Env            []EnvVar
	Timeout        time.Duration
	Fixture        FixtureIdentity
}

// EnvVar is one environment override added to an otherwise inherited process
// environment.
type EnvVar struct {
	Name  string
	Value string
}

// FixtureIdentity identifies the Semble validation corpus and query contract.
type FixtureIdentity struct {
	FileName    string
	FileContent string
	Query       string
	TopK        int
	ContentMode string
}

type DiagnosticKind string

const (
	DiagnosticMissingExecutable       DiagnosticKind = "missing_executable"
	DiagnosticModelUnavailableOffline DiagnosticKind = "model_unavailable_offline"
	DiagnosticExecutionFailure        DiagnosticKind = "execution_failure"
)

// Diagnostic is a bounded operator-visible validation diagnostic.
type Diagnostic struct {
	Kind    DiagnosticKind
	Message string
}

// CommandRunner executes one fixed Semble validation command plan.
type CommandRunner func(CommandPlan) (CommandResult, error)

// CommandResult captures bounded subprocess facts used for Semble diagnostics.
type CommandResult struct {
	ExitCode int
	Stdout   string
	Stderr   string
}

// ValidationOptions configures one Semble prewarm or offline readiness check.
type ValidationOptions struct {
	TargetRoot string
	LookPath   ExecutableLookup
	Runner     CommandRunner
	Timeout    time.Duration
	Fixture    FixtureIdentity
}

// ValidationResult reports Semble prewarm/offline readiness without exposing raw
// command output.
type ValidationResult struct {
	Enabled    bool
	Ready      bool
	Cached     bool
	Diagnostic Diagnostic
}

// TargetKind identifies the Semble target-root safety rules to apply.
type TargetKind string

const (
	TargetKindProjectRoot  TargetKind = "project_root"
	TargetKindTaskWorktree TargetKind = "task_worktree"
)

// TargetSafetyOptions identifies the intended Semble target root and caller
// context.
type TargetSafetyOptions struct {
	Kind                 TargetKind
	TargetRoot           string
	ExpectedWorktreeRoot string
}

// TargetSafetyResult reports whether a target root is safe for Semble guidance.
type TargetSafetyResult struct {
	Safe                  bool
	Kind                  TargetKind
	TargetRoot            string
	ExpectedWorktreeRoot  string
	MissingIgnorePatterns []string
	Diagnostic            Diagnostic
}

// PromptMetadataOptions configures prompt-safe Semble metadata construction.
type PromptMetadataOptions struct {
	Kind                 TargetKind
	TargetRoot           string
	ExpectedWorktreeRoot string
	LookPath             ExecutableLookup
	Runner               CommandRunner
	Timeout              time.Duration
	Fixture              FixtureIdentity
}

// PromptMetadata is safe for prompt rendering; it intentionally excludes
// diagnostics, command output, executable paths, and cache paths.
type PromptMetadata struct {
	TargetRoot      string
	ShellTargetRoot string
}

type validationCacheKey struct {
	ExecutablePath string
	ModelName      string
	HFHome         string
	XDGCacheHome   string
	Timeout        time.Duration
	Fixture        FixtureIdentity
}

var (
	readinessCacheMu sync.Mutex
	readinessCache   = map[validationCacheKey]ValidationResult{}
)

// ParseEnvGate reports whether a supplied Semble activation value is enabled.
func ParseEnvGate(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "1", "true":
		return true
	default:
		return false
	}
}

// RuntimeEnabled reports whether Semble behavior is active for this process.
func RuntimeEnabled() bool {
	return ParseEnvGate(envgate.Value(EnvEnableSemble))
}

// PlanCommands returns fixed Semble prewarm and offline validation plans. When
// disabled, it returns no command plans, diagnostics, or executable lookup.
func PlanCommands(opts CommandPlanOptions) PlanResult {
	if !RuntimeEnabled() {
		return PlanResult{}
	}

	lookup := opts.LookPath
	if lookup == nil {
		lookup = exec.LookPath
	}
	executablePath, err := lookup(sembleExecutableName)
	if err != nil {
		return PlanResult{
			Enabled: true,
			Diagnostics: []Diagnostic{{
				Kind:    DiagnosticMissingExecutable,
				Message: boundedDiagnosticMessage("semble executable not found", err.Error()),
			}},
		}
	}

	fixtureDir := opts.FixtureDir
	if abs, err := filepath.Abs(fixtureDir); err == nil {
		fixtureDir = abs
	}
	fixture := opts.Fixture
	if fixture == (FixtureIdentity{}) {
		fixture = DefaultValidationFixtureIdentity()
	}
	timeout := opts.Timeout
	if timeout == 0 {
		timeout = SembleValidationTimeout
	}
	topK := strconv.Itoa(fixture.TopK)

	base := CommandPlan{
		Enabled:        true,
		Name:           sembleExecutableName,
		ExecutablePath: executablePath,
		Args:           []string{"search", fixture.Query, fixtureDir, "--top-k", topK, "--content", fixture.ContentMode},
		Dir:            fixtureDir,
		Timeout:        timeout,
		Fixture:        fixture,
	}
	offline := base
	offline.Env = []EnvVar{{Name: "HF_HUB_OFFLINE", Value: "1"}}

	return PlanResult{
		Enabled:           true,
		ExecutablePath:    executablePath,
		Prewarm:           base,
		OfflineValidation: offline,
	}
}

// DefaultValidationFixtureIdentity returns the package-owned fixture identity
// shared by prewarm and offline validation planning.
func DefaultValidationFixtureIdentity() FixtureIdentity {
	return FixtureIdentity{
		FileName:    validationFixtureFileName,
		FileContent: validationFixtureContent,
		Query:       prewarmQuery,
		TopK:        validationTopK,
		ContentMode: validationContentMode,
	}
}

// ExecutePrewarm runs the controlled Semble prewarm fixture with inherited
// operator network behavior.
func ExecutePrewarm(opts ValidationOptions) ValidationResult {
	return executeValidation(opts, false)
}

// CheckOfflineReadiness runs or reuses the process-local offline validation
// result for the current executable, Semble model/cache environment, timeout,
// and validation fixture identity.
func CheckOfflineReadiness(opts ValidationOptions) ValidationResult {
	planResult := planValidationCommands(opts)
	if cached, ok := cachedReadinessResult(planResult); ok {
		cached.Cached = true
		return cached
	}
	result := executePlannedValidation(opts, planResult, true)
	if planResult.ExecutablePath != "" {
		storeReadinessResult(planResult, result)
	}
	return result
}

// DefaultIgnorePatterns returns the ordered Semble ignore source of truth for
// runtime, generated-index, and credential exclusions.
func DefaultIgnorePatterns() []string {
	patterns := make([]string, 0, len(defaultIgnorePatternsTail)+1)
	patterns = append(patterns, brand.RuntimeValues().ProjectDirName+"/")
	patterns = append(patterns, defaultIgnorePatternsTail...)
	return patterns
}

// DefaultIgnorePayload returns the physical .sembleignore content shared by
// project-root and generated task-worktree safety setup.
func DefaultIgnorePayload() string {
	return strings.Join(DefaultIgnorePatterns(), "\n") + "\n"
}

// GeneratedWorktreeIgnorePayload returns the generated task-worktree
// .sembleignore content. Lifecycle callers own writing this payload.
func GeneratedWorktreeIgnorePayload() string {
	return DefaultIgnorePayload()
}

// EnsureProjectRootIgnore creates or verifies the project-root .sembleignore
// required before pairing SessionStart may advertise Semble.
func EnsureProjectRootIgnore(root string) TargetSafetyResult {
	targetRoot, err := normalizeRoot(root)
	if err != nil {
		return TargetSafetyResult{
			Kind:       TargetKindProjectRoot,
			TargetRoot: root,
			Diagnostic: Diagnostic{
				Kind:    DiagnosticExecutionFailure,
				Message: boundedDiagnosticMessage("semble project root invalid", err.Error()),
			},
		}
	}

	ignorePath := filepath.Join(targetRoot, ".sembleignore")
	file, err := os.OpenFile(ignorePath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if errors.Is(err, os.ErrExist) {
		return ValidateTargetSafety(TargetSafetyOptions{
			Kind:       TargetKindProjectRoot,
			TargetRoot: targetRoot,
		})
	}
	if err != nil {
		return TargetSafetyResult{
			Kind:       TargetKindProjectRoot,
			TargetRoot: targetRoot,
			Diagnostic: Diagnostic{
				Kind:    DiagnosticExecutionFailure,
				Message: boundedDiagnosticMessage("semble create project root .sembleignore", err.Error()),
			},
		}
	}

	_, writeErr := file.WriteString(DefaultIgnorePayload())
	if closeErr := file.Close(); writeErr == nil {
		writeErr = closeErr
	}
	if writeErr != nil {
		_ = os.Remove(ignorePath)
		return TargetSafetyResult{
			Kind:       TargetKindProjectRoot,
			TargetRoot: targetRoot,
			Diagnostic: Diagnostic{
				Kind:    DiagnosticExecutionFailure,
				Message: boundedDiagnosticMessage("semble write project root .sembleignore", writeErr.Error()),
			},
		}
	}

	return ValidateTargetSafety(TargetSafetyOptions{
		Kind:       TargetKindProjectRoot,
		TargetRoot: targetRoot,
	})
}

// ValidateTargetSafety checks whether a target root is safe for Semble prompt
// guidance in the supplied context.
func ValidateTargetSafety(opts TargetSafetyOptions) TargetSafetyResult {
	targetRoot, err := normalizeRoot(opts.TargetRoot)
	if err != nil {
		return TargetSafetyResult{
			Kind:       opts.Kind,
			TargetRoot: opts.TargetRoot,
			Diagnostic: Diagnostic{
				Kind:    DiagnosticExecutionFailure,
				Message: boundedDiagnosticMessage("semble target root invalid", err.Error()),
			},
		}
	}

	result := TargetSafetyResult{
		Kind:       opts.Kind,
		TargetRoot: targetRoot,
	}
	switch opts.Kind {
	case TargetKindProjectRoot:
		missing, diagnostic := missingProjectIgnorePatterns(targetRoot)
		result.MissingIgnorePatterns = missing
		result.Diagnostic = diagnostic
		result.Safe = len(missing) == 0 && diagnostic == (Diagnostic{})
	case TargetKindTaskWorktree:
		expectedRoot, err := normalizeRoot(opts.ExpectedWorktreeRoot)
		if err != nil {
			result.Diagnostic = Diagnostic{
				Kind:    DiagnosticExecutionFailure,
				Message: boundedDiagnosticMessage("semble task worktree root invalid", err.Error()),
			}
			return result
		}
		result.ExpectedWorktreeRoot = expectedRoot
		if targetRoot != expectedRoot {
			result.Diagnostic = Diagnostic{
				Kind:    DiagnosticExecutionFailure,
				Message: "semble target root does not match explicit task worktree root",
			}
			return result
		}
		missing, diagnostic := missingTaskWorktreeIgnorePatterns(targetRoot)
		result.MissingIgnorePatterns = missing
		result.Diagnostic = diagnostic
		result.Safe = len(missing) == 0 && diagnostic == (Diagnostic{})
	default:
		result.Diagnostic = Diagnostic{
			Kind:    DiagnosticExecutionFailure,
			Message: boundedDiagnosticMessage("semble target kind invalid", string(opts.Kind)),
		}
	}
	return result
}

// BuildPromptMetadata returns prompt-safe Semble context only when activation,
// target safety, and offline readiness all pass.
func BuildPromptMetadata(opts PromptMetadataOptions) (PromptMetadata, bool) {
	if !RuntimeEnabled() {
		return PromptMetadata{}, false
	}
	safety := ValidateTargetSafety(TargetSafetyOptions{
		Kind:                 opts.Kind,
		TargetRoot:           opts.TargetRoot,
		ExpectedWorktreeRoot: opts.ExpectedWorktreeRoot,
	})
	if !safety.Safe {
		return PromptMetadata{}, false
	}
	readiness := CheckOfflineReadiness(ValidationOptions{
		TargetRoot: safety.TargetRoot,
		LookPath:   opts.LookPath,
		Runner:     opts.Runner,
		Timeout:    opts.Timeout,
		Fixture:    opts.Fixture,
	})
	if !readiness.Ready {
		return PromptMetadata{}, false
	}

	shellTargetRoot := shellQuote(safety.TargetRoot)
	return PromptMetadata{
		TargetRoot:      safety.TargetRoot,
		ShellTargetRoot: shellTargetRoot,
	}, true
}

func executeValidation(opts ValidationOptions, offline bool) ValidationResult {
	return executePlannedValidation(opts, planValidationCommands(opts), offline)
}

func executePlannedValidation(opts ValidationOptions, planResult PlanResult, offline bool) ValidationResult {
	if !planResult.Enabled {
		return ValidationResult{}
	}
	if len(planResult.Diagnostics) > 0 {
		return ValidationResult{Enabled: true, Diagnostic: planResult.Diagnostics[0]}
	}

	plan := planResult.Prewarm
	if offline {
		plan = planResult.OfflineValidation
	}
	fixtureDir, cleanup, err := createValidationFixture(opts.TargetRoot, plan.Fixture)
	if err != nil {
		return ValidationResult{
			Enabled: true,
			Diagnostic: Diagnostic{
				Kind:    DiagnosticExecutionFailure,
				Message: boundedDiagnosticMessage("semble validation fixture failed", err.Error()),
			},
		}
	}
	defer cleanup()

	plan.Dir = fixtureDir
	plan.Args = []string{"search", plan.Fixture.Query, fixtureDir, "--top-k", strconv.Itoa(plan.Fixture.TopK), "--content", plan.Fixture.ContentMode}
	runner := opts.Runner
	if runner == nil {
		runner = runCommandPlan
	}
	commandResult, runErr := runner(plan)
	if runErr == nil && commandResult.ExitCode == 0 {
		return ValidationResult{Enabled: true, Ready: true}
	}

	kind := DiagnosticExecutionFailure
	if offline && isOfflineModelFailure(commandResult, runErr) {
		kind = DiagnosticModelUnavailableOffline
	}
	return ValidationResult{
		Enabled: true,
		Diagnostic: Diagnostic{
			Kind:    kind,
			Message: boundedCommandDiagnostic(kind, commandResult, runErr),
		},
	}
}

func planValidationCommands(opts ValidationOptions) PlanResult {
	return PlanCommands(CommandPlanOptions{
		FixtureDir: os.TempDir(),
		LookPath:   opts.LookPath,
		Timeout:    opts.Timeout,
		Fixture:    opts.Fixture,
	})
}

func createValidationFixture(targetRoot string, fixture FixtureIdentity) (string, func(), error) {
	if fixture == (FixtureIdentity{}) {
		fixture = DefaultValidationFixtureIdentity()
	}
	dir, err := os.MkdirTemp("", "liza-semble-validation-*")
	if err != nil {
		return "", func() {}, fmt.Errorf("create temp dir: %w", err)
	}
	cleanup := func() {
		_ = os.RemoveAll(dir)
	}
	if targetRoot != "" && pathWithin(targetRoot, dir) {
		cleanup()
		return "", func() {}, fmt.Errorf("temp fixture %q is inside target root %q", dir, targetRoot)
	}
	if err := os.WriteFile(filepath.Join(dir, fixture.FileName), []byte(fixture.FileContent), 0o644); err != nil {
		cleanup()
		return "", func() {}, fmt.Errorf("write fixture file: %w", err)
	}
	return dir, cleanup, nil
}

func cachedReadinessResult(planResult PlanResult) (ValidationResult, bool) {
	if !planResult.Enabled || len(planResult.Diagnostics) > 0 || planResult.ExecutablePath == "" {
		return ValidationResult{}, false
	}
	key := readinessCacheKeyFor(planResult)
	readinessCacheMu.Lock()
	defer readinessCacheMu.Unlock()
	result, ok := readinessCache[key]
	return result, ok
}

func storeReadinessResult(planResult PlanResult, result ValidationResult) {
	key := readinessCacheKeyFor(planResult)
	readinessCacheMu.Lock()
	readinessCache[key] = result
	readinessCacheMu.Unlock()
}

func readinessCacheKeyFor(planResult PlanResult) validationCacheKey {
	plan := planResult.OfflineValidation
	return validationCacheKey{
		ExecutablePath: planResult.ExecutablePath,
		ModelName:      os.Getenv("SEMBLE_MODEL_NAME"),
		HFHome:         os.Getenv("HF_HOME"),
		XDGCacheHome:   os.Getenv("XDG_CACHE_HOME"),
		Timeout:        plan.Timeout,
		Fixture:        plan.Fixture,
	}
}

func runCommandPlan(plan CommandPlan) (CommandResult, error) {
	ctx, cancel := context.WithTimeout(context.Background(), plan.Timeout)
	defer cancel()

	name := plan.ExecutablePath
	if name == "" {
		name = plan.Name
	}
	cmd := exec.CommandContext(ctx, name, plan.Args...)
	cmd.Dir = plan.Dir
	cmd.Env = append(os.Environ(), envVars(plan.Env)...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	result := CommandResult{Stdout: stdout.String(), Stderr: stderr.String()}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		result.ExitCode = exitErr.ExitCode()
	}
	if err == nil {
		result.ExitCode = 0
	}
	return result, err
}

func envVars(vars []EnvVar) []string {
	values := make([]string, 0, len(vars))
	for _, env := range vars {
		values = append(values, env.Name+"="+env.Value)
	}
	return values
}

func normalizeRoot(root string) (string, error) {
	if strings.TrimSpace(root) == "" {
		return "", errors.New("empty target root")
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	return filepath.Clean(abs), nil
}

func missingProjectIgnorePatterns(root string) ([]string, Diagnostic) {
	return missingRequiredIgnorePatterns(root, "project root")
}

func missingTaskWorktreeIgnorePatterns(root string) ([]string, Diagnostic) {
	return missingRequiredIgnorePatterns(root, "task worktree")
}

func missingRequiredIgnorePatterns(root, targetLabel string) ([]string, Diagnostic) {
	content, err := os.ReadFile(filepath.Join(root, ".sembleignore"))
	if err != nil {
		return DefaultIgnorePatterns(), Diagnostic{
			Kind:    DiagnosticExecutionFailure,
			Message: boundedDiagnosticMessage("semble "+targetLabel+" .sembleignore missing", err.Error()),
		}
	}
	present := map[string]bool{}
	for _, line := range strings.Split(string(content), "\n") {
		pattern := strings.TrimSpace(line)
		if pattern == "" || strings.HasPrefix(pattern, "#") {
			continue
		}
		present[pattern] = true
	}
	var missing []string
	for _, pattern := range DefaultIgnorePatterns() {
		if !present[pattern] {
			missing = append(missing, pattern)
		}
	}
	if len(missing) > 0 {
		return missing, Diagnostic{
			Kind:    DiagnosticExecutionFailure,
			Message: "semble " + targetLabel + " .sembleignore missing required patterns",
		}
	}
	return nil, Diagnostic{}
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}

func boundedCommandDiagnostic(kind DiagnosticKind, result CommandResult, err error) string {
	prefix := "semble: execution failed"
	if kind == DiagnosticModelUnavailableOffline {
		prefix = "semble: model unavailable offline"
	}
	var parts []string
	if err != nil {
		parts = append(parts, err.Error())
	}
	if result.ExitCode != 0 {
		parts = append(parts, fmt.Sprintf("exit code %d", result.ExitCode))
	}
	if output := strings.TrimSpace(strings.Join([]string{result.Stdout, result.Stderr}, "\n")); output != "" {
		parts = append(parts, output)
	}
	return boundedDiagnosticMessage(prefix, strings.Join(parts, ": "))
}

func boundedDiagnosticMessage(prefix, detail string) string {
	message := strings.TrimSpace(prefix)
	if detail = strings.TrimSpace(detail); detail != "" {
		message += ": " + detail
	}
	if len(message) <= maxDiagnosticBytes {
		return message
	}
	return message[:maxDiagnosticBytes]
}

func isOfflineModelFailure(result CommandResult, err error) bool {
	text := strings.ToLower(strings.Join([]string{result.Stdout, result.Stderr}, "\n"))
	if err != nil {
		text += "\n" + strings.ToLower(err.Error())
	}
	markers := []string{
		"localentrynotfounderror",
		"hf_hub_offline",
		"offline",
		"not in cache",
		"from_pretrained",
	}
	for _, marker := range markers {
		if strings.Contains(text, marker) {
			return true
		}
	}
	return false
}

func pathWithin(root, path string) bool {
	root, err := filepath.Abs(root)
	if err != nil {
		return false
	}
	path, err = filepath.Abs(path)
	if err != nil {
		return false
	}
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return false
	}
	return rel == "." || (!strings.HasPrefix(rel, ".."+string(filepath.Separator)) && rel != ".." && !filepath.IsAbs(rel))
}

func resetReadinessCacheForTest() {
	readinessCacheMu.Lock()
	readinessCache = map[validationCacheKey]ValidationResult{}
	readinessCacheMu.Unlock()
}
