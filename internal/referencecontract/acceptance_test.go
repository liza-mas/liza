package referencecontract

import (
	"encoding/json"
	"strings"
	"testing"
)

const acceptanceDeclaration = `{"version":1,"manifest":"acceptance/task.json","obligations":["AC-identity","AC-rollback"],"validation":["project-test --acceptance"]}`

func acceptanceDocument(declaration string) string {
	return strictDocument(
		`- "source": "specs/requirements.md#Requirements"`,
		"- \"AC-identity\" -> \"source\"\n- \"AC-rollback\" -> \"source\"",
	) + "\n\n## Task One\n\n### Acceptance Contract\n\n```json\n" + declaration + "\n```\n"
}

func TestParseAcceptanceAllocation(t *testing.T) {
	document := acceptanceDocument(acceptanceDeclaration)
	contract, err := ParseAcceptance(document, "Task One")
	if err != nil {
		t.Fatal(err)
	}
	if contract.Version != 1 || contract.Manifest != "acceptance/task.json" || contract.TimeoutSeconds != 600 || strings.Join(contract.Obligations, ",") != "AC-identity,AC-rollback" || strings.Join(contract.Validation, ",") != "project-test --acceptance" {
		t.Fatalf("unexpected contract: %#v", contract)
	}
	if _, err := ParseAcceptance(document, ""); err != nil {
		t.Fatalf("whole-document allocation: %v", err)
	}
	second := strings.ReplaceAll(acceptanceDeclaration, `"AC-identity","AC-rollback"`, `"AC-rollback"`)
	document += "\n## Task Two\n\n### Acceptance Contract\n~~~json\n" + second + "\n~~~~\n"
	contract, err = ParseAcceptance(document, "Task Two")
	if err != nil || contract == nil || len(contract.Obligations) != 1 || contract.Obligations[0] != "AC-rollback" {
		t.Fatalf("subset allocation = %#v, error = %v", contract, err)
	}
	if _, err := ParseAcceptance(document, ""); err == nil || !strings.Contains(err.Error(), "exactly one") {
		t.Fatalf("ambiguous whole source error = %v", err)
	}
}

func TestParseAcceptanceMarkerAndFenceRules(t *testing.T) {
	for _, document := range []string{
		"# Legacy\nNo adoption.\n",
		"```markdown\n## Acceptance Contract\n```\n",
		"    ## Acceptance Contract\n",
		"##Acceptance Contract\n",
		"Acceptance Contract\n---\n",
	} {
		contract, err := ParseAcceptance(document, "")
		if err != nil || contract != nil {
			t.Fatalf("legacy declaration %q = %#v, %v", document, contract, err)
		}
	}
	document := acceptanceDocument(acceptanceDeclaration)
	for _, tc := range []struct{ name, document, heading, want string }{
		{"missing selected heading", document, "Missing", "heading"},
		{"duplicate selected heading", document + "\n## Task One\n", "Task One", "ambiguous"},
		{"missing sources", "## Acceptance Contract\n```json\n" + acceptanceDeclaration + "\n```", "", "Source References"},
		{"malformed sources", strings.Replace(document, "Source revision:", "Revision:", 1), "Task One", "source"},
		{"no JSON fence", strings.Replace(document, "```json", "```", 1), "Task One", "fenced JSON"},
		{"missing fence", strings.TrimSuffix(document, "```\n"), "Task One", "unclosed"},
		{"extra prose", document + "not part of the declaration\n", "Task One", "unexpected content"},
		{"second fence", document + "\n```json\n{}\n```\n", "Task One", "unexpected content"},
		{"nested heading", document + "\n#### Extra\n", "Task One", "unexpected content"},
		{"oversize source", document + strings.Repeat(" ", AcceptanceMaxBytes), "Task One", "exceeds"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ParseAcceptance(tc.document, tc.heading)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error = %v, want %q", err, tc.want)
			}
		})
	}
	if _, err := ParseAcceptance(strings.ReplaceAll(document, "\n", "\r\n"), "Task One"); err != nil {
		t.Fatalf("CRLF document: %v", err)
	}
}

func TestParseAcceptanceRejectsInvalidDeclarations(t *testing.T) {
	for _, tc := range []struct{ name, old, replacement, want string }{
		{"unsupported version", `"version":1`, `"version":2`, "version"},
		{"duplicate key", `"version":1`, `"version":1,"version":1`, "duplicate JSON key"},
		{"case variant", `"version":1`, `"Version":1`, "exact lowercase"},
		{"Unicode case fold alias", `"version":1`, `"verſion":1`, "exact lowercase"},
		{"unknown key", `"version":1`, `"version":1,"approver":"coder"`, "unknown field"},
		{"null version", `"version":1`, `"version":null`, "null"},
		{"unknown obligation", `AC-identity`, `AC-unknown`, "obligations"},
		{"duplicate obligation", `AC-identity`, `AC-rollback`, "obligations"},
		{"blank obligation", `AC-identity`, ` `, "obligations"},
		{"no obligations", `["AC-identity","AC-rollback"]`, `[]`, "obligations"},
		{"no commands", `["project-test --acceptance"]`, `[]`, "validation"},
		{"blank command", `project-test --acceptance`, ` `, "validation"},
		{"NUL command", `project-test --acceptance`, `test\u0000`, "validation"},
		{"zero timeout", `"version":1`, `"version":1,"timeout_seconds":0`, "timeout_seconds"},
		{"excess timeout", `"version":1`, `"version":1,"timeout_seconds":3601`, "timeout_seconds"},
		{"wrong path extension", `acceptance/task.json`, `acceptance/task.yaml`, "manifest"},
		{"manifest traversal", `acceptance/task.json`, `../task.json`, "manifest"},
		{"unallocated proof", `"version":1`, `"version":1,"approved_proofs":[{"obligation_id":"AC-other","reference_id":"source","rationale":"reviewed"}]`, "obligation_id"},
		{"unknown proof reference", `"version":1`, `"version":1,"approved_proofs":[{"obligation_id":"AC-identity","reference_id":"unknown","rationale":"reviewed"}]`, "reference_id"},
		{"empty proof rationale", `"version":1`, `"version":1,"approved_proofs":[{"obligation_id":"AC-identity","reference_id":"source","rationale":" "}]`, "rationale"},
		{"duplicate proof", `"version":1`, `"version":1,"approved_proofs":[{"obligation_id":"AC-identity","reference_id":"source","rationale":"approved"},{"obligation_id":"AC-identity","reference_id":"source","rationale":"approved"}]`, "obligation_id"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ParseAcceptance(acceptanceDocument(strings.Replace(acceptanceDeclaration, tc.old, tc.replacement, 1)), "Task One")
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error = %v, want %q", err, tc.want)
			}
		})
	}
	for _, suffix := range []string{"{}", " trailing"} {
		if _, err := ParseAcceptance(acceptanceDocument(acceptanceDeclaration+suffix), "Task One"); err == nil || !strings.Contains(err.Error(), "unexpected content") {
			t.Fatalf("trailing JSON error = %v", err)
		}
	}
}

func TestAcceptanceManifestExactMappingAndProofs(t *testing.T) {
	declaration := strings.Replace(acceptanceDeclaration, `"version":1`, `"version":1,"approved_proofs":[{"obligation_id":"AC-rollback","reference_id":"source","rationale":"approved exception"}]`, 1)
	contract, err := ParseAcceptance(acceptanceDocument(declaration), "Task One")
	if err != nil {
		t.Fatal(err)
	}
	manifestJSON := `{"version":1,"mappings":[{"obligation_id":"AC-identity","file":"tests/boundary.test","assertion":"reject invalid identity","command_index":0},{"obligation_id":"AC-rollback","approved_reference_id":"source"}]}`
	manifest, err := ParseAcceptanceManifest(manifestJSON)
	if err != nil {
		t.Fatal(err)
	}
	if err := ValidateAcceptanceManifest(contract, manifest); err != nil {
		t.Fatal(err)
	}
	if manifest.Mappings[0].CommandIndex == nil || *manifest.Mappings[0].CommandIndex != 0 {
		t.Fatal("zero command index must be retained")
	}
	for _, tc := range []struct{ name, old, replacement, want string }{
		{"missing allocation", `,{"obligation_id":"AC-rollback","approved_reference_id":"source"}`, ``, "AC-rollback]: missing"},
		{"unknown allocation", `AC-identity`, `AC-other`, "AC-other]: unknown"},
		{"out of range command", `"command_index":0`, `"command_index":1`, "command_index"},
		{"wrong exception", `"approved_reference_id":"source"`, `"approved_reference_id":"other"`, "approved_reference_id"},
		{"no exception approval", `"obligation_id":"AC-identity","file":"tests/boundary.test","assertion":"reject invalid identity","command_index":0`, `"obligation_id":"AC-identity","approved_reference_id":"source"`, "approved_reference_id"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			candidate, err := ParseAcceptanceManifest(strings.Replace(manifestJSON, tc.old, tc.replacement, 1))
			if err != nil {
				t.Fatal(err)
			}
			err = ValidateAcceptanceManifest(contract, candidate)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error = %v, want %q", err, tc.want)
			}
		})
	}
}

func TestParseAcceptanceManifestRejectsMalformedMappings(t *testing.T) {
	valid := `{"version":1,"mappings":[{"obligation_id":"AC-identity","file":"tests/boundary.test","assertion":"reject invalid identity","command_index":0}]}`
	for _, tc := range []struct{ name, old, replacement, want string }{
		{"unknown mapping key", `"command_index":0`, `"command_index":0,"extra":true`, "unknown field"},
		{"duplicate nested key", `"command_index":0`, `"command_index":0,"command_index":0`, "duplicate JSON key"},
		{"omitted index", `,"command_index":0`, ``, "command_index"},
		{"negative index", `"command_index":0`, `"command_index":-1`, "command_index"},
		{"fractional index", `"command_index":0`, `"command_index":0.5`, "command_index"},
		{"null index", `"command_index":0`, `"command_index":null`, "null"},
		{"empty selector", `reject invalid identity`, ` `, "assertion"},
		{"credential path", `tests/boundary.test`, `.env`, "credential"},
		{"mixed proof", `"command_index":0`, `"command_index":0,"approved_reference_id":"source"`, "cannot mix"},
		{"empty proof", `"command_index":0`, `"command_index":0,"approved_reference_id":""`, "approved_reference_id"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ParseAcceptanceManifest(strings.Replace(valid, tc.old, tc.replacement, 1))
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error = %v, want %q", err, tc.want)
			}
		})
	}
	for _, invalid := range []string{
		`{"version":2,"mappings":[]}`, `{"version":1,"mappings":[]}`, `null`, `[]`,
		`{"version":1,"mappings":[{"obligation_id":"A","approved_reference_id":"source","file":""}]}`,
		`{"version":1,"mappings":[{"obligation_id":"A","approved_reference_id":"source"},{"obligation_id":"A","approved_reference_id":"source"}]}`,
		valid + `{}`, strings.Repeat(" ", AcceptanceMaxBytes) + valid,
	} {
		if _, err := ParseAcceptanceManifest(invalid); err == nil {
			t.Errorf("accepted invalid manifest (length %d)", len(invalid))
		}
	}
}

func TestAcceptanceBounds(t *testing.T) {
	contract := AcceptanceContract{Version: 1, Manifest: "acceptance/task.json", Obligations: []string{"AC-identity"}, Validation: []string{"test"}, TimeoutSeconds: 3600}
	for _, count := range []int{64, 65} {
		contract.Validation = make([]string, count)
		for i := range contract.Validation {
			contract.Validation[i] = "test"
		}
		data, err := json.Marshal(contract)
		if err != nil {
			t.Fatal(err)
		}
		_, err = ParseAcceptance(acceptanceDocument(string(data)), "Task One")
		if (err == nil) != (count == 64) {
			t.Fatalf("%d commands: %v", count, err)
		}
	}
	manifest := AcceptanceManifest{Version: 1}
	for i := 0; i < 257; i++ {
		manifest.Mappings = append(manifest.Mappings, AcceptanceMapping{ObligationID: string(rune(0x100 + i)), ApprovedReferenceID: "source"})
	}
	data, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ParseAcceptanceManifest(string(data)); err == nil || !strings.Contains(err.Error(), "1..256") {
		t.Fatalf("mapping bound error = %v", err)
	}
}

func TestValidateAcceptancePath(t *testing.T) {
	for _, value := range []string{"acceptance/task.json", "tests/boundary.test", "tests/identity cases.py"} {
		if err := ValidateAcceptancePath(value); err != nil {
			t.Errorf("%q: %v", value, err)
		}
	}
	for _, value := range []string{"", ".", "../x", "a/../x", "a//x", "/x", "C:/x", `a\x`, "x#part", "x\n", ".env", ".env.local", "config/credentials.json", "config/secrets/data.txt", "test.env", "key.pem", "id_ed25519", "serviceAccountKey.json"} {
		if err := ValidateAcceptancePath(value); err == nil {
			t.Errorf("accepted %q", value)
		}
	}
}
