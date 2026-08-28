package main

import (
	"context"
	"strings"
	"testing"

	"github.com/DecoyOtter/buyforbub/internal/store"
)

// TestSeedIsUsable guards seed.json against hand-editing mistakes: it must
// parse, and every item must survive the same validation a user-added item does.
func TestSeedIsUsable(t *testing.T) {
	items, err := loadSeed()
	if err != nil {
		t.Fatalf("loadSeed: %v", err)
	}
	if len(items) == 0 {
		t.Fatal("seed.json is empty")
	}

	s, err := store.Open(store.InMemory)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer s.Close()

	n, err := s.SeedIfEmpty(context.Background(), items)
	if err != nil {
		t.Fatalf("SeedIfEmpty rejected seed.json: %v", err)
	}
	if n != len(items) {
		t.Errorf("seeded %d items, want %d", n, len(items))
	}
}

func TestSeedHasNoDuplicateNames(t *testing.T) {
	items, err := loadSeed()
	if err != nil {
		t.Fatalf("loadSeed: %v", err)
	}

	seen := make(map[string]int, len(items))
	for i, it := range items {
		key := strings.ToLower(strings.TrimSpace(it.Name))
		if first, dup := seen[key]; dup {
			t.Errorf("duplicate name %q at entries %d and %d", it.Name, first, i)
			continue
		}
		seen[key] = i
	}
}

// TestSeedCoversEveryCategory keeps the default list from silently dropping a
// category, which would leave that heading empty on a fresh install.
func TestSeedCoversEveryCategory(t *testing.T) {
	items, err := loadSeed()
	if err != nil {
		t.Fatalf("loadSeed: %v", err)
	}

	count := make(map[string]int, len(store.Categories))
	for _, it := range items {
		count[it.Category]++
	}
	for _, c := range store.Categories {
		if count[c] == 0 {
			t.Errorf("no seed items in category %q", c)
		}
	}
}
