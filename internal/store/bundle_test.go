package store

import (
	"context"
	"database/sql"
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
