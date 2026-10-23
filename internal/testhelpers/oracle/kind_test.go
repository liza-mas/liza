package oracle

import (
	"strings"
	"testing"
)

func TestJSONFormSortsKeysPreservesNumbersKeepsCodes(t *testing.T) {
	n := &Normaliser{}
	got, err := n.Form(JSON, []byte(`{"ok":false,"result":null,"error":{"message":"<b> & x","code":"not_found"},"n":[1.0,12345678901234567890,2.50]}`))
	if err != nil {
		t.Fatal(err)
	}
	want := `{
  "error": {
    "code": "not_found",
    "message": "<b> & x"
  },
  "n": [
    1.0,
    12345678901234567890,
    2.50
  ],
  "ok": false,
  "result": null
}
`
	if string(got) != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestJSONFormNormalisesStringValuesNotCodes(t *testing.T) {
	n := &Normaliser{}
	n.Dir("/work/p", TokenProject)
	got, err := n.Form(JSON, []byte(`{"code":"validation","at":"2026-10-23T10:00:00Z","path":"/work/p/a","l":["/work/p"]}`))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"code": "validation"`, `"at": "<TIME>"`, `"path": "<PROJECT>/a"`, `"<PROJECT>"`} {
		if !strings.Contains(string(got), want) {
			t.Errorf("missing %s in:\n%s", want, got)
		}
	}
}

func TestJSONFormRejectsNonJSONAndTrailingData(t *testing.T) {
	n := &Normaliser{}
	for _, in := range []string{"not json", `{"a":1} {"b":2}`, ""} {
		if _, err := n.Form(JSON, []byte(in)); err == nil {
			t.Errorf("Form(JSON, %q) accepted its input", in)
		}
	}
}

func TestHumanFormRemovesANSIAndLayoutKeepsOrder(t *testing.T) {
	n := &Normaliser{}
	aligned := "\x1b[1mNAME\x1b[0m    STATUS\n\nalpha   ready\nbeta    blocked\n"
	realigned := "NAME STATUS\r\nalpha\t\tready  \n\n\nbeta blocked"
	a, _ := n.Form(Human, []byte(aligned))
	b, _ := n.Form(Human, []byte(realigned))
	if string(a) != "NAME STATUS\nalpha ready\nbeta blocked\n" {
		t.Errorf("unexpected human form %q", a)
	}
	if string(a) != string(b) {
		t.Errorf("re-aligned table must compare equal: %q vs %q", a, b)
	}
	swapped, _ := n.Form(Human, []byte("NAME STATUS\nbeta blocked\nalpha ready\n"))
	if string(swapped) == string(a) {
		t.Error("a swapped row must not compare equal")
	}
	tokens, _ := n.Form(Human, []byte("NAME STATUS\nalpha blocked\nbeta ready\n"))
	if string(tokens) == string(a) {
		t.Error("a changed value must not compare equal")
	}
}

func TestHumanFormNormalisesVolatileValues(t *testing.T) {
	n := &Normaliser{}
	got, _ := n.Form(Human, []byte("started 2026-10-23T10:00:00+02:00\n"))
	if string(got) != "started <TIME>\n" {
		t.Errorf("got %q", got)
	}
}

func TestPromptAndArtifactAreRawBytes(t *testing.T) {
	n := NewNormaliser()
	raw := []byte("  tabs\t and 2026-10-23T10:00:00Z \x1b[1m\n\n")
	for _, k := range []Kind{Prompt, Artifact} {
		got, err := n.Form(k, raw)
		if err != nil || string(got) != string(raw) {
			t.Errorf("%s form altered its input: %q, %v", k, got, err)
		}
	}
}

func TestFormRejectsStateKind(t *testing.T) {
	if _, err := (&Normaliser{}).Form(State, nil); err == nil {
		t.Error("State has its own form")
	}
}

func TestStateFormBeforeAfterLayout(t *testing.T) {
	n := &Normaliser{}
	got := n.StateForm([]byte("a: 1\nat: 2026-10-23T10:00:00Z  \n\n"), []byte("a: 2\r\n"))
	want := "# before\na: 1\nat: <TIME>\n# after\na: 2\n"
	if string(got) != want {
		t.Errorf("got %q, want %q", got, want)
	}
}
