package ops

import (
	"fmt"
	"slices"
	"sort"
	"strings"
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
	change       string
	reviewed     string
	carriers     map[string]bool
	obligations  map[string]bool
	referenceIDs map[string]bool
}

const (
	obligationDriftRepinned   = "repinned"
	obligationDriftRetargeted = "retargeted"
	// obligationDriftDropped names a section the obligation rested on and no
	// longer does. Its current_section is empty: there is nothing to read.
	obligationDriftDropped = "dropped"
	// obligationDriftStale names a section edited under a pin that did not
	// move. Its reviewed_section is the text at the carrier's current pin,
	// which a post-review re-pin may have made unreviewed; its current_section
	// is empty when the section no longer resolves at the merge commit.
	obligationDriftStale = "stale"
)

// driftRecorder is the accumulator detectObligationDrift feeds.
type driftRecorder func(key obligationDriftKey, change, reviewedSection, carrier, obligation string, referenceIDs []string)

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
				if span, err := referencecontract.ExtractSection(strings.ReplaceAll(content, "\r\n", "\n"), target.Heading); err == nil {
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

// backingEntry is one section an obligation rests on: where it lives and what
// it says. Keying on the pair is what stops one reference's unchanged content
// from answering for another reference that moved.
type backingEntry struct {
	referenceLocation
	section string
}

// backingEntriesFor resolves an obligation's references to entries, mapping
// each to the reference IDs that reach it. Two references landing on the same
// section share one entry, because the obligation rests on it once.
func backingEntriesFor(cache blobCache, root string, contract *referencecontract.Contract, referenceIDs []string) map[backingEntry][]string {
	entries := map[backingEntry][]string{}
	for _, referenceID := range referenceIDs {
		span, ok := resolveDeclaredReferenceCached(cache, root, contract, referenceID)
		if !ok {
			continue
		}
		target, found := referenceTarget(contract, referenceID)
		if !found {
			continue
		}
		entry := backingEntry{referenceLocation{target.Path, target.Heading}, spanObjectID(span)}
		entries[entry] = append(entries[entry], referenceID)
	}
	return entries
}

func sortedEntries(entries map[backingEntry][]string) []backingEntry {
	keys := make([]backingEntry, 0, len(entries))
	for entry := range entries {
		keys = append(keys, entry)
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i].path != keys[j].path {
			return keys[i].path < keys[j].path
		}
		if keys[i].heading != keys[j].heading {
			return keys[i].heading < keys[j].heading
		}
		return keys[i].section < keys[j].section
	})
	return keys
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
				if drift.change == obligationDriftRetargeted && existing.Details["change"] != obligationDriftRetargeted {
					existing.Details["change"] = drift.change
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
					"change":           drift.change,
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

// detectObligationDrift returns the changed sections, in stable order.
func detectObligationDrift(state *models.State, projectRoot, integrationCommit string) []obligationDrift {
	byKey := map[obligationDriftKey]*obligationDrift{}
	var order []obligationDriftKey
	// Carriers across a campaign cite the same protocol files, so one pass
	// resolves far fewer distinct blobs than it has references.
	cache := blobCache{}

	var record driftRecorder = func(key obligationDriftKey, change, reviewedSection, carrier, obligation string, referenceIDs []string) {
		drift, seen := byKey[key]
		if !seen {
			drift = &obligationDrift{
				key: key, change: change, reviewed: reviewedSection,
				carriers: map[string]bool{}, obligations: map[string]bool{}, referenceIDs: map[string]bool{},
			}
			byKey[key] = drift
			order = append(order, key)
		}
		if change == obligationDriftRetargeted && drift.change != obligationDriftRetargeted {
			// A retarget is the substitution this exists to surface; it
			// outranks a re-pin reported for the same section, and carries the
			// evidence the stronger claim rests on.
			drift.change = obligationDriftRetargeted
			drift.reviewed = reviewedSection
		}
		drift.carriers[carrier] = true
		drift.obligations[obligation] = true
		for _, referenceID := range referenceIDs {
			drift.referenceIDs[referenceID] = true
		}
	}

	g := git.New(projectRoot)
	oids := map[string]string{}
	blobOID := func(revision, path string) (string, bool) {
		key := revision + "\x00" + path
		if oid, ok := oids[key]; ok {
			return oid, true
		}
		oid, err := g.BlobOID(revision, path)
		if err != nil {
			return "", false
		}
		oids[key] = oid
		return oid, true
	}
	staleChecked := map[string]bool{}
	for _, group := range acceptanceCarrierGroups(state) {
		// A section edited under a pin that did not move: the carrier is
		// unchanged, so the walk below cannot see it, and prompt construction
		// only discloses it. Bounded to carriers with live children — a plan
		// whose children all finished can no longer strand anything — and
		// checked once per carrier, since it reads only the integration side.
		if group.live && !staleChecked[group.path] {
			staleChecked[group.path] = true
			detectStaleUnderPins(cache, blobOID, projectRoot, integrationCommit, group.path, record)
		}

		// An unchanged carrier declares the same references at the same pinned
		// revisions, so no pin it rests on can have moved. Most merges touch
		// no carrier at all, and this keeps their cost at one blob read per
		// carrier instead of two git reads per declared reference.
		reviewedBlob, reviewedOK := blobOID(group.reviewCommit, group.path)
		integrationBlob, integrationOK := blobOID(integrationCommit, group.path)
		if reviewedOK && integrationOK && reviewedBlob == integrationBlob {
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
		// Walk obligations, not reference IDs. A reference ID is a name the
		// carrier chose; an obligation is what the reviewer approved. A
		// carrier that renames "counters" to "replacement", points it
		// elsewhere and updates Obligation Coverage leaves the allocation
		// section untouched, so acceptance sees nothing — and following the
		// reviewed IDs would find no such reference at integration and skip
		// the substitution in silence. Following the obligation finds it.
		reviewedBacking := obligationBackedReferences(reviewed)
		currentBacking := obligationBackedReferences(current)
		for _, obligation := range sortedObligations(reviewedBacking) {
			reviewedEntries := backingEntriesFor(cache, projectRoot, reviewed, reviewedBacking[obligation])
			currentEntries := backingEntriesFor(cache, projectRoot, current, currentBacking[obligation])
			if len(reviewedEntries) == 0 {
				continue
			}
			reviewedAt := map[referenceLocation]string{}
			for entry := range reviewedEntries {
				reviewedAt[entry.referenceLocation] = entry.section
			}

			// Compare (location, content) pairs, not content alone. A
			// reference moved onto a section a sibling already covers leaves
			// the obligation resting on one section fewer, and a content-only
			// comparison would let the sibling's unchanged text answer for the
			// reference that moved. Pairing keeps rename support — an
			// identical section under a new reference ID still matches — while
			// making each section the obligation rests on account for itself.
			addedLocations := map[referenceLocation]bool{}
			for _, entry := range sortedEntries(currentEntries) {
				if _, unchanged := reviewedEntries[entry]; unchanged {
					continue
				}
				reviewedSection, sameLocation := reviewedAt[entry.referenceLocation]
				change := obligationDriftRepinned
				if !sameLocation {
					// The approval never covered this section for this
					// obligation.
					change = obligationDriftRetargeted
					reviewedSection = anyReviewedSection(reviewedAt)
				}
				addedLocations[entry.referenceLocation] = true
				record(obligationDriftKey{entry.path, entry.heading, entry.section},
					change, reviewedSection, group.path, obligation, currentEntries[entry])
			}

			// A section the obligation rested on and no longer does. Suppressed
			// where the same location also gained content, because that pair is
			// one re-pin rather than a loss and a gain.
			for _, entry := range sortedEntries(reviewedEntries) {
				if _, kept := currentEntries[entry]; kept {
					continue
				}
				if addedLocations[entry.referenceLocation] {
					continue
				}
				record(obligationDriftKey{entry.path, entry.heading, ""},
					obligationDriftDropped, entry.section, group.path, obligation, reviewedEntries[entry])
			}
		}
	}

	result := make([]obligationDrift, 0, len(order))
	for _, key := range order {
		result = append(result, *byKey[key])
	}
	return result
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

// referenceLocation is what a reference points at, independent of the revision
// it is pinned at and of the ID the carrier gave it.
type referenceLocation struct {
	path    string
	heading string
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

// anyReviewedSection returns the content the obligation rested on when no
// reviewed reference named the section it rests on now, so the record still
// carries both sides of the comparison.
func anyReviewedSection(reviewedAt map[referenceLocation]string) string {
	locations := make([]referenceLocation, 0, len(reviewedAt))
	for location := range reviewedAt {
		locations = append(locations, location)
	}
	sort.Slice(locations, func(i, j int) bool {
		if locations[i].path != locations[j].path {
			return locations[i].path < locations[j].path
		}
		return locations[i].heading < locations[j].heading
	})
	if len(locations) == 0 {
		return ""
	}
	return reviewedAt[locations[0]]
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

// allDriftRecorded reports whether every drift already has a record that the
// widening in RecordObligationContentDrift would leave unchanged.
func allDriftRecorded(anomalies []models.Anomaly, drifts []obligationDrift) bool {
	for _, drift := range drifts {
		existing := findObligationDrift(anomalies, drift.key)
		if existing == nil {
			return false
		}
		if drift.change == obligationDriftRetargeted && existing.Details["change"] != obligationDriftRetargeted {
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
