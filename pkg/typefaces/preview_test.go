package typefaces

import (
	"context"
	"strings"
	"testing"
)

func TestGoogleCSSURL(t *testing.T) {
	t.Parallel()
	got := GoogleCSSURL([]string{"Playfair Display", "Inter"})
	if !strings.Contains(got, "fonts.googleapis.com") || !strings.Contains(got, "Playfair") || !strings.Contains(got, "Inter") {
		t.Fatalf("%s", got)
	}
}

func TestSearchCatalogEmptyIsFeatured(t *testing.T) {
	t.Parallel()
	items, err := SearchCatalogCat(context.Background(), "", "", 40)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) < 8 {
		t.Fatalf("expected popular list, got %d", len(items))
	}
	var hasInter, hasPlayfair bool
	for _, it := range items {
		if it.Family == "Inter" {
			hasInter = true
		}
		if it.Family == "Playfair Display" {
			hasPlayfair = true
		}
	}
	if !hasInter || !hasPlayfair {
		t.Fatalf("featured missing Inter/Playfair: %+v", items)
	}
	gothic, err := SearchCatalogCat(context.Background(), "", "blackletter", 40)
	if err != nil {
		t.Fatal(err)
	}
	if len(gothic) == 0 {
		t.Fatal("blackletter chip empty")
	}
	for _, it := range gothic {
		if !blackletterIDs[it.ID] {
			t.Fatalf("non-blackletter %s", it.ID)
		}
	}
}

func TestNormalizeCategory(t *testing.T) {
	t.Parallel()
	if got := NormalizeCategory("Sans"); got != "sans-serif" {
		t.Fatalf("%s", got)
	}
	if got := NormalizeCategory("gothic"); got != "blackletter" {
		t.Fatalf("%s", got)
	}
}
