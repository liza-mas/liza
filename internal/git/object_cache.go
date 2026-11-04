package git

import "sync"

// objectQueries memoizes answers that a full object ID fixes forever: the
// entry at a path in a commit or tree, a blob's bytes, and an ancestry that
// holds. Git objects are content-addressed, so these answers cannot change
// while the object exists. Entries are keyed by project root as well, so one
// repository never answers for another that may lack the object.
//
// Only successes are stored: a missing object may arrive by a later fetch.
// "Not an ancestor" is not stored either, since deepening a shallow clone can
// reveal ancestry.
//
// Ceiling: `git replace` refs and pruned objects would make a stored answer
// stale. Liza creates neither; a repository that uses replace refs needs this
// cache keyed by its replace refs, or bypassed.
var objectQueries = struct {
	sync.Mutex
	entries map[objectQueryKey]any
	bytes   int
}{entries: map[objectQueryKey]any{}}

type objectQueryKey struct {
	root  string
	query string
	a, b  string
}

const (
	// maxObjectQueryEntries and maxObjectQueryBytes bound the memo; reaching
	// either clears it, so a long-lived supervisor holds at most this much.
	maxObjectQueryEntries = 8192
	maxObjectQueryBytes   = 32 << 20
	// maxCachedBlobBytes keeps one large blob from displacing everything else.
	maxCachedBlobBytes = 1 << 20
)

func cachedObjectQuery(key objectQueryKey) (any, bool) {
	objectQueries.Lock()
	defer objectQueries.Unlock()
	value, ok := objectQueries.entries[key]
	return value, ok
}

func storeObjectQuery(key objectQueryKey, value any, size int) {
	objectQueries.Lock()
	defer objectQueries.Unlock()
	if len(objectQueries.entries) >= maxObjectQueryEntries || objectQueries.bytes+size > maxObjectQueryBytes {
		clear(objectQueries.entries)
		objectQueries.bytes = 0
	}
	objectQueries.entries[key] = value
	objectQueries.bytes += size
}

// isFullObjectID reports whether s is a complete SHA-1 or SHA-256 object ID.
// Abbreviations and ref names are excluded: what they resolve to can change.
func isFullObjectID(s string) bool {
	if len(s) != 40 && len(s) != 64 {
		return false
	}
	for _, r := range s {
		if (r < '0' || r > '9') && (r < 'a' || r > 'f') {
			return false
		}
	}
	return true
}
