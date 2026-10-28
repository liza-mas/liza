package ops

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	lizaerrors "github.com/liza-mas/liza/internal/errors"
)

// capabilityMethods maps each classification capability method of
// internal/errors to the only ops type allowed to declare it.
var capabilityMethods = map[string]string{
	"LifecycleOutcomeName":        "LifecycleError",
	"StaleCallerMarker":           "AgentAuthorityError",
	"PostWriteValidationMarker":   "PostWriteValidationError",
	"PreconditionReason":          "PreconditionError",
	"UnblockRebaseConflictMarker": "UnblockRebaseConflictError",
	"IntegrationFailureReason":    "IntegrationFailedError",
	"OperationalCode":             "OperationalError",
	"OperationalMessage":          "OperationalError",
	"OperationalCause":            "OperationalError",
}

var capabilityInterfaces = []string{
	"LifecycleOutcomeError", "StaleCallerError", "PostWriteValidationFailure",
	"PreconditionFailure", "UnblockRebaseConflict", "IntegrationFailure", "OperationalFailure",
}

// TestClassificationCapabilitiesAreExactlyImplemented pins the SI-5 contract:
// each ops error type implements its own capability and no other.
func TestClassificationCapabilitiesAreExactlyImplemented(t *testing.T) {
	implemented := func(err error) []string {
		var names []string
		if _, ok := err.(lizaerrors.LifecycleOutcomeError); ok {
			names = append(names, "LifecycleOutcomeError")
		}
		if _, ok := err.(lizaerrors.StaleCallerError); ok {
			names = append(names, "StaleCallerError")
		}
		if _, ok := err.(lizaerrors.PostWriteValidationFailure); ok {
			names = append(names, "PostWriteValidationFailure")
		}
		if _, ok := err.(lizaerrors.PreconditionFailure); ok {
			names = append(names, "PreconditionFailure")
		}
		if _, ok := err.(lizaerrors.UnblockRebaseConflict); ok {
			names = append(names, "UnblockRebaseConflict")
		}
		if _, ok := err.(lizaerrors.IntegrationFailure); ok {
			names = append(names, "IntegrationFailure")
		}
		if _, ok := err.(lizaerrors.OperationalFailure); ok {
			names = append(names, "OperationalFailure")
		}
		return names
	}
	tests := []struct {
		err  error
		want string
	}{
		{&LifecycleError{}, "LifecycleOutcomeError"},
		{&AgentAuthorityError{}, "StaleCallerError"},
		{&PostWriteValidationError{}, "PostWriteValidationFailure"},
		{&PreconditionError{}, "PreconditionFailure"},
		{&UnblockRebaseConflictError{}, "UnblockRebaseConflict"},
		{&IntegrationFailedError{}, "IntegrationFailure"},
		{&OperationalError{}, "OperationalFailure"},
	}
	for _, tt := range tests {
		if got := implemented(tt.err); !slices.Equal(got, []string{tt.want}) {
			t.Errorf("%T implements %v, want only %s", tt.err, got, tt.want)
		}
	}
}

func TestClassificationCapabilityValues(t *testing.T) {
	cause := &PreconditionError{Reason: "cause"}
	op := &OperationalError{Code: "c", Message: "m", Err: cause}
	if op.OperationalCode() != "c" || op.OperationalMessage() != "m" || op.OperationalCause() != error(cause) {
		t.Fatal("operational capability does not expose the code, message and cause")
	}
	if (&PreconditionError{Reason: "r"}).PreconditionReason() != "r" {
		t.Fatal("precondition capability does not expose the reason")
	}
	if (&IntegrationFailedError{Reason: IntegrationReasonMergeConflict}).IntegrationFailureReason() != string(IntegrationReasonMergeConflict) {
		t.Fatal("integration capability does not expose the reason")
	}
	lifecycle := &LifecycleError{}
	lifecycle.Outcome.Outcome = "CONFLICT"
	if lifecycle.LifecycleOutcomeName() != "CONFLICT" {
		t.Fatal("lifecycle capability does not expose the outcome name")
	}
}

// TestNoOtherModuleTypeImplementsOrEmbedsACapability is the recorded audit:
// over every Go file of the module (tests included), a classification
// capability method is declared only by its ops type, and no struct embeds an
// ops error type or a capability interface (which would promote the methods).
func TestNoOtherModuleTypeImplementsOrEmbedsACapability(t *testing.T) {
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot locate the module root")
	}
	root := filepath.Join(filepath.Dir(thisFile), "..", "..")
	embeddable := map[string]bool{}
	for _, owner := range capabilityMethods {
		embeddable[owner] = true
	}
	for _, name := range capabilityInterfaces {
		embeddable[name] = true
	}
	fset := token.NewFileSet()
	var violations []string
	for _, top := range []string{"cmd", "internal", "plugin"} {
		_ = filepath.WalkDir(filepath.Join(root, top), func(path string, entry fs.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			if entry.IsDir() && (entry.Name() == "testdata" || entry.Name() == "node_modules") {
				return fs.SkipDir
			}
			if entry.IsDir() || !strings.HasSuffix(path, ".go") {
				return nil
			}
			file, parseErr := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
			if parseErr != nil {
				t.Errorf("parse %s: %v", path, parseErr)
				return nil
			}
			inOps := filepath.Base(filepath.Dir(path)) == "ops" && file.Name.Name == "ops"
			ast.Inspect(file, func(n ast.Node) bool {
				switch d := n.(type) {
				case *ast.FuncDecl:
					owner, isCapability := capabilityMethods[d.Name.Name]
					if d.Recv == nil || !isCapability {
						return true
					}
					if !inOps || receiverTypeName(d.Recv.List[0].Type) != owner {
						violations = append(violations, fset.Position(d.Pos()).String()+" declares "+d.Name.Name)
					}
				case *ast.StructType:
					for _, field := range d.Fields.List {
						if len(field.Names) == 0 && embeddable[receiverTypeName(field.Type)] {
							violations = append(violations, fset.Position(field.Pos()).String()+" embeds "+receiverTypeName(field.Type))
						}
					}
				}
				return true
			})
			return nil
		})
	}
	for _, v := range violations {
		t.Errorf("classification capability audit: %s", strings.TrimPrefix(v, root+string(filepath.Separator)))
	}
}

// receiverTypeName names the type of a receiver or embedded field, ignoring
// pointers and package qualifiers.
func receiverTypeName(expr ast.Expr) string {
	switch e := expr.(type) {
	case *ast.StarExpr:
		return receiverTypeName(e.X)
	case *ast.Ident:
		return e.Name
	case *ast.SelectorExpr:
		return e.Sel.Name
	}
	return ""
}
