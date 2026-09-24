package referencecontract

import (
	"encoding/json"
	"fmt"
	"io"
	"path"
	"strings"
	"unicode"
)

const (
	// AcceptanceMaxBytes bounds each source carrier and submitted manifest.
	AcceptanceMaxBytes       = 256 * 1024
	acceptanceHeading        = "Acceptance Contract"
	acceptanceMaxObligations = 256
	acceptanceMaxCommands    = 64
)

// AcceptanceContract allocates existing source obligations to one coding task.
// Its command list and proof exceptions require independent source approval.
type AcceptanceContract struct {
	Version        int             `json:"version" yaml:"version"`
	Manifest       string          `json:"manifest" yaml:"manifest"`
	Obligations    []string        `json:"obligations" yaml:"obligations"`
	Validation     []string        `json:"validation" yaml:"validation"`
	TimeoutSeconds int             `json:"timeout_seconds" yaml:"timeout_seconds"`
	ApprovedProofs []ApprovedProof `json:"approved_proofs,omitempty" yaml:"approved_proofs,omitempty"`
}

// ApprovedProof names an upstream-approved non-executable proof exception.
type ApprovedProof struct {
	ObligationID string `json:"obligation_id" yaml:"obligation_id"`
	ReferenceID  string `json:"reference_id" yaml:"reference_id"`
	Rationale    string `json:"rationale" yaml:"rationale"`
}

// AcceptanceMapping binds an obligation to an executable assertion or a named
// approved proof. CommandIndex is a pointer to distinguish zero from omission.
type AcceptanceMapping struct {
	ObligationID        string `json:"obligation_id" yaml:"obligation_id"`
	File                string `json:"file,omitempty" yaml:"file,omitempty"`
	Assertion           string `json:"assertion,omitempty" yaml:"assertion,omitempty"`
	CommandIndex        *int   `json:"command_index,omitempty" yaml:"command_index,omitempty"`
	ApprovedReferenceID string `json:"approved_reference_id,omitempty" yaml:"approved_reference_id,omitempty"`
}

// AcceptanceManifest is the committed mapping submitted for admission.
type AcceptanceManifest struct {
	Version  int                 `json:"version" yaml:"version"`
	Mappings []AcceptanceMapping `json:"mappings" yaml:"mappings"`
}

// ParseAcceptance selects one declaration using the existing fence-aware
// Markdown scanner. No eligible marker means legacy (nil, nil). Once present,
// both its source references and declaration must be valid. An empty heading
// selects the whole carrier, which must contain at most one declaration.
func ParseAcceptance(markdown, heading string) (*AcceptanceContract, error) {
	if len(markdown) > AcceptanceMaxBytes {
		return nil, fmt.Errorf("acceptance.source: exceeds %d bytes", AcceptanceMaxBytes)
	}
	selected := markdown
	if heading != "" {
		var err error
		selected, err = ExtractSection(markdown, heading)
		if err != nil {
			return nil, fmt.Errorf("acceptance.source.heading: %w", err)
		}
	}
	scan := scanMarkdown(selected)
	markers := headingsMatching(scan.headings, 0, acceptanceHeading)
	if len(markers) == 0 {
		return nil, nil
	}
	if len(markers) != 1 {
		return nil, fmt.Errorf("acceptance: expected exactly one Acceptance Contract marker, got %d", len(markers))
	}
	// The shared heading scanner accepts CRLF; normalize only the parser's
	// input so its line-oriented source declarations accept the same carrier.
	sources, err := Parse(strings.ReplaceAll(markdown, "\r\n", "\n"))
	if err != nil {
		return nil, fmt.Errorf("acceptance.source: %w", err)
	}
	if sources == nil {
		return nil, fmt.Errorf("acceptance.source: strict Source References are required")
	}
	section, err := ExtractSection(selected, acceptanceHeading)
	if err != nil {
		return nil, fmt.Errorf("acceptance: %w", err)
	}
	data, err := acceptanceJSONFence(section)
	if err != nil {
		return nil, err
	}
	contract := &AcceptanceContract{TimeoutSeconds: 600}
	if err := decodeAcceptanceJSON(data, contract); err != nil {
		return nil, fmt.Errorf("acceptance: %w", err)
	}
	if err := validateAcceptanceContract(contract, sources); err != nil {
		return nil, err
	}
	return contract, nil
}

// ApprovedProofReferenceIDs returns the reference IDs an allocation's
// approved_proofs cite, or nil when the section declares no acceptance
// contract. An invalid declaration is an error: the caller cannot tell which
// references it meant to prove.
func ApprovedProofReferenceIDs(markdown, heading string) (map[string]bool, error) {
	contract, err := ParseAcceptance(markdown, heading)
	if err != nil || contract == nil {
		return nil, err
	}
	ids := make(map[string]bool, len(contract.ApprovedProofs))
	for _, proof := range contract.ApprovedProofs {
		ids[proof.ReferenceID] = true
	}
	return ids, nil
}

// acceptanceJSONFence uses the same line/fence primitives as scanMarkdown.
// The section body must consist solely of one explicitly JSON-labelled fence.
func acceptanceJSONFence(section string) (string, error) {
	lines := splitSourceLines(section)
	start := 1 // ExtractSection includes the selected heading.
	for start < len(lines) && strings.TrimSpace(lines[start].text) == "" {
		start++
	}
	if start == len(lines) {
		return "", fmt.Errorf("acceptance: expected one fenced JSON object")
	}
	line := strings.TrimSuffix(lines[start].text, "\r")
	open, ok := openingFence(line)
	content, _ := afterFenceIndent(line)
	if !ok || strings.TrimSpace(content[open.length:]) != "json" {
		return "", fmt.Errorf("acceptance: expected one fenced JSON object")
	}
	for end := start + 1; end < len(lines); end++ {
		if !isClosingFence(strings.TrimSuffix(lines[end].text, "\r"), open) {
			continue
		}
		if strings.TrimSpace(section[lines[end].end:]) != "" {
			return "", fmt.Errorf("acceptance: unexpected content after JSON fence")
		}
		return section[lines[start].end:lines[end].start], nil
	}
	return "", fmt.Errorf("acceptance: unclosed JSON fence")
}

func validateAcceptanceContract(contract *AcceptanceContract, sources *Contract) error {
	if contract.Version != 1 {
		return fmt.Errorf("acceptance.version: unsupported version %d", contract.Version)
	}
	if err := ValidateAcceptancePath(contract.Manifest); err != nil {
		return fmt.Errorf("acceptance.manifest: %w", err)
	}
	if path.Ext(contract.Manifest) != ".json" {
		return fmt.Errorf("acceptance.manifest: must name a JSON (.json) file")
	}
	if len(contract.Obligations) == 0 || len(contract.Obligations) > acceptanceMaxObligations {
		return fmt.Errorf("acceptance.obligations: require 1..%d IDs", acceptanceMaxObligations)
	}
	available := make(map[string]bool, len(sources.ObligationCoverage))
	for _, obligation := range sources.ObligationCoverage {
		available[obligation.ID] = true
	}
	allocated := make(map[string]bool, len(contract.Obligations))
	for _, id := range contract.Obligations {
		if err := acceptanceID(id); err != nil {
			return fmt.Errorf("acceptance.obligations: %w", err)
		}
		if allocated[id] || !available[id] {
			return fmt.Errorf("acceptance.obligations[%s]: duplicate or absent from Source References obligation coverage", id)
		}
		allocated[id] = true
	}
	if len(contract.Validation) == 0 || len(contract.Validation) > acceptanceMaxCommands {
		return fmt.Errorf("acceptance.validation: require 1..%d commands", acceptanceMaxCommands)
	}
	for i, command := range contract.Validation {
		if strings.TrimSpace(command) == "" || strings.ContainsRune(command, 0) {
			return fmt.Errorf("acceptance.validation[%d]: command must be non-empty and contain no NUL", i)
		}
	}
	if contract.TimeoutSeconds < 1 || contract.TimeoutSeconds > 3600 {
		return fmt.Errorf("acceptance.timeout_seconds: must be 1..3600")
	}
	references := make(map[string]bool, len(sources.DirectReferences))
	for _, reference := range sources.DirectReferences {
		references[reference.ID] = true
	}
	proofs := make(map[string]bool, len(contract.ApprovedProofs))
	for i, proof := range contract.ApprovedProofs {
		if !allocated[proof.ObligationID] || proofs[proof.ObligationID] {
			return fmt.Errorf("acceptance.approved_proofs[%d].obligation_id: duplicate or unallocated ID", i)
		}
		if !references[proof.ReferenceID] {
			return fmt.Errorf("acceptance.approved_proofs[%d].reference_id: undeclared direct reference", i)
		}
		if strings.TrimSpace(proof.Rationale) == "" {
			return fmt.Errorf("acceptance.approved_proofs[%d].rationale: must not be empty", i)
		}
		proofs[proof.ObligationID] = true
	}
	return nil
}

// ParseAcceptanceManifest validates JSON syntax and mapping shape. It does not
// read the filesystem; admission must verify each mapped file at review commit.
func ParseAcceptanceManifest(data string) (*AcceptanceManifest, error) {
	manifest := &AcceptanceManifest{}
	if err := decodeAcceptanceJSON(data, manifest); err != nil {
		return nil, fmt.Errorf("acceptance.manifest: %w", err)
	}
	// Preserve field presence for mutually exclusive alternatives: even an
	// explicitly empty executable field cannot accompany a proof mapping.
	var raw struct {
		Mappings []map[string]json.RawMessage `json:"mappings"`
	}
	if err := json.Unmarshal([]byte(data), &raw); err != nil {
		return nil, fmt.Errorf("acceptance.manifest: %w", err)
	}
	for i, fields := range raw.Mappings {
		if _, proof := fields["approved_reference_id"]; !proof {
			continue
		}
		if manifest.Mappings[i].ApprovedReferenceID == "" {
			return nil, fmt.Errorf("acceptance.mappings[%d].approved_reference_id: must not be empty", i)
		}
		for _, field := range []string{"file", "assertion", "command_index"} {
			if _, mixed := fields[field]; mixed {
				return nil, fmt.Errorf("acceptance.mappings[%d]: cannot mix executable and approved proof fields", i)
			}
		}
	}
	if err := validateAcceptanceManifestShape(manifest); err != nil {
		return nil, err
	}
	return manifest, nil
}

func validateAcceptanceManifestShape(manifest *AcceptanceManifest) error {
	if manifest == nil || manifest.Version != 1 {
		return fmt.Errorf("acceptance.manifest.version: require version 1")
	}
	if len(manifest.Mappings) == 0 || len(manifest.Mappings) > acceptanceMaxObligations {
		return fmt.Errorf("acceptance.mappings: require 1..%d mappings", acceptanceMaxObligations)
	}
	seen := make(map[string]bool, len(manifest.Mappings))
	for i, mapping := range manifest.Mappings {
		if err := acceptanceID(mapping.ObligationID); err != nil {
			return fmt.Errorf("acceptance.mappings[%d].obligation_id: %w", i, err)
		}
		field := "acceptance.mappings[" + mapping.ObligationID + "]"
		if seen[mapping.ObligationID] {
			return fmt.Errorf("%s: duplicate obligation ID", field)
		}
		seen[mapping.ObligationID] = true
		if mapping.ApprovedReferenceID != "" {
			if err := acceptanceID(mapping.ApprovedReferenceID); err != nil {
				return fmt.Errorf("%s.approved_reference_id: %w", field, err)
			}
			if mapping.File != "" || mapping.Assertion != "" || mapping.CommandIndex != nil {
				return fmt.Errorf("%s: cannot mix executable and approved proof fields", field)
			}
			continue
		}
		if err := ValidateAcceptancePath(mapping.File); err != nil {
			return fmt.Errorf("%s.file: %w", field, err)
		}
		if strings.TrimSpace(mapping.Assertion) == "" {
			return fmt.Errorf("%s.assertion: must not be empty", field)
		}
		if mapping.CommandIndex == nil || *mapping.CommandIndex < 0 {
			return fmt.Errorf("%s.command_index: require a non-negative command index", field)
		}
	}
	return nil
}

// ValidateAcceptanceManifest requires exactly one mapping per allocated
// obligation and checks command indices and independently approved exceptions.
func ValidateAcceptanceManifest(contract *AcceptanceContract, manifest *AcceptanceManifest) error {
	if contract == nil || contract.Version != 1 {
		return fmt.Errorf("acceptance.version: require a parsed version 1 contract")
	}
	if err := validateAcceptanceManifestShape(manifest); err != nil {
		return err
	}
	allocated := make(map[string]bool, len(contract.Obligations))
	for _, id := range contract.Obligations {
		allocated[id] = true
	}
	proofs := make(map[string]string, len(contract.ApprovedProofs))
	for _, proof := range contract.ApprovedProofs {
		proofs[proof.ObligationID] = proof.ReferenceID
	}
	for _, mapping := range manifest.Mappings {
		field := "acceptance.mappings[" + mapping.ObligationID + "]"
		if !allocated[mapping.ObligationID] {
			return fmt.Errorf("%s: unknown obligation ID", field)
		}
		delete(allocated, mapping.ObligationID)
		if mapping.ApprovedReferenceID != "" {
			if proofs[mapping.ObligationID] != mapping.ApprovedReferenceID {
				return fmt.Errorf("%s.approved_reference_id: no matching independently approved proof", field)
			}
		} else if *mapping.CommandIndex >= len(contract.Validation) {
			return fmt.Errorf("%s.command_index: outside canonical validation commands", field)
		}
	}
	// Follow declaration order for deterministic missing-field diagnostics.
	for _, id := range contract.Obligations {
		if allocated[id] {
			return fmt.Errorf("acceptance.mappings[%s]: missing mapping", id)
		}
	}
	return nil
}

// ValidateAcceptancePath checks a clean repository-relative artifact path and
// excludes credential filenames. Git object mode/existence checks belong to
// admission, so this helper never reads a file or resolves a symlink.
func ValidateAcceptancePath(value string) error {
	if value == "" || strings.TrimSpace(value) != value || path.IsAbs(value) || strings.ContainsAny(value, "\\:#") || path.Clean(value) != value || value == "." {
		return fmt.Errorf("path must be a clean repository-relative file path")
	}
	for _, r := range value {
		if unicode.IsControl(r) {
			return fmt.Errorf("path must contain no control characters")
		}
	}
	for _, segment := range strings.Split(value, "/") {
		if segment == ".." || segment == "." || segment == "" {
			return fmt.Errorf("path must contain no traversal or empty segments")
		}
		lower := strings.ToLower(segment)
		if lower == "secrets" || lower == ".env" || strings.HasPrefix(lower, ".env.") || strings.Contains(lower, "secret") || strings.HasPrefix(lower, "credentials.") || strings.HasSuffix(lower, "-credentials.json") || lower == "serviceaccountkey.json" {
			return fmt.Errorf("path must not name credential material")
		}
		for _, suffix := range []string{".env", ".pem", ".key", ".p12", ".pfx", ".jks", ".keystore", ".truststore", "_rsa", "_dsa", "_ecdsa", "_ed25519"} {
			if strings.HasSuffix(lower, suffix) {
				return fmt.Errorf("path must not name credential material")
			}
		}
	}
	return nil
}

func acceptanceID(value string) error {
	if strings.TrimSpace(value) == "" || strings.TrimSpace(value) != value {
		return fmt.Errorf("ID must be a non-empty trimmed string")
	}
	for _, r := range value {
		if unicode.IsControl(r) {
			return fmt.Errorf("ID must contain no control characters")
		}
	}
	return nil
}

// decodeAcceptanceJSON additionally rejects duplicate/case-variant keys and
// nulls, which encoding/json otherwise accepts for these concrete schema types.
func decodeAcceptanceJSON(data string, target any) error {
	if len(data) > AcceptanceMaxBytes {
		return fmt.Errorf("exceeds %d bytes", AcceptanceMaxBytes)
	}
	decoder := json.NewDecoder(strings.NewReader(data))
	if err := checkAcceptanceJSONValue(decoder, 0); err != nil {
		return err
	}
	if _, err := decoder.Token(); err != io.EOF {
		return fmt.Errorf("unexpected content after JSON object")
	}
	decoder = json.NewDecoder(strings.NewReader(data))
	decoder.DisallowUnknownFields()
	return decoder.Decode(target)
}

func checkAcceptanceJSONValue(decoder *json.Decoder, depth int) error {
	if depth > 16 {
		return fmt.Errorf("JSON nesting exceeds acceptance schema limit")
	}
	token, err := decoder.Token()
	if err != nil {
		return err
	}
	if token == nil {
		return fmt.Errorf("null is not allowed in acceptance JSON")
	}
	delim, container := token.(json.Delim)
	if !container {
		return nil
	}
	keys := make(map[string]bool)
	for decoder.More() {
		if delim == '{' {
			keyToken, err := decoder.Token()
			if err != nil {
				return err
			}
			key, ok := keyToken.(string)
			if !ok || strings.IndexFunc(key, func(r rune) bool {
				return r != '_' && (r < 'a' || r > 'z') && (r < '0' || r > '9')
			}) >= 0 {
				return fmt.Errorf("JSON field names must use exact lowercase schema names")
			}
			if keys[key] {
				return fmt.Errorf("duplicate JSON key %q", key)
			}
			keys[key] = true
		}
		if err := checkAcceptanceJSONValue(decoder, depth+1); err != nil {
			return err
		}
	}
	_, err = decoder.Token()
	return err
}
