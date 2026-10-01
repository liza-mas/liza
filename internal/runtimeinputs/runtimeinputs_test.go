package runtimeinputs

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/liza-mas/liza/internal/models"
)

func TestParseEnvelopeAcceptsOnlyTextTheEnvFileParserKeepsVerbatim(t *testing.T) {
	values, err := ParseEnvelope([]byte("# fixture\n\nA_FIXTURE=abc-123\n  # indented comment\nB_CREDENTIAL=s3cr3t=value#x\n"))
	if err != nil {
		t.Fatalf("ParseEnvelope: %v", err)
	}
	if values["A_FIXTURE"] != "abc-123" || values["B_CREDENTIAL"] != "s3cr3t=value#x" || len(values) != 2 {
		t.Fatalf("values = %v", values)
	}
	for name, input := range map[string]string{
		"trailing space":  "A=value \n",
		"leading space":   " A=value\n",
		"inline comment":  "A=value #note\n",
		"carriage return": "A=value\r\n",
		"empty value":     "A=\n",
		"no equals":       "A\n",
		"bad name":        "1A=value\n",
		"repeated name":   "A=one\nA=two\n",
		"nul":             "A=va\x00lue\n",
	} {
		t.Run(name, func(t *testing.T) {
			_, err := ParseEnvelope([]byte(input))
			if !errors.Is(err, ErrInvalid) {
				t.Fatalf("err = %v, want ErrInvalid", err)
			}
			if strings.Contains(err.Error(), "=value") || strings.Contains(err.Error(), "=one") {
				t.Fatalf("error echoes a value: %v", err)
			}
		})
	}
}

func TestKeyIdentityIgnoresEnvelopeFormattingAndFileLocation(t *testing.T) {
	key, err := CreateKey(filepath.Join(t.TempDir(), "k", KeyFileName))
	if err != nil {
		t.Fatal(err)
	}
	a := key.Identity(map[string]string{"X": "1", "F": "/a/one.json"}, map[string]string{"F": "digest"})
	b := key.Identity(map[string]string{"F": "/b/two.json", "X": "1"}, map[string]string{"F": "digest"})
	if a != b {
		t.Fatal("file location or map order changed identity")
	}
	if a == key.Identity(map[string]string{"X": "1", "F": "/a/one.json"}, map[string]string{"F": "other"}) {
		t.Fatal("file content change kept identity")
	}
	if a == key.Identity(map[string]string{"X": "2", "F": "/a/one.json"}, map[string]string{"F": "digest"}) {
		t.Fatal("value change kept identity")
	}
	other, err := CreateKey(filepath.Join(t.TempDir(), KeyFileName))
	if err != nil {
		t.Fatal(err)
	}
	if other.ID == key.ID || other.Identity(map[string]string{"X": "1"}, nil) == key.Identity(map[string]string{"X": "1"}, nil) {
		t.Fatal("different keys share identity space")
	}
}

func TestKeyFileLifecycle(t *testing.T) {
	path := filepath.Join(t.TempDir(), "dir", KeyFileName)
	if _, err := LoadKey(path); !errors.Is(err, ErrKeyUnavailable) {
		t.Fatalf("missing key err = %v", err)
	}
	created, err := CreateKey(path)
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil || (runtime.GOOS != "windows" && info.Mode().Perm() != 0o600) {
		t.Fatalf("key mode = %v, err %v", info.Mode(), err)
	}
	loaded, err := LoadKey(path)
	if err != nil || loaded.ID != created.ID {
		t.Fatalf("reload = %v, %v", loaded, err)
	}
	if _, err := CreateKey(path); !errors.Is(err, ErrKeyUnavailable) {
		t.Fatalf("overwrite err = %v", err)
	}
	if err := os.WriteFile(path, []byte("short\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadKey(path); !errors.Is(err, ErrKeyUnavailable) {
		t.Fatalf("malformed key err = %v", err)
	}
}

func TestParseRegistryIsStrict(t *testing.T) {
	registry, err := ParseRegistry([]byte("version: 1\nrecipes:\n  project.w03-fixture:\n    description: canary fixture\n"))
	if err != nil || !registry.Has("project.w03-fixture") || registry.Has("project.other") {
		t.Fatalf("registry = %v, %v", registry, err)
	}
	for name, input := range map[string]string{
		"unknown key": "version: 1\nrecipes:\n  a.b:\n    argv: [x]\n",
		"version":     "version: 2\nrecipes: {}\n",
		"bad name":    "version: 1\nrecipes:\n  A.B: {}\n",
		"empty":       "",
	} {
		if _, err := ParseRegistry([]byte(input)); err == nil {
			t.Errorf("%s: registry accepted", name)
		}
	}
}

func TestMaterializeChecksDeclarationLocationAndSecrets(t *testing.T) {
	repo := t.TempDir()
	outside := t.TempDir()
	key, err := CreateKey(filepath.Join(t.TempDir(), KeyFileName))
	if err != nil {
		t.Fatal(err)
	}
	artifact := filepath.Join(outside, "fixture.json")
	if err := os.WriteFile(artifact, []byte(`{"id":1}`), 0o600); err != nil {
		t.Fatal(err)
	}
	envelope := filepath.Join(outside, "input.env")
	write := func(content string) {
		t.Helper()
		if err := os.WriteFile(envelope, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	declaration := models.RuntimeInput{ID: "w03", Env: []string{"FIXTURE_FILE", "API_CREDENTIAL"}, Files: []string{"FIXTURE_FILE"}, Secret: true}
	write("FIXTURE_FILE=" + artifact + "\nAPI_CREDENTIAL=long-enough-secret\n")
	first, err := Materialize(key, declaration, envelope, []string{repo})
	if err != nil {
		t.Fatalf("Materialize: %v", err)
	}
	if got := strings.Join(first.Environment(), ","); !strings.Contains(got, "API_CREDENTIAL=long-enough-secret") {
		t.Fatalf("environment = %s", got)
	}

	// A copied, byte-identical artifact under a new name keeps identity.
	moved := filepath.Join(outside, "renamed.json")
	if err := os.WriteFile(moved, []byte(`{"id":1}`), 0o600); err != nil {
		t.Fatal(err)
	}
	write("# comment\nAPI_CREDENTIAL=long-enough-secret\n\nFIXTURE_FILE=" + moved + "\n")
	second, err := Materialize(key, declaration, envelope, []string{repo})
	if err != nil || second.Identity != first.Identity {
		t.Fatalf("relocated identity = %v, err %v", second, err)
	}

	// Changed content behind the same path is a new identity.
	if err := os.WriteFile(moved, []byte(`{"id":2}`), 0o600); err != nil {
		t.Fatal(err)
	}
	third, err := Materialize(key, declaration, envelope, []string{repo})
	if err != nil || third.Identity == first.Identity {
		t.Fatalf("changed content identity = %v, err %v", third, err)
	}

	for name, content := range map[string]string{
		"undeclared":   "FIXTURE_FILE=" + artifact + "\nAPI_CREDENTIAL=long-enough-secret\nOTHER=x\n",
		"missing":      "FIXTURE_FILE=" + artifact + "\n",
		"short secret": "FIXTURE_FILE=" + artifact + "\nAPI_CREDENTIAL=short\n",
		"relative":     "FIXTURE_FILE=fixture.json\nAPI_CREDENTIAL=long-enough-secret\n",
	} {
		write(content)
		if _, err := Materialize(key, declaration, envelope, []string{repo}); !errors.Is(err, ErrInvalid) {
			t.Errorf("%s: err = %v, want ErrInvalid", name, err)
		}
	}

	inside := filepath.Join(repo, "input.env")
	if err := os.WriteFile(inside, []byte("FIXTURE_FILE="+artifact+"\nAPI_CREDENTIAL=long-enough-secret\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Materialize(key, declaration, inside, []string{repo}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("envelope inside repository err = %v", err)
	}
	if _, err := Materialize(key, declaration, filepath.Join(outside, "absent.env"), []string{repo}); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("absent envelope err = %v", err)
	} else if strings.Contains(err.Error(), outside) {
		t.Fatalf("unavailable error names the path: %v", err)
	}
}
