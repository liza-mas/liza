package ops

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/testhelpers"
)

// driftFixture builds two plan carriers whose obligations rest on the same
// source section, then moves that section and re-pins both carriers — the
// shape a re-pin task produces across a campaign's plans.
type driftFixture struct {
	root         string
	reviewCommit string
	integration  string
}

func writeDriftFile(t *testing.T, root, name, contents string) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatal(err)
	}
}

// The reference carries an explicit per-reference revision override, as every
// carrier in a re-pinned campaign does: a re-pin rewrites that override, and
// the reviewed and current values therefore always differ.
func driftCarrier(revision, target, obligation string) string {
	return driftCarrierNamed(revision, "counters", target, obligation)
}

// driftCarrierNamed lets a carrier rename its reference, which is what makes
// obligation-keyed comparison necessary: the ID is the carrier's own label.
func driftCarrierNamed(revision, referenceID, target, obligation string) string {
	return fmt.Sprintf("# Plan\n\n## Task 1\nDo the work.\n\n## Source References\nSource revision: %q\n\n"+
		"### Direct References\n- %q: %q @ %q\n\n### Obligation Coverage\n- %q -> %q\n",
		revision, referenceID, target, revision, obligation, referenceID)
}

// Modes for plan-b at integration.
const (
	driftModeRepin        = "repin"         // both carriers re-pinned onto the moved section
	driftModeRetargetAway = "retarget-away" // plan-b points at a section the approval never covered
	driftModeRenameAway   = "rename-away"   // plan-b renames its reference and retargets it
	driftModeRetargetOnto = "retarget-onto" // plan-b moves onto the section plan-a rests on
)

// setupDriftFixture returns carriers pinned at the reviewed revision, then
// moves the section they rest on and re-pins them at integration. The mode
// decides what plan-b does at integration.
func setupDriftFixture(t *testing.T, mode string) driftFixture {
	t.Helper()
	root := t.TempDir()
	testhelpers.SetupTestGitRepo(t, root)

	writeDriftFile(t, root, "specs/source.md", "# Source\n\n## Counters\nCount observations.\n\n## Other\nUnrelated.\n")
	testhelpers.MustGit(t, root, "add", "--all")
	testhelpers.MustGit(t, root, "commit", "-m", "test: record source")
	pinned := testhelpers.MustGit(t, root, "rev-parse", "HEAD")

	reviewedB := "specs/source.md#Counters"
	if mode == driftModeRetargetOnto {
		reviewedB = "specs/source.md#Other"
	}
	writeDriftFile(t, root, "specs/plan-a.md", driftCarrier(pinned, "specs/source.md#Counters", "AC-1"))
	writeDriftFile(t, root, "specs/plan-b.md", driftCarrier(pinned, reviewedB, "AC-2"))
	testhelpers.MustGit(t, root, "add", "--all")
	testhelpers.MustGit(t, root, "commit", "-m", "test: reviewed carriers")
	reviewCommit := testhelpers.MustGit(t, root, "rev-parse", "HEAD")

	// The section legitimately grows, as output 0 extended lifecycle-results.
	writeDriftFile(t, root, "specs/source.md",
		"# Source\n\n## Counters\nCount observations.\nAlso count coverage and skipped observations.\n\n## Other\nUnrelated.\n")
	testhelpers.MustGit(t, root, "add", "--all")
	testhelpers.MustGit(t, root, "commit", "-m", "test: extend the counted section")
	moved := testhelpers.MustGit(t, root, "rev-parse", "HEAD")

	writeDriftFile(t, root, "specs/plan-a.md", driftCarrier(moved, "specs/source.md#Counters", "AC-1"))
	switch mode {
	case driftModeRetargetAway:
		writeDriftFile(t, root, "specs/plan-b.md", driftCarrier(moved, "specs/source.md#Other", "AC-2"))
	case driftModeRenameAway:
		writeDriftFile(t, root, "specs/plan-b.md", driftCarrierNamed(moved, "replacement", "specs/source.md#Other", "AC-2"))
	case driftModeRetargetOnto:
		writeDriftFile(t, root, "specs/plan-b.md", driftCarrier(moved, "specs/source.md#Counters", "AC-2"))
	default:
		writeDriftFile(t, root, "specs/plan-b.md", driftCarrier(moved, "specs/source.md#Counters", "AC-2"))
	}
	testhelpers.MustGit(t, root, "add", "--all")
	testhelpers.MustGit(t, root, "commit", "-m", "test: re-pin carriers onto the moved section")

	return driftFixture{root: root, reviewCommit: reviewCommit, integration: testhelpers.MustGit(t, root, "rev-parse", "HEAD")}
}

func driftState(reviewCommit string, refs ...string) *models.State {
	state := &models.State{Version: 1, Config: models.Config{IntegrationBranch: "main"}}
	for i, ref := range refs {
		state.Tasks = append(state.Tasks, models.Task{
			ID:     fmt.Sprintf("child-%d", i),
			Status: models.TaskStatusReady,
			AcceptanceSource: &models.AcceptanceSource{
				Ref: ref, ParentTask: "planner", ParentReviewCommit: reviewCommit,
			},
		})
	}
	return state
}

// One re-pin across two plans is one fact a reviewer must judge, not two. The
// event is keyed on the section that moved, so both carriers and both
// obligations arrive on a single record.
func TestObligationDriftRecordsOnePerChangedSection(t *testing.T) {
	fixture := setupDriftFixture(t, driftModeRepin)
	state := driftState(fixture.reviewCommit, "specs/plan-a.md#Task 1", "specs/plan-b.md#Task 1")

	drifts := detectObligationDrift(state, fixture.root, fixture.integration)
	if len(drifts) != 1 {
		t.Fatalf("detected %d drifts, want 1 for one moved section: %+v", len(drifts), drifts)
	}
	drift := drifts[0]
	if drift.key.path != "specs/source.md" || drift.key.heading != "Counters" {
		t.Errorf("drift names %s#%s, want specs/source.md#Counters", drift.key.path, drift.key.heading)
	}
	if drift.change != obligationDriftRepinned {
		t.Errorf("change = %q, want %q: the target heading did not move", drift.change, obligationDriftRepinned)
	}
	if got := sortedKeys(drift.carriers); !reflect.DeepEqual(got, []string{"specs/plan-a.md", "specs/plan-b.md"}) {
		t.Errorf("carriers = %v, want both plans: a reviewer needs the full blast radius", got)
	}
	if got := sortedKeys(drift.obligations); !reflect.DeepEqual(got, []string{"AC-1", "AC-2"}) {
		t.Errorf("obligations = %v, want [AC-1 AC-2]", got)
	}
	if drift.reviewed == drift.key.current {
		t.Error("reviewed and current section identities are equal; nothing drifted")
	}
}

// A reference repointed at a different heading is the substitution the
// approval never covered, and outranks a re-pin reported for the same section.
func TestObligationDriftDistinguishesRetargetFromRepin(t *testing.T) {
	fixture := setupDriftFixture(t, driftModeRetargetAway)
	state := driftState(fixture.reviewCommit, "specs/plan-a.md#Task 1", "specs/plan-b.md#Task 1")

	drifts := detectObligationDrift(state, fixture.root, fixture.integration)
	byHeading := map[string]obligationDrift{}
	for _, drift := range drifts {
		byHeading[drift.key.heading] = drift
	}
	other, ok := byHeading["Other"]
	if !ok {
		t.Fatalf("no drift recorded for the retargeted heading: %+v", drifts)
	}
	if other.change != obligationDriftRetargeted {
		t.Errorf("change = %q, want %q for a reference pointed at a different heading",
			other.change, obligationDriftRetargeted)
	}
	if got := sortedKeys(other.obligations); !reflect.DeepEqual(got, []string{"AC-2"}) {
		t.Errorf("obligations = %v, want only the retargeted carrier's", got)
	}
}

// A carrier whose references still resolve to the approved content is not an
// event. Reporting it would bury the ones that are.
func TestObligationDriftIgnoresUnchangedReferences(t *testing.T) {
	fixture := setupDriftFixture(t, driftModeRepin)
	state := driftState(fixture.reviewCommit, "specs/plan-a.md#Task 1")

	if drifts := detectObligationDrift(state, fixture.root, fixture.reviewCommit); len(drifts) != 0 {
		t.Fatalf("detected %d drifts comparing the reviewed commit with itself: %+v", len(drifts), drifts)
	}
}

// The record is a reviewable event, never a gate: it must not touch the tasks
// whose obligations drifted, and re-running must not pile up duplicates.
func TestRecordObligationContentDriftReportsWithoutBlocking(t *testing.T) {
	fixture := setupDriftFixture(t, driftModeRepin)
	stateFile := filepath.Join(fixture.root, "state.yaml")
	bb := testhelpers.WriteInitialState(t, stateFile,
		driftState(fixture.reviewCommit, "specs/plan-a.md#Task 1", "specs/plan-b.md#Task 1"))

	recorded, err := RecordObligationContentDrift(bb, fixture.root, fixture.integration, "coder-1")
	if err != nil {
		t.Fatalf("RecordObligationContentDrift: %v", err)
	}
	if len(recorded) != 1 {
		t.Fatalf("recorded %d events, want 1: %v", len(recorded), recorded)
	}

	after, err := bb.Read()
	if err != nil {
		t.Fatal(err)
	}
	if len(after.Anomalies) != 1 || after.Anomalies[0].Type != models.AnomalyTypeObligationContentDrifted {
		t.Fatalf("anomalies = %+v, want one %s", after.Anomalies, models.AnomalyTypeObligationContentDrifted)
	}
	for _, task := range after.Tasks {
		if task.Status != models.TaskStatusReady {
			t.Errorf("task %s moved to %s; drift reporting must not block work", task.ID, task.Status)
		}
	}

	// Same section, same content: the second merge widens the record instead
	// of adding one, so a campaign-wide re-pin stays one event.
	if _, err := RecordObligationContentDrift(bb, fixture.root, fixture.integration, "coder-2"); err != nil {
		t.Fatalf("second RecordObligationContentDrift: %v", err)
	}
	again, err := bb.Read()
	if err != nil {
		t.Fatal(err)
	}
	if len(again.Anomalies) != 1 {
		t.Errorf("anomalies = %d after re-running, want 1 deduplicated record", len(again.Anomalies))
	}
}

// setupUnchangedPinFixture pins plan-a at the reviewed source revision, then
// edits the pinned section at integration without touching the carrier: the
// D76 shape, which no carrier comparison can see.
func setupUnchangedPinFixture(t *testing.T) driftFixture {
	t.Helper()
	root := t.TempDir()
	testhelpers.SetupTestGitRepo(t, root)
	writeDriftFile(t, root, "specs/source.md", "# Source\n\n## Counters\nCount observations.\n\n## Other\nUnrelated.\n")
	testhelpers.MustGit(t, root, "add", "--all")
	testhelpers.MustGit(t, root, "commit", "-m", "test: record source")
	pinned := testhelpers.MustGit(t, root, "rev-parse", "HEAD")
	writeDriftFile(t, root, "specs/plan-a.md", driftCarrier(pinned, "specs/source.md#Counters", "AC-1"))
	testhelpers.MustGit(t, root, "add", "--all")
	testhelpers.MustGit(t, root, "commit", "-m", "test: reviewed carrier")
	reviewCommit := testhelpers.MustGit(t, root, "rev-parse", "HEAD")
	writeDriftFile(t, root, "specs/source.md",
		"# Source\n\n## Counters\nCount observations.\n\n| recorder | attestation |\n|---|---|\n\n## Other\nUnrelated.\n")
	testhelpers.MustGit(t, root, "add", "--all")
	testhelpers.MustGit(t, root, "commit", "-m", "test: append a table inside the pinned section")
	return driftFixture{root: root, reviewCommit: reviewCommit, integration: testhelpers.MustGit(t, root, "rev-parse", "HEAD")}
}

// A merge that edits a pinned section while the carrier stays put is the
// drift prompt build now discloses instead of refusing; the merge must leave a
// reviewable record of it, once.
func TestObligationDriftRecordsSectionEditedUnderAnUnchangedPin(t *testing.T) {
	fixture := setupUnchangedPinFixture(t)
	state := driftState(fixture.reviewCommit, "specs/plan-a.md#Task 1")

	drifts := detectObligationDrift(state, fixture.root, fixture.integration)
	if len(drifts) != 1 {
		t.Fatalf("detected %d drifts, want 1 for the section edited under an unchanged pin: %+v", len(drifts), drifts)
	}
	drift := drifts[0]
	if drift.key.path != "specs/source.md" || drift.key.heading != "Counters" || drift.change != "stale" {
		t.Errorf("drift = %s#%s (%s), want specs/source.md#Counters (stale)", drift.key.path, drift.key.heading, drift.change)
	}
	if got := sortedKeys(drift.carriers); !reflect.DeepEqual(got, []string{"specs/plan-a.md"}) {
		t.Errorf("carriers = %v, want [specs/plan-a.md]", got)
	}
	if got := sortedKeys(drift.obligations); !reflect.DeepEqual(got, []string{"AC-1"}) {
		t.Errorf("obligations = %v, want [AC-1]", got)
	}
	if drift.reviewed == drift.key.current || drift.key.current == "" {
		t.Errorf("reviewed %q / current %q: want two distinct section identities", drift.reviewed, drift.key.current)
	}

	stateFile := filepath.Join(fixture.root, "state.yaml")
	bb := testhelpers.WriteInitialState(t, stateFile, state)
	recorded, err := RecordObligationContentDrift(bb, fixture.root, fixture.integration, "coder-1")
	if err != nil || len(recorded) != 1 {
		t.Fatalf("first record = %v, %v; want one event", recorded, err)
	}
	written, err := os.Stat(stateFile)
	if err != nil {
		t.Fatal(err)
	}
	// Every later merge re-observes the same pin and section. It must widen,
	// not repeat, or each merge re-announces a drift already on record.
	writeDriftFile(t, fixture.root, "README.md", "unrelated\n")
	testhelpers.MustGit(t, fixture.root, "add", "README.md")
	testhelpers.MustGit(t, fixture.root, "commit", "-m", "test: unrelated merge")
	later := testhelpers.MustGit(t, fixture.root, "rev-parse", "HEAD")
	recorded, err = RecordObligationContentDrift(bb, fixture.root, later, "coder-2")
	if err != nil || len(recorded) != 0 {
		t.Fatalf("re-observed record = %v, %v; want no new event", recorded, err)
	}
	// Nothing to widen means no lock-held rewrite of the whole state.
	if rewritten, err := os.Stat(stateFile); err != nil || !rewritten.ModTime().Equal(written.ModTime()) {
		t.Errorf("state rewritten on a re-observed drift (stat err %v)", err)
	}
	after, err := bb.Read()
	if err != nil {
		t.Fatal(err)
	}
	if len(after.Anomalies) != 1 {
		t.Fatalf("anomalies = %d, want 1 deduplicated record", len(after.Anomalies))
	}
}

// A pinned file deleted at the merge is stale with nothing current to read:
// the record carries the pinned section and an empty current section.
func TestObligationDriftRecordsStaleWithEmptySectionWhenPinnedFileIsDeleted(t *testing.T) {
	fixture := setupUnchangedPinFixture(t)
	testhelpers.MustGit(t, fixture.root, "rm", "-q", "specs/source.md")
	testhelpers.MustGit(t, fixture.root, "commit", "-m", "test: delete the pinned file")
	deleted := testhelpers.MustGit(t, fixture.root, "rev-parse", "HEAD")

	drifts := detectObligationDrift(driftState(fixture.reviewCommit, "specs/plan-a.md#Task 1"), fixture.root, deleted)
	if len(drifts) != 1 {
		t.Fatalf("detected %d drifts, want 1 for the deleted pinned file: %+v", len(drifts), drifts)
	}
	if drift := drifts[0]; drift.change != "stale" || drift.key.current != "" || drift.reviewed == "" {
		t.Errorf("drift = %+v, want stale with an empty current section and the pinned one reviewed", drift)
	}
}

// A plan whose children have all finished can no longer strand anything, so
// editing a section it pins is not an event: without this bound every docs
// merge would grow state with records about finished work.
func TestObligationDriftSkipsStaleUnderAPlanWithOnlyTerminalChildren(t *testing.T) {
	fixture := setupUnchangedPinFixture(t)
	state := driftState(fixture.reviewCommit, "specs/plan-a.md#Task 1", "specs/plan-a.md#Task 1")
	for i := range state.Tasks {
		state.Tasks[i].Status = models.TaskStatusMerged
	}
	if drifts := detectObligationDrift(state, fixture.root, fixture.integration); len(drifts) != 0 {
		t.Fatalf("detected %d drifts under a plan with only terminal children: %+v", len(drifts), drifts)
	}

	state.Tasks[1].Status = models.TaskStatusReady
	if drifts := detectObligationDrift(state, fixture.root, fixture.integration); len(drifts) != 1 {
		t.Fatalf("detected %d drifts with one live child, want 1: %+v", len(drifts), drifts)
	}
}

// Recording must satisfy the state validator, or the first merge that finds
// drift leaves a state no command can read back.
func TestRecordedObligationDriftAnomalyValidates(t *testing.T) {
	fixture := setupDriftFixture(t, driftModeRepin)
	stateFile := filepath.Join(fixture.root, "state.yaml")
	bb := testhelpers.WriteInitialState(t, stateFile,
		driftState(fixture.reviewCommit, "specs/plan-a.md#Task 1"))
	if _, err := RecordObligationContentDrift(bb, fixture.root, fixture.integration, "coder-1"); err != nil {
		t.Fatalf("RecordObligationContentDrift: %v", err)
	}
	state, err := bb.Read()
	if err != nil {
		t.Fatal(err)
	}
	anomaly := state.Anomalies[0]
	if !anomaly.IsValidType() {
		t.Fatalf("recorded anomaly type %q is not in the vocabulary", anomaly.Type)
	}
	for _, field := range []string{"path", "heading", "change", "reviewed_section", "current_section", "carriers", "obligations"} {
		if anomaly.Details[field] == nil {
			t.Errorf("recorded anomaly missing required detail %q", field)
		}
	}
}

// The substitution a reference ID cannot catch: the carrier renames its
// reference, points the new name at material the approval never covered, and
// updates Obligation Coverage to match. The allocation section is untouched,
// so acceptance sees nothing. Following the reviewed reference ID finds no
// such reference at integration and would skip it in silence; following the
// obligation finds it.
func TestObligationDriftCatchesRenamedAndRetargetedReference(t *testing.T) {
	fixture := setupDriftFixture(t, driftModeRenameAway)
	state := driftState(fixture.reviewCommit, "specs/plan-b.md#Task 1")

	// The trap this test exists for: the reviewed reference ID no longer
	// resolves at integration, so anything keyed on that ID skips the
	// substitution instead of reporting it.
	current, ok := carrierReferences(fixture.root, fixture.integration, "specs/plan-b.md")
	if !ok {
		t.Fatal("integration carrier does not parse")
	}
	if _, found := resolveDeclaredReferenceCached(nil, fixture.root, current, "counters"); found {
		t.Fatal("fixture no longer renames the reference; this test would pass on ID-keyed detection too")
	}

	// Two facts, both true: the obligation gained material the approval never
	// covered, and lost the material it did.
	drifts := byHeading(detectObligationDrift(state, fixture.root, fixture.integration))
	if len(drifts) != 2 {
		t.Fatalf("detected %d drifts, want the gain and the loss: %+v", len(drifts), drifts)
	}
	gained, ok := drifts["Other"]
	if !ok {
		t.Fatalf("no drift for the section the obligation now rests on: %+v", drifts)
	}
	if gained.change != obligationDriftRetargeted {
		t.Errorf("change = %q, want %q", gained.change, obligationDriftRetargeted)
	}
	if got := sortedKeys(gained.obligations); !reflect.DeepEqual(got, []string{"AC-2"}) {
		t.Errorf("obligations = %v, want [AC-2]: the obligation is what survived the rename", got)
	}
	if got := sortedKeys(gained.referenceIDs); !reflect.DeepEqual(got, []string{"replacement"}) {
		t.Errorf("reference_ids = %v, want the current name", got)
	}
	if gained.reviewed == "" || gained.reviewed == gained.key.current {
		t.Error("record lost the reviewed side of the comparison")
	}
	lost, ok := drifts["Counters"]
	if !ok {
		t.Fatalf("no drift for the section the obligation no longer rests on: %+v", drifts)
	}
	if lost.change != obligationDriftDropped || lost.key.current != "" {
		t.Errorf("lost section recorded as %q with current %q, want %q and empty",
			lost.change, lost.key.current, obligationDriftDropped)
	}
}

func byHeading(drifts []obligationDrift) map[string]obligationDrift {
	indexed := map[string]obligationDrift{}
	for _, drift := range drifts {
		indexed[drift.key.heading] = drift
	}
	return indexed
}

// The blocker a content-only comparison let through: an obligation backed by
// two sections has one of its references pointed at the other. Both current
// references then resolve to content the approval covered, so comparing
// content alone reports nothing — while the obligation has quietly stopped
// resting on the section that was redirected away.
func TestObligationDriftCatchesRetargetOntoAnUnchangedSibling(t *testing.T) {
	root := t.TempDir()
	testhelpers.SetupTestGitRepo(t, root)
	writeDriftFile(t, root, "specs/source.md", "# Source\n\n## Counters\nCount observations.\n\n## Other\nUnrelated.\n")
	testhelpers.MustGit(t, root, "add", "--all")
	testhelpers.MustGit(t, root, "commit", "-m", "test: record source")
	pinned := testhelpers.MustGit(t, root, "rev-parse", "HEAD")

	twoSection := func(revision, firstTarget string) string {
		return fmt.Sprintf("# Plan\n\n## Task 1\nDo the work.\n\n## Source References\nSource revision: %q\n\n"+
			"### Direct References\n- \"a\": %q @ %q\n- \"b\": \"specs/source.md#Other\" @ %q\n\n"+
			"### Obligation Coverage\n- \"AC-1\" -> \"a\", \"b\"\n", revision, firstTarget, revision, revision)
	}
	writeDriftFile(t, root, "specs/plan-a.md", twoSection(pinned, "specs/source.md#Counters"))
	testhelpers.MustGit(t, root, "add", "--all")
	testhelpers.MustGit(t, root, "commit", "-m", "test: reviewed carrier backed by two sections")
	reviewCommit := testhelpers.MustGit(t, root, "rev-parse", "HEAD")

	// "a" is redirected onto the section "b" already covers. No section
	// changed; the obligation simply stopped resting on Counters.
	writeDriftFile(t, root, "specs/plan-a.md", twoSection(pinned, "specs/source.md#Other"))
	testhelpers.MustGit(t, root, "add", "--all")
	testhelpers.MustGit(t, root, "commit", "-m", "test: redirect one reference onto its sibling")
	integration := testhelpers.MustGit(t, root, "rev-parse", "HEAD")

	drifts := detectObligationDrift(driftState(reviewCommit, "specs/plan-a.md#Task 1"), root, integration)
	if len(drifts) != 1 {
		t.Fatalf("detected %d drifts, want 1 for the section the obligation lost: %+v", len(drifts), drifts)
	}
	drift := drifts[0]
	if drift.key.heading != "Counters" || drift.change != obligationDriftDropped {
		t.Errorf("drift = %s#%s (%s), want specs/source.md#Counters (%s)",
			drift.key.path, drift.key.heading, drift.change, obligationDriftDropped)
	}
	if drift.reviewed == "" {
		t.Error("dropped record carries no evidence of what the obligation rested on")
	}
}

// A later merge can retarget onto a section already reported as a re-pin. The
// stored record must carry the stronger claim, or a reviewer reads a
// substitution as a routine move.
func TestRecordObligationContentDriftUpgradesRepinToRetarget(t *testing.T) {
	fixture := setupDriftFixture(t, driftModeRetargetOnto)
	stateFile := filepath.Join(fixture.root, "state.yaml")
	bb := testhelpers.WriteInitialState(t, stateFile, driftState(fixture.reviewCommit, "specs/plan-a.md#Task 1"))

	if _, err := RecordObligationContentDrift(bb, fixture.root, fixture.integration, "coder-1"); err != nil {
		t.Fatalf("first RecordObligationContentDrift: %v", err)
	}
	first, err := bb.Read()
	if err != nil {
		t.Fatal(err)
	}
	if len(first.Anomalies) != 1 || first.Anomalies[0].Details["change"] != obligationDriftRepinned {
		t.Fatalf("first record = %+v, want one %q", first.Anomalies, obligationDriftRepinned)
	}
	reviewedBefore := first.Anomalies[0].Details["reviewed_section"]

	// plan-b's obligation now rests on the same section, having been pointed
	// there from a heading the approval covered instead.
	if err := bb.Modify(func(s *models.State) error {
		s.Tasks = append(s.Tasks, models.Task{
			ID: "child-b", Status: models.TaskStatusReady,
			AcceptanceSource: &models.AcceptanceSource{
				Ref: "specs/plan-b.md#Task 1", ParentTask: "planner", ParentReviewCommit: fixture.reviewCommit,
			},
		})
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := RecordObligationContentDrift(bb, fixture.root, fixture.integration, "coder-2"); err != nil {
		t.Fatalf("second RecordObligationContentDrift: %v", err)
	}

	after, err := bb.Read()
	if err != nil {
		t.Fatal(err)
	}
	// Two records now: the shared section upgraded, and the section plan-b's
	// obligation stopped resting on.
	if len(after.Anomalies) != 2 {
		t.Fatalf("anomalies = %d, want the widened record plus the dropped section", len(after.Anomalies))
	}
	var anomaly models.Anomaly
	for _, candidate := range after.Anomalies {
		if candidate.Details["heading"] == "Counters" {
			anomaly = candidate
		}
	}
	if anomaly.Type == "" {
		t.Fatalf("no record for the shared section: %+v", after.Anomalies)
	}
	if anomaly.Details["change"] != obligationDriftRetargeted {
		t.Errorf("change = %v, want %q after a carrier retargeted onto this section",
			anomaly.Details["change"], obligationDriftRetargeted)
	}
	if anomaly.Details["reviewed_section"] == reviewedBefore {
		t.Error("reviewed_section still shows the re-pin's evidence, not the retarget's")
	}
	// Details survive a YAML round-trip, so the slice comes back as []any.
	if got := fmt.Sprint(anomaly.Details["obligations"]); got != "[AC-1 AC-2]" {
		t.Errorf("obligations = %s, want both", got)
	}
}
