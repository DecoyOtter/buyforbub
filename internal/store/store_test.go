package store

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"
)

func cents(v int64) *int64 { return &v }

func newStore(t *testing.T) *Store {
	t.Helper()
	s, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

// mustAdd adds an item, failing the test if it cannot.
func mustAdd(t *testing.T, s *Store, in ItemInput) Item {
	t.Helper()
	it, err := s.Add(context.Background(), in)
	if err != nil {
		t.Fatalf("Add(%+v): %v", in, err)
	}
	return it
}

func TestOpenIsIdempotent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "dir", "test.db")

	s, err := Open(path)
	if err != nil {
		t.Fatalf("first Open: %v", err)
	}
	mustAdd(t, s, ItemInput{Name: "Cot", Category: "Nursery"})
	if err := s.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	// Reopening must apply the schema without clobbering existing rows.
	s2, err := Open(path)
	if err != nil {
		t.Fatalf("second Open: %v", err)
	}
	defer s2.Close()

	items, err := s2.List(context.Background())
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("got %d items after reopen, want 1", len(items))
	}
}

func TestOpenMigratesBudgetColumn(t *testing.T) {
	path := filepath.Join(t.TempDir(), "legacy.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	if _, err := db.Exec(`CREATE TABLE items (
        id INTEGER PRIMARY KEY AUTOINCREMENT, name TEXT NOT NULL, qty INTEGER NOT NULL DEFAULT 1,
        category TEXT NOT NULL, status TEXT NOT NULL DEFAULT 'needed', notes TEXT NOT NULL DEFAULT '',
        created_at TEXT NOT NULL)`); err != nil {
		t.Fatalf("create legacy items: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO items (name, qty, category, status, notes, created_at) VALUES ('Cot', 1, 'Nursery', 'needed', '', '2026-01-01T00:00:00Z')`); err != nil {
		t.Fatalf("insert legacy item: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close legacy db: %v", err)
	}

	s, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer s.Close()
	items, err := s.List(context.Background())
	if err != nil || len(items) != 1 || items[0].Name != "Cot" || items[0].BudgetCents != nil {
		t.Fatalf("legacy item after migration = %+v, %v", items, err)
	}
}

func TestAddValidation(t *testing.T) {
	tests := []struct {
		name    string
		in      ItemInput
		wantErr error
		want    ItemInput // expected stored values, when wantErr is nil
	}{
		{
			name: "trims and defaults qty",
			in:   ItemInput{Name: "  Cot  ", Category: "Nursery", Notes: "  white  "},
			want: ItemInput{Name: "Cot", Qty: 1, Category: "Nursery", Notes: "white"},
		},
		{
			name: "keeps explicit qty",
			in:   ItemInput{Name: "Bottles", Qty: 6, Category: "Feeding"},
			want: ItemInput{Name: "Bottles", Qty: 6, Category: "Feeding"},
		},
		{
			name: "negative qty falls back to one",
			in:   ItemInput{Name: "Bibs", Qty: -3, Category: "Feeding"},
			want: ItemInput{Name: "Bibs", Qty: 1, Category: "Feeding"},
		},
		{
			name:    "empty name rejected",
			in:      ItemInput{Name: "   ", Category: "Nursery"},
			wantErr: ErrInvalidName,
		},
		{
			name:    "unknown category rejected",
			in:      ItemInput{Name: "Cot", Category: "Furniture"},
			wantErr: ErrInvalidCategory,
		},
		{
			name:    "empty category rejected",
			in:      ItemInput{Name: "Cot"},
			wantErr: ErrInvalidCategory,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := newStore(t)
			got, err := s.Add(context.Background(), tt.in)

			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("Add error = %v, want %v", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("Add: %v", err)
			}

			if got.Name != tt.want.Name || got.Qty != tt.want.Qty ||
				got.Category != tt.want.Category || got.Notes != tt.want.Notes {
				t.Errorf("Add returned %+v, want name=%q qty=%d category=%q notes=%q",
					got, tt.want.Name, tt.want.Qty, tt.want.Category, tt.want.Notes)
			}
			if got.Status != StatusNeeded {
				t.Errorf("new item status = %q, want %q", got.Status, StatusNeeded)
			}
			if got.CreatedAt.IsZero() {
				t.Error("new item has zero CreatedAt")
			}

			// What was returned must match what was stored.
			stored, err := s.Get(context.Background(), got.ID)
			if err != nil {
				t.Fatalf("Get: %v", err)
			}
			if stored != got {
				t.Errorf("Get returned %+v, want %+v", stored, got)
			}
		})
	}
}

func TestBudgetPersistenceAndValidation(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()

	it := mustAdd(t, s, ItemInput{Name: "Pram", Qty: 2, Category: "Travel", Budget: "$1,199.50"})
	if it.BudgetCents == nil || *it.BudgetCents != 119950 || it.BudgetText() != "$1,199.50" {
		t.Errorf("budget = %v (%q), want 119950 ($1,199.50)", it.BudgetCents, it.BudgetText())
	}
	updated, err := s.Update(ctx, it.ID, ItemInput{Name: "Pram", Qty: 5, Category: "Travel", Budget: ""})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if updated.BudgetCents != nil {
		t.Errorf("cleared budget = %v, want nil", updated.BudgetCents)
	}
	for _, budget := range []string{"about $1k", "-1"} {
		if _, err := s.Add(ctx, ItemInput{Name: "Bad", Category: "Other", Budget: budget}); !errors.Is(err, ErrInvalidBudget) {
			t.Errorf("Add budget %q error = %v, want %v", budget, err, ErrInvalidBudget)
		}
	}
}

func TestListIsInsertionOrdered(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()

	names := []string{"Cot", "Pram", "Bottles", "Nappies"}
	for _, n := range names {
		mustAdd(t, s, ItemInput{Name: n, Category: "Other"})
	}

	// Deleting and re-adding must not reuse an id and jump the queue.
	items, err := s.List(ctx)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if err := s.Delete(ctx, items[1].ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	mustAdd(t, s, ItemInput{Name: "Car seat", Category: "Other"})

	items, err = s.List(ctx)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	got := make([]string, len(items))
	for i, it := range items {
		got[i] = it.Name
	}
	want := []string{"Cot", "Bottles", "Nappies", "Car seat"}
	if len(got) != len(want) {
		t.Fatalf("List returned %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("List returned %v, want %v", got, want)
		}
	}
}

func TestToggleAndSetStatus(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()
	it := mustAdd(t, s, ItemInput{Name: "Cot", Category: "Nursery"})

	toggled, err := s.Toggle(ctx, it.ID)
	if err != nil {
		t.Fatalf("Toggle: %v", err)
	}
	if toggled.Status != StatusBought {
		t.Errorf("after first toggle status = %q, want %q", toggled.Status, StatusBought)
	}

	back, err := s.Toggle(ctx, it.ID)
	if err != nil {
		t.Fatalf("Toggle: %v", err)
	}
	if back.Status != StatusNeeded {
		t.Errorf("after second toggle status = %q, want %q", back.Status, StatusNeeded)
	}

	if _, err := s.SetStatus(ctx, it.ID, "maybe"); !errors.Is(err, ErrInvalidStatus) {
		t.Errorf("SetStatus with bad status = %v, want %v", err, ErrInvalidStatus)
	}
	if _, err := s.SetStatus(ctx, 9999, StatusBought); !errors.Is(err, ErrNotFound) {
		t.Errorf("SetStatus on missing id = %v, want %v", err, ErrNotFound)
	}
	if _, err := s.Toggle(ctx, 9999); !errors.Is(err, ErrNotFound) {
		t.Errorf("Toggle on missing id = %v, want %v", err, ErrNotFound)
	}
}

func TestUpdatePreservesStatusAndCreatedAt(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()

	it := mustAdd(t, s, ItemInput{Name: "Cot", Qty: 1, Category: "Nursery", Notes: "any"})
	if _, err := s.Toggle(ctx, it.ID); err != nil {
		t.Fatalf("Toggle: %v", err)
	}

	updated, err := s.Update(ctx, it.ID, ItemInput{
		Name: "Cot mattress", Qty: 2, Category: "Feeding", Notes: "firm",
	})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}

	if updated.Name != "Cot mattress" || updated.Qty != 2 ||
		updated.Category != "Feeding" || updated.Notes != "firm" {
		t.Errorf("Update returned %+v, fields not applied", updated)
	}
	if updated.Status != StatusBought {
		t.Errorf("Update changed status to %q, want it left at %q", updated.Status, StatusBought)
	}
	if !updated.CreatedAt.Equal(it.CreatedAt) {
		t.Errorf("Update changed CreatedAt from %v to %v", it.CreatedAt, updated.CreatedAt)
	}

	if _, err := s.Update(ctx, it.ID, ItemInput{Name: "", Category: "Nursery"}); !errors.Is(err, ErrInvalidName) {
		t.Errorf("Update with empty name = %v, want %v", err, ErrInvalidName)
	}
	if _, err := s.Update(ctx, 9999, ItemInput{Name: "Cot", Category: "Nursery"}); !errors.Is(err, ErrNotFound) {
		t.Errorf("Update on missing id = %v, want %v", err, ErrNotFound)
	}
}

func TestDelete(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()
	it := mustAdd(t, s, ItemInput{Name: "Cot", Category: "Nursery"})

	if err := s.Delete(ctx, it.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := s.Get(ctx, it.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("Get after delete = %v, want %v", err, ErrNotFound)
	}
	if err := s.Delete(ctx, it.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("second Delete = %v, want %v", err, ErrNotFound)
	}
}

func TestSeedIfEmpty(t *testing.T) {
	ctx := context.Background()
	seed := []ItemInput{
		{Name: "Cot", Category: "Nursery"},
		{Name: "Bottles", Qty: 6, Category: "Feeding"},
	}

	t.Run("seeds an empty database", func(t *testing.T) {
		s := newStore(t)
		n, err := s.SeedIfEmpty(ctx, seed)
		if err != nil {
			t.Fatalf("SeedIfEmpty: %v", err)
		}
		if n != len(seed) {
			t.Errorf("seeded %d items, want %d", n, len(seed))
		}
		items, err := s.List(ctx)
		if err != nil {
			t.Fatalf("List: %v", err)
		}
		if len(items) != len(seed) {
			t.Fatalf("got %d items, want %d", len(items), len(seed))
		}
		if items[0].Name != "Cot" || items[1].Qty != 6 {
			t.Errorf("seeded items not stored as given: %+v", items)
		}
	})

	t.Run("skips a non-empty database", func(t *testing.T) {
		s := newStore(t)
		mustAdd(t, s, ItemInput{Name: "Pram", Category: "Travel"})

		n, err := s.SeedIfEmpty(ctx, seed)
		if err != nil {
			t.Fatalf("SeedIfEmpty: %v", err)
		}
		if n != 0 {
			t.Errorf("seeded %d items into a non-empty database, want 0", n)
		}
		items, _ := s.List(ctx)
		if len(items) != 1 {
			t.Errorf("got %d items, want the 1 pre-existing", len(items))
		}
	})

	t.Run("rejects invalid seed data without writing", func(t *testing.T) {
		s := newStore(t)
		_, err := s.SeedIfEmpty(ctx, []ItemInput{
			{Name: "Cot", Category: "Nursery"},
			{Name: "Mystery", Category: "Nope"},
		})
		if !errors.Is(err, ErrInvalidCategory) {
			t.Fatalf("SeedIfEmpty = %v, want %v", err, ErrInvalidCategory)
		}
		items, _ := s.List(ctx)
		if len(items) != 0 {
			t.Errorf("got %d items after failed seed, want 0", len(items))
		}
	})
}
