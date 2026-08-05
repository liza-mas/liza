package pairingindex

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/brand"
	"github.com/liza-mas/liza/internal/scipsearch"
	"github.com/liza-mas/liza/internal/testhelpers"
)

func TestResolveEffectiveHooksDirDefault(t *testing.T) {
	repo := initGitRepo(t)

	got, err := ResolveEffectiveHooksDir(repo)
	if err != nil {
		t.Fatalf("ResolveEffectiveHooksDir() error = %v", err)
	}

	want := filepath.Join(repo, ".git", "hooks")
	if got != want {
		t.Fatalf("ResolveEffectiveHooksDir() = %q, want %q", got, want)
	}
}

func TestInstallLifecycleHooksDefaultHooks(t *testing.T) {
	repo := initGitRepo(t)

	result, err := InstallLifecycleHooks(InstallHooksOptions{RepoRoot: repo})
	if err != nil {
		t.Fatalf("InstallLifecycleHooks() error = %v", err)
	}

	wantHooksDir := filepath.Join(repo, ".git", "hooks")
	if result.HooksDir != wantHooksDir {
		t.Fatalf("HooksDir = %q, want %q", result.HooksDir, wantHooksDir)
	}
	assertHookActions(t, result, HookActionInstalled)

	for _, hook := range DefaultLifecycleHooks() {
		hookPath := filepath.Join(wantHooksDir, hook)
		if _, err := os.Stat(hookPath); err != nil {
			t.Fatalf("%s missing: %v", hookPath, err)
		}
		testhelpers.AssertExecutableScript(t, hookPath)
		target, err := os.Readlink(hookPath)
		if err != nil {
			t.Fatalf("%s is not a dispatcher symlink: %v", hookPath, err)
		}
		if target != hookDispatcherName() {
			t.Fatalf("%s symlink target = %q, want %q", hookPath, target, hookDispatcherName())
		}
		content := readFile(t, hookPath)
		if !strings.Contains(content, ManagedHookMarker) {
			t.Fatalf("%s missing managed marker in:\n%s", hook, content)
		}
		if !strings.Contains(content, brand.BinaryName+"-index.sh") {
			t.Fatalf("%s does not invoke liza-index.sh in:\n%s", hook, content)
		}
		if strings.Contains(content, brand.BinaryName+"-index.sh ai") {
			t.Fatalf("%s lifecycle wrapper must not request AI summary in:\n%s", hook, content)
		}
	}
}

func TestInstallLifecycleHooksRespectsRelativeCoreHooksPath(t *testing.T) {
	repo := initGitRepo(t)
	runGit(t, repo, "config", "core.hooksPath", ".githooks")

	result, err := InstallLifecycleHooks(InstallHooksOptions{RepoRoot: repo})
	if err != nil {
		t.Fatalf("InstallLifecycleHooks() error = %v", err)
	}

	want := filepath.Join(repo, ".githooks")
	if result.HooksDir != want {
		t.Fatalf("HooksDir = %q, want %q", result.HooksDir, want)
	}
	for _, hook := range DefaultLifecycleHooks() {
		if _, err := os.Stat(filepath.Join(want, hook)); err != nil {
			t.Fatalf("relative hooksPath hook %s missing: %v", hook, err)
		}
	}
	if _, err := os.Stat(filepath.Join(repo, ".git", "hooks", "post-commit")); err == nil {
		t.Fatal("post-commit was installed into inert default .git/hooks")
	} else if !os.IsNotExist(err) {
		t.Fatalf("inspect default hook path: %v", err)
	}
}

func TestInstallLifecycleHooksRespectsAbsoluteCoreHooksPath(t *testing.T) {
	repo := initGitRepo(t)
	hooksDir := filepath.Join(t.TempDir(), "hooks")
	runGit(t, repo, "config", "core.hooksPath", hooksDir)

	result, err := InstallLifecycleHooks(InstallHooksOptions{RepoRoot: repo})
	if err != nil {
		t.Fatalf("InstallLifecycleHooks() error = %v", err)
	}

	if result.HooksDir != hooksDir {
		t.Fatalf("HooksDir = %q, want %q", result.HooksDir, hooksDir)
	}
	for _, hook := range DefaultLifecycleHooks() {
		if _, err := os.Stat(filepath.Join(hooksDir, hook)); err != nil {
			t.Fatalf("absolute hooksPath hook %s missing: %v", hook, err)
		}
	}
}

func TestInstallLifecycleHooksIsIdempotentForManagedHooks(t *testing.T) {
	repo := initGitRepo(t)

	first, err := InstallLifecycleHooks(InstallHooksOptions{RepoRoot: repo})
	if err != nil {
		t.Fatalf("first InstallLifecycleHooks() error = %v", err)
	}
	second, err := InstallLifecycleHooks(InstallHooksOptions{RepoRoot: repo})
	if err != nil {
		t.Fatalf("second InstallLifecycleHooks() error = %v", err)
	}

	assertHookActions(t, first, HookActionInstalled)
	assertHookActions(t, second, HookActionVerified)
	for _, hook := range DefaultLifecycleHooks() {
		hookPath := filepath.Join(first.HooksDir, hook)
		if got, err := os.Readlink(hookPath); err != nil || got != hookDispatcherName() {
			t.Fatalf("%s symlink = %q, err=%v; want %q", hook, got, err, hookDispatcherName())
		}
	}
}

func TestInstallLifecycleHooksRefreshesStaleManagedHook(t *testing.T) {
	repo := initGitRepo(t)
	hooksDir := filepath.Join(repo, ".git", "hooks")
	hookPath := filepath.Join(hooksDir, "post-merge")
	staleContent := ManagedHookMarker + "\n# stale wrapper from an older release\n"
	if err := os.WriteFile(hookPath, []byte(staleContent), 0644); err != nil {
		t.Fatalf("write stale managed hook: %v", err)
	}

	result, err := InstallLifecycleHooks(InstallHooksOptions{RepoRoot: repo})
	if err != nil {
		t.Fatalf("InstallLifecycleHooks() error = %v", err)
	}

	index := slices.IndexFunc(result.Hooks, func(got HookInstallResult) bool {
		return got.Hook == "post-merge"
	})
	if index == -1 {
		t.Fatalf("missing post-merge result: %#v", result.Hooks)
	}
	if result.Hooks[index].Action != HookActionUpdated {
		t.Fatalf("post-merge action = %q, want %q", result.Hooks[index].Action, HookActionUpdated)
	}
	if got, err := os.Readlink(hookPath); err != nil || got != hookDispatcherName() {
		t.Fatalf("post-merge symlink = %q, err=%v; want %q", got, err, hookDispatcherName())
	}
	if _, err := os.Stat(hookPath); err != nil {
		t.Fatalf("stat refreshed hook: %v", err)
	}
	testhelpers.AssertExecutableScript(t, hookPath)
}

func TestInstallLifecycleHooksRefreshPreservesUnrelatedStagingFile(t *testing.T) {
	repo := initGitRepo(t)
	hooksDir := filepath.Join(repo, ".git", "hooks")
	hookPath := filepath.Join(hooksDir, "post-merge")
	staleContent := ManagedHookMarker + "\n# stale wrapper from an older Liza release\n"
	if err := os.WriteFile(hookPath, []byte(staleContent), 0644); err != nil {
		t.Fatalf("write stale managed hook: %v", err)
	}

	unrelatedStaged := hookPath + ".tmp"
	unrelatedContent := "not managed by liza\n"
	if err := os.WriteFile(unrelatedStaged, []byte(unrelatedContent), 0644); err != nil {
		t.Fatalf("write unrelated %s: %v", unrelatedStaged, err)
	}

	result, err := InstallLifecycleHooks(InstallHooksOptions{RepoRoot: repo})
	if err != nil {
		t.Fatalf("InstallLifecycleHooks() error = %v", err)
	}

	index := slices.IndexFunc(result.Hooks, func(got HookInstallResult) bool {
		return got.Hook == "post-merge"
	})
	if index == -1 {
		t.Fatalf("missing post-merge result: %#v", result.Hooks)
	}
	if result.Hooks[index].Action != HookActionUpdated {
		t.Fatalf("post-merge action = %q, want %q", result.Hooks[index].Action, HookActionUpdated)
	}
	got := readFile(t, unrelatedStaged)
	if got != unrelatedContent {
		t.Fatalf("unrelated staging file %s was modified: got %q, want %q", unrelatedStaged, got, unrelatedContent)
	}
}

func TestManagedHookDispatcherInvokesLocalIndexScriptWithoutLifecycleArguments(t *testing.T) {
	repo := initGitRepo(t)
	result, err := InstallLifecycleHooks(InstallHooksOptions{
		RepoRoot: repo,
		Hooks:    []string{"post-rewrite"},
	})
	if err != nil {
		t.Fatalf("InstallLifecycleHooks() error = %v", err)
	}
	logPath := filepath.Join(t.TempDir(), "args.log")
	scriptPath := filepath.Join(result.HooksDir, brand.BinaryName+"-index.sh")
	script := "#!/bin/sh\nprintf 'args:%s\\n' \"$*\" > \"$LIZA_TEST_HOOK_LOG\"\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0755); err != nil {
		t.Fatalf("write liza-index.sh fixture: %v", err)
	}

	cmd := exec.Command(filepath.Join(result.HooksDir, "post-rewrite"), "rebase", "amend")
	cmd.Dir = repo
	cmd.Env = append(os.Environ(), "LIZA_TEST_HOOK_LOG="+logPath)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("post-rewrite hook failed: %v\n%s", err, output)
	}

	args := readFile(t, logPath)
	if args != "args:\n" {
		t.Fatalf("liza-index.sh args = %q, want no lifecycle arguments", args)
	}
	if strings.Contains(args, "ai") {
		t.Fatalf("automatic lifecycle wrapper must not request AI summary: %q", args)
	}
}

func TestManagedHookDispatcherSkipsPostCheckoutFileCheckout(t *testing.T) {
	repo := initGitRepo(t)
	result, err := InstallLifecycleHooks(InstallHooksOptions{
		RepoRoot: repo,
		Hooks:    []string{"post-checkout"},
	})
	if err != nil {
		t.Fatalf("InstallLifecycleHooks() error = %v", err)
	}
	logPath := filepath.Join(t.TempDir(), "args.log")
	scriptPath := filepath.Join(result.HooksDir, brand.BinaryName+"-index.sh")
	script := "#!/bin/sh\nprintf 'ran\\n' > \"$LIZA_TEST_HOOK_LOG\"\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0755); err != nil {
		t.Fatalf("write liza-index.sh fixture: %v", err)
	}

	cmd := exec.Command(filepath.Join(result.HooksDir, "post-checkout"), "old", "new", "0")
	cmd.Dir = repo
	cmd.Env = append(os.Environ(), "LIZA_TEST_HOOK_LOG="+logPath)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("post-checkout hook failed: %v\n%s", err, output)
	}
	if _, err := os.Stat(logPath); !os.IsNotExist(err) {
		t.Fatalf("liza-index.sh ran for file checkout; stat err=%v", err)
	}
}

func TestManagedHookWrapperPassesHookNameToDispatcherForPostCheckoutFileCheckout(t *testing.T) {
	repo := initGitRepo(t)
	hooksDir := filepath.Join(repo, ".git", "hooks")
	if err := os.MkdirAll(hooksDir, 0755); err != nil {
		t.Fatalf("create hooks dir: %v", err)
	}

	dispatcherPath := filepath.Join(hooksDir, hookDispatcherName())
	if err := os.WriteFile(dispatcherPath, []byte(managedHookDispatcherContent()), 0755); err != nil {
		t.Fatalf("write dispatcher fixture: %v", err)
	}

	logPath := filepath.Join(t.TempDir(), "args.log")
	scriptPath := filepath.Join(hooksDir, scriptName())
	script := "#!/bin/sh\nprintf 'ran\\n' > \"$LIZA_TEST_HOOK_LOG\"\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0755); err != nil {
		t.Fatalf("write liza-index.sh fixture: %v", err)
	}

	wrapper := managedHookContent("post-checkout")
	if !strings.Contains(wrapper, hookNameEnvVar+"="+shellQuote("post-checkout")) {
		t.Fatalf("wrapper does not pass %s to the dispatcher:\n%s", hookNameEnvVar, wrapper)
	}
	wrapperPath := filepath.Join(hooksDir, "post-checkout")
	if err := os.WriteFile(wrapperPath, []byte(wrapper), 0755); err != nil {
		t.Fatalf("write wrapper fixture: %v", err)
	}

	runWrapper := func(flag string) {
		t.Helper()
		cmd := exec.Command(wrapperPath, "old", "new", flag)
		cmd.Dir = repo
		cmd.Env = append(os.Environ(), "LIZA_TEST_HOOK_LOG="+logPath)
		if output, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("wrapper hook (flag=%s) failed: %v\n%s", flag, err, output)
		}
	}

	runWrapper("0")
	if _, err := os.Stat(logPath); !os.IsNotExist(err) {
		t.Fatalf("liza-index.sh ran through the wrapper for a file checkout; stat err=%v", err)
	}

	runWrapper("1")
	if _, err := os.Stat(logPath); err != nil {
		t.Fatalf("liza-index.sh did not run through the wrapper for a branch checkout: %v", err)
	}
}

func TestRenderIndexScriptUsesLegacyStacklitRefreshWithoutAutomaticAISummary(t *testing.T) {
	repo := initGitRepo(t)

	script, err := RenderIndexScript(repo)
	if err != nil {
		t.Fatalf("RenderIndexScript() error = %v", err)
	}

	if !strings.Contains(script, ManagedIndexScriptMarker) {
		t.Fatalf("script missing managed marker:\n%s", script)
	}
	if !strings.Contains(script, "stacklit diff -i stacklit.json") {
		t.Fatalf("script missing Stacklit diff short-circuit:\n%s", script)
	}
	if !strings.Contains(script, "stacklit generate-json -o stacklit.json --parse-workers 3") {
		t.Fatalf("script missing no-AI Stacklit generation command:\n%s", script)
	}
	if !strings.Contains(script, "stacklit init-insights -i stacklit.json -o stacklit-insights.json") {
		t.Fatalf("script missing Stacklit insights initialization command:\n%s", script)
	}
	if !strings.Contains(script, "stacklit ai-summary") {
		t.Fatalf("script missing manual AI-summary command:\n%s", script)
	}
	if strings.Contains(script, "stacklit generate-json -o stacklit.json --ai") {
		t.Fatalf("automatic Stacklit generation command includes AI flag:\n%s", script)
	}
}

func TestInstallActivationWritesScipCommandsWithoutStacklit(t *testing.T) {
	repo := initGitRepo(t)
	outputPath := filepath.Join(repo, "go.scip")

	result, err := InstallActivation(InstallActivationOptions{
		RepoRoot:  repo,
		Hooks:     []string{"post-commit"},
		ScipPlans: []scipsearch.LanguageAggregatePlan{goAggregatePlan(repo, outputPath)},
	})
	if err != nil {
		t.Fatalf("InstallActivation() error = %v", err)
	}

	script := readFile(t, result.Script.Path)
	if strings.Contains(script, "stacklit generate-json") {
		t.Fatalf("script contains Stacklit command despite Stacklit being disabled:\n%s", script)
	}
	for _, want := range []string{"scip-go index --module-root", repo, "--output", outputPath} {
		if !strings.Contains(script, want) {
			t.Fatalf("script missing %q:\n%s", want, script)
		}
	}
	hook := readFile(t, filepath.Join(result.HooksDir, "post-commit"))
	if !strings.Contains(hook, ManagedHookMarker) || !strings.Contains(hook, brand.BinaryName+"-index.sh") {
		t.Fatalf("post-commit hook missing managed wrapper:\n%s", hook)
	}
	if got := runGitOutput(t, repo, "check-ignore", "go.scip"); got != "go.scip" {
		t.Fatalf("git check-ignore go.scip = %q, want private exclude", got)
	}
}

func TestInstallActivationOmitsFunctionalClustersWhenStacklitDisabled(t *testing.T) {
	repo := initGitRepo(t)
	outputPath := filepath.Join(repo, "go.scip")

	result, err := InstallActivation(InstallActivationOptions{
		RepoRoot:                 repo,
		Hooks:                    []string{"post-commit"},
		EnableFunctionalClusters: true,
		ScipPlans:                []scipsearch.LanguageAggregatePlan{goAggregatePlan(repo, outputPath)},
	})
	if err != nil {
		t.Fatalf("InstallActivation() error = %v", err)
	}

	script := readFile(t, result.Script.Path)
	if strings.Contains(script, "functional-clusters build") ||
		strings.Contains(script, "stacklit export-architecture") ||
		strings.Contains(script, "scip-search graph-export") {
		t.Fatalf("script contains Functional Clusters commands despite Stacklit being disabled:\n%s", script)
	}
}

func TestInstallIndexScriptWritesExecutableManagedScript(t *testing.T) {
	repo := initGitRepo(t)

	result, err := InstallIndexScript(InstallIndexScriptOptions{RepoRoot: repo})
	if err != nil {
		t.Fatalf("InstallIndexScript() error = %v", err)
	}

	wantPath := filepath.Join(repo, ".git", "hooks", brand.BinaryName+"-index.sh")
	if result.Path != wantPath {
		t.Fatalf("script path = %q, want %q", result.Path, wantPath)
	}
	if result.Action != HookActionInstalled {
		t.Fatalf("script action = %q, want %q", result.Action, HookActionInstalled)
	}
	if _, err := os.Stat(wantPath); err != nil {
		t.Fatalf("installed script missing: %v", err)
	}
	testhelpers.AssertExecutableScript(t, wantPath)
	if got := readFile(t, wantPath); !strings.Contains(got, ManagedIndexScriptMarker) {
		t.Fatalf("installed script missing managed marker:\n%s", got)
	}
}

func TestInstallIndexScriptUpdatesLegacyManagedScript(t *testing.T) {
	repo := initGitRepo(t)
	scriptPath := filepath.Join(repo, ".git", "hooks", "liza-index.sh")
	legacyScript := "#!/bin/sh\n" + legacyManagedIndexScriptMarker + "\necho stale\n"
	if err := os.WriteFile(scriptPath, []byte(legacyScript), 0644); err != nil {
		t.Fatalf("write legacy managed script: %v", err)
	}

	result, err := InstallIndexScript(InstallIndexScriptOptions{RepoRoot: repo})
	if err != nil {
		t.Fatalf("InstallIndexScript() error = %v", err)
	}
	if result.Action != HookActionUpdated {
		t.Fatalf("script action = %q, want %q", result.Action, HookActionUpdated)
	}
	updated := readFile(t, scriptPath)
	if !strings.Contains(updated, ManagedIndexScriptMarker) {
		t.Fatalf("updated script missing new managed marker:\n%s", updated)
	}
	if strings.Contains(updated, legacyManagedIndexScriptMarker) {
		t.Fatalf("updated script retained legacy marker:\n%s", updated)
	}
	if _, err := os.Stat(scriptPath); err != nil {
		t.Fatalf("updated script missing: %v", err)
	}
	testhelpers.AssertExecutableScript(t, scriptPath)
}

func TestInstalledIndexScriptRefreshesStacklitJSONWithoutAIByDefault(t *testing.T) {
	repo := initGitRepo(t)
	result, err := InstallIndexScript(InstallIndexScriptOptions{RepoRoot: repo})
	if err != nil {
		t.Fatalf("InstallIndexScript() error = %v", err)
	}
	logPath := filepath.Join(t.TempDir(), "stacklit.log")
	pathDir := writeFakeStacklit(t)

	cmd := exec.Command(result.Path)
	cmd.Env = append(os.Environ(),
		"PATH="+pathDir+string(os.PathListSeparator)+os.Getenv("PATH"),
		"LIZA_TEST_STACKLIT_LOG="+logPath,
	)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("liza-index.sh failed: %v\n%s", err, output)
	}

	want := "generate-json -o stacklit.json --parse-workers 3\ninit-insights -i stacklit.json -o stacklit-insights.json\ngenerate-json -o stacklit.json --parse-workers 3\n"
	if got := readFile(t, logPath); got != want {
		t.Fatalf("stacklit calls = %q, want no-AI generate-json only", got)
	}
	if got := readFile(t, filepath.Join(repo, "stacklit.json")); got != "generated index\n" {
		t.Fatalf("stacklit.json = %q, want generated index", got)
	}
	if got := runGitOutput(t, repo, "status", "--porcelain"); got != "" {
		t.Fatalf("git status --porcelain = %q, want clean generated Stacklit artifact", got)
	}
	if got := runGitOutput(t, repo, "check-ignore", "stacklit.json"); got != "stacklit.json" {
		t.Fatalf("git check-ignore stacklit.json = %q, want private exclude", got)
	}
}

func TestInstallIndexScriptAllowsTrackedStacklitJSON(t *testing.T) {
	repo := initGitRepo(t)
	writeFile(t, filepath.Join(repo, "stacklit.json"), "tracked index\n", 0644)
	commitPath(t, repo, "stacklit.json", "Add tracked Stacklit index")

	result, err := InstallIndexScript(InstallIndexScriptOptions{RepoRoot: repo})
	if err != nil {
		t.Fatalf("InstallIndexScript() error = %v", err)
	}
	runInstalledIndexScript(t, result.Path)

	status := runGitOutput(t, repo, "status", "--porcelain")
	if strings.Contains(status, "?? stacklit.json") {
		t.Fatalf("git status --porcelain = %q, want tracked Stacklit index, not accidental untracked file", status)
	}
	if got := readFile(t, filepath.Join(repo, "stacklit.json")); got != "generated index\n" {
		t.Fatalf("stacklit.json = %q, want generated index", got)
	}
}

func TestInstallIndexScriptAllowsIgnoredStacklitJSON(t *testing.T) {
	repo := initGitRepo(t)
	writeFile(t, filepath.Join(repo, ".gitignore"), "stacklit.json\n", 0644)
	commitPath(t, repo, ".gitignore", "Ignore Stacklit index")

	result, err := InstallIndexScript(InstallIndexScriptOptions{RepoRoot: repo})
	if err != nil {
		t.Fatalf("InstallIndexScript() error = %v", err)
	}
	runInstalledIndexScript(t, result.Path)

	if got := runGitOutput(t, repo, "status", "--porcelain"); got != "" {
		t.Fatalf("git status --porcelain = %q, want ignored generated Stacklit artifact", got)
	}
}

func TestInstallIndexScriptAllowsPrivatelyExcludedStacklitJSON(t *testing.T) {
	repo := initGitRepo(t)
	writeFile(t, filepath.Join(repo, ".git", "info", "exclude"), "stacklit.json\n", 0644)

	result, err := InstallIndexScript(InstallIndexScriptOptions{RepoRoot: repo})
	if err != nil {
		t.Fatalf("InstallIndexScript() error = %v", err)
	}
	runInstalledIndexScript(t, result.Path)

	if got := runGitOutput(t, repo, "status", "--porcelain"); got != "" {
		t.Fatalf("git status --porcelain = %q, want privately excluded generated Stacklit artifact", got)
	}
}

func TestInstallIndexScriptRejectsUnsafeUntrackedStacklitJSON(t *testing.T) {
	repo := initGitRepo(t)
	writeFile(t, filepath.Join(repo, "stacklit.json"), "unsafe untracked index\n", 0644)

	_, err := InstallIndexScript(InstallIndexScriptOptions{RepoRoot: repo})
	if err == nil {
		t.Fatal("InstallIndexScript() error = nil, want unsafe stacklit.json diagnostic")
	}
	if !strings.Contains(err.Error(), "stacklit.json is untracked") {
		t.Fatalf("error = %v, want unsafe untracked stacklit.json diagnostic", err)
	}
	if _, statErr := os.Stat(filepath.Join(repo, ".git", "hooks", brand.BinaryName+"-index.sh")); statErr == nil {
		t.Fatal(brand.BinaryName + "-index.sh installed despite unsafe untracked stacklit.json")
	} else if !os.IsNotExist(statErr) {
		t.Fatalf("inspect liza-index.sh: %v", statErr)
	}
}

func TestInstalledIndexScriptManualAIArgumentRunsAISummary(t *testing.T) {
	repo := initGitRepo(t)
	result, err := InstallIndexScript(InstallIndexScriptOptions{RepoRoot: repo})
	if err != nil {
		t.Fatalf("InstallIndexScript() error = %v", err)
	}
	logPath := filepath.Join(t.TempDir(), "stacklit.log")
	pathDir := writeFakeStacklit(t)

	cmd := exec.Command(result.Path, "ai")
	cmd.Env = append(os.Environ(),
		"PATH="+pathDir+string(os.PathListSeparator)+os.Getenv("PATH"),
		"LIZA_TEST_STACKLIT_LOG="+logPath,
	)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("liza-index.sh ai failed: %v\n%s", err, output)
	}

	want := "generate-json -o stacklit.json --parse-workers 3\ninit-insights -i stacklit.json -o stacklit-insights.json\nai-summary\ngenerate-json -o stacklit.json --parse-workers 3\n"
	if got := readFile(t, logPath); got != want {
		t.Fatalf("stacklit calls = %q, want %q", got, want)
	}
}

func TestInstalledIndexScriptSkipsStacklitRefreshWhenDiffReportsNoChanges(t *testing.T) {
	repo := initGitRepo(t)
	writeFile(t, filepath.Join(repo, "stacklit.json"), "tracked index\n", 0644)
	commitPath(t, repo, "stacklit.json", "Add tracked Stacklit index")
	result, err := InstallIndexScript(InstallIndexScriptOptions{RepoRoot: repo})
	if err != nil {
		t.Fatalf("InstallIndexScript() error = %v", err)
	}
	logPath := filepath.Join(t.TempDir(), "stacklit.log")
	pathDir := writeFakeStacklit(t)

	cmd := exec.Command(result.Path)
	cmd.Env = append(os.Environ(),
		"PATH="+pathDir+string(os.PathListSeparator)+os.Getenv("PATH"),
		"LIZA_TEST_STACKLIT_LOG="+logPath,
		"LIZA_TEST_STACKLIT_DIFF_EXIT=0",
	)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("liza-index.sh failed: %v\n%s", err, output)
	}

	if got := readFile(t, logPath); got != "diff -i stacklit.json\n" {
		t.Fatalf("stacklit calls = %q, want diff-only short-circuit", got)
	}
	if got := readFile(t, filepath.Join(repo, "stacklit.json")); got != "tracked index\n" {
		t.Fatalf("stacklit.json = %q, want unchanged index", got)
	}
}

func TestInstalledIndexScriptSkipsFreshScipIndexAndRefreshesWhenSourceIsNewer(t *testing.T) {
	repo := initGitRepo(t)
	sourcePath := filepath.Join(repo, "main.go")
	writeFile(t, sourcePath, "package main\n", 0644)
	outputPath := filepath.Join(repo, "go.scip")
	result, err := InstallIndexScript(InstallIndexScriptOptions{
		RepoRoot:        repo,
		DisableStacklit: true,
		ScipPlans:       []scipsearch.LanguageAggregatePlan{goAggregatePlan(repo, outputPath)},
	})
	if err != nil {
		t.Fatalf("InstallIndexScript() error = %v", err)
	}
	logPath := filepath.Join(t.TempDir(), "scip.log")
	pathDir := writeFakeScipGo(t)

	runIndexScriptWithPath(t, result.Path, pathDir, "LIZA_TEST_SCIP_LOG="+logPath)
	if got := readFile(t, logPath); !containsAll(got,
		"scip-go index --module-root "+repo+" --output ",
		"scip-search aggregate-index --project-root "+repo+" --root . --index ",
		" --out "+outputPath,
	) {
		t.Fatalf("first SCIP calls = %q", got)
	}

	writeFile(t, logPath, "", 0644)
	runIndexScriptWithPath(t, result.Path, pathDir, "LIZA_TEST_SCIP_LOG="+logPath)
	if got := readFile(t, logPath); got != "" {
		t.Fatalf("fresh SCIP calls = %q, want skipped", got)
	}

	newer := time.Now().Add(10 * time.Second)
	if err := os.Chtimes(sourcePath, newer, newer); err != nil {
		t.Fatalf("Chtimes(source) error = %v", err)
	}
	runIndexScriptWithPath(t, result.Path, pathDir, "LIZA_TEST_SCIP_LOG="+logPath)
	if got := readFile(t, logPath); !containsAll(got,
		"scip-go index --module-root "+repo+" --output ",
		"scip-search aggregate-index --project-root "+repo+" --root . --index ",
		" --out "+outputPath,
	) {
		t.Fatalf("stale SCIP calls = %q, want refresh", got)
	}
}

func TestInstalledIndexScriptUsesScipCommandRootForFreshness(t *testing.T) {
	repo := initGitRepo(t)
	webSrc := filepath.Join(repo, "apps", "web", "src")
	infraSrc := filepath.Join(repo, "infra", "cdk")
	if err := os.MkdirAll(webSrc, 0755); err != nil {
		t.Fatalf("MkdirAll(webSrc) error = %v", err)
	}
	if err := os.MkdirAll(infraSrc, 0755); err != nil {
		t.Fatalf("MkdirAll(infraSrc) error = %v", err)
	}
	webSource := filepath.Join(webSrc, "App.tsx")
	infraSource := filepath.Join(infraSrc, "app.ts")
	writeFile(t, webSource, "export const app = 1\n", 0644)
	writeFile(t, infraSource, "export const cdk = 1\n", 0644)
	outputPath := filepath.Join(repo, "typescript.scip")
	result, err := InstallIndexScript(InstallIndexScriptOptions{
		RepoRoot:        repo,
		DisableStacklit: true,
		ScipPlans:       []scipsearch.LanguageAggregatePlan{typescriptAggregatePlan(repo, outputPath, webSrc, filepath.Join(repo, "apps", "web"))},
	})
	if err != nil {
		t.Fatalf("InstallIndexScript() error = %v", err)
	}
	logPath := filepath.Join(t.TempDir(), "scip.log")
	pathDir := writeFakeScipTypeScript(t)

	runIndexScriptWithPath(t, result.Path, pathDir, "LIZA_TEST_SCIP_LOG="+logPath)
	writeFile(t, logPath, "", 0644)
	newer := time.Now().Add(10 * time.Second)
	if err := os.Chtimes(infraSource, newer, newer); err != nil {
		t.Fatalf("Chtimes(infraSource) error = %v", err)
	}
	runIndexScriptWithPath(t, result.Path, pathDir, "LIZA_TEST_SCIP_LOG="+logPath)
	if got := readFile(t, logPath); got != "" {
		t.Fatalf("unrelated TypeScript source triggered refresh: %q", got)
	}

	if err := os.Chtimes(webSource, newer.Add(10*time.Second), newer.Add(10*time.Second)); err != nil {
		t.Fatalf("Chtimes(webSource) error = %v", err)
	}
	runIndexScriptWithPath(t, result.Path, pathDir, "LIZA_TEST_SCIP_LOG="+logPath)
	if got := readFile(t, logPath); !containsAll(got,
		"scip-typescript index --cwd "+webSrc+" --output ",
		" "+filepath.Join(repo, "apps", "web"),
		"scip-search aggregate-index --project-root "+repo+" --root apps/web/src --index ",
		" --out "+outputPath,
	) {
		t.Fatalf("stale TypeScript source calls = %q, want refresh", got)
	}
}

func TestInstalledIndexScriptAggregatesSuccessfulTypeScriptRootsWhenAnotherFails(t *testing.T) {
	repo := initGitRepo(t)
	workingSrc := filepath.Join(repo, "apps", "web", "src")
	failingSrc := filepath.Join(repo, "apps", "mobile", "src")
	if err := os.MkdirAll(workingSrc, 0755); err != nil {
		t.Fatalf("MkdirAll(%q) error = %v", workingSrc, err)
	}
	if err := os.MkdirAll(failingSrc, 0755); err != nil {
		t.Fatalf("MkdirAll(%q) error = %v", failingSrc, err)
	}
	writeFile(t, filepath.Join(workingSrc, "App.tsx"), "export const app = 1\n", 0644)
	writeFile(t, filepath.Join(failingSrc, "App.tsx"), "export const app = 2\n", 0644)
	outputPath := filepath.Join(repo, "typescript.scip")
	plan := typescriptAggregatePlan(repo, outputPath, workingSrc, filepath.Join(repo, "apps", "web"))
	plan.IndexPlans = append(plan.IndexPlans, scipsearch.RuntimeCommandPlan{
		Language:   "typescript",
		Name:       "scip-typescript",
		Args:       []string{"index", "--cwd", failingSrc, "--output", "__LIZA_SCIP_OUTPUT__", filepath.Join(repo, "apps", "mobile")},
		Dir:        repo,
		OutputPath: "__LIZA_SCIP_OUTPUT__",
		Root:       "apps/mobile/src",
	})
	result, err := InstallIndexScript(InstallIndexScriptOptions{
		RepoRoot:        repo,
		DisableStacklit: true,
		ScipPlans:       []scipsearch.LanguageAggregatePlan{plan},
	})
	if err != nil {
		t.Fatalf("InstallIndexScript() error = %v", err)
	}
	logPath := filepath.Join(t.TempDir(), "scip.log")
	pathDir := writeFakeScipTypeScript(t)

	runIndexScriptWithPath(t, result.Path, pathDir,
		"LIZA_TEST_SCIP_LOG="+logPath,
		"LIZA_TEST_SCIP_TYPESCRIPT_FAIL_ROOT="+filepath.Join(repo, "apps", "mobile"),
	)

	if got := readFile(t, logPath); !containsAll(got,
		"scip-typescript index --cwd "+workingSrc,
		"scip-typescript index --cwd "+failingSrc,
		"scip-search aggregate-index --project-root "+repo+" --root apps/web/src --index ",
	) {
		t.Fatalf("SCIP calls = %q, want the successful TypeScript root aggregated", got)
	} else if strings.Contains(got, "--root apps/mobile/src --index") {
		t.Fatalf("SCIP calls = %q, want the failed TypeScript root excluded from aggregation", got)
	}
	if got := readFile(t, outputPath); got != "aggregated scip index\n" {
		t.Fatalf("typescript.scip = %q, want aggregate from successful root", got)
	}
}

func TestInstalledIndexScriptRefreshesFunctionalClustersJSON(t *testing.T) {
	repo := initGitRepo(t)
	sourcePath := filepath.Join(repo, "main.go")
	writeFile(t, sourcePath, "package main\n", 0644)
	commitPath(t, repo, "main.go", "Add Go source")
	outputPath := filepath.Join(repo, "go.scip")
	result, err := InstallIndexScript(InstallIndexScriptOptions{
		RepoRoot:                 repo,
		EnableFunctionalClusters: true,
		ScipPlans:                []scipsearch.LanguageAggregatePlan{goAggregatePlan(repo, outputPath)},
	})
	if err != nil {
		t.Fatalf("InstallIndexScript() error = %v", err)
	}
	stacklitLogPath := filepath.Join(t.TempDir(), "stacklit.log")
	scipLogPath := filepath.Join(t.TempDir(), "scip.log")
	functionalClustersLogPath := filepath.Join(t.TempDir(), "functional-clusters.log")
	stacklitDir := writeFakeStacklit(t)
	scipDir := writeFakeScipGo(t)
	functionalClustersDir := writeFakeFunctionalClusters(t)

	cmd := exec.Command(result.Path)
	cmd.Dir = t.TempDir()
	cmd.Env = append(os.Environ(),
		"PATH="+stacklitDir+string(os.PathListSeparator)+scipDir+string(os.PathListSeparator)+functionalClustersDir+string(os.PathListSeparator)+os.Getenv("PATH"),
		"LIZA_TEST_STACKLIT_LOG="+stacklitLogPath,
		"LIZA_TEST_SCIP_LOG="+scipLogPath,
		"LIZA_TEST_FUNCTIONAL_CLUSTERS_LOG="+functionalClustersLogPath,
	)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("liza-index.sh failed: %v\n%s", err, output)
	}

	if got := readFile(t, stacklitLogPath); !containsAll(got,
		"generate-json -o stacklit.json --parse-workers 3",
		"export-architecture -i stacklit.json -o ",
		"stacklit-architecture.json",
	) {
		t.Fatalf("stacklit calls = %q, want generate plus architecture export", got)
	}
	if got := readFile(t, scipLogPath); !containsAll(got,
		"scip-go index --module-root "+repo+" --output ",
		"scip-search aggregate-index --project-root "+repo+" --root . --index ",
		"scip-search graph-export --index "+outputPath+" -o ",
		"go-scip-graph.json",
	) {
		t.Fatalf("scip calls = %q, want index plus graph export", got)
	}
	if got := readFile(t, functionalClustersLogPath); !containsAll(got,
		"functional-clusters build --scip-graph ",
		"go-scip-graph.json",
		" --stacklit-architecture ",
		"stacklit-architecture.json",
		" -o functional-clusters.json",
	) {
		t.Fatalf("functional-clusters calls = %q, want build from exports", got)
	}
	if got := readFile(t, filepath.Join(repo, "functional-clusters.json")); got != "generated functional clusters\n" {
		t.Fatalf("functional-clusters.json = %q, want generated artifact", got)
	}
	if got := runGitOutput(t, repo, "status", "--porcelain"); got != "" {
		t.Fatalf("git status --porcelain = %q, want clean generated artifacts", got)
	}
	if got := runGitOutput(t, repo, "check-ignore", "functional-clusters.json"); got != "functional-clusters.json" {
		t.Fatalf("git check-ignore functional-clusters.json = %q, want private exclude", got)
	}
}

func TestManagedLifecycleHookInvokesInstalledIndexScriptWithoutAI(t *testing.T) {
	repo := initGitRepo(t)
	hookResult, err := InstallLifecycleHooks(InstallHooksOptions{
		RepoRoot: repo,
		Hooks:    []string{"post-commit"},
	})
	if err != nil {
		t.Fatalf("InstallLifecycleHooks() error = %v", err)
	}
	if _, err := InstallIndexScript(InstallIndexScriptOptions{RepoRoot: repo}); err != nil {
		t.Fatalf("InstallIndexScript() error = %v", err)
	}
	logPath := filepath.Join(t.TempDir(), "stacklit.log")
	pathDir := writeFakeStacklit(t)

	cmd := exec.Command(filepath.Join(hookResult.HooksDir, "post-commit"))
	cmd.Dir = repo
	cmd.Env = append(os.Environ(),
		"PATH="+pathDir+string(os.PathListSeparator)+os.Getenv("PATH"),
		"LIZA_TEST_STACKLIT_LOG="+logPath,
	)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("post-commit hook failed: %v\n%s", err, output)
	}

	want := "generate-json -o stacklit.json --parse-workers 3\ninit-insights -i stacklit.json -o stacklit-insights.json\ngenerate-json -o stacklit.json --parse-workers 3\n"
	if got := readFile(t, logPath); got != want {
		t.Fatalf("lifecycle stacklit calls = %q, want no-AI generate-json only", got)
	}
}

func TestInstallLifecycleHooksCreatesMissingHooksDirectory(t *testing.T) {
	repo := initGitRepo(t)
	hooksDir := filepath.Join(repo, ".githooks", "nested")
	runGit(t, repo, "config", "core.hooksPath", ".githooks/nested")

	result, err := InstallLifecycleHooks(InstallHooksOptions{RepoRoot: repo})
	if err != nil {
		t.Fatalf("InstallLifecycleHooks() error = %v", err)
	}

	if result.HooksDir != hooksDir {
		t.Fatalf("HooksDir = %q, want %q", result.HooksDir, hooksDir)
	}
	for _, hook := range DefaultLifecycleHooks() {
		if _, err := os.Stat(filepath.Join(hooksDir, hook)); err != nil {
			t.Fatalf("hook %s missing from created hooks directory: %v", hook, err)
		}
	}
}

func TestInstallLifecycleHooksRejectsUnsafeHooksPathFile(t *testing.T) {
	repo := initGitRepo(t)
	hooksFile := filepath.Join(repo, ".git", "hooks-file")
	if err := os.WriteFile(hooksFile, []byte("not a directory\n"), 0644); err != nil {
		t.Fatalf("write hooks file: %v", err)
	}
	runGit(t, repo, "config", "core.hooksPath", hooksFile)

	_, err := InstallLifecycleHooks(InstallHooksOptions{RepoRoot: repo})
	if err == nil {
		t.Fatal("InstallLifecycleHooks() error = nil, want unsafe hooks path error")
	}
	if !strings.Contains(err.Error(), "not a directory") {
		t.Fatalf("error = %v, want not-a-directory diagnostic", err)
	}
}

func TestInstallLifecycleHooksReportsExistingHookCollision(t *testing.T) {
	repo := initGitRepo(t)
	hooksDir := filepath.Join(repo, ".git", "hooks")
	collidingHook := filepath.Join(hooksDir, "post-commit")
	if err := os.WriteFile(collidingHook, []byte("#!/bin/sh\necho user hook\n"), 0755); err != nil {
		t.Fatalf("write colliding hook: %v", err)
	}

	_, err := InstallLifecycleHooks(InstallHooksOptions{RepoRoot: repo})
	if err == nil {
		t.Fatal("InstallLifecycleHooks() error = nil, want collision")
	}
	var collision *HookCollisionError
	if !errors.As(err, &collision) {
		t.Fatalf("error type = %T, want *HookCollisionError: %v", err, err)
	}
	if len(collision.Collisions) != 1 {
		t.Fatalf("collision count = %d, want 1", len(collision.Collisions))
	}
	if collision.Collisions[0].Hook != "post-commit" || collision.Collisions[0].Path != collidingHook {
		t.Fatalf("collision = %#v, want post-commit at %s", collision.Collisions[0], collidingHook)
	}
	if !strings.Contains(err.Error(), "post-commit") || !strings.Contains(err.Error(), "not "+brand.NameTitle+"-managed") {
		t.Fatalf("error = %v, want explicit hook collision diagnostic", err)
	}
	if _, err := os.Stat(filepath.Join(hooksDir, "post-checkout")); err == nil {
		t.Fatal("preflight should not install other hooks after detecting a collision")
	} else if !os.IsNotExist(err) {
		t.Fatalf("inspect post-checkout: %v", err)
	}
}

func assertHookActions(t *testing.T, result InstallHooksResult, action HookAction) {
	t.Helper()
	lifecycleHooks := DefaultLifecycleHooks()
	if len(result.Hooks) != len(lifecycleHooks) {
		t.Fatalf("hook results = %d, want %d", len(result.Hooks), len(lifecycleHooks))
	}
	for _, hook := range lifecycleHooks {
		index := slices.IndexFunc(result.Hooks, func(got HookInstallResult) bool {
			return got.Hook == hook
		})
		if index == -1 {
			t.Fatalf("missing result for hook %s: %#v", hook, result.Hooks)
		}
		if result.Hooks[index].Action != action {
			t.Fatalf("%s action = %q, want %q", hook, result.Hooks[index].Action, action)
		}
	}
}

func initGitRepo(t *testing.T) string {
	t.Helper()

	repo := t.TempDir()
	runGit(t, repo, "init")
	return repo
}

func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()

	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s in %s failed: %v\n%s", strings.Join(args, " "), dir, err, output)
	}
}

func runGitOutput(t *testing.T, dir string, args ...string) string {
	t.Helper()

	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s in %s failed: %v\n%s", strings.Join(args, " "), dir, err, output)
	}
	return strings.TrimSpace(string(output))
}

func commitPath(t *testing.T, repo, path, message string) {
	t.Helper()

	runGit(t, repo, "config", "user.email", "test@example.invalid")
	runGit(t, repo, "config", "user.name", "Liza Test")
	runGit(t, repo, "add", path)
	runGit(t, repo, "commit", "-m", message)
}

func readFile(t *testing.T, path string) string {
	t.Helper()

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile(%q) error = %v", path, err)
	}
	return string(data)
}

func containsAll(content string, needles ...string) bool {
	for _, needle := range needles {
		if !strings.Contains(content, needle) {
			return false
		}
	}
	return true
}

func writeFile(t *testing.T, path, content string, mode os.FileMode) {
	t.Helper()

	if err := os.WriteFile(path, []byte(content), mode); err != nil {
		t.Fatalf("WriteFile(%q) error = %v", path, err)
	}
}

func runIndexScriptWithPath(t *testing.T, scriptPath, pathDir string, extraEnv ...string) {
	t.Helper()

	cmd := exec.Command(scriptPath)
	cmd.Env = append(os.Environ(), "PATH="+pathDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	cmd.Env = append(cmd.Env, extraEnv...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%s failed: %v\n%s", scriptPath, err, output)
	}
}

func runInstalledIndexScript(t *testing.T, scriptPath string) {
	t.Helper()

	logPath := filepath.Join(t.TempDir(), "stacklit.log")
	pathDir := writeFakeStacklit(t)
	cmd := exec.Command(scriptPath)
	cmd.Env = append(os.Environ(),
		"PATH="+pathDir+string(os.PathListSeparator)+os.Getenv("PATH"),
		"LIZA_TEST_STACKLIT_LOG="+logPath,
	)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("liza-index.sh failed: %v\n%s", err, output)
	}
}

func writeFakeStacklit(t *testing.T) string {
	t.Helper()

	dir := t.TempDir()
	path := filepath.Join(dir, "stacklit")
	script := `#!/bin/sh
	printf '%s\n' "$*" >> "$LIZA_TEST_STACKLIT_LOG"
	if [ "$1" = "diff" ]; then
		exit "${LIZA_TEST_STACKLIT_DIFF_EXIT:-1}"
	fi
	if [ "$1" = "generate-json" ]; then
		printf '%s\n' "generated index" > "$PWD/stacklit.json"
	fi
	if [ "$1" = "init-insights" ]; then
		printf '%s\n' "generated insights" > "$PWD/stacklit-insights.json"
	fi
	if [ "$1" = "export-architecture" ]; then
		output=""
		while [ "$#" -gt 0 ]; do
			if [ "$1" = "-o" ]; then
				shift
				output="$1"
			fi
			shift
		done
		if [ -n "$output" ]; then
			printf '%s\n' "generated architecture" > "$output"
		fi
	fi
	if [ "$1" = "ai-summary" ] && [ ! -f "$PWD/stacklit.json" ]; then
		echo "stacklit generate-json must run before ai-summary" >&2
		exit 7
fi
	`
	if err := os.WriteFile(path, []byte(script), 0755); err != nil {
		t.Fatalf("write fake stacklit: %v", err)
	}
	return dir
}

func writeFakeScipGo(t *testing.T) string {
	t.Helper()

	dir := t.TempDir()
	writeFakeScipSearch(t, dir)
	path := filepath.Join(dir, "scip-go")
	script := `#!/bin/sh
printf '%s\n' "scip-go $*" >> "$LIZA_TEST_SCIP_LOG"
output=""
while [ "$#" -gt 0 ]; do
	if [ "$1" = "--output" ]; then
		shift
		output="$1"
	fi
	shift
done
if [ -n "$output" ]; then
	printf '%s\n' "generated scip index" > "$output"
fi
	`
	if err := os.WriteFile(path, []byte(script), 0755); err != nil {
		t.Fatalf("write fake scip-go: %v", err)
	}
	return dir
}

func writeFakeScipTypeScript(t *testing.T) string {
	t.Helper()

	dir := t.TempDir()
	writeFakeScipSearch(t, dir)
	path := filepath.Join(dir, "scip-typescript")
	script := `#!/bin/sh
printf '%s\n' "scip-typescript $*" >> "$LIZA_TEST_SCIP_LOG"
output=""
target=""
while [ "$#" -gt 0 ]; do
	if [ "$1" = "--output" ]; then
		shift
		output="$1"
	fi
	target="$1"
	shift
done
if [ -n "${LIZA_TEST_SCIP_TYPESCRIPT_FAIL_ROOT:-}" ] && [ "$target" = "$LIZA_TEST_SCIP_TYPESCRIPT_FAIL_ROOT" ]; then
	exit 1
fi
if [ -n "$output" ]; then
	printf '%s\n' "generated scip index" > "$output"
fi
`
	if err := os.WriteFile(path, []byte(script), 0755); err != nil {
		t.Fatalf("write fake scip-typescript: %v", err)
	}
	return dir
}

func writeFakeScipSearch(t *testing.T, dir string) {
	t.Helper()

	path := filepath.Join(dir, "scip-search")
	script := `#!/bin/sh
printf '%s\n' "scip-search $*" >> "$LIZA_TEST_SCIP_LOG"
command="${1:-}"
output=""
while [ "$#" -gt 0 ]; do
	if [ "$1" = "--out" ]; then
		shift
		output="$1"
	fi
	if [ "$1" = "-o" ]; then
		shift
		output="$1"
	fi
	shift
done
if [ -n "$output" ]; then
	if [ "$command" = "graph-export" ]; then
		printf '%s\n' "generated scip graph" > "$output"
	else
		printf '%s\n' "aggregated scip index" > "$output"
	fi
fi
`
	if err := os.WriteFile(path, []byte(script), 0755); err != nil {
		t.Fatalf("write fake scip-search: %v", err)
	}
}

func writeFakeFunctionalClusters(t *testing.T) string {
	t.Helper()

	dir := t.TempDir()
	path := filepath.Join(dir, "functional-clusters")
	script := `#!/bin/sh
printf '%s\n' "functional-clusters $*" >> "$LIZA_TEST_FUNCTIONAL_CLUSTERS_LOG"
command="${1:-}"
output=""
while [ "$#" -gt 0 ]; do
	if [ "$1" = "-o" ]; then
		shift
		output="$1"
	fi
	shift
done
if [ "$command" = "build" ] && [ -n "$output" ]; then
	printf '%s\n' "generated functional clusters" > "$output"
fi
`
	if err := os.WriteFile(path, []byte(script), 0755); err != nil {
		t.Fatalf("write fake functional-clusters: %v", err)
	}
	return dir
}

func goAggregatePlan(repo, outputPath string) scipsearch.LanguageAggregatePlan {
	const outputPlaceholder = "__LIZA_SCIP_OUTPUT__"
	return scipsearch.LanguageAggregatePlan{
		Language:    "go",
		ProjectRoot: repo,
		OutputPath:  outputPath,
		IndexPlans: []scipsearch.RuntimeCommandPlan{{
			Language:   "go",
			Name:       "scip-go",
			Args:       []string{"index", "--module-root", repo, "--output", outputPlaceholder},
			Dir:        repo,
			OutputPath: outputPlaceholder,
			Root:       ".",
		}},
	}
}

func typescriptAggregatePlan(repo, outputPath, cwd, projectRoot string) scipsearch.LanguageAggregatePlan {
	const outputPlaceholder = "__LIZA_SCIP_OUTPUT__"
	root, err := filepath.Rel(repo, cwd)
	if err != nil {
		root = cwd
	}
	return scipsearch.LanguageAggregatePlan{
		Language:    "typescript",
		ProjectRoot: repo,
		OutputPath:  outputPath,
		IndexPlans: []scipsearch.RuntimeCommandPlan{{
			Language:   "typescript",
			Name:       "scip-typescript",
			Args:       []string{"index", "--cwd", cwd, "--output", outputPlaceholder, projectRoot},
			Dir:        repo,
			OutputPath: outputPlaceholder,
			Root:       filepath.ToSlash(root),
		}},
	}
}
