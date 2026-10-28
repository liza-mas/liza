// Package archguard holds the architecture guards of the repository: the S1
// size guard and, later, the import-direction guard. It is test-only and uses
// only the standard library. Per-area data files (area_aN_test.go) are the
// seam through which each area's refactor shrinks the ratchet and records its
// layer entries; see ADR-0218.
package archguard

import "fmt"

// AreaID names one of the nine areas of the consolidation run (A1…A9).
type AreaID string

// Key identifies a measured unit by repo-relative slash path. Func is empty
// for a file key; otherwise it is the function name, receiver-qualified for a
// method (Type.Method, type parameters dropped), with "#n" appended to the
// n-th declaration (n ≥ 2) of a name repeated in the file.
type Key struct {
	File string
	Func string
}

// String renders the key as "file" or "file:Func" for messages.
func (k Key) String() string {
	if k.Func == "" {
		return k.File
	}
	return fmt.Sprintf("%s:%s", k.File, k.Func)
}

// RatchetEntry lists an S1 offender that may only shrink: Ceiling is the
// largest line count it may measure.
type RatchetEntry struct {
	Key     Key
	Ceiling int
}

// S2Category is the data-like kind of code allowed to exceed S1.
type S2Category string

// The S2 categories; any other exception needs a human ruling.
const (
	S2Table        S2Category = "table"
	S2Registration S2Category = "registration"
	S2Generated    S2Category = "generated"
	S2HumanRuling  S2Category = "human-ruling"
)

// S2Entry exempts a data-like offender from S1. Ruling names the human
// ruling and is required exactly for S2HumanRuling.
type S2Entry struct {
	Key       Key
	Category  S2Category
	Ruling    string
	Rationale string
}

// Layer is a package's layer in the import-direction rules. Its values
// belong to the import-direction guard.
type Layer string

// LayerEntry maps a production package (module-relative slash path, such as
// internal/ops) to its layer.
type LayerEntry struct {
	Package string
	Layer   Layer
}

// AllowEntry permits one import edge that the layer rules forbid.
type AllowEntry struct {
	Importer  string
	Imported  string
	Owner     string
	Rationale string
}

// Area is the guard data of one area. Each area's file declares exactly one
// Area value; only the area's own scopes edit it.
type Area struct {
	ID        AreaID
	Layers    []LayerEntry
	Allowlist []AllowEntry
	Ratchet   []RatchetEntry
	S2        []S2Entry
}
