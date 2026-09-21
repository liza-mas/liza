package payloadschema

import (
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/liza-mas/liza/internal/models"
)

// registerForTest registers a schema and removes it when the test ends, so that
// tests stay independent of each other and of schemas registered by sibling
// files in this package.
func registerForTest(t *testing.T, schema Schema) {
	t.Helper()
	Register(schema)
	t.Cleanup(func() {
		registryMu.Lock()
		defer registryMu.Unlock()
		delete(registry, schema.Operation)
	})
}

func testRegistrySchema(operation string, version int) Schema {
	return Schema{
		Operation: operation,
		Version:   version,
		Validate:  func(any) []models.FieldDiagnostic { return nil },
	}
}

func TestRegistryRegisterLookupList(t *testing.T) {
	const operation = "test-register-lookup-list"

	if _, found := Lookup(operation); found {
		t.Fatalf("Lookup(%q) found a schema before registration", operation)
	}

	registerForTest(t, testRegistrySchema(operation, 3))

	schema, found := Lookup(operation)
	if !found {
		t.Fatalf("Lookup(%q) did not find the registered schema", operation)
	}
	if schema.Operation != operation || schema.Version != 3 {
		t.Fatalf("Lookup(%q) = %+v, want operation %q version 3", operation, Descriptor{Operation: schema.Operation, Version: schema.Version}, operation)
	}
	if schema.Validate == nil {
		t.Fatal("Lookup returned a schema without its Validate function")
	}

	if _, found := Lookup("test-never-registered"); found {
		t.Fatal("Lookup found a schema for an unregistered operation")
	}

	// Registered out of order, so List's ordering is observable and not incidental.
	registerForTest(t, testRegistrySchema(operation+"-z", 1))
	registerForTest(t, testRegistrySchema(operation+"-a", 2))

	descriptors := List()
	for _, want := range []Descriptor{
		{Operation: operation, Version: 3},
		{Operation: operation + "-a", Version: 2},
		{Operation: operation + "-z", Version: 1},
	} {
		if !slices.Contains(descriptors, want) {
			t.Fatalf("List() = %+v, missing descriptor %+v", descriptors, want)
		}
	}
	if !slices.IsSortedFunc(descriptors, func(a, b Descriptor) int { return strings.Compare(a.Operation, b.Operation) }) {
		t.Fatalf("List() = %+v, want ascending operation order", descriptors)
	}

	// The caller owns the returned slice: mutating it must not reach the registry.
	descriptors[0] = Descriptor{Operation: "mutated", Version: 99}
	again := List()
	if slices.Contains(again, Descriptor{Operation: "mutated", Version: 99}) {
		t.Fatalf("List() = %+v, mutating a previous result changed the registry", again)
	}
}

func TestRegistryRegisterRejectsInvalid(t *testing.T) {
	const duplicate = "test-register-duplicate"
	registerForTest(t, testRegistrySchema(duplicate, 1))

	tests := []struct {
		name          string
		schema        Schema
		wantInMessage string
	}{
		{name: "empty operation", schema: testRegistrySchema("", 1), wantInMessage: "operation"},
		{name: "version below one", schema: testRegistrySchema("test-register-version-zero", 0), wantInMessage: "test-register-version-zero"},
		{name: "nil validate", schema: Schema{Operation: "test-register-nil-validate", Version: 1}, wantInMessage: "test-register-nil-validate"},
		{name: "duplicate operation", schema: testRegistrySchema(duplicate, 2), wantInMessage: duplicate},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			defer func() {
				recovered := recover()
				if recovered == nil {
					t.Fatalf("Register(%+v) did not panic", Descriptor{Operation: tt.schema.Operation, Version: tt.schema.Version})
				}
				message := fmt.Sprint(recovered)
				if !strings.Contains(message, tt.wantInMessage) {
					t.Fatalf("panic message %q does not name %q", message, tt.wantInMessage)
				}
			}()
			Register(tt.schema)
		})
	}

	// A rejected registration must leave the registry untouched.
	if schema, found := Lookup(duplicate); !found || schema.Version != 1 {
		t.Fatalf("Lookup(%q) = %+v found=%v, want the original version 1 schema", duplicate, schema, found)
	}
	for _, operation := range []string{"", "test-register-version-zero", "test-register-nil-validate"} {
		if _, found := Lookup(operation); found {
			t.Fatalf("Lookup(%q) found a schema rejected by Register", operation)
		}
	}
}

func TestRegistryValidate(t *testing.T) {
	t.Run("unknown operation", func(t *testing.T) {
		version, diagnostics, err := Validate("test-validate-unknown", map[string]any{})
		if !errors.Is(err, ErrUnknownOperation) {
			t.Fatalf("Validate error = %v, want ErrUnknownOperation", err)
		}
		if !strings.Contains(err.Error(), "test-validate-unknown") {
			t.Fatalf("Validate error %q does not name the operation", err)
		}
		if version != 0 || diagnostics != nil {
			t.Fatalf("Validate = (%d, %+v), want (0, nil) for an unknown operation", version, diagnostics)
		}
	})

	t.Run("valid payload", func(t *testing.T) {
		const operation = "test-validate-valid"
		payload := &struct{ TaskID string }{TaskID: "t-1"}
		var seen any
		registerForTest(t, Schema{
			Operation: operation,
			Version:   7,
			Validate: func(p any) []models.FieldDiagnostic {
				seen = p
				return []models.FieldDiagnostic{}
			},
		})

		version, diagnostics, err := Validate(operation, payload)
		if err != nil {
			t.Fatalf("Validate returned %v, want nil", err)
		}
		if version != 7 {
			t.Fatalf("Validate version = %d, want 7", version)
		}
		if diagnostics != nil {
			t.Fatalf("Validate diagnostics = %+v, want nil for an empty result", diagnostics)
		}
		// Structural only: the schema sees the caller's payload value, unwrapped and uncopied.
		if seen != any(payload) {
			t.Fatalf("schema received %#v, want the caller's payload value", seen)
		}
	})

	t.Run("invalid payload", func(t *testing.T) {
		const operation = "test-validate-invalid"
		oversized := strings.Repeat("é", 200) // 400 bytes
		raw := make([]models.FieldDiagnostic, 0, models.LifecycleDiagnosticsMaxEntries+1)
		for i := range cap(raw) {
			raw = append(raw, models.FieldDiagnostic{
				SchemaVersion: 99,
				Field:         fmt.Sprintf("/questions/%d", i),
				Constraint:    oversized,
				ValueClass:    models.FieldValueClassOversized,
				SafeAction:    "shout",
			})
		}
		raw[0].SafeAction = models.FieldDiagnosticRequery

		registerForTest(t, Schema{
			Operation: operation,
			Version:   4,
			Validate:  func(any) []models.FieldDiagnostic { return raw },
		})

		version, diagnostics, err := Validate(operation, nil)
		if err != nil {
			t.Fatalf("Validate returned %v, want nil", err)
		}
		if version != 4 {
			t.Fatalf("Validate version = %d, want 4", version)
		}
		if len(diagnostics) != models.LifecycleDiagnosticsMaxEntries {
			t.Fatalf("Validate returned %d diagnostics, want %d", len(diagnostics), models.LifecycleDiagnosticsMaxEntries)
		}
		for i, diagnostic := range diagnostics {
			if diagnostic.SchemaVersion != 4 {
				t.Fatalf("diagnostic %d schema_version = %d, want the schema version 4", i, diagnostic.SchemaVersion)
			}
			if len(diagnostic.Constraint) > models.LifecycleDiagnosticMaxBytes {
				t.Fatalf("diagnostic %d constraint is %d bytes, want at most %d", i, len(diagnostic.Constraint), models.LifecycleDiagnosticMaxBytes)
			}
			want := models.FieldDiagnosticCorrectInput
			if i == 0 {
				want = models.FieldDiagnosticRequery
			}
			if diagnostic.SafeAction != want {
				t.Fatalf("diagnostic %d safe_action = %q, want %q", i, diagnostic.SafeAction, want)
			}
		}
		// The schema's own slice is never rewritten by the registry.
		if raw[0].SchemaVersion != 99 || raw[1].SafeAction != "shout" {
			t.Fatalf("Validate mutated the schema's diagnostics: %+v", raw[:2])
		}
	})
}

func TestRegistryConcurrentLookup(t *testing.T) {
	const existing = "test-concurrent-existing"
	registerForTest(t, testRegistrySchema(existing, 2))

	added := Schema{
		Operation: "test-concurrent-added",
		Version:   1,
		Validate:  func(any) []models.FieldDiagnostic { return nil },
	}
	t.Cleanup(func() {
		registryMu.Lock()
		defer registryMu.Unlock()
		delete(registry, added.Operation)
	})

	var wg sync.WaitGroup
	start := make(chan struct{})
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			for range 50 {
				if _, found := Lookup(existing); !found {
					t.Errorf("Lookup(%q) lost the registered schema under concurrency", existing)
					return
				}
				if _, _, err := Validate(existing, map[string]any{}); err != nil {
					t.Errorf("Validate(%q) returned %v under concurrency", existing, err)
					return
				}
				_ = List()
			}
		}()
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		<-start
		Register(added)
	}()

	close(start)
	wg.Wait()

	if _, found := Lookup(added.Operation); !found {
		t.Fatalf("Lookup(%q) did not find the concurrently registered schema", added.Operation)
	}
}

func TestRegistryImportBoundary(t *testing.T) {
	// Structural validation means no state read, no lock file and no Git, so
	// every non-test file of this package may import the standard library and
	// these two pure-validation packages only.
	allowedImports := []string{
		"github.com/liza-mas/liza/internal/models",
		"github.com/liza-mas/liza/internal/statevalidate",
	}
	// The no-Register assertion is scoped to the files this package's registry
	// owns: sibling schema files added by the consuming commands do register
	// their own schemas here.
	ownedNonTestFiles := []string{"doc.go", "registry.go"}

	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("read package directory: %v", err)
	}

	fset := token.NewFileSet()
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}

		file, err := parser.ParseFile(fset, name, nil, parser.SkipObjectResolution)
		if err != nil {
			t.Fatalf("parse %s: %v", name, err)
		}
		for _, imported := range file.Imports {
			path := strings.Trim(imported.Path.Value, `"`)
			if isStandardLibraryImport(path) || slices.Contains(allowedImports, path) {
				continue
			}
			t.Errorf("%s imports %q; structural validation allows only the standard library and %v", name, path, allowedImports)
		}
		if slices.Contains(ownedNonTestFiles, name) {
			assertNoRegisterCall(t, fset, name, file)
		}
	}

	// Without the owned files the no-Register assertion would pass vacuously.
	for _, owned := range ownedNonTestFiles {
		if _, err := os.Stat(owned); err != nil {
			t.Errorf("stat %s: %v", owned, err)
		}
	}
}

// assertNoRegisterCall proves this package registers no schema of its own: the
// registry is the API, and every schema belongs to its command owner.
func assertNoRegisterCall(t *testing.T, fset *token.FileSet, name string, file *ast.File) {
	t.Helper()
	ast.Inspect(file, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok {
			return true
		}
		if identifier, ok := call.Fun.(*ast.Ident); ok && identifier.Name == "Register" {
			t.Errorf("%s calls Register at %s; this package registers no schema", name, fset.Position(call.Pos()))
		}
		return true
	})
}

// isStandardLibrary reports whether an import path belongs to the standard
// library: its first segment carries no dot, so it names no external host.
func isStandardLibraryImport(path string) bool {
	first, _, _ := strings.Cut(path, "/")
	return !strings.Contains(first, ".")
}
