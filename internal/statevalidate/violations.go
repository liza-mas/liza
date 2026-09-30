package statevalidate

import (
	"strings"
)

// violation is one independently evaluable constraint a state breaks.
//
// id is its identity when a candidate is compared with its baseline
// (ValidateCandidate): it names the record, the constraint and the offending
// element. It defaults to the rendered message; a check whose message embeds
// context a mutation may change without touching the constraint (a status, a
// path) sets a narrower id so the old violation is still recognised. err keeps
// the original, possibly typed, error for errors.As.
type violation struct {
	id  string
	err error
}

// violations collects every violation a validation pass finds instead of
// stopping at the first, so a later check cannot hide behind an earlier one
// and a candidate can be compared with its baseline check by check (D84).
type violations struct {
	list []violation
}

// add records err under its message as identity. A nil err is ignored, so
// callers can pass a helper's result straight through. A *ViolationList (the
// result of a nested collector) is merged entry by entry, keeping each
// entry's identity.
func (v *violations) add(err error) {
	if err == nil {
		return
	}
	if nested, ok := err.(*ViolationList); ok {
		v.list = append(v.list, nested.list...)
		return
	}
	v.list = append(v.list, violation{id: err.Error(), err: err})
}

// addID records err under an explicit identity.
func (v *violations) addID(id string, err error) {
	v.list = append(v.list, violation{id: id, err: err})
}

// within runs check against a nested collector and records what it finds with
// owner prefixed to each identity, messages unchanged. Checks of sibling
// entries (two attestations of one plan, two commands of one receipt) often
// share messages; the owner keeps the same defect on two entries two
// identities, so repairing one entry cannot pay for corrupting another.
// owner must be stable across a legitimate mutation: an ID, or an index into
// an append-only or immutable list.
func (v *violations) within(owner string, check func(*violations)) {
	var nested violations
	check(&nested)
	for _, item := range nested.list {
		v.list = append(v.list, violation{id: owner + " / " + item.id, err: item.err})
	}
}

// err returns the collected violations as one error, or nil when there are
// none.
func (v *violations) err() error {
	if len(v.list) == 0 {
		return nil
	}
	return &ViolationList{list: v.list}
}

// ViolationList is the error ValidateState returns: every violation found, in
// validation order, one per line. errors.Is and errors.As see each violation's
// original error.
type ViolationList struct {
	list []violation
}

func (l *ViolationList) Error() string {
	messages := make([]string, len(l.list))
	for i, item := range l.list {
		messages[i] = item.err.Error()
	}
	return strings.Join(messages, "\n")
}

func (l *ViolationList) Unwrap() []error {
	errs := make([]error, len(l.list))
	for i, item := range l.list {
		errs[i] = item.err
	}
	return errs
}

// Len reports how many violations the list holds.
func (l *ViolationList) Len() int { return len(l.list) }

// collectErr runs a check written against a collector and returns its result
// as a single error, for callers outside the whole-state pass.
func collectErr(check func(*violations)) error {
	var v violations
	check(&v)
	return v.err()
}
