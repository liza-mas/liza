package a9

import (
	"bytes"
	"encoding/json"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/liza-mas/liza/internal/testhelpers/oracle"
)

// Interactive launch (closes the gap disclosed in doc_test.go: the --interactive
// launch line was unreachable from a plain fixture). One coder run per catalog
// tool with stdin not a terminal: the stub ends the run with `stop` when the
// supervisor reaches it. The run is recorded whatever its outcome, so a tool
// whose interactive route is refused (an ACP tool whose prerequisites the
// fixture does not provide) pins the refusal text and exit code instead.

type interactiveInvocation struct {
	Executable string   `json:"executable"`
	Arguments  []string `json:"arguments"`
	Directory  string   `json:"directory"`
	Stdin      string   `json:"stdin,omitempty"`
}

func TestOracleA9InteractiveLaunch(t *testing.T) {
	unixOnly(t)
	launchRequireCatalog(t)
	for _, tool := range launchCatalog {
		t.Run(tool, func(t *testing.T) {
			p := launchScenario(t, "i26-"+tool)
			launchState(t, p)
			r := launchRun(t, p, "agent", "coder", "task-1", "--cli", tool, "--agent-id", "coder-1", "--interactive")
			interactiveCheckInvocations(t, p, tool, p.records(t))
			launchHuman(t, p, r)
			p.check(t, r)
		})
	}
}

// interactiveCheckInvocations compares the recorded invocation sequence with the
// scenario's json golden. A saved prompt, when the run got that far, must not
// appear in the document.
func interactiveCheckInvocations(t *testing.T, p *scenario, tool string, records []stubRecord) {
	t.Helper()
	doc := struct {
		Tool        string                  `json:"tool"`
		Invocations []interactiveInvocation `json:"invocations"`
	}{Tool: tool, Invocations: []interactiveInvocation{}}
	for _, r := range records {
		doc.Invocations = append(doc.Invocations, interactiveInvocation{Executable: r.Executable, Arguments: r.Args, Directory: r.Directory, Stdin: r.Stdin})
	}
	var data bytes.Buffer
	enc := json.NewEncoder(&data)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(doc); err != nil {
		t.Fatalf("A9 %s collect: encode invocations", p.name)
	}
	if saved, _ := filepath.Glob(filepath.Join(p.dir, ".liza", "agent-prompts", "*.txt")); len(saved) > 0 {
		sort.Strings(saved)
		interactiveRejectPrompt(t, p, mustRead(t, saved[len(saved)-1]), data.Bytes())
	}
	shas := p.fixedSHAs(t)
	n := p.harness(t, shas)
	n.Dir(filepath.Dir(p.cli), "<CLI>")
	n.Check(t, oracle.JSON, p.name+"/invocations.json", []byte(newA9Normaliser(shas).text(data.String())))
}

// interactiveRejectPrompt fails when a substantial prompt line is in the document.
func interactiveRejectPrompt(t *testing.T, p *scenario, prompt, doc []byte) {
	t.Helper()
	for line := range strings.SplitSeq(string(prompt), "\n") {
		if line = strings.TrimSpace(line); len(line) >= 24 && bytes.Contains(doc, []byte(line)) {
			t.Fatalf("A9 %s collect: invocation document records prompt content", p.name)
		}
	}
}
