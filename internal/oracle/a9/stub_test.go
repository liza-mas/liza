package a9

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"
)

type stubProbe struct {
	Args  []string `json:"args"`
	Reply string   `json:"reply"`
}
type stubEdit struct {
	Path    string `json:"path"`
	Content string `json:"content"`
}
type stubAction struct {
	WaitFile        string     `json:"wait_file,omitempty"`
	DeadlineSeconds int        `json:"deadline_seconds,omitempty"`
	Print           []string   `json:"print,omitempty"`
	Stderr          []string   `json:"stderr,omitempty"`
	Commands        [][]string `json:"commands,omitempty"`
	Edits           []stubEdit `json:"edits,omitempty"`
	Commit          string     `json:"commit,omitempty"`
	ExitCode        int        `json:"exit_code"`
}
type stubScript struct {
	Executables  map[string][]stubProbe `json:"executables"`
	Calls        []stubAction           `json:"calls"`
	RepeatLast   bool                   `json:"repeat_last,omitempty"`
	SnapshotEach bool                   `json:"snapshot_each,omitempty"`
}
type stubContext struct{ Project, CLI, Script string }
type stubRecord struct {
	Executable     string   `json:"executable"`
	Args           []string `json:"args"`
	Directory      string   `json:"directory"`
	Stdin          string   `json:"stdin,omitempty"`
	ContainsPrompt bool     `json:"contains_prompt,omitempty"`
	PromptEqual    bool     `json:"prompt_equal"`
	EnvNames       []string `json:"env_names"`
	Deadline       bool     `json:"deadline"`
	Error          string   `json:"error,omitempty"`
}

// TestOracleA9Stub covers the executable boundary itself: probes do not consume
// call slots, records contain names only, each action is observed, and a missing
// barrier reports a deadline instead of silently succeeding.
func TestOracleA9Stub(t *testing.T) {
	t.Parallel()
	unixOnly(t)
	p := newScenario(t, "i01-success")
	script := stubScript{Executables: map[string][]stubProbe{"claude": {{Args: []string{"--version"}, Reply: "2.1.300"}}}, Calls: []stubAction{
		{WaitFile: "ready", Edits: []stubEdit{{Path: "changed.txt", Content: "stub change\n"}}, Commit: "stub fixture", Print: []string{"stdout proof"}, Stderr: []string{"stderr proof"}, ExitCode: 42},
		{Commands: [][]string{{"stop"}}, ExitCode: 0},
		{WaitFile: "never-created", DeadlineSeconds: 1, ExitCode: 0},
	}}
	p.installStub(t, script)
	probe := p.execute(t, filepath.Join(p.bin, "claude"), "--version")
	if probe.code != 0 || strings.TrimSpace(probe.stdout) != "2.1.300" || len(p.records(t)) != 0 {
		t.Fatal("version probe consumed a scripted call or returned wrong reply")
	}
	p.write(t, "ready", []byte("ready\n"))
	first := p.execute(t, filepath.Join(p.bin, "claude"), "argument")
	if first.code != 42 || !strings.Contains(first.stdout, "stdout proof") || !strings.Contains(first.stderr, "stderr proof") {
		t.Fatal("stub output/exit action not observed")
	}
	if string(p.read(t, "changed.txt")) != "stub change\n" || !strings.Contains(p.git(t, "log", "-1", "--format=%s"), "stub fixture") {
		t.Fatal("stub edit/commit action not observed")
	}
	if second := p.execute(t, filepath.Join(p.bin, "claude")); second.code != 0 || !bytes.Contains(p.state(t), []byte("mode: STOPPED")) {
		t.Fatal("stub CLI action did not stop fixture")
	}
	last := p.execute(t, filepath.Join(p.bin, "claude"))
	records := p.records(t)
	if last.code != 97 || len(records) != 3 || !records[2].Deadline || records[2].Error != "wait deadline: never-created" {
		t.Fatal("stub deadline did not refuse missing barrier")
	}
	// The record keeps the process's own working directory (launch goldens embed it);
	// a symlinked TMPDIR makes that and p.dir differ in form, so compare resolved paths.
	recorded, recordedErr := filepath.EvalSymlinks(records[0].Directory)
	expected, expectedErr := filepath.EvalSymlinks(p.dir)
	if records[0].Executable != "claude" || !reflect.DeepEqual(records[0].Args, []string{"argument"}) || recordedErr != nil || expectedErr != nil || recorded != expected {
		t.Fatal("invocation record lost executable/argv/cwd")
	}
	if _, err := os.Stat(filepath.Join(p.recordDir, "launch-1", "state.yaml")); err != nil {
		t.Fatal("first invocation did not copy launch state")
	}
	if _, err := os.Stat(filepath.Join(p.recordDir, "launch-2")); !os.IsNotExist(err) {
		t.Fatal("first-only snapshot policy ignored")
	}
	// Both argument and stdin transport are checked against the saved prompt;
	// neither prompt bytes nor their length enter a record.
	script.Calls = []stubAction{{}}
	script.RepeatLast, script.SnapshotEach = true, true
	p.installStub(t, script)
	p.write(t, ".liza/agent-prompts/coder-1-20200101-000000.txt", []byte("private fixture prompt"))
	cmd, cancel := p.command(filepath.Join(p.bin, "claude"), "private fixture prompt")
	defer cancel()
	cmd.Stdin = strings.NewReader("private fixture prompt")
	if err := cmd.Run(); err != nil {
		t.Fatal("prompt transport stub failed")
	}
	r := p.records(t)[3]
	if !r.PromptEqual || r.Stdin != "<PROMPT>" || !reflect.DeepEqual(r.Args, []string{"<PROMPT>"}) {
		t.Fatal("stub prompt equality/redaction failed")
	}
}

func stubMain() int {
	dir := os.Getenv("ORACLE_A9_RECORD")
	var ctx stubContext
	if err := readJSON(filepath.Join(dir, "context.json"), &ctx); err != nil {
		fmt.Fprintln(os.Stderr, "A9 stub context unavailable")
		return 97
	}
	var script stubScript
	if err := readJSON(ctx.Script, &script); err != nil {
		fmt.Fprintln(os.Stderr, "A9 stub script unavailable")
		return 97
	}
	name, args := os.Getenv("ORACLE_A9_STUB"), os.Args[1:]
	for _, probe := range script.Executables[name] {
		if reflect.DeepEqual(args, probe.Args) {
			fmt.Fprintln(os.Stdout, probe.Reply)
			return 0
		}
	}
	files, _ := filepath.Glob(filepath.Join(dir, "call-*.json"))
	index := len(files)
	r := stubRecord{Executable: name, Args: append([]string{}, args...), EnvNames: environmentNames(os.Environ())}
	r.Directory, _ = os.Getwd()
	input, _ := io.ReadAll(os.Stdin)
	r.Stdin = string(input)
	redactPrompt(ctx.Project, &r)
	record := filepath.Join(dir, fmt.Sprintf("call-%03d.json", index+1))
	writeRecord := func() { _ = writeJSON(record, r) }
	writeRecord()
	if index == 0 || script.SnapshotEach {
		snapshot := filepath.Join(dir, fmt.Sprintf("launch-%d", index+1))
		_ = os.MkdirAll(snapshot, 0o755)
		paths, _ := filepath.Glob(filepath.Join(ctx.Project, ".liza", "state.yaml*"))
		for _, path := range paths {
			if filepath.Base(path) != "state.yaml" && !strings.Contains(filepath.Base(path), ".liveness-") {
				continue
			}
			if data, err := os.ReadFile(path); err == nil {
				_ = os.WriteFile(filepath.Join(snapshot, filepath.Base(path)), data, 0o644)
			}
		}
	}
	if index >= len(script.Calls) {
		if !script.RepeatLast || len(script.Calls) == 0 {
			r.Error = "unexpected invocation"
			writeRecord()
			return 97
		}
		index = len(script.Calls) - 1
	}
	action := script.Calls[index]
	if action.WaitFile != "" {
		deadline := action.DeadlineSeconds
		if deadline == 0 {
			deadline = 90
		}
		end := time.NewTimer(time.Duration(deadline) * time.Second)
		tick := time.NewTicker(10 * time.Millisecond)
		defer end.Stop()
		defer tick.Stop()
	wait:
		for {
			matches, _ := filepath.Glob(filepath.Join(ctx.Project, action.WaitFile))
			if len(matches) > 0 {
				break wait
			}
			select {
			case <-end.C:
				r.Deadline = true
				r.Error = "wait deadline: " + action.WaitFile
				writeRecord()
				return 97
			case <-tick.C:
			}
		}
	}
	for _, edit := range action.Edits {
		path := filepath.Join(r.Directory, edit.Path)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			r.Error = "edit parent"
			writeRecord()
			return 97
		}
		if err := os.WriteFile(path, []byte(edit.Content), 0o644); err != nil {
			r.Error = "edit write"
			writeRecord()
			return 97
		}
	}
	run := func(bin string, args ...string) bool {
		cmd := exec.Command(bin, args...)
		cmd.Dir, cmd.Env = r.Directory, os.Environ() // already the explicit fixture environment
		cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
		return cmd.Run() == nil
	}
	if action.Commit != "" {
		paths := []string{}
		for _, edit := range action.Edits {
			paths = append(paths, edit.Path)
		}
		// git is the scenario PATH symlink, not a host PATH directory.
		git, _ := exec.LookPath("git")
		if !run(git, append([]string{"add", "--"}, paths...)...) || !run(git, "commit", "-q", "-m", action.Commit) {
			r.Error = "commit action"
			writeRecord()
			return 97
		}
	}
	for _, args := range action.Commands {
		if !run(ctx.CLI, append([]string{"-C", ctx.Project}, args...)...) {
			r.Error = "CLI action"
			writeRecord()
			return 97
		}
	}
	for _, line := range action.Print {
		fmt.Fprintln(os.Stdout, line)
	}
	for _, line := range action.Stderr {
		fmt.Fprintln(os.Stderr, line)
	}
	return action.ExitCode
}

func redactPrompt(project string, r *stubRecord) {
	paths, _ := filepath.Glob(filepath.Join(project, ".liza", "agent-prompts", "*.txt"))
	sort.Strings(paths)
	if len(paths) == 0 {
		return
	}
	prompt, err := os.ReadFile(paths[len(paths)-1])
	if err != nil || len(prompt) == 0 {
		return
	}
	for i, arg := range r.Args {
		if arg == string(prompt) {
			r.Args[i] = "<PROMPT>"
			r.PromptEqual = true
		} else if strings.Contains(arg, string(prompt)) {
			r.Args[i] = "<CONTAINS_PROMPT>"
			r.ContainsPrompt = true
		}
	}
	if r.Stdin == string(prompt) {
		r.Stdin = "<PROMPT>"
		r.PromptEqual = true
	} else if strings.Contains(r.Stdin, string(prompt)) {
		r.Stdin = ""
		r.ContainsPrompt = true
	}
}

func readJSON(path string, target any) error {
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return json.Unmarshal(b, target)
}
func writeJSON(path string, value any) error {
	b, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	// Readers glob call-*.json while a stub writes: a record must appear whole.
	// The temp name cannot match that glob.
	tmp, err := os.CreateTemp(filepath.Dir(path), ".record-*.tmp")
	if err != nil {
		return err
	}
	if _, err := tmp.Write(append(b, '\n')); err != nil {
		tmp.Close()
		os.Remove(tmp.Name())
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmp.Name())
		return err
	}
	if err := os.Chmod(tmp.Name(), 0o644); err != nil {
		os.Remove(tmp.Name())
		return err
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		os.Remove(tmp.Name())
		return err
	}
	return nil
}
func environmentNames(env []string) []string {
	names := make([]string, 0, len(env))
	for _, pair := range env {
		name, _, _ := strings.Cut(pair, "=")
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}
