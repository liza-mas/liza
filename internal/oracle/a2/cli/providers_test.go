//go:build linux

package cli

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/liza-mas/liza/internal/testhelpers/oracle"
)

func catalogServer(t *testing.T, f *fixture) *atomic.Int32 {
	t.Helper()
	catalog := input(t, "catalog.yaml")
	requests := new(atomic.Int32)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		if r.URL.Path != "/catalog.yaml" {
			t.Error("unexpected catalog request path")
			http.NotFound(w, r)
			return
		}
		w.Header().Set("ETag", `"oracle-catalog"`)
		_, err := w.Write(catalog)
		if err != nil {
			t.Error(err)
		}
	}))
	t.Cleanup(srv.Close)
	f.norm.Literal(strings.TrimPrefix(srv.URL, "http://"), "127.0.0.1:<PORT>")
	// Replace our own known fixture setting, never a host-derived value.
	f.extra[1] = "LIZA_PROVIDER_CATALOG_URL=" + srv.URL + "/catalog.yaml"
	return requests
}

func TestOracleA2ProvidersList(t *testing.T) {
	for _, name := range []string{"catalog", "fallback"} {
		t.Run(name, func(t *testing.T) {
			f := newFixture(t)
			var requests *atomic.Int32
			if name == "catalog" {
				requests = catalogServer(t, f)
			}
			f.check("providers-list/"+name, oracle.Human, f.run(nil, "providers", "list"), 0)
			if requests != nil && requests.Load() != 1 {
				t.Fatal("catalog case did not fetch exactly once")
			}
		})
	}
}

func TestOracleA2ProvidersDetect(t *testing.T) {
	f := newFixture(t)
	f.stub("claude", "present")
	f.stub("codex", "present")
	f.check("providers-detect/two-present", oracle.Human, f.run(nil, "providers", "detect"), 0)
}

func TestOracleA2ProvidersRefresh(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		f := newFixture(t)
		requests := catalogServer(t, f)
		f.check("providers-refresh/success", oracle.Human, f.run(nil, "providers", "refresh"), 0)
		if requests.Load() != 1 {
			t.Fatal("refresh did not fetch exactly once")
		}
		f.fileEffects("providers-refresh/success", map[string]string{"home": f.home})
	})
	t.Run("refusal", func(t *testing.T) {
		f := newFixture(t)
		f.check("providers-refresh/refusal", oracle.Human, f.run(nil, "providers", "refresh"), 1)
		f.fileEffects("providers-refresh/refusal", map[string]string{"home": f.home})
	})
}
