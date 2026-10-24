package semble

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"time"

	"github.com/liza-mas/liza/internal/brand"
)

const (
	prewarmQuery              = "__semble_prewarm__"
	validationFixtureFileName = "prewarm.py"
	validationFixtureContent  = "def semble_prewarm(): pass\n"
	validationTopK            = 1
	validationContentMode     = "code"
)

// FixtureIdentity identifies the Semble validation corpus and query contract.
type FixtureIdentity struct {
	FileName    string
	FileContent string
	Query       string
	TopK        int
	ContentMode string
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
	dir, err := os.MkdirTemp("", brand.RuntimeValues().BinaryName+"-semble-validation-*")
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

func resetReadinessCacheForTest() {
	readinessCacheMu.Lock()
	readinessCache = map[validationCacheKey]ValidationResult{}
	readinessCacheMu.Unlock()
}
