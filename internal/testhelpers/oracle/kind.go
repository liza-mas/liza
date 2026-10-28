package oracle

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

// Kind is the surface kind of a golden; it names the testdata/golden
// subdirectory and selects the golden form.
type Kind string

// Golden kinds.
const (
	Prompt   Kind = "prompt"
	Artifact Kind = "artifact"
	JSON     Kind = "json"
	Human    Kind = "human"
	State    Kind = "state"
)

// Form returns the stored form of got for the kind: what a golden holds and
// what a comparison tests, so regeneration compares bytes. State goldens are
// built by StateForm, not here.
func (n *Normaliser) Form(kind Kind, got []byte) ([]byte, error) {
	switch kind {
	case Prompt, Artifact:
		return got, nil
	case JSON:
		return n.jsonForm(got)
	case Human:
		return []byte(n.humanForm(string(got))), nil
	default:
		return nil, fmt.Errorf("oracle: no single-value form for kind %q", kind)
	}
}

// StateForm is the state golden: the normalised state.yaml before and after
// the operation, each side normalised line by line (no YAML parser).
func (n *Normaliser) StateForm(before, after []byte) []byte {
	return []byte("# before\n" + n.stateSide(before) + "# after\n" + n.stateSide(after))
}

func (n *Normaliser) stateSide(side []byte) string {
	var b strings.Builder
	lines := strings.Split(strings.TrimRight(string(side), "\r\n"), "\n")
	for _, line := range lines {
		b.WriteString(strings.TrimRight(n.Text(line), " \t\r"))
		b.WriteByte('\n')
	}
	return b.String()
}

// jsonForm decodes with numbers preserved, normalises string values and
// re-encodes with sorted keys, two-space indent and a trailing newline. Codes
// are string values that survive unchanged.
func (n *Normaliser) jsonForm(got []byte) ([]byte, error) {
	dec := json.NewDecoder(bytes.NewReader(got))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		return nil, fmt.Errorf("oracle: json golden source is not JSON: %w", err)
	}
	if _, err := dec.Token(); err != io.EOF {
		return nil, fmt.Errorf("oracle: json golden source has data after the first value")
	}
	var out bytes.Buffer
	enc := json.NewEncoder(&out)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(n.jsonValue(v)); err != nil {
		return nil, fmt.Errorf("oracle: encode json golden: %w", err)
	}
	return out.Bytes(), nil
}

func (n *Normaliser) jsonValue(v any) any {
	switch t := v.(type) {
	case string:
		return n.Text(t)
	case []any:
		for i := range t {
			t[i] = n.jsonValue(t[i])
		}
		return t
	case map[string]any:
		for k := range t {
			t[k] = n.jsonValue(t[k])
		}
		return t
	default:
		return v
	}
}

// humanForm drops ANSI sequences and layout whitespace: the tokens of each
// line are joined by one space and empty lines are dropped. Line and token
// order are kept, so a re-aligned table compares equal and a swapped row does
// not.
func (n *Normaliser) humanForm(got string) string {
	text := n.Text(ansiRE.ReplaceAllString(got, ""))
	var b strings.Builder
	for _, line := range strings.Split(text, "\n") {
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		b.WriteString(strings.Join(fields, " "))
		b.WriteByte('\n')
	}
	return b.String()
}
