package typefaces

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"
)

const fontsourceListURL = "https://api.fontsource.org/v1/fonts"

// CatalogItem is one Google Fonts family from Fontsource.
type CatalogItem struct {
	ID       string `json:"id"`
	Family   string `json:"family"`
	Category string `json:"category"`
	Variable bool   `json:"variable"`
}

type catalogFont struct {
	ID       string `json:"id"`
	Family   string `json:"family"`
	Category string `json:"category"`
	Variable bool   `json:"variable"`
	Type     string `json:"type"`
}

var (
	catalogMu  sync.Mutex
	catalog    []catalogFont
	catalogAt  time.Time
	httpClient = &http.Client{Timeout: 45 * time.Second}
)

// SearchCatalog filters the Fontsource Google corpus.
// Empty q returns Popular (or Blackletter) so the picker is not ABeeZee.
func SearchCatalog(ctx context.Context, q string, limit int) ([]CatalogItem, error) {
	return SearchCatalogCat(ctx, q, "", limit)
}

// SearchCatalogCat is SearchCatalog with a category chip (sans-serif, serif, display, blackletter, …).
func SearchCatalogCat(ctx context.Context, q, cat string, limit int) ([]CatalogItem, error) {
	if limit <= 0 || limit > 80 {
		limit = 40
	}
	q = strings.ToLower(strings.TrimSpace(q))
	cat = NormalizeCategory(cat)
	if q == "" {
		src := Popular
		if cat == "blackletter" {
			src = Blackletter
		}
		return filterCatalog(src, "", cat, limit), nil
	}
	all, err := loadCatalog(ctx)
	if err != nil {
		return nil, err
	}
	items := make([]CatalogItem, 0, limit)
	for _, f := range all {
		if f.Type != "" && f.Type != "google" {
			continue
		}
		items = append(items, CatalogItem{ID: f.ID, Family: f.Family, Category: f.Category, Variable: f.Variable})
	}
	return filterCatalog(items, q, cat, limit), nil
}

func filterCatalog(items []CatalogItem, q, cat string, limit int) []CatalogItem {
	out := make([]CatalogItem, 0, limit)
	for _, it := range items {
		if q != "" && !strings.Contains(strings.ToLower(it.Family), q) && !strings.Contains(it.ID, q) {
			continue
		}
		if !itemMatchesCat(it, cat) {
			continue
		}
		out = append(out, it)
		if len(out) >= limit {
			break
		}
	}
	return out
}

func loadCatalog(ctx context.Context) ([]catalogFont, error) {
	catalogMu.Lock()
	defer catalogMu.Unlock()
	if len(catalog) > 0 && time.Since(catalogAt) < time.Hour {
		return catalog, nil
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fontsourceListURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "Rewind/fonts")
	res, err := httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(io.LimitReader(res.Body, 1024))
		return nil, fmt.Errorf("font catalog: HTTP %d %s", res.StatusCode, b)
	}
	var list []catalogFont
	if err := json.NewDecoder(res.Body).Decode(&list); err != nil {
		return nil, fmt.Errorf("font catalog: %w", err)
	}
	catalog = list
	catalogAt = time.Now()
	return catalog, nil
}

func findCatalogID(ctx context.Context, family string) (catalogFont, error) {
	all, err := loadCatalog(ctx)
	if err != nil {
		return catalogFont{}, err
	}
	want := strings.ToLower(strings.TrimSpace(family))
	wantSlug := slug(family)
	for _, f := range all {
		if f.ID == want || f.ID == wantSlug || strings.ToLower(f.Family) == want {
			return f, nil
		}
	}
	return catalogFont{}, fmt.Errorf("unknown font family %q", family)
}
