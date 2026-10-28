package providersignal

import (
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"

	"github.com/liza-mas/liza/internal/agentlaunch"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/paths"
	"github.com/liza-mas/liza/internal/providers"
)

// C3: a provider-unavailable record keeps the launch tool that raised it and
// stops every tool whose C2 provider key equals that tool's, under the
// configuration and catalog current when the signal is checked.

func c3Project(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, paths.ProjectDirName()), 0o755); err != nil {
		t.Fatal(err)
	}
	return root
}

func c3Keys(config models.Config) func() MatchKeys {
	return func() MatchKeys {
		return NewMatchKeys(agentlaunch.BuiltInAgentTools(), config)
	}
}

func c3Raise(t *testing.T, root, output, cli string) *ProviderUnavailable {
	t.Helper()
	pu := DetectProviderUnavailable(output, cli)
	if pu == nil {
		t.Fatalf("%s output not classified as provider unavailable", cli)
	}
	if err := WriteProviderUnavailableSignal(root, pu); err != nil {
		t.Fatal(err)
	}
	return pu
}

var c3SplitAlias = models.Config{AgentTools: map[string]models.AgentToolConfig{"codex-acp": {ProviderKey: "acp-lane"}}}

const c3CodexFailure = "Error: thread/start: thread/start failed: error creating thread: Fatal error: Codex cannot access session files at /tmp/.codex/sessions (permission denied)."

func TestC3ClassifierKeepsOriginatingTool(t *testing.T) {
	for _, cli := range []string{"codex", "codex-acp"} {
		pu := DetectProviderUnavailable(c3CodexFailure, cli)
		if pu == nil || pu.Tool != cli || pu.Provider != "codex" {
			t.Fatalf("%s classified as %+v, want label codex and tool %s", cli, pu, cli)
		}
	}
}

func TestC3RaiseCodexStopsCodexACPAndBack(t *testing.T) {
	for _, tc := range []struct{ raiser, stopped string }{
		{"codex", "codex-acp"},
		{"codex-acp", "codex"},
	} {
		t.Run(tc.raiser+" stops "+tc.stopped, func(t *testing.T) {
			root := c3Project(t)
			c3Raise(t, root, c3CodexFailure, tc.raiser)
			for _, tool := range []string{tc.raiser, tc.stopped} {
				if !CheckProviderUnavailableSignal(root, tool, c3Keys(models.Config{})) {
					t.Fatalf("%s must observe the signal raised by %s", tool, tc.raiser)
				}
			}
			if CheckProviderUnavailableSignal(root, "claude", c3Keys(models.Config{})) {
				t.Fatal("an unrelated provider must not observe the signal")
			}
		})
	}
}

func TestC3MatchCustomSharedKey(t *testing.T) {
	config := models.Config{AgentTools: map[string]models.AgentToolConfig{
		"foo": {ProviderKey: "shared"}, "bar": {ProviderKey: "shared"}, "baz": {ProviderKey: "other"},
	}}
	root := c3Project(t)
	if err := WriteProviderUnavailableSignal(root, &ProviderUnavailable{Provider: "foo", Message: "m"}); err != nil {
		t.Fatal(err)
	}
	if !CheckProviderUnavailableSignal(root, "bar", c3Keys(config)) {
		t.Fatal("bar shares foo's provider key and must be stopped")
	}
	if CheckProviderUnavailableSignal(root, "baz", c3Keys(config)) {
		t.Fatal("baz has an unrelated key and must not be stopped")
	}
	if CheckProviderUnavailableSignal(root, "bar", c3Keys(models.Config{})) {
		t.Fatal("without the shared key, keyless unknown tools stay distinct")
	}
}

func TestC3MatchOverrideSeparatesAliases(t *testing.T) {
	config := models.Config{AgentTools: map[string]models.AgentToolConfig{"codex-acp": {ProviderKey: "acp-lane"}}}
	root := c3Project(t)
	c3Raise(t, root, c3CodexFailure, "codex")
	if CheckProviderUnavailableSignal(root, "codex-acp", c3Keys(config)) {
		t.Fatal("an override that gives codex-acp its own key must stop alias matching")
	}
	if !CheckProviderUnavailableSignal(root, "codex", c3Keys(config)) {
		t.Fatal("the raising tool still observes its own signal")
	}
}

func TestC3MatchHonoursLoadedCatalog(t *testing.T) {
	t.Cleanup(agentlaunch.UseRuntimeCatalog(providers.Catalog{Providers: []providers.Provider{{
		ID: "codex-acp", Backend: providers.BackendACPX,
		Runtime: providers.Runtime{ProviderKey: "loaded-lane"},
	}, {
		ID: "zz-loaded", Backend: providers.BackendCLI,
		Runtime: providers.Runtime{Executable: "zz-loaded", ProviderKey: "loaded-lane"},
	}}}))
	root := c3Project(t)
	c3Raise(t, root, c3CodexFailure, "codex-acp")
	if !CheckProviderUnavailableSignal(root, "zz-loaded", c3Keys(models.Config{})) {
		t.Fatal("a tool sharing the loaded catalog key must be stopped")
	}
	if CheckProviderUnavailableSignal(root, "codex", c3Keys(models.Config{})) {
		t.Fatal("the loaded catalog moved codex-acp off codex's key")
	}
}

func TestC3MatchEvaluatesKeysAtCheckTime(t *testing.T) {
	root := c3Project(t)
	c3Raise(t, root, c3CodexFailure, "codex")
	split := models.Config{AgentTools: map[string]models.AgentToolConfig{"codex-acp": {ProviderKey: "acp-lane"}}}
	if CheckProviderUnavailableSignal(root, "codex-acp", c3Keys(split)) {
		t.Fatal("under the splitting config the alias is not stopped")
	}
	if !CheckProviderUnavailableSignal(root, "codex-acp", c3Keys(models.Config{})) {
		t.Fatal("after the config changes back the same record stops the alias: keys are not stored")
	}
}

func TestC3MatchLegacyRecords(t *testing.T) {
	root := c3Project(t)
	dir := filepath.Join(root, paths.ProjectDirName())
	// Older versions wrote only provider, detected and message, named by the
	// provider after alias normalisation; a stray alias-named file also exists.
	legacy := "provider: codex-acp\ndetected: 2020-01-01T00:00:00Z\nmessage: thread/start failed\n"
	if err := os.WriteFile(filepath.Join(dir, "provider-unavailable-codex-acp"), []byte(legacy), 0o644); err != nil {
		t.Fatal(err)
	}
	if !CheckProviderUnavailableSignal(root, "codex", c3Keys(models.Config{})) {
		t.Fatal("a legacy codex-acp record must stop codex under the default config")
	}
	split := models.Config{AgentTools: map[string]models.AgentToolConfig{"codex-acp": {ProviderKey: "acp-lane"}}}
	if CheckProviderUnavailableSignal(root, "codex", c3Keys(split)) {
		t.Fatal("a legacy record is interpreted through the current keys")
	}
	signals := ListProviderUnavailableSignals(root)
	if len(signals) != 1 || signals[0].Provider != "codex-acp" || signals[0].Tool != "codex-acp" {
		t.Fatalf("legacy listing = %+v, want the file's own name as provider and identity", signals)
	}
}

func TestC3CheckReadsKeysOnlyWhenARecordExists(t *testing.T) {
	root := c3Project(t)
	calls := 0
	keys := func() MatchKeys { calls++; return c3Keys(models.Config{})() }
	if CheckProviderUnavailableSignal(root, "codex", keys) || calls != 0 {
		t.Fatalf("no record: stopped or derived keys (calls=%d)", calls)
	}
	c3Raise(t, root, c3CodexFailure, "codex")
	c3Raise(t, root, c3CodexFailure, "codex-acp")
	if CheckProviderUnavailableSignal(root, "claude", keys) || calls != 1 {
		t.Fatalf("two records: calls=%d, want the keys derived once and shared", calls)
	}
}

func TestC3StoreRecordsDoNotConflateDistinctTools(t *testing.T) {
	root := c3Project(t)
	c3Raise(t, root, c3CodexFailure, "codex")
	c3Raise(t, root, c3CodexFailure, "codex-acp")
	signals := ListProviderUnavailableSignals(root)
	if len(signals) != 2 {
		t.Fatalf("records = %+v, want one per launch tool", signals)
	}
	var tools []string
	for _, s := range signals {
		tools = append(tools, s.Tool)
		if s.Provider != "codex" {
			t.Fatalf("display provider = %q, want the classifier label for %s", s.Provider, s.Tool)
		}
	}
	if !reflect.DeepEqual(tools, []string{"codex", "codex-acp"}) {
		t.Fatalf("tools = %v", tools)
	}
	split := models.Config{AgentTools: map[string]models.AgentToolConfig{"codex-acp": {ProviderKey: "acp-lane"}}}
	if err := ClearProviderUnavailableSignal(signals[0].Path); err != nil {
		t.Fatal(err)
	}
	if !CheckProviderUnavailableSignal(root, "codex-acp", c3Keys(split)) {
		t.Fatal("clearing codex's record must leave codex-acp's own record intact")
	}
	if CheckProviderUnavailableSignal(root, "codex", c3Keys(split)) {
		t.Fatal("codex's cleared record must not survive under the other alias's file")
	}
}

func TestC3StoreNewRecordFormatKeepsDisplayLine(t *testing.T) {
	root := c3Project(t)
	c3Raise(t, root, c3CodexFailure, "codex")
	data, err := os.ReadFile(ProviderUnavailableSignalPath(root, "codex"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "tool:") || !strings.HasPrefix(string(data), "provider: codex\ndetected: ") {
		t.Fatalf("a record whose tool equals its label must read as before:\n%s", data)
	}
	c3Raise(t, root, c3CodexFailure, "codex-acp")
	data, err = os.ReadFile(ProviderUnavailableSignalPath(root, "codex-acp"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(data), "provider: codex\ntool: codex-acp\n") {
		t.Fatalf("an alias record must keep label and tool:\n%s", data)
	}
}

func TestC3StoreToolNamesStayInsideSignalDirectory(t *testing.T) {
	root := c3Project(t)
	dir := filepath.Join(root, paths.ProjectDirName())
	hostile := []string{"../../escape", "a/b", `a\b`, "..", ".", ".hidden", "with space", "pct%41", "nul\x00byte", "weird:name"}
	seen := map[string]string{}
	for _, tool := range hostile {
		path := ProviderUnavailableSignalPath(root, tool)
		if filepath.Dir(path) != dir {
			t.Fatalf("tool %q addresses %q outside %q", tool, path, dir)
		}
		if other, dup := seen[path]; dup {
			t.Fatalf("tools %q and %q share %q", tool, other, path)
		}
		seen[path] = tool
	}
	if encodeSignalName("my_tool-2.x") != "my_tool-2.x" {
		t.Fatal("ordinary tool names must stay readable and equal their legacy file names")
	}
	// A recorded hostile tool is matched by the identity its tool line carries,
	// not by its file name, even when the label equals the tool.
	pu := &ProviderUnavailable{Provider: "../../escape", Message: "m"}
	if err := WriteProviderUnavailableSignal(root, pu); err != nil {
		t.Fatal(err)
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 {
		t.Fatalf("signal directory = %v, want exactly the one record", entries)
	}
	if !CheckProviderUnavailableSignal(root, "../../escape", c3Keys(models.Config{})) {
		t.Fatal("the hostile tool still matches its own record")
	}
	if signals := ListProviderUnavailableSignals(root); len(signals) != 1 || signals[0].Tool != "../../escape" || signals[0].Provider != "../../escape" {
		t.Fatalf("records = %+v, want the identity and display read from the record", signals)
	}
	if _, err := os.Stat(filepath.Join(root, "escape")); err == nil {
		t.Fatal("record escaped the signal directory")
	}
}

func TestC3FailUnreadableRecordStillBlocksAndClearFailureNamesHandle(t *testing.T) {
	root := c3Project(t)
	path := ProviderUnavailableSignalPath(root, "codex")
	if err := os.MkdirAll(path, 0o755); err != nil { // present but unreadable as a record
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(path, "blocker"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if !CheckProviderUnavailableSignal(root, "codex-acp", c3Keys(models.Config{})) {
		t.Fatal("an unreadable record must still block, identified by its file name")
	}
	err := ClearProviderUnavailableSignal(path)
	if err == nil || !strings.Contains(err.Error(), "remove provider-unavailable signal") || !strings.Contains(err.Error(), path) {
		t.Fatalf("clear error = %v, want the existing message naming the handle", err)
	}
	results := ClearProviderUnavailableSignals(root)
	if len(results) != 1 || results[0].Provider != "codex" || results[0].Err == nil {
		t.Fatalf("clear results = %+v, want one failed codex row", results)
	}
	if err := ClearProviderUnavailableSignal(filepath.Join(root, "missing")); err != nil {
		t.Fatalf("clearing a missing record = %v, want nil as before", err)
	}
}

func TestC3PresentCollapsesRowsByDisplayProviderAndClearsEveryHandle(t *testing.T) {
	root := c3Project(t)
	c3Raise(t, root, c3CodexFailure, "codex")
	c3Raise(t, root, c3CodexFailure, "codex-acp")
	if err := WriteProviderUnavailableSignal(root, &ProviderUnavailable{Provider: "claude", Message: "m"}); err != nil {
		t.Fatal(err)
	}
	results := ClearProviderUnavailableSignals(root)
	var rows []string
	for _, r := range results {
		if r.Err != nil {
			t.Fatalf("clear %s: %v", r.Provider, r.Err)
		}
		rows = append(rows, r.Provider)
	}
	if !reflect.DeepEqual(rows, []string{"claude", "codex"}) {
		t.Fatalf("rows = %v, want the codex and codex-acp records reported as one codex row, in file order", rows)
	}
	if left := ListProviderUnavailableSignals(root); len(left) != 0 {
		t.Fatalf("records left after clearing: %+v", left)
	}
	for _, tool := range []string{"codex", "codex-acp", "claude"} {
		if CheckProviderUnavailableSignal(root, tool, c3Keys(models.Config{})) {
			t.Fatalf("%s still blocked after clearing every represented record", tool)
		}
	}
}

func TestC3QuotaAliasBehaviourUnchanged(t *testing.T) {
	root := c3Project(t)
	if err := WriteQuotaSignal(root, "codex-acp", "quota hit"); err != nil {
		t.Fatal(err)
	}
	if !CheckQuotaSignal(root, "codex") || !CheckQuotaSignal(root, "codex-acp") {
		t.Fatal("quota keeps its spelled alias normalisation")
	}
	custom := c3Project(t)
	if err := WriteQuotaSignal(custom, "foo", "quota hit"); err != nil {
		t.Fatal(err)
	}
	if CheckQuotaSignal(custom, "bar") {
		t.Fatal("quota matching is not converted to provider keys")
	}
}

// c3Unlistable makes the signal directory searchable but not listable.
func c3Unlistable(t *testing.T, root string) {
	t.Helper()
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("directory modes are not enforced here")
	}
	dir := filepath.Join(root, paths.ProjectDirName())
	if err := os.Chmod(dir, 0o111); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o755) })
}

func TestC3FailUnlistableDirectoryKeepsStatVisibleSignal(t *testing.T) {
	root := c3Project(t)
	c3Raise(t, root, c3CodexFailure, "codex")
	c3Unlistable(t, root)

	for _, tool := range []string{"codex", "codex-acp"} {
		if !CheckProviderUnavailableSignal(root, tool, c3Keys(models.Config{})) {
			t.Fatalf("%s: a record that can still be stat'ed must keep stopping it", tool)
		}
	}
	if CheckProviderUnavailableSignal(root, "claude", c3Keys(models.Config{})) {
		t.Fatal("an unrelated provider has no visible record and must not stop")
	}
}

func TestC3FailUnlistableDirectoryStopsBothWaysOnActualRaise(t *testing.T) {
	for _, tc := range []struct{ raiser, stopped string }{{"codex-acp", "codex"}, {"codex", "codex-acp"}} {
		t.Run(tc.raiser+" raise stops "+tc.stopped, func(t *testing.T) {
			root := c3Project(t)
			c3Raise(t, root, c3CodexFailure, tc.raiser)
			c3Unlistable(t, root)
			if !CheckProviderUnavailableSignal(root, tc.stopped, c3Keys(models.Config{})) {
				t.Fatalf("%s must stop on the %s record that can still be stat'ed", tc.stopped, tc.raiser)
			}
			if CheckProviderUnavailableSignal(root, tc.stopped, c3Keys(c3SplitAlias)) {
				t.Fatal("an override-separated alias stays unrelated, as with a listable directory")
			}
		})
	}
}

func TestC3FailUnlistableDirectoryFindsSiblingCustomKeyRecord(t *testing.T) {
	root := c3Project(t)
	if err := WriteProviderUnavailableSignal(root, &ProviderUnavailable{Provider: "foo", Message: "m"}); err != nil {
		t.Fatal(err)
	}
	c3Unlistable(t, root)
	config := models.Config{AgentTools: map[string]models.AgentToolConfig{
		"foo": {ProviderKey: "shared"}, "bar": {ProviderKey: "shared"}, "baz": {ProviderKey: "other"},
	}}
	if !CheckProviderUnavailableSignal(root, "bar", c3Keys(config)) {
		t.Fatal("a configured sibling sharing the key must be stopped by foo's visible record")
	}
	if CheckProviderUnavailableSignal(root, "baz", c3Keys(config)) {
		t.Fatal("an unrelated key stays running")
	}
}

func TestC3FailUnlistableDirectoryMissingRecordDoesNotStop(t *testing.T) {
	root := c3Project(t)
	c3Unlistable(t, root)
	if CheckProviderUnavailableSignal(root, "codex", c3Keys(models.Config{})) {
		t.Fatal("no visible record: not stopped")
	}
}

func TestC3FailUnlistableDirectoryStillComparesCanonicalKeys(t *testing.T) {
	root := c3Project(t)
	c3Raise(t, root, c3CodexFailure, "codex")
	c3Unlistable(t, root)
	if CheckProviderUnavailableSignal(root, "codex-acp", c3Keys(c3SplitAlias)) {
		t.Fatal("the override gives codex-acp its own key: the visible codex record must not stop it, as with a listable directory")
	}
	if !CheckProviderUnavailableSignal(root, "codex", c3Keys(c3SplitAlias)) {
		t.Fatal("the raising tool still observes its own visible record")
	}
}

func TestC3FailUnlistableDirectoryFindsLegacyLiteralName(t *testing.T) {
	root := c3Project(t)
	dir := filepath.Join(root, paths.ProjectDirName())
	legacy := "provider: foo%41\ndetected: 2020-01-01T00:00:00Z\nmessage: m\n"
	if err := os.WriteFile(filepath.Join(dir, "provider-unavailable-foo%41"), []byte(legacy), 0o644); err != nil {
		t.Fatal(err)
	}
	c3Unlistable(t, root)
	if !CheckProviderUnavailableSignal(root, "foo%41", c3Keys(models.Config{})) {
		t.Fatal("a stat-visible legacy record keeps its literal identity")
	}
	if CheckProviderUnavailableSignal(root, "fooA", c3Keys(models.Config{})) {
		t.Fatal("the legacy name must not be reinterpreted as fooA")
	}
}

func TestC3MatchLegacyPercentNameKeepsLiteralIdentity(t *testing.T) {
	root := c3Project(t)
	dir := filepath.Join(root, paths.ProjectDirName())
	legacy := "provider: foo%41\ndetected: 2020-01-01T00:00:00Z\nmessage: m\n"
	if err := os.WriteFile(filepath.Join(dir, "provider-unavailable-foo%41"), []byte(legacy), 0o644); err != nil {
		t.Fatal(err)
	}
	if !CheckProviderUnavailableSignal(root, "foo%41", c3Keys(models.Config{})) {
		t.Fatal("a legacy record with a literal percent keeps its identity")
	}
	if CheckProviderUnavailableSignal(root, "fooA", c3Keys(models.Config{})) {
		t.Fatal("the legacy name must not be reinterpreted as fooA")
	}
	if signals := ListProviderUnavailableSignals(root); len(signals) != 1 || signals[0].Provider != "foo%41" || signals[0].Tool != "foo%41" {
		t.Fatalf("legacy listing = %+v, want the literal name", signals)
	}
	// A new record for the same tool is stored under an escaped name that
	// cannot collide with the legacy literal one, and carries its tool line.
	if err := WriteProviderUnavailableSignal(root, &ProviderUnavailable{Provider: "foo%41", Message: "m"}); err != nil {
		t.Fatal(err)
	}
	if signals := ListProviderUnavailableSignals(root); len(signals) != 2 {
		t.Fatalf("records = %+v, want the legacy and the new record apart", signals)
	}
}

func TestC3PresentKeepsPreC3RowOrderForAliasTools(t *testing.T) {
	root := c3Project(t)
	c3Raise(t, root, c3CodexFailure, "acpx-codex")
	if err := WriteProviderUnavailableSignal(root, &ProviderUnavailable{Provider: "claude", Message: "m"}); err != nil {
		t.Fatal(err)
	}
	var rows []string
	for _, r := range ClearProviderUnavailableSignals(root) {
		if r.Err != nil {
			t.Fatal(r.Err)
		}
		rows = append(rows, r.Provider)
	}
	// Before C3 the files were named claude and codex.
	if !reflect.DeepEqual(rows, []string{"claude", "codex"}) {
		t.Fatalf("rows = %v, want [claude codex] whatever the physical tool file names", rows)
	}
}
