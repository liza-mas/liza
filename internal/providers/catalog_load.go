package providers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	neturl "net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/liza-mas/liza/internal/brand"
	"github.com/liza-mas/liza/internal/brandrender"
	"github.com/liza-mas/liza/internal/paths"
)

var errCatalogNotModified = errors.New("provider catalog not modified")

const (
	DefaultCatalogURL       = "https://raw.githubusercontent.com/liza-mas/liza/main/provider-catalog.yaml"
	defaultCatalogTTL       = time.Hour
	defaultCatalogTimeout   = 1500 * time.Millisecond
	envCatalogURLSuffix     = "PROVIDER_CATALOG_URL"
	envCatalogTTLSuffix     = "PROVIDER_CATALOG_TTL"
	envCatalogTimeoutSuffix = "PROVIDER_CATALOG_TIMEOUT"
)

var (
	EnvCatalogURL     = brand.EnvName(envCatalogURLSuffix)
	EnvCatalogTTL     = brand.EnvName(envCatalogTTLSuffix)
	EnvCatalogTimeout = brand.EnvName(envCatalogTimeoutSuffix)
)

type LoadOptions struct {
	URL      string
	HomeDir  string
	TTL      time.Duration
	Timeout  time.Duration
	Force    bool
	Client   *http.Client
	LookPath func(string) (string, error)
	Now      func() time.Time
}

type CacheMeta struct {
	URL          string    `json:"url"`
	ETag         string    `json:"etag,omitempty"`
	LastModified string    `json:"last_modified,omitempty"`
	FetchedAt    time.Time `json:"fetched_at"`
}

func EmbeddedCatalog() Catalog {
	rendered, err := brandrender.RenderBytes([]byte(embeddedFallbackCatalogYAML), brand.RuntimeValues())
	if err != nil {
		panic(err)
	}
	cat, err := ParseCatalog(rendered)
	if err != nil {
		panic(err)
	}
	return cat
}

func Load(ctx context.Context, opts LoadOptions) (Catalog, error) {
	url := firstNonEmpty(opts.URL, catalogEnvValue(envCatalogURLSuffix), DefaultCatalogURL)
	homeDir := opts.HomeDir
	if homeDir == "" {
		var err error
		homeDir, err = paths.UserHomeDir()
		if err != nil {
			if opts.Force {
				return Catalog{}, err
			}
			return EmbeddedCatalog(), nil
		}
	}
	ttl := opts.TTL
	if ttl <= 0 {
		ttl = durationFromEnv(envCatalogTTLSuffix, defaultCatalogTTL)
	}
	timeout := opts.Timeout
	if timeout <= 0 {
		timeout = durationFromEnv(envCatalogTimeoutSuffix, defaultCatalogTimeout)
	}
	now := opts.Now
	if now == nil {
		now = time.Now
	}
	cachePath, metaPath := CachePaths(homeDir)
	meta, _ := readMeta(metaPath)
	if !opts.Force && meta.URL == url && !meta.FetchedAt.IsZero() && now().Sub(meta.FetchedAt) < ttl {
		if cat, err := readCatalogFile(cachePath); err == nil {
			return cat, nil
		}
	}
	requestMeta := meta
	if requestMeta.URL != url {
		requestMeta = CacheMeta{}
	}
	cat, metaOut, err := fetchCatalog(ctx, opts.Client, url, timeout, requestMeta, now().UTC())
	if err == nil {
		if cacheErr := writeCatalogCache(cachePath, metaPath, cat.raw, metaOut); cacheErr != nil && opts.Force {
			// The fetch itself succeeded and cat.catalog is valid; only the
			// disk persist failed. Return it alongside the error instead of
			// discarding it, so a caller that wants the freshly-fetched data
			// despite a non-durable refresh isn't forced to refetch.
			return cat.catalog, cacheErr
		}
		return cat.catalog, nil
	}
	if errors.Is(err, errCatalogNotModified) {
		if meta.URL != url {
			if opts.Force {
				return Catalog{}, fmt.Errorf("provider catalog not modified but cache belongs to %q", meta.URL)
			}
			return EmbeddedCatalog(), nil
		}
		cached, cacheErr := readCatalogFile(cachePath)
		if cacheErr != nil {
			if opts.Force {
				return Catalog{}, fmt.Errorf("provider catalog not modified but cache is unavailable: %w", cacheErr)
			}
		} else {
			if writeErr := writeMeta(metaPath, metaOut); writeErr != nil && opts.Force {
				return Catalog{}, writeErr
			}
			return cached, nil
		}
	}
	if opts.Force {
		return Catalog{}, err
	}
	if meta.URL == url {
		if cat, cacheErr := readCatalogFile(cachePath); cacheErr == nil {
			return cat, nil
		}
	}
	if cat, cacheErr := readCatalogFile(cachePath); cacheErr == nil && meta.URL == "" {
		return cat, nil
	}
	if cat, legacyPath, legacyErr := readLegacyCatalogCache(homeDir, cachePath); legacyErr == nil {
		fmt.Fprintf(os.Stderr, "Warning: using legacy provider catalog cache %s; refresh providers to migrate it to %s\n", legacyPath, cachePath)
		return cat, nil
	}
	return EmbeddedCatalog(), nil
}

func CachePaths(homeDir string) (catalogPath, metaPath string) {
	cacheDir := filepath.Join(homeDir, paths.GlobalDirName(), "cache")
	return filepath.Join(cacheDir, "provider-catalog.yaml"), filepath.Join(cacheDir, "provider-catalog.meta.json")
}

func readLegacyCatalogCache(homeDir, brandedCachePath string) (Catalog, string, error) {
	if paths.GlobalDirName() == ".liza" {
		return Catalog{}, "", os.ErrNotExist
	}
	if _, err := os.Stat(brandedCachePath); err == nil || !os.IsNotExist(err) {
		return Catalog{}, "", os.ErrNotExist
	}
	// Pre-brand cache location retained for offline upgrades.
	legacyPath := filepath.Join(homeDir, ".liza", "cache", "provider-catalog.yaml")
	cat, err := readCatalogFile(legacyPath)
	return cat, legacyPath, err
}

type fetchedCatalog struct {
	catalog Catalog
	raw     []byte
}

func fetchCatalog(ctx context.Context, client *http.Client, url string, timeout time.Duration, meta CacheMeta, fetchedAt time.Time) (fetchedCatalog, CacheMeta, error) {
	parsed, err := neturl.Parse(url)
	if err != nil {
		return fetchedCatalog{}, CacheMeta{}, fmt.Errorf("invalid provider catalog URL: %w", err)
	}
	host := parsed.Hostname()
	isLoopback := host == "localhost" || host == "127.0.0.1" || host == "::1"
	if parsed.Scheme != "https" && !(parsed.Scheme == "http" && isLoopback) {
		return fetchedCatalog{}, CacheMeta{}, fmt.Errorf("provider catalog URL must use HTTPS")
	}
	if client == nil {
		client = &http.Client{Timeout: timeout}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return fetchedCatalog{}, CacheMeta{}, err
	}
	if meta.ETag != "" {
		req.Header.Set("If-None-Match", meta.ETag)
	}
	if meta.LastModified != "" {
		req.Header.Set("If-Modified-Since", meta.LastModified)
	}
	resp, err := client.Do(req)
	if err != nil {
		return fetchedCatalog{}, CacheMeta{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotModified {
		metaOut := meta
		metaOut.URL = url
		if etag := resp.Header.Get("ETag"); etag != "" {
			metaOut.ETag = etag
		}
		if lastModified := resp.Header.Get("Last-Modified"); lastModified != "" {
			metaOut.LastModified = lastModified
		}
		metaOut.FetchedAt = fetchedAt
		return fetchedCatalog{}, metaOut, errCatalogNotModified
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fetchedCatalog{}, CacheMeta{}, fmt.Errorf("provider catalog fetch returned HTTP %d", resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return fetchedCatalog{}, CacheMeta{}, err
	}
	cat, err := ParseCatalog(data)
	if err != nil {
		return fetchedCatalog{}, CacheMeta{}, err
	}
	return fetchedCatalog{catalog: cat, raw: data}, CacheMeta{
		URL:          url,
		ETag:         resp.Header.Get("ETag"),
		LastModified: resp.Header.Get("Last-Modified"),
		FetchedAt:    fetchedAt,
	}, nil
}

func writeCatalogCache(cachePath, metaPath string, data []byte, meta CacheMeta) error {
	if err := os.MkdirAll(filepath.Dir(cachePath), 0755); err != nil {
		return err
	}
	if err := os.WriteFile(cachePath, data, 0644); err != nil {
		return err
	}
	return writeMeta(metaPath, meta)
}

func readCatalogFile(path string) (Catalog, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Catalog{}, err
	}
	return ParseCatalog(data)
}

func readMeta(path string) (CacheMeta, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return CacheMeta{}, err
	}
	var meta CacheMeta
	if err := json.Unmarshal(data, &meta); err != nil {
		return CacheMeta{}, err
	}
	return meta, nil
}

func writeMeta(path string, meta CacheMeta) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0644)
}

func catalogEnvValue(suffix string) string {
	lookup := brand.LookupEnv(os.Getenv, suffix)
	if lookup.Warning != "" {
		fmt.Fprintf(os.Stderr, "Warning: %s\n", lookup.Warning)
	}
	return lookup.Value
}

func durationFromEnv(suffix string, fallback time.Duration) time.Duration {
	value := strings.TrimSpace(catalogEnvValue(suffix))
	if value == "" {
		return fallback
	}
	d, err := time.ParseDuration(value)
	if err != nil || d <= 0 {
		return fallback
	}
	return d
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
