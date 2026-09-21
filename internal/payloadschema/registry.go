package payloadschema

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"

	"github.com/liza-mas/liza/internal/models"
)

// Schema validates one operation's canonical payload structurally. Version is
// stable and is bumped only on an incompatible payload change; Validate returns
// one diagnostic per rejected field and nothing for a valid payload.
type Schema struct {
	Operation string
	Version   int
	Validate  func(payload any) []models.FieldDiagnostic
}

// Descriptor identifies a registered schema without exposing its validator.
type Descriptor struct {
	Operation string `json:"operation" yaml:"operation"`
	Version   int    `json:"version" yaml:"version"`
}

// ErrUnknownOperation reports that no schema is registered for an operation.
// It is not a rejected payload: the caller learns nothing about the payload.
var ErrUnknownOperation = errors.New("payload schema: unknown operation")

var (
	registryMu sync.RWMutex
	registry   = map[string]Schema{}
)

// Register adds a schema at init time. A malformed or duplicate registration is
// a programming error in the registering file, so it panics rather than letting
// an operation run against a schema nobody can reach.
func Register(schema Schema) {
	if schema.Operation == "" {
		panic("payload schema: Register requires a non-empty operation")
	}
	if schema.Version < 1 {
		panic(fmt.Sprintf("payload schema: operation %q requires a version of at least 1, got %d", schema.Operation, schema.Version))
	}
	if schema.Validate == nil {
		panic(fmt.Sprintf("payload schema: operation %q requires a non-nil Validate function", schema.Operation))
	}

	registryMu.Lock()
	defer registryMu.Unlock()
	if _, registered := registry[schema.Operation]; registered {
		panic(fmt.Sprintf("payload schema: operation %q is already registered", schema.Operation))
	}
	registry[schema.Operation] = schema
}

// Lookup returns the schema registered for an operation.
func Lookup(operation string) (Schema, bool) {
	registryMu.RLock()
	defer registryMu.RUnlock()
	schema, registered := registry[operation]
	return schema, registered
}

// Validate checks a payload against its operation's schema. It returns the
// schema version and one diagnostic per rejected field, bounded and normalized
// for the result contract; nil diagnostics mean the payload is structurally
// valid. An unregistered operation yields ErrUnknownOperation and no version.
func Validate(operation string, payload any) (int, []models.FieldDiagnostic, error) {
	schema, registered := Lookup(operation)
	if !registered {
		return 0, nil, fmt.Errorf("%w: %s", ErrUnknownOperation, operation)
	}

	// Normalizing first keeps the schema's own slice untouched and bounds what
	// the version is stamped onto.
	diagnostics := models.NormalizeFieldDiagnostics(schema.Validate(payload))
	for i := range diagnostics {
		diagnostics[i].SchemaVersion = schema.Version
	}
	return schema.Version, diagnostics, nil
}

// List returns every registered schema's identity, sorted by operation. The
// caller owns the returned slice.
func List() []Descriptor {
	registryMu.RLock()
	defer registryMu.RUnlock()

	descriptors := make([]Descriptor, 0, len(registry))
	for _, schema := range registry {
		descriptors = append(descriptors, Descriptor{Operation: schema.Operation, Version: schema.Version})
	}
	slices.SortFunc(descriptors, func(a, b Descriptor) int {
		return strings.Compare(a.Operation, b.Operation)
	})
	return descriptors
}
