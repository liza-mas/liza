package agentlaunch

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/providers"
)

// PB-10: BuiltInAgentTools loads the runtime catalog once and renders the
// embedded catalog once; every later call reuses both. The test resets the two
// memos so the real load path runs (TestMain seeds the runtime memo for the other
// tests), serves the runtime catalog from a fresh on-disk cache in a temporary
// home, and points the catalog URL at a closed port so a cache miss fails fast
// instead of reaching the network. It observes the memos through the registry:
// changing the cache file, and the embedded catalog, between calls changes
// nothing.
func TestA9CatalogCachesLoadOnce(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	const catalogURL = "http://127.0.0.1:1/provider-catalog.yaml"
	t.Setenv(providers.EnvCatalogURL, catalogURL)
	t.Setenv(providers.EnvCatalogTTL, "1h")
	resetCatalogMemos(t)

	catalogPath, metaPath := providers.CachePaths(home)
	meta, err := json.Marshal(providers.CacheMeta{URL: catalogURL, FetchedAt: time.Now()})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(catalogPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(metaPath, meta, 0o644); err != nil {
		t.Fatal(err)
	}
	writeCatalogCache := func(id string) {
		t.Helper()
		if err := os.WriteFile(catalogPath, []byte(a9MinimalCatalogYAML(id)), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	writeCatalogCache("zz-first")
	first := BuiltInAgentTools()
	if _, ok := first["zz-first"]; !ok {
		t.Fatal("first call did not load the cached runtime catalog")
	}
	if _, ok := first["claude"]; !ok {
		t.Fatal("first call did not merge the embedded catalog")
	}

	writeCatalogCache("zz-second")
	sentinel, err := providers.ParseCatalog([]byte(a9MinimalCatalogYAML("zz-embedded")))
	if err != nil {
		t.Fatal(err)
	}
	embeddedCatalog = sentinel
	second := BuiltInAgentTools()
	if _, ok := second["zz-first"]; !ok {
		t.Error("second call reloaded the runtime catalog")
	}
	if _, ok := second["zz-second"]; ok {
		t.Error("second call read the changed cache file")
	}
	if _, ok := second["zz-embedded"]; !ok {
		t.Error("second call reloaded the embedded catalog")
	}
	if _, ok := second["claude"]; ok {
		t.Error("second call rendered the embedded catalog again")
	}
}

// resetCatalogMemos makes the next BuiltInAgentTools call load both catalogs and
// restores the seeded state when the test ends.
func resetCatalogMemos(t *testing.T) {
	t.Helper()
	previousRuntime := runtimeCatalog
	runtimeCatalogOnce, embeddedCatalogOnce = sync.Once{}, sync.Once{}
	t.Cleanup(func() {
		runtimeCatalog, embeddedCatalog = previousRuntime, providers.EmbeddedCatalog()
		runtimeCatalogOnce, embeddedCatalogOnce = sync.Once{}, sync.Once{}
		runtimeCatalogOnce.Do(func() {})
		embeddedCatalogOnce.Do(func() {})
	})
}

func a9MinimalCatalogYAML(id string) string {
	return "version: 1\nproviders:\n  - id: " + id + "\n    display_name: " + id + "\n    backend: cli\n    runtime:\n      executable: " + id + "\n"
}
