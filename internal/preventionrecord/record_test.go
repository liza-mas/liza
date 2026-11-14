package preventionrecord

import (
	"reflect"
	"strings"
	"testing"
)

func TestIsFixSubject(t *testing.T) {
	cases := map[string]bool{
		"fix: a":           true,
		"fix(s): a":        true,
		"fix!: a":          true,
		"fix(s)!: a":       true,
		"fix(): a":         true,
		"Fix: a":           false,
		"fix:a":            false,
		"fix :a":           false,
		"fixup! fix: a":    false,
		"revert: a":        false,
		`Revert "fix: a"`:  false,
		"docs: fix: a":     false,
		"fix(a)(b): a":     false,
		"fixes: a":         false,
		"chore(fix): a":    false,
		"fix(s) !: a":      false,
		"":                 false,
		"fix: ":            true,
		"fix(scope)!:  x ": true,
	}
	for subject, want := range cases {
		if got := IsFixSubject(subject); got != want {
			t.Errorf("IsFixSubject(%q) = %t, want %t", subject, got, want)
		}
	}
}

// valid is a well-formed record block.
const valid = "Prevention-Cause: a closed store accepted writes\n" +
	"Prevention-Concept: T0.2\n" +
	"Prevention-Disposition: removed\n" +
	"Prevention-Class: gap\n"

// message builds a raw commit message whose last paragraph is the block.
func message(block string) string {
	return "fix: x\n\nBody.\n\n" + block
}

func validate(block string) []Finding { return Validate(message(block), block) }

func f(problem Problem, key string) Finding { return Finding{Problem: problem, Key: key} }

func TestValidateAcceptsWellFormedRecords(t *testing.T) {
	blocks := map[string]string{
		"plain":                valid,
		"scheduled human":      strings.Replace(valid, "removed", "scheduled", 1) + "Prevention-Owner: human\n",
		"scheduled ticket":     strings.Replace(valid, "removed", "scheduled", 1) + "Prevention-Owner: TASK-42\n",
		"multi concept":        valid + "Prevention-Concept: internal/api/[id].go#Handle\n",
		"other trailer":        valid + "Co-Authored-By: A <a@example.invalid>\n",
		"blank before colon":   strings.Replace(valid, "Prevention-Class:", "Prevention-Class :", 1),
		"tab and padded value": strings.Replace(valid, "Prevention-Class: gap", "Prevention-Class:\t gap  ", 1),
		"every class":          strings.Replace(valid, "gap", "not-consulted", 1),
	}
	for name, block := range blocks {
		if got := validate(block); len(got) != 0 {
			t.Errorf("%s: unexpected problems %v", name, got)
		}
	}
}

func TestValidateRecordMissingAndOutsideFinalBlock(t *testing.T) {
	if got := Validate("fix: x\n\nNo record here.\n", ""); !reflect.DeepEqual(got, []Finding{f(RecordMissing, "")}) {
		t.Errorf("no record: %v", got)
	}
	if got := Validate("fix: x\n\nSigned-off-by: A <a@example.invalid>\n", "Signed-off-by: A <a@example.invalid>\n"); !reflect.DeepEqual(got, []Finding{f(RecordMissing, "")}) {
		t.Errorf("trailer block without record: %v", got)
	}
	// Record lines only above a final block that has none: sole problem.
	above := valid + "\nCo-Authored-By: A <a@example.invalid>\n"
	if got := Validate("fix: x\n\n"+above, "Co-Authored-By: A <a@example.invalid>\n"); !reflect.DeepEqual(got, []Finding{f(OutsideFinalBlock, "")}) {
		t.Errorf("record above the final block: %v", got)
	}
	// A valid final block with a stale record paragraph above it: reported, block still validated.
	squash := "fix: x\n\n" + valid + "\n" + valid
	if got := Validate(squash, valid); !reflect.DeepEqual(got, []Finding{f(OutsideFinalBlock, "")}) {
		t.Errorf("squash: %v", got)
	}
	bad := strings.Replace(valid, "gap", "wrong", 1)
	squash = "fix: x\n\n" + valid + "\n" + bad
	want := []Finding{f(OutsideFinalBlock, ""), f(ValueInvalid, KeyClass)}
	if got := Validate(squash, bad); !reflect.DeepEqual(got, want) {
		t.Errorf("squash with an invalid block: %v, want %v", got, want)
	}
}

func TestValidateKeyRules(t *testing.T) {
	tests := []struct {
		name  string
		block string
		want  []Finding
	}{
		{"mis-cased key", valid + "prevention-owner: human\n", []Finding{f(KeyUnknown, "prevention-owner")}},
		{"unknown key", valid + "Prevention-Old_Key: stale\n", []Finding{f(KeyUnknown, "Prevention-Old_Key")}},
		{"unknown key only counts nowhere", strings.Replace(valid, "Prevention-Class: gap\n", "PREVENTION-CLASS: gap\n", 1), []Finding{f(KeyUnknown, "PREVENTION-CLASS"), f(KeyMissing, KeyClass)}},
		{"repeated cause", valid + "Prevention-Cause: again\n", []Finding{f(KeyRepeated, KeyCause)}},
		{"repeated class", valid + "Prevention-Class: gap\n", []Finding{f(KeyRepeated, KeyClass)}},
		{"repeated disposition", valid + "Prevention-Disposition: removed\n", []Finding{f(KeyRepeated, KeyDisposition)}},
		{"repeated owner", strings.Replace(valid, "removed", "scheduled", 1) + "Prevention-Owner: human\nPrevention-Owner: TASK-1\n", []Finding{f(KeyRepeated, KeyOwner)}},
		{"missing cause", strings.Replace(valid, "Prevention-Cause: a closed store accepted writes\n", "", 1), []Finding{f(KeyMissing, KeyCause)}},
		{"missing concept", strings.Replace(valid, "Prevention-Concept: T0.2\n", "", 1), []Finding{f(KeyMissing, KeyConcept)}},
		{"missing disposition", strings.Replace(valid, "Prevention-Disposition: removed\n", "", 1), []Finding{f(KeyMissing, KeyDisposition)}},
		{"missing class", strings.Replace(valid, "Prevention-Class: gap\n", "", 1), []Finding{f(KeyMissing, KeyClass)}},
		{"scheduled without owner", strings.Replace(valid, "removed", "scheduled", 1), []Finding{f(KeyMissing, KeyOwner)}},
		{"owner with removed", valid + "Prevention-Owner: human\n", []Finding{f(OwnerUnexpected, KeyOwner)}},
		{"accepted", strings.Replace(valid, "removed", "accepted", 1), []Finding{f(DispositionAccepted, KeyDisposition)}},
		{"accepted with owner", strings.Replace(valid, "removed", "accepted", 1) + "Prevention-Owner: human\n", []Finding{f(OwnerUnexpected, KeyOwner), f(DispositionAccepted, KeyDisposition)}},
		{"invalid disposition has no owner rule", strings.Replace(valid, "removed", "deferred", 1) + "Prevention-Owner: human\n", []Finding{f(ValueInvalid, KeyDisposition)}},
	}
	for _, tt := range tests {
		if got := validate(tt.block); !reflect.DeepEqual(got, tt.want) {
			t.Errorf("%s: %v, want %v", tt.name, got, tt.want)
		}
	}
}

func TestValidateValueRules(t *testing.T) {
	with := func(key, value string) string {
		var lines []string
		for _, line := range strings.Split(strings.TrimRight(valid, "\n"), "\n") {
			if strings.HasPrefix(line, key+":") {
				line = key + ":" + value
			}
			lines = append(lines, line)
		}
		return strings.Join(lines, "\n") + "\n"
	}
	t.Run("empty values", func(t *testing.T) {
		for _, key := range []string{KeyCause, KeyConcept, KeyDisposition, KeyClass} {
			for _, value := range []string{"", " ", "\t"} {
				if got := validate(with(key, value)); !reflect.DeepEqual(got, []Finding{f(ValueInvalid, key)}) {
					t.Errorf("%s %q: %v", key, value, got)
				}
			}
		}
	})
	t.Run("concepts", func(t *testing.T) {
		accepted := []string{
			"T0.2", "S1.1", "a", "A-b_c.d", "internal/ops/x.go", "internal/ops/x.go#Func", "internal/ops/x.go#T.Method_1",
			"./config.yaml", "a/b", "internal/api/[id].go#Handle", "a/.../b",
		}
		for _, concept := range accepted {
			if got := validate(with(KeyConcept, " "+concept)); len(got) != 0 {
				t.Errorf("concept %q: %v", concept, got)
			}
		}
		rejected := []string{
			"/abs/path.go", "../x/y.go", "a/../b", "a/b/..", "has space", "internal/ops/x.go#", "internal/ops/x.go#a#b",
			"internal/ops/x.go#bad-symbol", "0T2", "1.1", "T0.2#Sym", "x.go#Sym", "-a", ".hidden", "a b/c", "a/b#sym bol",
			"..", "#Sym",
		}
		for _, concept := range rejected {
			if got := validate(with(KeyConcept, " "+concept)); !reflect.DeepEqual(got, []Finding{f(ValueInvalid, KeyConcept)}) {
				t.Errorf("concept %q: %v", concept, got)
			}
		}
	})
	t.Run("disposition and class", func(t *testing.T) {
		for _, value := range []string{"Removed", "deferred", "removed scheduled", "none"} {
			if got := validate(with(KeyDisposition, " "+value)); !reflect.DeepEqual(got, []Finding{f(ValueInvalid, KeyDisposition)}) {
				t.Errorf("disposition %q: %v", value, got)
			}
		}
		for _, value := range []string{"Gap", "none needed", "none", "gap unenforced"} {
			if got := validate(with(KeyClass, " "+value)); !reflect.DeepEqual(got, []Finding{f(ValueInvalid, KeyClass)}) {
				t.Errorf("class %q: %v", value, got)
			}
		}
	})
	t.Run("owner", func(t *testing.T) {
		scheduled := strings.Replace(valid, "removed", "scheduled", 1)
		if got := validate(scheduled + "Prevention-Owner: two words\n"); !reflect.DeepEqual(got, []Finding{f(ValueInvalid, KeyOwner)}) {
			t.Errorf("owner with whitespace: %v", got)
		}
		if got := validate(scheduled + "Prevention-Owner:\n"); !reflect.DeepEqual(got, []Finding{f(ValueInvalid, KeyOwner)}) {
			t.Errorf("empty owner: %v", got)
		}
	})
	t.Run("a value spanning lines", func(t *testing.T) {
		folded := strings.Replace(valid, "accepted writes\n", "accepted writes\n assumption\n", 1)
		if got := validate(folded); !reflect.DeepEqual(got, []Finding{f(ValueInvalid, KeyCause)}) {
			t.Errorf("folded cause: %v", got)
		}
		tabbed := strings.Replace(valid, "Prevention-Class: gap\n", "Prevention-Class: gap\n\tmore\n", 1)
		if got := validate(tabbed); !reflect.DeepEqual(got, []Finding{f(ValueInvalid, KeyClass)}) {
			t.Errorf("continued class: %v", got)
		}
	})
	t.Run("values are never echoed", func(t *testing.T) {
		secret := "TOPSECRETVALUE"
		for _, got := range [][]Finding{validate(with(KeyDisposition, " "+secret)), validate(with(KeyConcept, " "+secret+"#a#b"))} {
			for _, finding := range got {
				if strings.Contains(string(finding.Problem)+finding.Key, secret) {
					t.Errorf("finding %v echoes a value", finding)
				}
			}
		}
	})
}

func TestCheckCommitsSelectsFixCommitsOnly(t *testing.T) {
	commits := []Commit{
		{SHA: "1", Subject: "fix: a", Message: "fix: a\n"},
		{SHA: "2", Subject: "chore: a", Message: "chore: a\n"},
		{SHA: "3", Subject: `Revert "fix: a"`, Message: "Revert\n"},
		{SHA: "4", Subject: "fix(s): ok", Message: message(valid), Trailers: valid},
	}
	got := CheckCommits(commits)
	if got.Checked != 2 || len(got.Offenders) != 1 || got.Offenders[0].SHA != "1" ||
		!reflect.DeepEqual(got.Offenders[0].Findings, []Finding{f(RecordMissing, "")}) {
		t.Errorf("result %+v", got)
	}
}

// A concept or owner is one token: any Unicode whitespace inside it, not only
// space and TAB, makes the value invalid.
func TestValidateTokenValuesRejectAnyWhitespace(t *testing.T) {
	for name, ws := range map[string]string{
		"vertical_tab":      "\v",
		"form_feed":         "\f",
		"carriage_return":   "\r",
		"nonbreaking_space": " ",
	} {
		t.Run(name, func(t *testing.T) {
			concept := strings.Replace(valid, "T0.2", "internal/"+ws+"store.go", 1)
			owner := strings.Replace(valid, "removed", "scheduled", 1) + "Prevention-Owner: TASK-" + ws + "42\n"
			for key, block := range map[string]string{KeyConcept: concept, KeyOwner: owner} {
				want := []Finding{f(ValueInvalid, key)}
				if got := validate(block); !reflect.DeepEqual(got, want) {
					t.Errorf("%s with %s: %v; want %v", key, name, got, want)
				}
			}
		})
	}
}
