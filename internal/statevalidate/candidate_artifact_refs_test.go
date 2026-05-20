package statevalidate

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/liza-mas/liza/internal/testhelpers"
)

func TestValidateCandidateArtifactRefsAcceptsRegularFileModes(t *testing.T) {
	tests := []struct {
		name        string
		requirement string
		ref         ArtifactRef
		mode        string
	}{
		{
			name:        "regular file",
			requirement: "FR-001-9 accepts mode 100644",
			ref:         candidateRef("specs/plain.md", "spec_ref", "task-1", nil),
			mode:        "100644",
		},
		{
			name:        "executable file",
			requirement: "FR-001-9 accepts mode 100755",
			ref:         candidateRef("specs/executable.sh", "plan_ref", "task-2", nil),
			mode:        "100755",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lookup := &fakeCandidateTreeLookup{
				entries: map[string]fakeCandidateTreeEntry{
					tt.ref.Path: {mode: tt.mode, present: true},
				},
			}

			if err := ValidateCandidateArtifactRefs("candidate", []ArtifactRef{tt.ref}, lookup); err != nil {
				t.Fatalf("%s: ValidateCandidateArtifactRefs returned error: %v", tt.requirement, err)
			}

			wantCalls := []string{"candidate|" + tt.ref.Path}
			if got := strings.Join(lookup.calls, ","); got != strings.Join(wantCalls, ",") {
				t.Fatalf("%s: lookup calls = %v, want %v", tt.requirement, lookup.calls, wantCalls)
			}
		})
	}
}

func TestValidateCandidateArtifactRefsRejectsMissingAndNonRegularModes(t *testing.T) {
	tests := []struct {
		name        string
		entry       fakeCandidateTreeEntry
		wantCause   string
		wantMode    string
		wantText    []string
		requirement string
	}{
		{
			name:        "missing path",
			entry:       fakeCandidateTreeEntry{},
			wantCause:   candidateArtifactRefMissingCause,
			wantText:    []string{"specs/artifact.md", candidateArtifactRefMissingCause, "arch_ref", "task-1"},
			requirement: "FR-001-10 rejects missing paths",
		},
		{
			name:        "directory",
			entry:       fakeCandidateTreeEntry{mode: "040000", present: true},
			wantCause:   candidateArtifactRefInvalidModeCause,
			wantMode:    "040000",
			wantText:    []string{"specs/artifact.md", candidateArtifactRefInvalidModeCause, "040000", "arch_ref", "task-1"},
			requirement: "FR-001-10 rejects directories",
		},
		{
			name:        "submodule gitlink",
			entry:       fakeCandidateTreeEntry{mode: "160000", present: true},
			wantCause:   candidateArtifactRefInvalidModeCause,
			wantMode:    "160000",
			wantText:    []string{"specs/artifact.md", candidateArtifactRefInvalidModeCause, "160000", "arch_ref", "task-1"},
			requirement: "FR-001-10 rejects submodules/gitlinks",
		},
		{
			name:        "symlink",
			entry:       fakeCandidateTreeEntry{mode: "120000", present: true},
			wantCause:   candidateArtifactRefInvalidModeCause,
			wantMode:    "120000",
			wantText:    []string{"specs/artifact.md", candidateArtifactRefInvalidModeCause, "120000", "arch_ref", "task-1"},
			requirement: "FR-001-10 rejects symlinks",
		},
		{
			name:        "unknown mode",
			entry:       fakeCandidateTreeEntry{mode: "100600", present: true},
			wantCause:   candidateArtifactRefInvalidModeCause,
			wantMode:    "100600",
			wantText:    []string{"specs/artifact.md", candidateArtifactRefInvalidModeCause, "100600", "arch_ref", "task-1"},
			requirement: "FR-001-9 only exact regular modes are accepted",
		},
		{
			name:        "unsupported regular-like mode",
			entry:       fakeCandidateTreeEntry{mode: "100640", present: true},
			wantCause:   candidateArtifactRefInvalidModeCause,
			wantMode:    "100640",
			wantText:    []string{"specs/artifact.md", candidateArtifactRefInvalidModeCause, "100640", "arch_ref", "task-1"},
			requirement: "FR-001-9 rejects modes other than 100644 and 100755",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			refs := []ArtifactRef{candidateRef("specs/artifact.md", "arch_ref", "task-1", nil)}
			lookup := &fakeCandidateTreeLookup{
				entries: map[string]fakeCandidateTreeEntry{"specs/artifact.md": tt.entry},
			}

			err := ValidateCandidateArtifactRefs("candidate", refs, lookup)
			assertCandidateArtifactRefError(t, err, candidateErrorWant{
				cause:       tt.wantCause,
				path:        "specs/artifact.md",
				value:       "specs/artifact.md#section",
				mode:        tt.wantMode,
				text:        tt.wantText,
				requirement: tt.requirement,
				owners: []ownerWant{
					{field: "arch_ref", taskID: "task-1"},
				},
			})
		})
	}
}

func TestValidateCandidateArtifactRefsReportsOwnersDeterministically(t *testing.T) {
	outputIndex := 0
	refs := []ArtifactRef{
		candidateRef("specs/a.md", "arch_ref", "task-a", nil),
		candidateRef("specs/a.md", "output[0].plan_ref", "task-b", &outputIndex),
		candidateRef("specs/z.md", "spec_ref", "task-z", nil),
	}
	lookup := &fakeCandidateTreeLookup{}

	err := ValidateCandidateArtifactRefs("candidate", refs, lookup)
	assertCandidateArtifactRefError(t, err, candidateErrorWant{
		cause:       candidateArtifactRefMissingCause,
		path:        "specs/a.md",
		requirement: "FR-001-11 deterministic owner provenance ordering",
		text: []string{
			"specs/a.md",
			candidateArtifactRefMissingCause,
			"arch_ref",
			"task-a",
			"output[0].plan_ref",
			"task-b",
			"output: 0",
		},
		owners: []ownerWant{
			{field: "arch_ref", taskID: "task-a"},
			{field: "output[0].plan_ref", taskID: "task-b", outputIndex: &outputIndex},
		},
	})
}

func TestValidateCandidateArtifactRefsIncludesRawRefValue(t *testing.T) {
	refs := []ArtifactRef{
		{
			Path: "specs/a.md",
			Raw:  "specs/a.md#flow",
			Owner: ArtifactRefOwner{
				Field:  "arch_ref",
				TaskID: "task-a",
			},
		},
	}
	lookup := &fakeCandidateTreeLookup{}

	err := ValidateCandidateArtifactRefs("candidate", refs, lookup)
	assertCandidateArtifactRefError(t, err, candidateErrorWant{
		cause:       candidateArtifactRefMissingCause,
		path:        "specs/a.md",
		value:       "specs/a.md#flow",
		requirement: "FR-001-11 diagnostics preserve raw ref values",
		text:        []string{"specs/a.md", "specs/a.md#flow", "arch_ref", "task-a"},
		owners: []ownerWant{
			{field: "arch_ref", taskID: "task-a"},
		},
	})
}

func TestValidateCandidateStateArtifactRefsPropagatesCollectorInvalidRef(t *testing.T) {
	state := testhelpers.CreateValidState()
	state.Goal.SpecRef = "#intro"
	state.Tasks = nil
	lookup := &fakeCandidateTreeLookup{}

	err := ValidateCandidateStateArtifactRefs("candidate", state, t.TempDir(), lookup)
	assertCandidateArtifactRefError(t, err, candidateErrorWant{
		cause:       artifactRefEmptyPathCause,
		value:       "#intro",
		text:        []string{"candidate artifact ref", artifactRefEmptyPathCause, "#intro", "goal.spec_ref", "empty path"},
		requirement: "FR-001-6 invalid refs fail closed before candidate lookup",
		owners: []ownerWant{
			{field: "goal.spec_ref"},
		},
	})
	if len(lookup.calls) != 0 {
		t.Fatalf("lookup calls = %v, want none for invalid collector ref", lookup.calls)
	}
}

func TestValidateCandidateArtifactRefsPropagatesLookupErrors(t *testing.T) {
	lookupErr := errors.New("git unavailable")
	refs := []ArtifactRef{candidateRef("specs/artifact.md", "spec_ref", "task-1", nil)}
	lookup := &fakeCandidateTreeLookup{
		entries: map[string]fakeCandidateTreeEntry{
			"specs/artifact.md": {err: lookupErr},
		},
	}

	err := ValidateCandidateArtifactRefs("candidate", refs, lookup)
	if !errors.Is(err, lookupErr) {
		t.Fatalf("error = %v, want to wrap lookup error", err)
	}
}

type fakeCandidateTreeLookup struct {
	entries map[string]fakeCandidateTreeEntry
	calls   []string
}

func (l *fakeCandidateTreeLookup) TreePathMode(treeish, path string) (string, bool, error) {
	l.calls = append(l.calls, fmt.Sprintf("%s|%s", treeish, path))
	entry, ok := l.entries[path]
	if !ok {
		return "", false, nil
	}
	return entry.mode, entry.present, entry.err
}

type fakeCandidateTreeEntry struct {
	mode    string
	present bool
	err     error
}

func candidateRef(path, field, taskID string, outputIndex *int) ArtifactRef {
	return ArtifactRef{
		Path: path,
		Raw:  path + "#section",
		Owner: ArtifactRefOwner{
			Field:       field,
			TaskID:      taskID,
			OutputIndex: cloneInt(outputIndex),
		},
	}
}

type candidateErrorWant struct {
	cause       string
	path        string
	value       string
	mode        string
	text        []string
	owners      []ownerWant
	requirement string
}

type ownerWant struct {
	field       string
	taskID      string
	outputIndex *int
}

func assertCandidateArtifactRefError(t *testing.T, err error, want candidateErrorWant) {
	t.Helper()

	if err == nil {
		t.Fatal("ValidateCandidateArtifactRefs returned nil error")
	}
	var candidateErr *CandidateArtifactRefError
	if !errors.As(err, &candidateErr) {
		t.Fatalf("error type = %T, want *CandidateArtifactRefError", err)
	}
	if candidateErr.Cause != want.cause {
		t.Errorf("%s: Cause = %q, want %q", want.requirement, candidateErr.Cause, want.cause)
	}
	if candidateErr.Path != want.path {
		t.Errorf("%s: Path = %q, want %q", want.requirement, candidateErr.Path, want.path)
	}
	if want.value != "" && candidateErr.Value != want.value {
		t.Errorf("%s: Value = %q, want %q", want.requirement, candidateErr.Value, want.value)
	}
	if candidateErr.Mode != want.mode {
		t.Errorf("%s: Mode = %q, want %q", want.requirement, candidateErr.Mode, want.mode)
	}
	assertCandidateOwners(t, candidateErr.Owners, want.owners)
	for _, text := range want.text {
		if !strings.Contains(err.Error(), text) {
			t.Errorf("%s: Error = %q, want to contain %q", want.requirement, err.Error(), text)
		}
	}

	details := candidateErr.SafeDetails()
	if details["cause"] != want.cause {
		t.Errorf("%s: SafeDetails cause = %v, want %q", want.requirement, details["cause"], want.cause)
	}
	if details["candidate_treeish"] != "candidate" {
		t.Errorf("%s: SafeDetails candidate_treeish = %v, want %q", want.requirement, details["candidate_treeish"], "candidate")
	}
	if want.path != "" && details["path"] != want.path {
		t.Errorf("%s: SafeDetails path = %v, want %q", want.requirement, details["path"], want.path)
	}
	if want.value != "" && details["value"] != want.value {
		t.Errorf("%s: SafeDetails value = %v, want %q", want.requirement, details["value"], want.value)
	}
	if want.mode != "" && details["mode"] != want.mode {
		t.Errorf("%s: SafeDetails mode = %v, want %q", want.requirement, details["mode"], want.mode)
	}
	assertOwnerDetails(t, details["owners"], want.owners)
}

func assertCandidateOwners(t *testing.T, got []ArtifactRefOwner, want []ownerWant) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("owner count = %d, want %d: %#v", len(got), len(want), got)
	}
	for i := range want {
		if got[i].Field != want[i].field {
			t.Errorf("Owners[%d].Field = %q, want %q", i, got[i].Field, want[i].field)
		}
		if got[i].TaskID != want[i].taskID {
			t.Errorf("Owners[%d].TaskID = %q, want %q", i, got[i].TaskID, want[i].taskID)
		}
		if !sameOutputIndex(got[i].OutputIndex, want[i].outputIndex) {
			t.Errorf("Owners[%d].OutputIndex = %v, want %v", i, got[i].OutputIndex, want[i].outputIndex)
		}
	}
}

func assertOwnerDetails(t *testing.T, got any, want []ownerWant) {
	t.Helper()
	owners, ok := got.([]map[string]any)
	if !ok {
		t.Fatalf("SafeDetails owners type = %T, want []map[string]any", got)
	}
	if len(owners) != len(want) {
		t.Fatalf("SafeDetails owner count = %d, want %d: %#v", len(owners), len(want), owners)
	}
	for i := range want {
		if owners[i]["field"] != want[i].field {
			t.Errorf("SafeDetails owners[%d].field = %v, want %q", i, owners[i]["field"], want[i].field)
		}
		if want[i].taskID != "" && owners[i]["task_id"] != want[i].taskID {
			t.Errorf("SafeDetails owners[%d].task_id = %v, want %q", i, owners[i]["task_id"], want[i].taskID)
		}
		if want[i].outputIndex != nil && owners[i]["output_index"] != *want[i].outputIndex {
			t.Errorf("SafeDetails owners[%d].output_index = %v, want %d", i, owners[i]["output_index"], *want[i].outputIndex)
		}
	}
}
