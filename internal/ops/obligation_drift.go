package ops

import (
	"fmt"
	"slices"
	"sort"
	"time"

	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/git"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/paths"
	"github.com/liza-mas/liza/internal/referencecontract"
)

// Obligation drift is the half of reference freshness that approval does not
// cover. An approved proof is compared by content at every acceptance boundary
// (see reviewedReferencesResolveAlike), so repointing one at material no
// reviewer saw fails closed. A reference that only backs an obligation is not
// compared there, deliberately: a re-pin onto legitimately extended content is
// indistinguishable from a substitution, and blocking it strands every child of
// a merged plan whose review boundary no command can move.
//
// So this reports instead of refusing. Drift enters only through a merge —
// either one that edits a referenced section under a pin that stays put
// (stale), or one that moves a carrier's pins (repinned, retargeted, dropped)
// — which is why detection runs there and nowhere hotter. Prompt construction
// discloses the same drift to the agent but writes nothing.

// obligationDriftKey identifies one changed section. Two carriers whose
// obligations rest on the same section share a key, so one repin that touches
// seven plans is one event, not seven: the fact a reviewer must judge is that
// the section moved, not that each carrier noticed.
type obligationDriftKey struct {
	path    string
	heading string
	current string
}

// obligationDrift accumulates who is affected by one changed section.
type obligationDrift struct {
	key          obligationDriftKey
	change       obligationDriftChange
	reviewed     string
	carriers     map[string]bool
	obligations  map[string]bool
	referenceIDs map[string]bool
}

// obligationDriftChange is how a section's backing moved. It is converted to
// string where it is written to an anomaly's Details, so the persisted bytes
// and comparisons against persisted values are those of the plain string.
type obligationDriftChange string

const (
	obligationDriftRepinned   obligationDriftChange = "repinned"
	obligationDriftRetargeted obligationDriftChange = "retargeted"
	// obligationDriftDropped names a section the obligation rested on and no
	// longer does. Its current_section is empty: there is nothing to read.
	obligationDriftDropped obligationDriftChange = "dropped"
	// obligationDriftStale names a section edited under a pin that did not
	// move. Its reviewed_section is the text at the carrier's current pin,
	// which a post-review re-pin may have made unreviewed; its current_section
	// is empty when the section no longer resolves at the merge commit.
	obligationDriftStale obligationDriftChange = "stale"
)

// driftRecorder is the driftAccumulator.record method the detectors feed.
type driftRecorder func(key obligationDriftKey, change obligationDriftChange, reviewedSection, carrier, obligation string, referenceIDs []string)

// detectStaleUnderPins compares every obligation-backing reference of the
// carrier at integrationCommit with the same path and heading at that commit,
// and records each section whose text moved away from its pin. Blob identity
// is the fast path, as at prompt build: an edit elsewhere in the file is not
// drift.
func detectStaleUnderPins(cache blobCache, blobOID func(revision, path string) (string, bool), root, integrationCommit, carrier string, record driftRecorder) {
	current, ok := carrierReferences(root, integrationCommit, carrier)
	if !ok {
		return
	}
	backing := obligationBackedReferences(current)
	for _, obligation := range sortedObligations(backing) {
		for _, referenceID := range backing[obligation] {
			target, found := referenceTarget(current, referenceID)
			if !found {
				continue
			}
			pin := target.EffectiveRevision(current.SourceRevision)
			pinnedBlob, pinnedOK := blobOID(pin, target.Path)
			integrationBlob, integrationOK := blobOID(integrationCommit, target.Path)
			if pinnedOK && integrationOK && pinnedBlob == integrationBlob {
				continue
			}
			pinned, resolved := resolveDeclaredReferenceCached(cache, root, current, referenceID)
			if !resolved {
				// An unreadable pin is refused at prompt build; there is no
				// reviewed text to report against.
				continue
			}
			section := ""
			if content, readable := cache.read(root, integrationCommit, target.Path); readable {
				if span, err := referenceSection(content, target.Heading); err == nil {
					section = spanObjectID(span)
				}
			}
			if section == spanObjectID(pinned) {
				continue
			}
			record(obligationDriftKey{target.Path, target.Heading, section},
				obligationDriftStale, spanObjectID(pinned), carrier, obligation, []string{referenceID})
		}
	}
}

// RecordObligationContentDrift compares what every obligation-backing reference
// resolves to at the review commit that authorized a child against what it
// resolves to at integrationCommit, and records one anomaly per changed
// section. It never blocks: the caller treats a failure as a warning.
//
// Scope is the carriers that actually authorize work — those named by a task's
// AcceptanceSource — so an unreferenced plan in the tree costs nothing.
func RecordObligationContentDrift(bb *db.Blackboard, projectRoot, integrationCommit, reporter string) ([]string, error) {
	state, err := bb.Read()
	if err != nil {
		return nil, err
	}
	drifts := detectObligationDrift(state, projectRoot, integrationCommit)
	if len(drifts) == 0 || allDriftRecorded(state.Anomalies, drifts) {
		// A stale section persists across every merge until its carrier is
		// re-pinned. Re-observing it must not cost a lock-held rewrite of the
		// whole state each time; Modify below re-checks under the lock anyway.
		return nil, nil
	}

	var recorded []string
	err = bb.Modify(func(s *models.State) error {
		recorded = nil
		now := time.Now().UTC()
		for _, drift := range drifts {
			if existing := findObligationDrift(s.Anomalies, drift.key); existing != nil {
				// The section is already reported at this content. Widen who
				// it affects; a later merge can pull in another carrier.
				existing.Details["carriers"] = sortedKeys(drift.carriers)
				existing.Details["obligations"] = sortedKeys(drift.obligations)
				existing.Details["reference_ids"] = sortedKeys(drift.referenceIDs)
				// A later carrier can retarget onto a section already reported
				// as a re-pin. The record must carry the stronger claim and
				// the evidence it rests on, or the reviewer reads a
				// substitution as a routine move.
				if retargetOutranksRecord(drift, existing) {
					existing.Details["change"] = string(drift.change)
					existing.Details["reviewed_section"] = drift.reviewed
				}
				continue
			}
			s.Anomalies = append(s.Anomalies, models.Anomaly{
				Timestamp: now,
				Reporter:  agentIDOrSystem(reporter),
				Type:      models.AnomalyTypeObligationContentDrifted,
				Details: map[string]any{
					"path":             drift.key.path,
					"heading":          drift.key.heading,
					"change":           string(drift.change),
					"reviewed_section": drift.reviewed,
					"current_section":  drift.key.current,
					"carriers":         sortedKeys(drift.carriers),
					"obligations":      sortedKeys(drift.obligations),
					"reference_ids":    sortedKeys(drift.referenceIDs),
				},
			})
			recorded = append(recorded,
				fmt.Sprintf("%s#%s (%s)", drift.key.path, drift.key.heading, drift.change))
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return recorded, nil
}

// driftAccumulator collects who is affected by each changed section, keyed so
// that one section reached through several carriers is one event, in the order
// its first observer reported it.
type driftAccumulator struct {
	byKey map[obligationDriftKey]*obligationDrift
	order []obligationDriftKey
}

func newDriftAccumulator() *driftAccumulator {
	return &driftAccumulator{byKey: map[obligationDriftKey]*obligationDrift{}}
}

// record is the driftRecorder of one detection pass.
func (a *driftAccumulator) record(key obligationDriftKey, change obligationDriftChange, reviewedSection, carrier, obligation string, referenceIDs []string) {
	drift, seen := a.byKey[key]
	if !seen {
		drift = &obligationDrift{
			key: key, change: change, reviewed: reviewedSection,
			carriers: map[string]bool{}, obligations: map[string]bool{}, referenceIDs: map[string]bool{},
		}
		a.byKey[key] = drift
		a.order = append(a.order, key)
	}
	if change == obligationDriftRetargeted && drift.change != obligationDriftRetargeted {
		// A retarget is the substitution this exists to surface; it outranks a
		// re-pin reported for the same section, and carries the evidence the
		// stronger claim rests on.
		drift.change = obligationDriftRetargeted
		drift.reviewed = reviewedSection
	}
	drift.carriers[carrier] = true
	drift.obligations[obligation] = true
	for _, referenceID := range referenceIDs {
		drift.referenceIDs[referenceID] = true
	}
}

// drifts returns the accumulated sections in first-reported order.
func (a *driftAccumulator) drifts() []obligationDrift {
	result := make([]obligationDrift, 0, len(a.order))
	for _, key := range a.order {
		result = append(result, *a.byKey[key])
	}
	return result
}

// blobIdentities reads Git blob object IDs once per (revision, path). An
// unreadable blob is reported as absent, never as an error: drift is advisory.
type blobIdentities struct {
	repo *git.Git
	oids map[string]string
}

func newBlobIdentities(projectRoot string) *blobIdentities {
	return &blobIdentities{repo: git.New(projectRoot), oids: map[string]string{}}
}

func (b *blobIdentities) oid(revision, path string) (string, bool) {
	key := revision + "\x00" + path
	if oid, ok := b.oids[key]; ok {
		return oid, true
	}
	oid, err := b.repo.BlobOID(revision, path)
	if err != nil {
		return "", false
	}
	b.oids[key] = oid
	return oid, true
}

// sameBlob reports whether both revisions carry path as one readable blob.
func (b *blobIdentities) sameBlob(revisionA, revisionB, path string) bool {
	blobA, okA := b.oid(revisionA, path)
	blobB, okB := b.oid(revisionB, path)
	return okA && okB && blobA == blobB
}

// detectObligationDrift returns the changed sections, in stable order.
func detectObligationDrift(state *models.State, projectRoot, integrationCommit string) []obligationDrift {
	accumulator := newDriftAccumulator()
	// Carriers across a campaign cite the same protocol files, so one pass
	// resolves far fewer distinct blobs than it has references.
	cache := blobCache{}
	blobs := newBlobIdentities(projectRoot)
	staleChecked := map[string]bool{}
	for _, group := range acceptanceCarrierGroups(state) {
		// A section edited under a pin that did not move: the carrier is
		// unchanged, so the walk below cannot see it, and prompt construction
		// only discloses it. Bounded to carriers with live children — a plan
		// whose children all finished can no longer strand anything — and
		// checked once per carrier, since it reads only the integration side.
		if group.live && !staleChecked[group.path] {
			staleChecked[group.path] = true
			detectStaleUnderPins(cache, blobs.oid, projectRoot, integrationCommit, group.path, accumulator.record)
		}

		// An unchanged carrier declares the same references at the same pinned
		// revisions, so no pin it rests on can have moved. Most merges touch
		// no carrier at all, and this keeps their cost at one blob read per
		// carrier instead of two git reads per declared reference.
		if blobs.sameBlob(group.reviewCommit, integrationCommit, group.path) {
			continue
		}

		reviewed, reviewedOK := carrierReferences(projectRoot, group.reviewCommit, group.path)
		current, currentOK := carrierReferences(projectRoot, integrationCommit, group.path)
		if !reviewedOK || !currentOK {
			// A carrier that no longer parses is an acceptance failure, caught
			// where acceptance is enforced. Reporting it again here would
			// duplicate that signal under a type that means something else.
			continue
		}
		recordCarrierDrift(cache, projectRoot, reviewed, current, group.path, accumulator.record)
	}
	return accumulator.drifts()
}

// acceptanceCarrierGroup is one carrier at one authorizing review commit.
// live reports whether any child it authorizes is still non-terminal.
type acceptanceCarrierGroup struct {
	path         string
	reviewCommit string
	live         bool
}

// acceptanceCarrierGroups lists the distinct (carrier, review commit) pairs
// that authorize work. Children sharing an allocation share a group, so the
// walk costs one comparison per carrier rather than one per child.
func acceptanceCarrierGroups(state *models.State) []acceptanceCarrierGroup {
	index := map[[2]string]int{}
	var groups []acceptanceCarrierGroup
	for i := range state.Tasks {
		task := &state.Tasks[i]
		source := task.AcceptanceSource
		if source == nil || source.ParentReviewCommit == "" || source.Ref == "" {
			continue
		}
		key := [2]string{paths.SplitRefFile(source.Ref), source.ParentReviewCommit}
		if at, seen := index[key]; seen {
			groups[at].live = groups[at].live || !task.Status.IsTerminal()
			continue
		}
		index[key] = len(groups)
		groups = append(groups, acceptanceCarrierGroup{path: key[0], reviewCommit: key[1], live: !task.Status.IsTerminal()})
	}
	sort.Slice(groups, func(i, j int) bool {
		if groups[i].path != groups[j].path {
			return groups[i].path < groups[j].path
		}
		return groups[i].reviewCommit < groups[j].reviewCommit
	})
	return groups
}

// obligationBackedReferences maps each obligation to the references backing it.
// An approved proof appears here too and is compared the same way; it simply
// cannot differ, because the acceptance boundary already refuses that case.
func obligationBackedReferences(contract *referencecontract.Contract) map[string][]string {
	backing := map[string][]string{}
	for _, coverage := range contract.ObligationCoverage {
		backing[coverage.ID] = append(backing[coverage.ID], coverage.ReferenceIDs...)
	}
	return backing
}

func sortedObligations(backing map[string][]string) []string {
	ids := make([]string, 0, len(backing))
	for id := range backing {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

func referenceTarget(contract *referencecontract.Contract, referenceID string) (referencecontract.DirectReference, bool) {
	for _, direct := range contract.DirectReferences {
		if direct.ID == referenceID {
			return direct, true
		}
	}
	return referencecontract.DirectReference{}, false
}

func findObligationDrift(anomalies []models.Anomaly, key obligationDriftKey) *models.Anomaly {
	for i := range anomalies {
		anomaly := &anomalies[i]
		if anomaly.Type != models.AnomalyTypeObligationContentDrifted {
			continue
		}
		if anomaly.Details["path"] == key.path &&
			anomaly.Details["heading"] == key.heading &&
			anomaly.Details["current_section"] == key.current {
			return anomaly
		}
	}
	return nil
}

// retargetOutranksRecord reports whether drift is a retarget that an existing
// record still describes as something weaker. The comparison is against the
// persisted string, which is a plain string whether written in this process or
// round-tripped through YAML.
func retargetOutranksRecord(drift obligationDrift, recorded *models.Anomaly) bool {
	return drift.change == obligationDriftRetargeted && recorded.Details["change"] != string(obligationDriftRetargeted)
}

// allDriftRecorded reports whether every drift already has a record that the
// widening in RecordObligationContentDrift would leave unchanged.
func allDriftRecorded(anomalies []models.Anomaly, drifts []obligationDrift) bool {
	for _, drift := range drifts {
		existing := findObligationDrift(anomalies, drift.key)
		if existing == nil {
			return false
		}
		if retargetOutranksRecord(drift, existing) {
			return false
		}
		for field, want := range map[string]map[string]bool{
			"carriers": drift.carriers, "obligations": drift.obligations, "reference_ids": drift.referenceIDs,
		} {
			if !slices.Equal(detailStrings(existing.Details[field]), sortedKeys(want)) {
				return false
			}
		}
	}
	return true
}

// detailStrings reads a recorded string list, which is []string when written
// in this process and []any once it has round-tripped through YAML.
func detailStrings(value any) []string {
	switch list := value.(type) {
	case []string:
		return list
	case []any:
		out := make([]string, 0, len(list))
		for _, item := range list {
			text, ok := item.(string)
			if !ok {
				return nil
			}
			out = append(out, text)
		}
		return out
	}
	return nil
}

func sortedKeys(set map[string]bool) []string {
	keys := make([]string, 0, len(set))
	for key := range set {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
