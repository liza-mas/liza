package ops

import (
	"sort"

	"github.com/liza-mas/liza/internal/referencecontract"
)

// The reviewed-versus-current half of obligation drift: one carrier read at the
// commit that authorized its children and again at integration, compared per
// obligation. It reads only carriers, never the sections they point at beyond
// resolving them; the section-edited-under-a-pin half lives with
// detectStaleUnderPins.

// referenceLocation is what a reference points at, independent of the revision
// it is pinned at and of the ID the carrier gave it.
type referenceLocation struct {
	path    string
	heading string
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

// recordCarrierDrift compares every obligation of the reviewed carrier with the
// same obligation of the current one, and records each section whose backing
// moved.
//
// Walk obligations, not reference IDs. A reference ID is a name the carrier
// chose; an obligation is what the reviewer approved. A carrier that renames
// "counters" to "replacement", points it elsewhere and updates Obligation
// Coverage leaves the allocation section untouched, so acceptance sees nothing
// — and following the reviewed IDs would find no such reference at integration
// and skip the substitution in silence. Following the obligation finds it.
func recordCarrierDrift(cache blobCache, root string, reviewed, current *referencecontract.Contract, carrier string, record driftRecorder) {
	reviewedBacking := obligationBackedReferences(reviewed)
	currentBacking := obligationBackedReferences(current)
	for _, obligation := range sortedObligations(reviewedBacking) {
		reviewedEntries := backingEntriesFor(cache, root, reviewed, reviewedBacking[obligation])
		currentEntries := backingEntriesFor(cache, root, current, currentBacking[obligation])
		if len(reviewedEntries) == 0 {
			continue
		}
		recordObligationDrift(reviewedEntries, currentEntries, carrier, obligation, record)
	}
}

// recordObligationDrift records the sections one obligation gained, re-pinned
// or lost between the reviewed and the current carrier.
func recordObligationDrift(reviewedEntries, currentEntries map[backingEntry][]string, carrier, obligation string, record driftRecorder) {
	reviewedAt := map[referenceLocation]string{}
	for entry := range reviewedEntries {
		reviewedAt[entry.referenceLocation] = entry.section
	}

	// Compare (location, content) pairs, not content alone. A reference moved
	// onto a section a sibling already covers leaves the obligation resting on
	// one section fewer, and a content-only comparison would let the sibling's
	// unchanged text answer for the reference that moved. Pairing keeps rename
	// support — an identical section under a new reference ID still matches —
	// while making each section the obligation rests on account for itself.
	addedLocations := map[referenceLocation]bool{}
	for _, entry := range sortedEntries(currentEntries) {
		if _, unchanged := reviewedEntries[entry]; unchanged {
			continue
		}
		reviewedSection, sameLocation := reviewedAt[entry.referenceLocation]
		change := obligationDriftRepinned
		if !sameLocation {
			// The approval never covered this section for this obligation.
			change = obligationDriftRetargeted
			reviewedSection = anyReviewedSection(reviewedAt)
		}
		addedLocations[entry.referenceLocation] = true
		record(obligationDriftKey{entry.path, entry.heading, entry.section},
			change, reviewedSection, carrier, obligation, currentEntries[entry])
	}

	// A section the obligation rested on and no longer does. Suppressed where
	// the same location also gained content, because that pair is one re-pin
	// rather than a loss and a gain.
	for _, entry := range sortedEntries(reviewedEntries) {
		if _, kept := currentEntries[entry]; kept {
			continue
		}
		if addedLocations[entry.referenceLocation] {
			continue
		}
		record(obligationDriftKey{entry.path, entry.heading, ""},
			obligationDriftDropped, entry.section, carrier, obligation, reviewedEntries[entry])
	}
}
