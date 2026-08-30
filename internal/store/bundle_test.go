package store

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"
)

func TestOpenAddsBundleSchemaWithoutChangingExistingData(t *testing.T) {
	path := filepath.Join(t.TempDir(), "existing.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	if _, err := db.Exec(`
		CREATE TABLE items (id INTEGER PRIMARY KEY, name TEXT NOT NULL, qty INTEGER NOT NULL, category TEXT NOT NULL, status TEXT NOT NULL, notes TEXT NOT NULL, budget_cents INTEGER, created_at TEXT NOT NULL);
		CREATE TABLE options (id INTEGER PRIMARY KEY, item_id INTEGER NOT NULL, url TEXT NOT NULL, label TEXT NOT NULL, price_cents INTEGER, chosen INTEGER NOT NULL, created_at TEXT NOT NULL);
		CREATE TABLE comments (id INTEGER PRIMARY KEY, option_id INTEGER NOT NULL, body TEXT NOT NULL, created_at TEXT NOT NULL);
		INSERT INTO items VALUES (1, 'Cot', 1, 'Nursery', 'needed', 'keep', NULL, '2026-01-01T00:00:00Z');
		INSERT INTO options VALUES (1, 1, 'https://example.com/cot', '', NULL, 0, '2026-01-01T00:00:00Z');
		INSERT INTO comments VALUES (1, 1, 'consider it', '2026-01-01T00:00:00Z');`); err != nil {
		db.Close()
		t.Fatalf("create existing data: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close existing db: %v", err)
	}

	s, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer s.Close()

	for _, table := range []string{"bundles", "bundle_members", "bundle_comments"} {
		var name string
		if err := s.db.QueryRow(`SELECT name FROM sqlite_master WHERE type = 'table' AND name = ?`, table).Scan(&name); err != nil {
			t.Errorf("%s table: %v", table, err)
		}
	}
	item, err := s.Get(context.Background(), 1)
	if err != nil || item.Name != "Cot" || item.Notes != "keep" {
		t.Fatalf("item after schema update = %+v, %v", item, err)
	}
	option, err := s.GetOption(context.Background(), 1)
	if err != nil || option.URL != "https://example.com/cot" {
		t.Fatalf("option after schema update = %+v, %v", option, err)
	}
	comments, err := s.ListComments(context.Background(), 1)
	if err != nil || len(comments) != 1 || comments[0].Body != "consider it" {
		t.Fatalf("comments after schema update = %+v, %v", comments, err)
	}
}

func TestBundleSchemaRelationships(t *testing.T) {
	s := newStore(t)

	for _, name := range []string{"bundle_members_by_bundle", "bundle_members_by_item", "bundle_comments_by_bundle"} {
		var got string
		if err := s.db.QueryRow(`SELECT name FROM sqlite_master WHERE type = 'index' AND name = ?`, name).Scan(&got); err != nil {
			t.Errorf("%s index: %v", name, err)
		}
	}

	item := mustAdd(t, s, ItemInput{Name: "Cot", Category: "Nursery"})
	option := mustAddOption(t, s, item.ID, OptionInput{URL: "https://example.com/cot"})
	if _, err := s.db.Exec(`INSERT INTO bundles (name, url, price_cents, created_at) VALUES ('Nursery', 'https://example.com/nursery', 10000, '2026-01-01T00:00:00Z')`); err != nil {
		t.Fatalf("insert bundle: %v", err)
	}
	if _, err := s.db.Exec(`INSERT INTO bundle_members (bundle_id, item_id, option_id, position) VALUES (1, ?, ?, 0)`, item.ID, option.ID); err != nil {
		t.Fatalf("insert bundle member: %v", err)
	}
	if _, err := s.db.Exec(`INSERT INTO bundle_comments (bundle_id, body, created_at) VALUES (1, 'worth it', '2026-01-01T00:00:00Z')`); err != nil {
		t.Fatalf("insert bundle comment: %v", err)
	}
	if _, err := s.db.Exec(`DELETE FROM bundles WHERE id = 1`); err != nil {
		t.Fatalf("delete bundle: %v", err)
	}
	for _, table := range []string{"bundle_members", "bundle_comments"} {
		var count int
		if err := s.db.QueryRow(`SELECT COUNT(*) FROM ` + table).Scan(&count); err != nil || count != 0 {
			t.Errorf("%s after bundle delete = %d, %v; want 0", table, count, err)
		}
	}
}

func TestAddBundleAllocatesOrderedShares(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()
	first := mustAdd(t, s, ItemInput{Name: "Cot", Qty: 3, Category: "Nursery"})
	second := mustAdd(t, s, ItemInput{Name: "Pram", Category: "Travel"})
	third := mustAdd(t, s, ItemInput{Name: "Monitor", Category: "Nursery"})

	bundle, err := s.AddBundle(ctx, BundleInput{
		Name: " Nursery set ", URL: "https://shop.example.com/set", Price: "100.00", RegularPrice: "120",
		Members: []BundleMemberInput{
			{ItemID: second.ID, ComponentLabel: " Pram frame "},
			{ItemID: first.ID},
			{ItemID: third.ID, ComponentLabel: " Monitor "},
		},
	})
	if err != nil {
		t.Fatalf("AddBundle: %v", err)
	}
	if bundle.Name != "Nursery set" || bundle.URL != "https://shop.example.com/set" || bundle.PriceCents != 10000 {
		t.Errorf("bundle = %+v", bundle)
	}
	if bundle.RegularPriceCents == nil || *bundle.RegularPriceCents != 12000 {
		t.Errorf("RegularPriceCents = %v, want 12000", bundle.RegularPriceCents)
	}
	if len(bundle.Members) != 3 {
		t.Fatalf("members = %d, want 3", len(bundle.Members))
	}
	wantItems := []int64{second.ID, first.ID, third.ID}
	wantPrices := []int64{3334, 3333, 3333}
	for i, member := range bundle.Members {
		if member.Position != i || member.ItemID != wantItems[i] {
			t.Errorf("member %d = %+v", i, member)
		}
		option, err := s.GetOption(ctx, member.OptionID)
		if err != nil {
			t.Fatalf("GetOption(%d): %v", member.OptionID, err)
		}
		if option.URL != bundle.URL || option.Chosen || option.PriceCents == nil || *option.PriceCents != wantPrices[i] {
			t.Errorf("option %d = %+v", i, option)
		}
	}
	if got, err := s.Get(ctx, first.ID); err != nil || got.Status != StatusNeeded {
		t.Errorf("first item after bundle = %+v, %v", got, err)
	}

	all, err := s.ListBundles(ctx)
	if err != nil || len(all) != 1 || all[0].ID != bundle.ID || len(all[0].Members) != 3 {
		t.Errorf("ListBundles = %+v, %v", all, err)
	}
}

func TestAddBundleRejectsInvalidInputWithoutWriting(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()
	first := mustAdd(t, s, ItemInput{Name: "Cot", Category: "Nursery"})
	second := mustAdd(t, s, ItemInput{Name: "Pram", Category: "Travel"})
	valid := BundleInput{Name: "Set", URL: "https://shop.example.com/set", Price: "10", Members: []BundleMemberInput{{ItemID: first.ID}, {ItemID: second.ID}}}

	tests := []struct {
		name string
		in   BundleInput
		want error
	}{
		{"blank name", BundleInput{URL: valid.URL, Price: valid.Price, Members: valid.Members}, ErrInvalidBundleName},
		{"relative URL", BundleInput{Name: valid.Name, URL: "shop.example.com/set", Price: valid.Price, Members: valid.Members}, ErrInvalidURL},
		{"three decimals", BundleInput{Name: valid.Name, URL: valid.URL, Price: "10.001", Members: valid.Members}, ErrInvalidBundlePrice},
		{"zero price", BundleInput{Name: valid.Name, URL: valid.URL, Price: "0", Members: valid.Members}, ErrInvalidBundlePrice},
		{"regular price below", BundleInput{Name: valid.Name, URL: valid.URL, Price: valid.Price, RegularPrice: "9", Members: valid.Members}, ErrInvalidRegularPrice},
		{"one member", BundleInput{Name: valid.Name, URL: valid.URL, Price: valid.Price, Members: valid.Members[:1]}, ErrInvalidBundleMembership},
		{"duplicate member", BundleInput{Name: valid.Name, URL: valid.URL, Price: valid.Price, Members: []BundleMemberInput{{ItemID: first.ID}, {ItemID: first.ID}}}, ErrInvalidBundleMembership},
		{"missing member", BundleInput{Name: valid.Name, URL: valid.URL, Price: valid.Price, Members: []BundleMemberInput{{ItemID: first.ID}, {ItemID: 9999}}}, ErrNotFound},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := s.AddBundle(ctx, tt.in); !errors.Is(err, tt.want) {
				t.Fatalf("AddBundle error = %v, want %v", err, tt.want)
			}
			var bundles, options int
			if err := s.db.QueryRow(`SELECT COUNT(*) FROM bundles`).Scan(&bundles); err != nil || bundles != 0 {
				t.Fatalf("bundles after failure = %d, %v", bundles, err)
			}
			if err := s.db.QueryRow(`SELECT COUNT(*) FROM options`).Scan(&options); err != nil || options != 0 {
				t.Fatalf("options after failure = %d, %v", options, err)
			}
		})
	}
}
