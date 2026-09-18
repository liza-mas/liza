package rolebudget

import "testing"

// A skill may link a shared reference with a heading anchor; the read set
// must count the file either way, and must not count links elsewhere.
func TestSharedReferenceLinkAcceptsAnchors(t *testing.T) {
	cases := map[string]string{
		"see [x](../shared/references/reference-first-authoring.md)":                      "reference-first-authoring.md",
		"see [x](../shared/references/reference-first-authoring.md#priority-commitments)": "reference-first-authoring.md",
		"see [x](../../skills/shared/references/acceptance-evidence.md#proofs)":           "acceptance-evidence.md",
		"see [x](../other/references/reference-first-authoring.md)":                       "",
		"see [x](../shared/references/notes.txt)":                                         "",
	}
	for input, want := range cases {
		m := sharedReferenceLink.FindStringSubmatch(input)
		got := ""
		if m != nil {
			got = m[1]
		}
		if got != want {
			t.Errorf("%q: matched %q, want %q", input, got, want)
		}
	}
}
