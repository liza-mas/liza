package commands

import (
	"context"
	"fmt"

	"github.com/liza-mas/liza/internal/providers"
)

func loadProviderCatalog() providers.Catalog {
	cat, _ := providers.Load(context.Background(), providers.LoadOptions{})
	return cat
}

func resolveCatalogProviders(cat providers.Catalog, ids []string) ([]providers.Provider, error) {
	resolved := make([]providers.Provider, 0, len(ids))
	seen := map[string]bool{}
	for _, id := range ids {
		provider, ok := cat.Resolve(id)
		if !ok {
			return nil, fmt.Errorf("unknown provider: %s", id)
		}
		if seen[provider.ID] {
			continue
		}
		seen[provider.ID] = true
		resolved = append(resolved, provider)
	}
	return resolved, nil
}
