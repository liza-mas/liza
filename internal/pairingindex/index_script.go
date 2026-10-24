package pairingindex

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/liza-mas/liza/internal/brand"
	"github.com/liza-mas/liza/internal/scipsearch"
	"github.com/liza-mas/liza/internal/stacklit"
)

const stacklitArtifactName = "stacklit.json"

const stacklitInsightsArtifactName = "stacklit-insights.json"

const stacklitArchitectureArtifactName = "stacklit-architecture.json"

const functionalClustersArtifactName = "functional-clusters.json"

// stagingDirName names the directory, under the Git common directory, that
// holds one private subdirectory per index-script run. Every artifact is
// generated there and published with a rename, so a reader never sees a
// partially written index and a failed generator leaves the previous one in
// place. Staging under .git keeps in-progress files out of the checkout and out
// of git status.
func stagingDirName() string {
	return brand.RuntimeValues().BinaryName + "-index-staging"
}

// RenderIndexScript returns the managed Stacklit index script content for repoRoot.
func RenderIndexScript(repoRoot string) (string, error) {
	return renderIndexScript(renderIndexScriptOptions{RepoRoot: repoRoot, EnableStacklit: true})
}

type renderIndexScriptOptions struct {
	RepoRoot                 string
	EnableStacklit           bool
	EnableFunctionalClusters bool
	ScipPlans                []scipsearch.LanguageAggregatePlan
}

func renderIndexScript(opts renderIndexScriptOptions) (string, error) {
	var stacklitPlan stacklit.RuntimeCommandPlan
	if opts.EnableStacklit {
		plan, err := stacklit.PlanRuntimeCommand(opts.RepoRoot)
		if err != nil {
			return "", err
		}
		stacklitPlan = plan
	} else {
		repoRoot, err := filepath.Abs(opts.RepoRoot)
		if err != nil {
			return "", fmt.Errorf("resolve repository root: %w", err)
		}
		stacklitPlan.Dir = repoRoot
	}
	body := strings.Builder{}
	body.WriteString("#!/bin/sh\n")
	body.WriteString(ManagedIndexScriptMarker)
	body.WriteString("\nset -eu\n")
	body.WriteString("repo_root=")
	body.WriteString(shellQuote(stacklitPlan.Dir))
	body.WriteString("\n")
	body.WriteString(`run_ai=0
if [ "${1:-}" = "ai" ]; then
	run_ai=1
	shift
fi
`)
	body.WriteString(renderStagingSetup())
	for _, plan := range opts.ScipPlans {
		body.WriteString(renderScipCommand(plan))
	}
	if opts.EnableStacklit {
		stacklitName := shellWord(stacklitPlan.Name)
		stagedIndex := `"$staging_dir/` + stacklitArtifactName + `"`
		stagedInsights := `"$staging_dir/` + stacklitInsightsArtifactName + `"`
		stacklitGenerateCommand := shellCommandWithOutputFlag(stacklitPlan.Name, append(stacklitPlan.Args, "--parse-workers", "3"), "-o", stagedIndex) + closeLockFD
		publishIndex := "mv -f " + stagedIndex + " " + stacklitArtifactName
		// init-insights and ai-summary read their -o file before updating it,
		// so the staged copy starts from the published insights. Starting from
		// an empty file would drop curated entries a normal run preserves.
		seedInsights := "if [ -f " + stacklitInsightsArtifactName + " ]; then cp " + stacklitInsightsArtifactName + " " + stagedInsights + "; fi"
		publishInsights := "mv -f " + stagedInsights + " " + stacklitInsightsArtifactName
		body.WriteString(fmt.Sprintf(`if ! command -v %s >/dev/null 2>&1; then
	echo "%s: %s not found; skipping Stacklit refresh" >&2
else
	cd "$repo_root"
	code=0
	stacklit_refresh=1
	if [ -f stacklit.json ]; then
		%s diff -i stacklit.json >/dev/null%s || code=$?
		case "$code" in
			0) [ "$run_ai" = "1" ] || stacklit_refresh=0 ;;
			1) ;;
			*) echo "stacklit diff failed"; exit "$code" ;;
		esac
	fi
	if [ "$stacklit_refresh" = "1" ]; then
		echo "Stacklit Indexing..."
		insights_before=""
		if [ -f stacklit-insights.json ]; then
			insights_before="$(cksum stacklit-insights.json | awk '{print $1 ":" $2}')"
		fi
		%s
		%s
		%s
		%s init-insights -i stacklit.json -o %s%s
		%s
		if [ "$run_ai" = "1" ]; then
			echo "Adding AI summary..."
			%s
			%s ai-summary -o %s%s
			%s
		fi
		insights_after=""
		if [ -f stacklit-insights.json ]; then
			insights_after="$(cksum stacklit-insights.json | awk '{print $1 ":" $2}')"
		fi
		if [ "$insights_before" != "$insights_after" ]; then
			%s
			%s
		fi
		echo "Wrote stacklit.json"
	fi
fi
`, stacklitName, scriptName(), stacklitPlan.Name, stacklitName, closeLockFD,
			stacklitGenerateCommand, publishIndex,
			seedInsights, stacklitName, stagedInsights, closeLockFD, publishInsights,
			seedInsights, stacklitName, stagedInsights, closeLockFD, publishInsights,
			stacklitGenerateCommand, publishIndex))
	}
	if opts.EnableFunctionalClusters && opts.EnableStacklit && len(opts.ScipPlans) > 0 {
		body.WriteString(renderFunctionalClustersCommand(stacklitPlan.Dir, opts.ScipPlans))
	}
	return body.String(), nil
}

// renderStagingSetup creates this run's private staging directory and removes
// it on exit. A signal exits the script so the EXIT trap still cleans up.
func renderStagingSetup() string {
	return fmt.Sprintf(`staging_root="$(cd "$repo_root" && cd "$(git rev-parse --git-common-dir)" && pwd -P)/%s"
mkdir -p "$staging_root"
staging_dir="$(mktemp -d "$staging_root/run.XXXXXX")"
cleanup_staging() { rm -rf "$staging_dir"; }
trap cleanup_staging EXIT
trap 'exit 1' HUP INT TERM
`, stagingDirName())
}

func renderScipCommand(plan scipsearch.LanguageAggregatePlan) string {
	needsVar := "needs_" + shellIdentifier(plan.Language) + "_scip"
	missingVar := "missing_" + shellIdentifier(plan.Language) + "_scip"
	tmpVar := "tmp_" + shellIdentifier(plan.Language) + "_scip"
	aggregateFunc := "aggregate_" + shellIdentifier(plan.Language) + "_scip"
	stagedOutput := `"$staging_dir/` + shellIdentifier(plan.Language) + `.scip"`
	indexedVar := "indexed_" + shellIdentifier(plan.Language) + "_roots"

	var freshness strings.Builder
	fmt.Fprintf(&freshness, `%s=0
if [ ! -f %s ]; then
	%s=1
`, needsVar, shellQuote(plan.OutputPath), needsVar)
	for _, indexPlan := range plan.IndexPlans {
		sourceExpr := scipFindSourceExpression(indexPlan.Language)
		if sourceExpr == "" {
			sourceExpr = "-type f"
		}
		sourceRoot := scipSourceRoot(indexPlan)
		exclusions := scipFindExclusions(indexPlan.Language, sourceRoot)
		fmt.Fprintf(&freshness, `elif find %s %s %s -newer %s -print -quit | grep -q .; then
	%s=1
`, shellQuote(sourceRoot), sourceExpr, exclusions, shellQuote(plan.OutputPath), needsVar)
	}
	freshness.WriteString("fi\n")

	var commandChecks strings.Builder
	checked := map[string]bool{"scip-search": true}
	fmt.Fprintf(&commandChecks, `%s=0
if [ "$%s" -eq 1 ] && ! command -v scip-search >/dev/null 2>&1; then
	echo "%s: scip-search not found; skipping %s SCIP refresh" >&2
	%s=1
fi
`, missingVar, needsVar, scriptName(), plan.Language, missingVar)
	for _, indexPlan := range plan.IndexPlans {
		if checked[indexPlan.Name] {
			continue
		}
		checked[indexPlan.Name] = true
		fmt.Fprintf(&commandChecks, `if [ "$%s" -eq 1 ] && ! command -v %s >/dev/null 2>&1; then
	echo "%s: %s not found; skipping %s SCIP refresh" >&2
	%s=1
fi
`, needsVar, shellWord(indexPlan.Name), scriptName(), indexPlan.Name, plan.Language, missingVar)
	}

	var commands strings.Builder
	fmt.Fprintf(&commands, "\t\t%s() {\n", aggregateFunc)
	fmt.Fprintf(&commands, "\t\t\tset -- scip-search aggregate-index --project-root %s\n", shellWord(plan.ProjectRoot))
	fmt.Fprintf(&commands, "\t\t\t%s=0\n", indexedVar)
	for i, indexPlan := range plan.IndexPlans {
		outputExpr := fmt.Sprintf("\"$%s/%s-%d.scip\"", tmpVar, plan.Language, i)
		fmt.Fprintf(&commands, "\t\t\tif %s%s; then\n", shellCommandWithOutput(indexPlan, outputExpr), closeLockFD)
		fmt.Fprintf(&commands, "\t\t\t\tset -- \"$@\" --root %s --index %s\n", shellWord(indexPlan.Root), outputExpr)
		fmt.Fprintf(&commands, "\t\t\t\t%s=$((%s + 1))\n", indexedVar, indexedVar)
		fmt.Fprintf(&commands, "\t\t\telse\n\t\t\t\techo \"%s: failed to index %s SCIP root %s; skipping it\" >&2\n\t\t\tfi\n", scriptName(), plan.Language, shellWord(indexPlan.Root))
	}
	fmt.Fprintf(&commands, "\t\t\tif [ \"$%s\" -gt 0 ]; then\n", indexedVar)
	fmt.Fprintf(&commands, "\t\t\t\t\"$@\" --out %s%s\n", stagedOutput, closeLockFD)
	fmt.Fprintf(&commands, "\t\t\t\tmv -f %s %s\n", stagedOutput, shellWord(plan.OutputPath))
	fmt.Fprintf(&commands, "\t\t\telif [ -f %s ]; then\n", shellQuote(plan.OutputPath))
	fmt.Fprintf(&commands, "\t\t\t\techo \"%s: no %s SCIP roots indexed; retaining existing index\" >&2\n", scriptName(), plan.Language)
	fmt.Fprintf(&commands, "\t\t\telse\n\t\t\t\techo \"%s: no %s SCIP roots indexed; no index produced\" >&2\n\t\t\tfi\n", scriptName(), plan.Language)
	commands.WriteString("\t\t}\n")
	fmt.Fprintf(&commands, "\t\t%s\n", aggregateFunc)

	return fmt.Sprintf(`%s%sif [ "$%s" -eq 1 ] && [ "$%s" -eq 0 ]; then
	%s="$staging_dir/%s-scip"
	mkdir "$%s"
	cd %s
%sfi
`, freshness.String(), commandChecks.String(), needsVar, missingVar, tmpVar, shellIdentifier(plan.Language), tmpVar, shellQuote(plan.ProjectRoot), commands.String())
}

func renderFunctionalClustersCommand(repoRoot string, plans []scipsearch.LanguageAggregatePlan) string {
	needsVar := "needs_functional_clusters"
	missingVar := "missing_functional_clusters"
	tmpVar := "tmp_functional_clusters"
	stagedOutput := `"$staging_dir/` + functionalClustersArtifactName + `"`

	var freshness strings.Builder
	fmt.Fprintf(&freshness, `%s=0
if [ ! -f %s ]; then
	%s=1
elif [ -f %s ] && [ %s -nt %s ]; then
	%s=1
`, needsVar, shellQuote(functionalClustersArtifactName), needsVar, shellQuote(stacklitArtifactName), shellQuote(stacklitArtifactName), shellQuote(functionalClustersArtifactName), needsVar)
	for _, plan := range plans {
		fmt.Fprintf(&freshness, `elif [ -f %s ] && [ %s -nt %s ]; then
	%s=1
`, shellQuote(plan.OutputPath), shellQuote(plan.OutputPath), shellQuote(functionalClustersArtifactName), needsVar)
	}
	freshness.WriteString("fi\n")

	var prerequisites strings.Builder
	fmt.Fprintf(&prerequisites, `%s=0
if [ "$%s" -eq 1 ] && ! command -v functional-clusters >/dev/null 2>&1; then
	echo "%s: functional-clusters not found; skipping Functional Clusters refresh" >&2
	%s=1
fi
if [ "$%s" -eq 1 ] && ! command -v stacklit >/dev/null 2>&1; then
	echo "%s: stacklit not found; skipping Functional Clusters refresh" >&2
	%s=1
fi
if [ "$%s" -eq 1 ] && ! command -v scip-search >/dev/null 2>&1; then
	echo "%s: scip-search not found; skipping Functional Clusters refresh" >&2
	%s=1
fi
if [ "$%s" -eq 1 ] && [ ! -f %s ]; then
	echo "%s: stacklit.json missing; skipping Functional Clusters refresh" >&2
	%s=1
fi
`, missingVar, needsVar, scriptName(), missingVar, needsVar, scriptName(), missingVar, needsVar, scriptName(), missingVar, needsVar, shellQuote(stacklitArtifactName), scriptName(), missingVar)
	for _, plan := range plans {
		fmt.Fprintf(&prerequisites, `if [ "$%s" -eq 1 ] && [ ! -f %s ]; then
	echo "%s: %s SCIP index missing; skipping Functional Clusters refresh" >&2
	%s=1
fi
`, needsVar, shellQuote(plan.OutputPath), scriptName(), plan.Language, missingVar)
	}

	var commands strings.Builder
	fmt.Fprintf(&commands, "\tstacklit export-architecture -i %s -o \"$%s/%s\"%s\n", shellQuote(stacklitArtifactName), tmpVar, stacklitArchitectureArtifactName, closeLockFD)
	graphPathExprs := make([]string, 0, len(plans))
	for _, plan := range plans {
		graphPathExpr := fmt.Sprintf("\"$%s/%s-scip-graph.json\"", tmpVar, shellIdentifier(plan.Language))
		graphPathExprs = append(graphPathExprs, graphPathExpr)
		fmt.Fprintf(&commands, "\tscip-search graph-export --index %s -o %s%s\n", shellQuote(plan.OutputPath), graphPathExpr, closeLockFD)
	}
	commands.WriteString("\tfunctional-clusters build")
	for _, graphPathExpr := range graphPathExprs {
		fmt.Fprintf(&commands, " --scip-graph %s", graphPathExpr)
	}
	fmt.Fprintf(&commands, " --stacklit-architecture \"$%s/%s\" -o %s%s\n", tmpVar, stacklitArchitectureArtifactName, stagedOutput, closeLockFD)
	fmt.Fprintf(&commands, "\tmv -f %s %s\n", stagedOutput, shellQuote(functionalClustersArtifactName))

	return fmt.Sprintf(`cd %s
%s%sif [ "$%s" -eq 1 ] && [ "$%s" -eq 0 ]; then
	%s="$staging_dir/functional-clusters"
	mkdir "$%s"
	echo "Functional Clusters Indexing..."
%s	echo "Wrote %s"
fi
`, shellQuote(repoRoot), freshness.String(), prerequisites.String(), needsVar, missingVar, tmpVar, tmpVar, commands.String(), functionalClustersArtifactName)
}

func scipFindSourceExpression(language string) string {
	switch language {
	case "go":
		return `-type f -name '*.go'`
	case "python":
		return `-type f -name '*.py'`
	case "typescript":
		return `-type f \( -name '*.ts' -o -name '*.tsx' -o -name '*.js' -o -name '*.jsx' \)`
	default:
		return ""
	}
}

func scipSourceRoot(plan scipsearch.RuntimeCommandPlan) string {
	switch plan.Language {
	case "go":
		if value, ok := argValueAfter(plan.Args, "--module-root"); ok {
			return value
		}
	case "python", "typescript":
		if value, ok := argValueAfter(plan.Args, "--cwd"); ok {
			return value
		}
	}
	return plan.Dir
}

func argValueAfter(args []string, flag string) (string, bool) {
	for i, arg := range args {
		if arg == flag && i+1 < len(args) {
			return args[i+1], true
		}
	}
	return "", false
}

func scipFindExclusions(language, sourceRoot string) string {
	paths := []string{
		filepath.Join(sourceRoot, ".git", "*"),
		filepath.Join(sourceRoot, ".worktrees", "*"),
	}
	switch language {
	case "typescript":
		paths = append(paths, filepath.Join(sourceRoot, "node_modules", "*"))
	case "python":
		paths = append(paths,
			filepath.Join(sourceRoot, ".venv", "*"),
			filepath.Join(sourceRoot, "venv", "*"),
			filepath.Join(sourceRoot, "__pycache__", "*"),
		)
	}

	parts := make([]string, 0, len(paths)*2)
	for _, path := range paths {
		parts = append(parts, "-not", "-path", shellQuote(path))
	}
	return strings.Join(parts, " ")
}

func shellCommandWithOutput(plan scipsearch.RuntimeCommandPlan, outputExpr string) string {
	parts := []string{shellWord(plan.Name)}
	for _, arg := range plan.Args {
		if arg == plan.OutputPath {
			parts = append(parts, outputExpr)
			continue
		}
		parts = append(parts, shellWord(arg))
	}
	return strings.Join(parts, " ")
}

func shellIdentifier(value string) string {
	var b strings.Builder
	for _, r := range value {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' {
			b.WriteRune(r)
		} else {
			b.WriteByte('_')
		}
	}
	if b.Len() == 0 {
		return "index"
	}
	return b.String()
}

// shellCommandWithOutputFlag renders a command whose value after outputFlag is
// replaced by outputExpr, an already-quoted shell expression.
func shellCommandWithOutputFlag(name string, args []string, outputFlag, outputExpr string) string {
	parts := []string{shellWord(name)}
	for i := 0; i < len(args); i++ {
		parts = append(parts, shellWord(args[i]))
		if args[i] == outputFlag && i+1 < len(args) {
			parts = append(parts, outputExpr)
			i++
		}
	}
	return strings.Join(parts, " ")
}

func shellWord(value string) string {
	if value != "" && !strings.ContainsAny(value, " \t\n'\"\\$`;&|<>*?!()[]{}") {
		return value
	}
	return shellQuote(value)
}

// indexBinaryAssignment starts the dispatcher line that bakes in the
// coordinator executable. CheckActivation reads the path back from it.
const indexBinaryAssignment = "index_binary="

// bakedIndexBinary returns the coordinator path baked into dispatcher content.
func bakedIndexBinary(content string) (string, bool) {
	for _, line := range strings.Split(content, "\n") {
		if value, ok := strings.CutPrefix(line, indexBinaryAssignment); ok {
			return shellUnquote(value), true
		}
	}
	return "", false
}

// shellUnquote reverses shellQuote.
func shellUnquote(value string) string {
	value = strings.TrimSuffix(strings.TrimPrefix(value, "'"), "'")
	return strings.ReplaceAll(value, `'"'"'`, "'")
}

// indexBinary resolves the coordinator executable the dispatcher runs. It is
// empty inside test binaries, whose executable would run the test suite if a
// hook invoked it; tests that need a coordinator install one with
// SetIndexBinaryForTest.
var indexBinary = func() string {
	if testing.Testing() {
		return ""
	}
	executable, err := os.Executable()
	if err != nil {
		return ""
	}
	return executable
}

// SetIndexBinaryForTest makes installs bake path in as the coordinator and
// returns a function restoring the default.
func SetIndexBinaryForTest(path string) (restore func()) {
	previous := indexBinary
	indexBinary = func() string { return path }
	return func() { indexBinary = previous }
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'"
}
