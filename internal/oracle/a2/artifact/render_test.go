//go:build linux

package artifact

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/liza-mas/liza/internal/testhelpers/oracle"
)

func moduleRoot(t *testing.T) string {
	t.Helper()
	root, err := os.Getwd()
	if err != nil {
		t.Fatal("locate tree under test")
	}
	for {
		if _, err := os.Stat(filepath.Join(root, "go.mod")); err == nil {
			return root
		}
		parent := filepath.Dir(root)
		if parent == root {
			t.Fatal("tree under test has no go.mod")
		}
		root = parent
	}
}

func TestOracleA2EmbeddedRenderDefault(t *testing.T) {
	isolateEnv(t, false)
	entries, err := renderedFiles(moduleRoot(t), nil)
	if err != nil {
		t.Fatal("render embedded tree with default brand")
	}
	oracle.Check(t, oracle.Artifact, "embedded-render-default.manifest", manifest(entries, true))
}

func TestOracleA2EmbeddedRenderFixtureBrand(t *testing.T) {
	isolateEnv(t, false)
	entries, err := renderedFiles(moduleRoot(t), map[string]string{
		"BRAND_NAME_LOWER": "acme-oracle", "BRAND_REPO": "acme-labs/acme-oracle",
	})
	if err != nil {
		t.Fatal("render embedded tree with fixture brand")
	}
	oracle.Check(t, oracle.Artifact, "embedded-render-fixture-brand.manifest", manifest(entries, true))
}

func TestOracleA2GlobalInstall(t *testing.T) {
	e := isolateEnv(t, false)
	target := filepath.Join(e.work, "global")
	if err := writeGlobal(target); err != nil {
		t.Fatal("write global install set")
	}
	oracle.Check(t, oracle.Artifact, "global-install.manifest", manifest(snapshot(t, target), true))
}
