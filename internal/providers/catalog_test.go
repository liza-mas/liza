package providers

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

func TestEmbeddedCatalogResolvesBuiltInsAndAliases(t *testing.T) {
	cat := EmbeddedCatalog()

	for _, id := range []string{"claude", "codex", "codex-acp", "cursor-acp", "opencode", "opencode-acp", "gemini", "mistral", "kimi"} {
		if _, ok := cat.Resolve(id); !ok {
			t.Fatalf("EmbeddedCatalog().Resolve(%q) = false", id)
		}
	}
	cursor, ok := cat.Resolve("cursor")
	if !ok || cursor.ID != "cursor-acp" {
		t.Fatalf("Resolve(cursor) = %+v, %v; want cursor-acp", cursor, ok)
	}
	if !cursor.Setup.ActivationAssets.CursorHooks {
		t.Fatal("cursor-acp missing Cursor hook activation asset")
	}
	p, ok := cat.Resolve("vibe")
	if !ok || p.ID != "mistral" {
		t.Fatalf("Resolve(vibe) = %+v, %v; want mistral", p, ok)
	}

	tools := cat.RuntimeTools()
	q := tools["codex-acp"]
	if q.Backend != "acpx" || q.ACPXAgent != "codex" || !slices.Equal(q.RequiredExecutables, []string{"acpx"}) {
		t.Fatalf("codex-acp runtime = %+v, want configured ACPX target", q)
	}
	if tools["kimi"].ContractKey != "claude" {
		t.Fatalf("kimi contract key = %q, want claude", tools["kimi"].ContractKey)
	}
}

func TestRepositoryCatalogAddsRemoteProviders(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "provider-catalog.yaml"))
	if err != nil {
		t.Fatalf("read provider-catalog.yaml: %v", err)
	}
	cat, err := ParseCatalog(data)
	if err != nil {
		t.Fatalf("ParseCatalog(provider-catalog.yaml) error = %v", err)
	}
	if _, ok := cat.Resolve("qwen"); !ok {
		t.Fatal("provider-catalog.yaml missing qwen")
	}
	cursor, ok := cat.Resolve("cursor")
	if !ok || cursor.ID != "cursor-acp" {
		t.Fatalf("provider-catalog.yaml Resolve(cursor) = %+v, %v; want cursor-acp", cursor, ok)
	}
	if !cursor.Setup.ActivationAssets.CursorHooks {
		t.Fatal("provider-catalog.yaml cursor-acp missing Cursor hook activation asset")
	}
	qwenACP, ok := cat.Resolve("qwen-acp")
	if !ok {
		t.Fatal("provider-catalog.yaml missing qwen-acp")
	}
	tool := qwenACP.RuntimeToolConfig()
	if tool.Backend != "acpx" || tool.ACPXAgent != "qwen" || tool.ContractKey != "qwen" {
		t.Fatalf("qwen-acp runtime = %+v, want qwen ACPX config", tool)
	}
	devin, ok := cat.Resolve("devin")
	if !ok {
		t.Fatal("provider-catalog.yaml missing devin")
	}
	if devin.Setup.Contract.RepoFile != ".windsurf/rules/liza.md" {
		t.Fatalf("devin contract repo file = %q, want .windsurf/rules/liza.md", devin.Setup.Contract.RepoFile)
	}
	devinACP, ok := cat.Resolve("devin-acp")
	if !ok {
		t.Fatal("provider-catalog.yaml missing devin-acp")
	}
	tool = devinACP.RuntimeToolConfig()
	if tool.Backend != "acpx" || tool.ACPXAgent != "devin acp" || tool.ContractKey != "devin" {
		t.Fatalf("devin-acp runtime = %+v, want devin ACPX config", tool)
	}
	if !slices.Equal(tool.RequiredExecutables, []string{"acpx", "devin"}) {
		t.Fatalf("devin-acp required executables = %v, want acpx and devin", tool.RequiredExecutables)
	}
	if !slices.Contains(tool.ACPXPromptArgs, "--agent") || !slices.Contains(tool.ACPXPromptArgs, "{{acpxAgent}}") {
		t.Fatalf("devin-acp prompt args = %v, want raw agent command placeholder", tool.ACPXPromptArgs)
	}
}

func TestParseCatalogRejectsUnknownFieldsAndUnsafeValues(t *testing.T) {
	t.Run("unknown field", func(t *testing.T) {
		_, err := ParseCatalog([]byte(`version: 1
providers:
  - id: qwen
    display_name: Qwen
    backend: cli
    shell: rm -rf /
    runtime:
      executable: qwen
`))
		if err == nil || !strings.Contains(err.Error(), "field shell not found") {
			t.Fatalf("ParseCatalog error = %v, want strict unknown field error", err)
		}
	})

	t.Run("unsafe executable", func(t *testing.T) {
		_, err := ParseCatalog([]byte(`version: 1
providers:
  - id: qwen
    display_name: Qwen
    backend: cli
    detection:
      binaries: ["qwen;rm"]
    runtime:
      executable: qwen
`))
		if err == nil || !strings.Contains(err.Error(), "invalid executable") {
			t.Fatalf("ParseCatalog error = %v, want invalid executable error", err)
		}
	})

	t.Run("unsafe setup path", func(t *testing.T) {
		tests := []struct {
			name  string
			field string
		}{
			{name: "config dir", field: "config_dir: ../outside"},
			{name: "skills dir", field: "skills_dir: /tmp/skills"},
			{name: "extra dir", field: "extra_dirs: [../../outside]"},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				_, err := ParseCatalog([]byte(`version: 1
providers:
  - id: qwen
    display_name: Qwen
    backend: cli
    setup:
      ` + tt.field + `
    runtime:
      executable: qwen
`))
				if err == nil || !strings.Contains(err.Error(), "invalid setup path") {
					t.Fatalf("ParseCatalog error = %v, want invalid setup path error", err)
				}
			})
		}
	})

	t.Run("unsafe contract path", func(t *testing.T) {
		tests := []struct {
			name  string
			field string
		}{
			{name: "repo file", field: "repo_file: /tmp/liza.md"},
			{name: "global fallback", field: "global_fallback: ../AGENTS.md"},
			{name: "local fallback", field: "local_fallback: /tmp/AGENTS.md"},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				_, err := ParseCatalog([]byte(`version: 1
providers:
  - id: qwen
    display_name: Qwen
    backend: cli
    setup:
      contract:
        ` + tt.field + `
    runtime:
      executable: qwen
`))
				if err == nil || !strings.Contains(err.Error(), "invalid setup path") {
					t.Fatalf("ParseCatalog error = %v, want invalid setup path error", err)
				}
			})
		}
	})

	t.Run("unsafe env file path", func(t *testing.T) {
		_, err := ParseCatalog([]byte(`version: 1
providers:
  - id: qwen
    display_name: Qwen
    backend: cli
    runtime:
      executable: qwen
      env_files: ["../../.env"]
`))
		if err == nil || !strings.Contains(err.Error(), "invalid runtime env file") {
			t.Fatalf("ParseCatalog error = %v, want invalid runtime env file error", err)
		}
	})

	t.Run("unsafe runtime executable", func(t *testing.T) {
		tests := []struct {
			name  string
			value string
		}{
			{name: "absolute path", value: "/tmp/payload"},
			{name: "relative traversal", value: "../payload"},
			{name: "shell metachar", value: "qwen;rm"},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				_, err := ParseCatalog([]byte(`version: 1
providers:
  - id: qwen
    display_name: Qwen
    backend: cli
    runtime:
      executable: ` + tt.value + `
`))
				if err == nil || !strings.Contains(err.Error(), "invalid runtime.executable") {
					t.Fatalf("ParseCatalog error = %v, want invalid runtime.executable error", err)
				}
			})
		}
	})
}

func TestParseCatalogRejectsInvalidStructure(t *testing.T) {
	t.Run("non-positive version", func(t *testing.T) {
		_, err := ParseCatalog([]byte(`version: 0
providers:
  - id: qwen
    display_name: Qwen
    backend: cli
    runtime:
      executable: qwen
`))
		if err == nil || !strings.Contains(err.Error(), "version must be positive") {
			t.Fatalf("ParseCatalog error = %v, want version error", err)
		}
	})

	t.Run("unsupported prompt transport", func(t *testing.T) {
		_, err := ParseCatalog([]byte(`version: 1
providers:
  - id: qwen
    display_name: Qwen
    backend: cli
    runtime:
      executable: qwen
      prompt_transport: telepathy
`))
		if err == nil || !strings.Contains(err.Error(), "unsupported prompt transport") {
			t.Fatalf("ParseCatalog error = %v, want prompt transport error", err)
		}
	})

	t.Run("duplicate provider id", func(t *testing.T) {
		_, err := ParseCatalog([]byte(`version: 1
providers:
  - id: qwen
    display_name: Qwen
    backend: cli
    runtime:
      executable: qwen
  - id: qwen
    display_name: Qwen Two
    backend: cli
    runtime:
      executable: qwen2
`))
		if err == nil || !strings.Contains(err.Error(), "duplicate provider id") {
			t.Fatalf("ParseCatalog error = %v, want duplicate id error", err)
		}
	})

	t.Run("alias conflicts with provider id declared later", func(t *testing.T) {
		_, err := ParseCatalog([]byte(`version: 1
providers:
  - id: alpha
    display_name: Alpha
    backend: cli
    aliases: ["beta"]
    runtime:
      executable: alpha
  - id: beta
    display_name: Beta
    backend: cli
    runtime:
      executable: beta
`))
		if err == nil || !strings.Contains(err.Error(), "conflicts with provider id") {
			t.Fatalf("ParseCatalog error = %v, want alias/id conflict error", err)
		}
	})

	t.Run("alias maps to two providers", func(t *testing.T) {
		_, err := ParseCatalog([]byte(`version: 1
providers:
  - id: alpha
    display_name: Alpha
    backend: cli
    aliases: ["shared"]
    runtime:
      executable: alpha
  - id: beta
    display_name: Beta
    backend: cli
    aliases: ["shared"]
    runtime:
      executable: beta
`))
		if err == nil || !strings.Contains(err.Error(), "maps to both") {
			t.Fatalf("ParseCatalog error = %v, want alias collision error", err)
		}
	})
}

func TestDetectUsesFakePathLookup(t *testing.T) {
	cat, err := ParseCatalog([]byte(`version: 1
providers:
  - id: qwen
    display_name: Qwen
    backend: cli
    detection:
      binaries: [qwen]
    runtime:
      executable: qwen
`))
	if err != nil {
		t.Fatalf("ParseCatalog() error = %v", err)
	}

	results := Detect(cat, func(name string) (string, error) {
		if name == "qwen" {
			return "/tmp/bin/qwen", nil
		}
		return "", errors.New("missing")
	})
	if len(results) != 1 || !results[0].Installed || results[0].Executable != "/tmp/bin/qwen" {
		t.Fatalf("Detect() = %+v, want installed qwen", results)
	}
}

func TestLoadUsesFreshCacheWithoutNetwork(t *testing.T) {
	home := t.TempDir()
	cachePath, metaPath := CachePaths(home)
	if err := os.MkdirAll(filepath.Dir(cachePath), 0755); err != nil {
		t.Fatal(err)
	}
	yaml := testQwenCatalogYAML()
	if err := os.WriteFile(cachePath, []byte(yaml), 0644); err != nil {
		t.Fatal(err)
	}
	meta := CacheMeta{URL: DefaultCatalogURL, FetchedAt: time.Date(2026, 6, 28, 12, 0, 0, 0, time.UTC)}
	if err := writeMeta(metaPath, meta); err != nil {
		t.Fatal(err)
	}

	cat, err := Load(context.Background(), LoadOptions{
		HomeDir: home,
		Now:     func() time.Time { return meta.FetchedAt.Add(5 * time.Minute) },
		Client: &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			t.Fatal("network should not be used for fresh cache")
			return nil, nil
		})},
	})
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if _, ok := cat.Resolve("qwen"); !ok {
		t.Fatalf("Load() did not return cached qwen catalog")
	}
}

func TestLoadRefreshesStaleCacheAndFallsBackOnNetworkFailure(t *testing.T) {
	home := t.TempDir()
	cachePath, metaPath := CachePaths(home)
	if err := os.MkdirAll(filepath.Dir(cachePath), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cachePath, []byte(testQwenCatalogYAML()), 0644); err != nil {
		t.Fatal(err)
	}
	old := time.Date(2026, 6, 28, 10, 0, 0, 0, time.UTC)
	if err := writeMeta(metaPath, CacheMeta{URL: DefaultCatalogURL, FetchedAt: old}); err != nil {
		t.Fatal(err)
	}

	cat, err := Load(context.Background(), LoadOptions{
		HomeDir: home,
		Now:     func() time.Time { return old.Add(2 * time.Hour) },
		Client: &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			return nil, errors.New("offline")
		})},
	})
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if _, ok := cat.Resolve("qwen"); !ok {
		t.Fatalf("Load() should fall back to stale cached qwen catalog")
	}
}

func TestLoadForcedRefreshReturnsNetworkError(t *testing.T) {
	_, err := Load(context.Background(), LoadOptions{
		URL:     "https://example.test/provider-catalog.yaml",
		HomeDir: t.TempDir(),
		Force:   true,
		Client: &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			return nil, errors.New("offline")
		})},
	})
	if err == nil || !strings.Contains(err.Error(), "offline") {
		t.Fatalf("Load(Force) error = %v, want offline error", err)
	}
}

func TestLoadForcedRefreshReturnsInvalidRemoteCatalogError(t *testing.T) {
	_, err := Load(context.Background(), LoadOptions{
		URL:     "https://example.test/provider-catalog.yaml",
		HomeDir: t.TempDir(),
		Force:   true,
		Client: &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(strings.NewReader("version: 1\nproviders:\n")),
				Header:     make(http.Header),
			}, nil
		})},
	})
	if err == nil || !strings.Contains(err.Error(), "catalog must define at least one provider") {
		t.Fatalf("Load(Force) error = %v, want invalid catalog error", err)
	}
}

func TestLoadForcedRefreshReturnsHTTPErrorStatus(t *testing.T) {
	_, err := Load(context.Background(), LoadOptions{
		URL:     "https://example.test/provider-catalog.yaml",
		HomeDir: t.TempDir(),
		Force:   true,
		Client: &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusInternalServerError,
				Body:       io.NopCloser(strings.NewReader("")),
				Header:     make(http.Header),
			}, nil
		})},
	})
	if err == nil || !strings.Contains(err.Error(), "HTTP 500") {
		t.Fatalf("Load(Force) error = %v, want HTTP 500 error", err)
	}
}

func TestLoadForcedRefreshReturnsFetchedCatalogWhenCacheWriteFails(t *testing.T) {
	home := t.TempDir()
	_, metaPath := CachePaths(home)
	// Force the catalog data write to succeed but the meta write to fail by
	// pre-occupying metaPath with a directory, so os.WriteFile errors on it.
	if err := os.MkdirAll(metaPath, 0755); err != nil {
		t.Fatal(err)
	}

	cat, err := Load(context.Background(), LoadOptions{
		URL:     "https://example.test/provider-catalog.yaml",
		HomeDir: home,
		Force:   true,
		Client: &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(strings.NewReader(testQwenCatalogYAML())),
				Header:     make(http.Header),
			}, nil
		})},
	})
	if err == nil {
		t.Fatal("Load(Force) error = nil, want cache-write error from the meta-write failure")
	}
	if _, ok := cat.Resolve("qwen"); !ok {
		t.Fatalf("Load(Force) catalog = %+v, want the freshly-fetched catalog returned alongside the cache-write error, not a discarded empty one", cat)
	}
}

func TestLoadNotModifiedRefreshesFetchedAtAndUsesCache(t *testing.T) {
	home := t.TempDir()
	cachePath, metaPath := CachePaths(home)
	if err := os.MkdirAll(filepath.Dir(cachePath), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cachePath, []byte(testQwenCatalogYAML()), 0644); err != nil {
		t.Fatal(err)
	}
	old := time.Date(2026, 6, 28, 10, 0, 0, 0, time.UTC)
	if err := writeMeta(metaPath, CacheMeta{URL: DefaultCatalogURL, ETag: `"old"`, FetchedAt: old}); err != nil {
		t.Fatal(err)
	}
	now := old.Add(2 * time.Hour)

	cat, err := Load(context.Background(), LoadOptions{
		HomeDir: home,
		Force:   true,
		Now:     func() time.Time { return now },
		Client: &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			if got := req.Header.Get("If-None-Match"); got != `"old"` {
				t.Fatalf("If-None-Match = %q, want old ETag", got)
			}
			return &http.Response{
				StatusCode: http.StatusNotModified,
				Body:       io.NopCloser(strings.NewReader("")),
				Header:     http.Header{"ETag": []string{`"old"`}},
			}, nil
		})},
	})
	if err != nil {
		t.Fatalf("Load(Force 304) error = %v", err)
	}
	if _, ok := cat.Resolve("qwen"); !ok {
		t.Fatalf("Load(Force 304) did not return cached qwen catalog")
	}
	meta, err := readMeta(metaPath)
	if err != nil {
		t.Fatalf("read meta: %v", err)
	}
	if !meta.FetchedAt.Equal(now) {
		t.Fatalf("FetchedAt = %s, want %s", meta.FetchedAt, now)
	}
}

func TestLoadNotModifiedRefreshesTTLSoSubsequentLoadSkipsNetwork(t *testing.T) {
	home := t.TempDir()
	cachePath, metaPath := CachePaths(home)
	if err := os.MkdirAll(filepath.Dir(cachePath), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cachePath, []byte(testQwenCatalogYAML()), 0644); err != nil {
		t.Fatal(err)
	}
	ttl := 10 * time.Minute
	old := time.Date(2026, 6, 28, 10, 0, 0, 0, time.UTC)
	if err := writeMeta(metaPath, CacheMeta{URL: DefaultCatalogURL, ETag: `"old"`, FetchedAt: old}); err != nil {
		t.Fatal(err)
	}
	// "now" is held fixed across both Load() calls: the first call sees a
	// stale cache (old + ttl*3 > ttl) and must hit the network; the second
	// call must see a *fresh* cache because the 304 should have persisted
	// FetchedAt = now, and now - now = 0 < ttl.
	now := old.Add(ttl * 3)

	requestCount := 0
	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		requestCount++
		if requestCount > 1 {
			t.Fatalf("unexpected network request #%d; FetchedAt should have been persisted after the first 304", requestCount)
		}
		if got := req.Header.Get("If-None-Match"); got != `"old"` {
			t.Fatalf("If-None-Match = %q, want old ETag", got)
		}
		return &http.Response{
			StatusCode: http.StatusNotModified,
			Body:       io.NopCloser(strings.NewReader("")),
			Header:     http.Header{"ETag": []string{`"old"`}},
		}, nil
	})}

	opts := LoadOptions{
		HomeDir: home,
		TTL:     ttl,
		Now:     func() time.Time { return now },
		Client:  client,
	}

	cat, err := Load(context.Background(), opts)
	if err != nil {
		t.Fatalf("first Load() error = %v", err)
	}
	if _, ok := cat.Resolve("qwen"); !ok {
		t.Fatalf("first Load() did not return cached qwen catalog")
	}
	if requestCount != 1 {
		t.Fatalf("requestCount after first Load() = %d, want 1 (network hit on stale cache)", requestCount)
	}

	meta, err := readMeta(metaPath)
	if err != nil {
		t.Fatalf("read meta after first Load(): %v", err)
	}
	if !meta.FetchedAt.Equal(now) {
		t.Fatalf("FetchedAt after first Load() = %s, want %s", meta.FetchedAt, now)
	}

	cat2, err := Load(context.Background(), opts)
	if err != nil {
		t.Fatalf("second Load() error = %v", err)
	}
	if _, ok := cat2.Resolve("qwen"); !ok {
		t.Fatalf("second Load() did not return cached qwen catalog")
	}
	if requestCount != 1 {
		t.Fatalf("requestCount after second Load() = %d, want 1 (TTL should not have expired)", requestCount)
	}
}

func TestLoadNotModifiedForDifferentURLDoesNotReuseCache(t *testing.T) {
	home := t.TempDir()
	cachePath, metaPath := CachePaths(home)
	if err := os.MkdirAll(filepath.Dir(cachePath), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cachePath, []byte(testQwenCatalogYAML()), 0644); err != nil {
		t.Fatal(err)
	}
	oldURL := "https://example.test/old-provider-catalog.yaml"
	newURL := "https://example.test/new-provider-catalog.yaml"
	old := time.Date(2026, 6, 28, 10, 0, 0, 0, time.UTC)
	if err := writeMeta(metaPath, CacheMeta{
		URL:          oldURL,
		ETag:         `"old"`,
		LastModified: "Sun, 28 Jun 2026 10:00:00 GMT",
		FetchedAt:    old,
	}); err != nil {
		t.Fatal(err)
	}

	_, err := Load(context.Background(), LoadOptions{
		URL:     newURL,
		HomeDir: home,
		Force:   true,
		Client: &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			if got := req.Header.Get("If-None-Match"); got != "" {
				t.Fatalf("If-None-Match = %q, want empty for different catalog URL", got)
			}
			if got := req.Header.Get("If-Modified-Since"); got != "" {
				t.Fatalf("If-Modified-Since = %q, want empty for different catalog URL", got)
			}
			return &http.Response{
				StatusCode: http.StatusNotModified,
				Body:       io.NopCloser(strings.NewReader("")),
				Header:     make(http.Header),
			}, nil
		})},
	})
	if err == nil || !strings.Contains(err.Error(), "cache belongs to") {
		t.Fatalf("Load(Force 304 different URL) error = %v, want cache ownership error", err)
	}
	meta, err := readMeta(metaPath)
	if err != nil {
		t.Fatalf("read meta: %v", err)
	}
	if meta.URL != oldURL {
		t.Fatalf("meta URL = %q, want old URL preserved", meta.URL)
	}
}

func TestLoadRefreshesStaleCacheFromHTTPS(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("ETag", `"fresh"`)
		_, _ = w.Write([]byte(testQwenCatalogYAML()))
	}))
	defer server.Close()

	home := t.TempDir()
	cat, err := Load(context.Background(), LoadOptions{
		URL:     server.URL,
		HomeDir: home,
		Client:  server.Client(),
	})
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if _, ok := cat.Resolve("qwen"); !ok {
		t.Fatalf("Load() did not return remote qwen catalog")
	}
	_, metaPath := CachePaths(home)
	meta, err := readMeta(metaPath)
	if err != nil {
		t.Fatalf("read meta: %v", err)
	}
	if meta.ETag != `"fresh"` {
		t.Fatalf("meta ETag = %q, want fresh", meta.ETag)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func testQwenCatalogYAML() string {
	return `version: 1
providers:
  - id: qwen
    display_name: Qwen
    aliases: [qwen-code]
    backend: cli
    detection:
      binaries: [qwen]
      version_args: [--version]
    setup:
      config_dir: .qwen
      skills_dir: skills
      contract:
        repo_file: QWEN.md
        global_fallback: .qwen/QWEN.md
    runtime:
      provider_key: qwen
      executable: qwen
      prompt_transport: stdin
      run_args: [-p]
      logged_run_args: [-p, --output-format, stream-json]
      contract_key: qwen
`
}
