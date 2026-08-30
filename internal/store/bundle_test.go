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

func TestUpdateBundlePropagatesMembersAndUnchooses(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()
	first := mustAdd(t, s, ItemInput{Name: "Cot", Category: "Nursery"})
	second := mustAdd(t, s, ItemInput{Name: "Pram", Category: "Travel"})
	third := mustAdd(t, s, ItemInput{Name: "Monitor", Category: "Nursery"})
	fourth := mustAdd(t, s, ItemInput{Name: "Carrier", Category: "Travel"})
	bundle, err := s.AddBundle(ctx, BundleInput{Name: "Set", URL: "https://shop.example.com/set", Price: "100", Members: []BundleMemberInput{{ItemID: second.ID, ComponentLabel: "Pram"}, {ItemID: first.ID}, {ItemID: third.ID}}})
	if err != nil {
		t.Fatalf("AddBundle: %v", err)
	}
	removedOption := bundle.Members[1].OptionID
	if _, err := s.AddComment(ctx, removedOption, "removed comment"); err != nil {
		t.Fatalf("AddComment: %v", err)
	}
	if _, err := s.AddBundleComment(ctx, bundle.ID, "keep shared comment"); err != nil {
		t.Fatalf("AddBundleComment: %v", err)
	}
	if _, err := s.db.Exec(`UPDATE options SET chosen = 1 WHERE id IN (SELECT option_id FROM bundle_members WHERE bundle_id = ?); UPDATE items SET status = ? WHERE id IN (SELECT item_id FROM bundle_members WHERE bundle_id = ?)`, bundle.ID, StatusBought, bundle.ID); err != nil {
		t.Fatalf("choose bundle fixture: %v", err)
	}

	updated, err := s.UpdateBundle(ctx, bundle.ID, BundleInput{Name: " Updated set ", URL: "https://shop.example.com/new", Price: "100.01", RegularPrice: "120", Members: []BundleMemberInput{{ItemID: third.ID, ComponentLabel: "Screen"}, {ItemID: fourth.ID, ComponentLabel: "Carrier"}, {ItemID: second.ID, ComponentLabel: "Frame"}}})
	if err != nil {
		t.Fatalf("UpdateBundle: %v", err)
	}
	if updated.Name != "Updated set" || updated.URL != "https://shop.example.com/new" || updated.RegularPriceCents == nil || *updated.RegularPriceCents != 12000 {
		t.Errorf("updated bundle = %+v", updated)
	}
	if len(updated.Members) != 3 {
		t.Fatalf("members = %d, want 3", len(updated.Members))
	}
	wantItems := []int64{second.ID, third.ID, fourth.ID}
	wantPositions := []int{0, 2, 3}
	wantPrices := []int64{3334, 3334, 3333}
	for i, member := range updated.Members {
		if member.ItemID != wantItems[i] || member.Position != wantPositions[i] {
			t.Errorf("member %d = %+v", i, member)
		}
		option, err := s.GetOption(ctx, member.OptionID)
		if err != nil || option.URL != updated.URL || option.Label != member.ComponentLabel || option.PriceCents == nil || *option.PriceCents != wantPrices[i] || option.Chosen {
			t.Errorf("option %d = %+v, %v", i, option, err)
		}
	}
	if _, err := s.GetOption(ctx, removedOption); !errors.Is(err, ErrNotFound) {
		t.Errorf("removed option = %v, want %v", err, ErrNotFound)
	}
	for _, itemID := range []int64{first.ID, second.ID, third.ID} {
		item, err := s.Get(ctx, itemID)
		if err != nil || item.Status != StatusNeeded {
			t.Errorf("item %d after edit = %+v, %v", itemID, item, err)
		}
	}
	if comments, err := s.ListBundleComments(ctx, bundle.ID); err != nil {
		t.Errorf("bundle comments after edit: %v", err)
	} else if len(comments) != 1 || comments[0].Body != "keep shared comment" {
		t.Errorf("bundle comments = %+v", comments)
	}
}

func TestUpdateBundleFailureLeavesDataUnchanged(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()
	first := mustAdd(t, s, ItemInput{Name: "Cot", Category: "Nursery"})
	second := mustAdd(t, s, ItemInput{Name: "Pram", Category: "Travel"})
	bundle, err := s.AddBundle(ctx, BundleInput{Name: "Set", URL: "https://shop.example.com/set", Price: "10", Members: []BundleMemberInput{{ItemID: first.ID}, {ItemID: second.ID}}})
	if err != nil {
		t.Fatalf("AddBundle: %v", err)
	}
	if _, err := s.UpdateBundle(ctx, bundle.ID, BundleInput{Name: "Broken", URL: "https://shop.example.com/new", Price: "20", Members: []BundleMemberInput{{ItemID: first.ID}, {ItemID: 999}}}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("UpdateBundle error = %v, want %v", err, ErrNotFound)
	}
	got, err := s.GetBundle(ctx, bundle.ID)
	if err != nil || got.Name != bundle.Name || got.URL != bundle.URL || got.PriceCents != bundle.PriceCents || len(got.Members) != 2 {
		t.Errorf("bundle after failed update = %+v, %v", got, err)
	}
}

func TestDeleteBundleRemovesGeneratedDataAndUnchoosesMembers(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()
	first := mustAdd(t, s, ItemInput{Name: "Cot", Category: "Nursery"})
	second := mustAdd(t, s, ItemInput{Name: "Pram", Category: "Travel"})
	normal := mustAddOption(t, s, first.ID, OptionInput{URL: "https://shop.example.com/normal"})
	if _, err := s.AddComment(ctx, normal.ID, "keep"); err != nil {
		t.Fatalf("AddComment: %v", err)
	}
	bundle, err := s.AddBundle(ctx, BundleInput{Name: "Set", URL: "https://shop.example.com/set", Price: "10", Members: []BundleMemberInput{{ItemID: first.ID}, {ItemID: second.ID}}})
	if err != nil {
		t.Fatalf("AddBundle: %v", err)
	}
	if _, err := s.AddComment(ctx, bundle.Members[0].OptionID, "remove"); err != nil {
		t.Fatalf("AddComment: %v", err)
	}
	if _, err := s.AddBundleComment(ctx, bundle.ID, "remove shared"); err != nil {
		t.Fatalf("AddBundleComment: %v", err)
	}
	if _, err := s.db.Exec(`UPDATE options SET chosen = 1 WHERE id IN (SELECT option_id FROM bundle_members WHERE bundle_id = ?); UPDATE items SET status = ? WHERE id IN (SELECT item_id FROM bundle_members WHERE bundle_id = ?)`, bundle.ID, StatusBought, bundle.ID); err != nil {
		t.Fatalf("choose fixture: %v", err)
	}

	if err := s.DeleteBundle(ctx, bundle.ID); err != nil {
		t.Fatalf("DeleteBundle: %v", err)
	}
	if _, err := s.GetBundle(ctx, bundle.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("GetBundle after delete = %v, want %v", err, ErrNotFound)
	}
	for _, member := range bundle.Members {
		if _, err := s.GetOption(ctx, member.OptionID); !errors.Is(err, ErrNotFound) {
			t.Errorf("generated option %d after delete = %v, want %v", member.OptionID, err, ErrNotFound)
		}
	}
	for _, itemID := range []int64{first.ID, second.ID} {
		item, err := s.Get(ctx, itemID)
		if err != nil || item.Status != StatusNeeded {
			t.Errorf("item %d after delete = %+v, %v", itemID, item, err)
		}
	}
	if comments, err := s.ListComments(ctx, normal.ID); err != nil || len(comments) != 1 || comments[0].Body != "keep" {
		t.Errorf("normal comments = %+v, %v", comments, err)
	}
	for _, table := range []string{"bundle_members", "bundle_comments"} {
		var count int
		if err := s.db.QueryRow(`SELECT COUNT(*) FROM ` + table).Scan(&count); err != nil || count != 0 {
			t.Errorf("%s after delete = %d, %v; want 0", table, count, err)
		}
	}
}

func TestDeleteBundleMemberUnchoosesAndReallocates(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()
	first := mustAdd(t, s, ItemInput{Name: "Cot", Category: "Nursery"})
	second := mustAdd(t, s, ItemInput{Name: "Pram", Category: "Travel"})
	third := mustAdd(t, s, ItemInput{Name: "Monitor", Category: "Nursery"})
	fourth := mustAdd(t, s, ItemInput{Name: "Carrier", Category: "Travel"})
	kept := mustAddOption(t, s, fourth.ID, OptionInput{URL: "https://shop.example.com/keep"})
	if _, err := s.AddComment(ctx, kept.ID, "keep"); err != nil {
		t.Fatalf("AddComment: %v", err)
	}
	bundle, err := s.AddBundle(ctx, BundleInput{Name: "Large", URL: "https://shop.example.com/large", Price: "0.05", Members: []BundleMemberInput{{ItemID: first.ID}, {ItemID: second.ID}, {ItemID: third.ID}}})
	if err != nil {
		t.Fatalf("AddBundle: %v", err)
	}
	removed, err := s.AddBundle(ctx, BundleInput{Name: "Small", URL: "https://shop.example.com/small", Price: "8", Members: []BundleMemberInput{{ItemID: first.ID}, {ItemID: fourth.ID}}})
	if err != nil {
		t.Fatalf("AddBundle small: %v", err)
	}
	if _, err := s.AddBundleComment(ctx, bundle.ID, "keep shared"); err != nil {
		t.Fatalf("AddBundleComment: %v", err)
	}
	if _, err := s.AddBundleComment(ctx, removed.ID, "remove shared"); err != nil {
		t.Fatalf("AddBundleComment: %v", err)
	}
	if _, err := s.db.Exec(`UPDATE options SET chosen = 1 WHERE id IN (SELECT option_id FROM bundle_members WHERE bundle_id = ?); UPDATE items SET status = ? WHERE id IN (SELECT item_id FROM bundle_members WHERE bundle_id = ?)`, bundle.ID, StatusBought, bundle.ID); err != nil {
		t.Fatalf("choose fixture: %v", err)
	}

	if err := s.Delete(ctx, first.ID); err != nil {
		t.Fatalf("Delete item: %v", err)
	}
	updated, err := s.GetBundle(ctx, bundle.ID)
	if err != nil {
		t.Fatalf("GetBundle: %v", err)
	}
	if len(updated.Members) != 2 || updated.Members[0].ItemID != second.ID || updated.Members[1].ItemID != third.ID {
		t.Fatalf("remaining members = %+v", updated.Members)
	}
	for i, member := range updated.Members {
		option, err := s.GetOption(ctx, member.OptionID)
		if err != nil || option.Chosen || option.PriceCents == nil || *option.PriceCents != []int64{3, 2}[i] {
			t.Errorf("remaining option %d = %+v, %v", i, option, err)
		}
		item, err := s.Get(ctx, member.ItemID)
		if err != nil || item.Status != StatusNeeded {
			t.Errorf("remaining item %d = %+v, %v", member.ItemID, item, err)
		}
	}
	if comments, err := s.ListBundleComments(ctx, bundle.ID); err != nil || len(comments) != 1 || comments[0].Body != "keep shared" {
		t.Errorf("surviving bundle comments = %+v, %v", comments, err)
	}
	if _, err := s.GetBundle(ctx, removed.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("small bundle after item delete = %v, want %v", err, ErrNotFound)
	}
	if comments, err := s.ListComments(ctx, kept.ID); err != nil || len(comments) != 1 || comments[0].Body != "keep" {
		t.Errorf("unrelated option comments = %+v, %v", comments, err)
	}
}

func TestChooseBundleDisplacesConflictsAndNormalChoices(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()
	a := mustAdd(t, s, ItemInput{Name: "Cot", Category: "Nursery"})
	b := mustAdd(t, s, ItemInput{Name: "Pram", Category: "Travel"})
	c := mustAdd(t, s, ItemInput{Name: "Monitor", Category: "Nursery"})
	d := mustAdd(t, s, ItemInput{Name: "Carrier", Category: "Travel"})
	normalA := mustAddOption(t, s, a.ID, OptionInput{URL: "https://shop.example.com/normal-a"})
	normalB := mustAddOption(t, s, b.ID, OptionInput{URL: "https://shop.example.com/normal-b"})

	target, err := s.AddBundle(ctx, BundleInput{Name: "Target", URL: "https://shop.example.com/target", Price: "20", Members: []BundleMemberInput{{ItemID: a.ID}, {ItemID: b.ID}}})
	if err != nil {
		t.Fatalf("AddBundle target: %v", err)
	}
	first, err := s.AddBundle(ctx, BundleInput{Name: "First", URL: "https://shop.example.com/first", Price: "20", Members: []BundleMemberInput{{ItemID: a.ID}, {ItemID: c.ID}}})
	if err != nil {
		t.Fatalf("AddBundle first: %v", err)
	}
	second, err := s.AddBundle(ctx, BundleInput{Name: "Second", URL: "https://shop.example.com/second", Price: "20", Members: []BundleMemberInput{{ItemID: b.ID}, {ItemID: d.ID}}})
	if err != nil {
		t.Fatalf("AddBundle second: %v", err)
	}
	if _, err := s.db.Exec(`
		UPDATE options SET chosen = 1 WHERE id IN (SELECT option_id FROM bundle_members WHERE bundle_id IN (?, ?));
		UPDATE options SET chosen = 1 WHERE id IN (?, ?);
		UPDATE items SET status = ? WHERE id IN (?, ?, ?, ?);`, first.ID, second.ID, normalA.ID, normalB.ID, StatusBought, a.ID, b.ID, c.ID, d.ID); err != nil {
		t.Fatalf("choose fixture: %v", err)
	}

	chosen, err := s.ChooseBundle(ctx, target.ID)
	if err != nil {
		t.Fatalf("ChooseBundle: %v", err)
	}
	if chosen.ID != target.ID {
		t.Errorf("chosen bundle = %d, want %d", chosen.ID, target.ID)
	}
	for _, member := range target.Members {
		assertChosen(t, s, member.ItemID, member.OptionID)
		item, err := s.Get(ctx, member.ItemID)
		if err != nil || item.Status != StatusBought {
			t.Errorf("target item %d = %+v, %v", member.ItemID, item, err)
		}
	}
	for _, bundle := range []Bundle{first, second} {
		for _, member := range bundle.Members {
			option, err := s.GetOption(ctx, member.OptionID)
			if err != nil || option.Chosen {
				t.Errorf("displaced option %d = %+v, %v", member.OptionID, option, err)
			}
			if member.ItemID == a.ID || member.ItemID == b.ID {
				continue
			}
			item, err := s.Get(ctx, member.ItemID)
			if err != nil || item.Status != StatusNeeded {
				t.Errorf("displaced item %d = %+v, %v", member.ItemID, item, err)
			}
		}
	}
}

func TestChooseMissingBundleLeavesChoicesUnchanged(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()
	first := mustAdd(t, s, ItemInput{Name: "Cot", Category: "Nursery"})
	second := mustAdd(t, s, ItemInput{Name: "Pram", Category: "Travel"})
	bundle, err := s.AddBundle(ctx, BundleInput{Name: "Set", URL: "https://shop.example.com/set", Price: "20", Members: []BundleMemberInput{{ItemID: first.ID}, {ItemID: second.ID}}})
	if err != nil {
		t.Fatalf("AddBundle: %v", err)
	}
	if _, err := s.ChooseBundle(ctx, bundle.ID); err != nil {
		t.Fatalf("ChooseBundle: %v", err)
	}
	if _, err := s.ChooseBundle(ctx, 9999); !errors.Is(err, ErrNotFound) {
		t.Fatalf("ChooseBundle missing = %v, want %v", err, ErrNotFound)
	}
	for _, member := range bundle.Members {
		assertChosen(t, s, member.ItemID, member.OptionID)
		item, err := s.Get(ctx, member.ItemID)
		if err != nil || item.Status != StatusBought {
			t.Errorf("item %d after failure = %+v, %v", member.ItemID, item, err)
		}
	}
}

func TestNormalActionsRespectChosenBundle(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()
	first := mustAdd(t, s, ItemInput{Name: "Cot", Category: "Nursery"})
	second := mustAdd(t, s, ItemInput{Name: "Pram", Category: "Travel"})
	normal := mustAddOption(t, s, first.ID, OptionInput{URL: "https://shop.example.com/normal"})
	bundle, err := s.AddBundle(ctx, BundleInput{Name: "Set", URL: "https://shop.example.com/set", Price: "20", Members: []BundleMemberInput{{ItemID: first.ID}, {ItemID: second.ID}}})
	if err != nil {
		t.Fatalf("AddBundle: %v", err)
	}

	if _, err := s.ChooseOption(ctx, bundle.Members[0].OptionID); err != nil {
		t.Fatalf("ChooseOption bundle member: %v", err)
	}
	for _, member := range bundle.Members {
		assertChosen(t, s, member.ItemID, member.OptionID)
	}

	if _, err := s.ChooseOption(ctx, normal.ID); err != nil {
		t.Fatalf("ChooseOption normal: %v", err)
	}
	assertChosen(t, s, first.ID, normal.ID)
	assertChosen(t, s, second.ID, 0)
	if got, _ := s.Get(ctx, second.ID); got.Status != StatusNeeded {
		t.Errorf("other bundle member = %q, want %q", got.Status, StatusNeeded)
	}

	if _, err := s.ChooseBundle(ctx, bundle.ID); err != nil {
		t.Fatalf("ChooseBundle: %v", err)
	}
	if _, err := s.Toggle(ctx, second.ID); err != nil {
		t.Fatalf("Toggle bundle member: %v", err)
	}
	for _, member := range bundle.Members {
		assertChosen(t, s, member.ItemID, 0)
		if got, _ := s.Get(ctx, member.ItemID); got.Status != StatusNeeded {
			t.Errorf("bundle member %d = %q, want %q", member.ItemID, got.Status, StatusNeeded)
		}
	}

	if _, err := s.SetStatus(ctx, first.ID, StatusBought); err != nil {
		t.Fatalf("SetStatus bought: %v", err)
	}
	assertChosen(t, s, first.ID, 0)
	assertChosen(t, s, second.ID, 0)
}

func TestBundleCommentCRUD(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()
	first := mustAdd(t, s, ItemInput{Name: "Cot", Category: "Nursery"})
	second := mustAdd(t, s, ItemInput{Name: "Pram", Category: "Travel"})
	bundle, err := s.AddBundle(ctx, BundleInput{
		Name: "Set", URL: "https://shop.example.com/set", Price: "10",
		Members: []BundleMemberInput{{ItemID: first.ID}, {ItemID: second.ID}},
	})
	if err != nil {
		t.Fatalf("AddBundle: %v", err)
	}

	firstComment, err := s.AddBundleComment(ctx, bundle.ID, "  includes the adapter  ")
	if err != nil {
		t.Fatalf("AddBundleComment: %v", err)
	}
	if firstComment.Body != "includes the adapter" || firstComment.BundleID != bundle.ID {
		t.Errorf("comment = %+v", firstComment)
	}
	if _, err := s.AddBundleComment(ctx, bundle.ID, ""); !errors.Is(err, ErrInvalidComment) {
		t.Errorf("blank AddBundleComment error = %v, want %v", err, ErrInvalidComment)
	}
	secondComment, err := s.AddBundleComment(ctx, bundle.ID, "good saving")
	if err != nil {
		t.Fatalf("AddBundleComment: %v", err)
	}

	comments, err := s.ListBundleComments(ctx, bundle.ID)
	if err != nil {
		t.Fatalf("ListBundleComments: %v", err)
	}
	if len(comments) != 2 || comments[0].ID != firstComment.ID || comments[1].ID != secondComment.ID {
		t.Errorf("comments = %+v", comments)
	}
	got, err := s.GetBundleComment(ctx, firstComment.ID)
	if err != nil || got.Body != firstComment.Body {
		t.Errorf("GetBundleComment = %+v, %v", got, err)
	}
	if err := s.DeleteBundleComment(ctx, firstComment.ID); err != nil {
		t.Fatalf("DeleteBundleComment: %v", err)
	}
	if _, err := s.GetBundleComment(ctx, firstComment.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("deleted GetBundleComment error = %v, want %v", err, ErrNotFound)
	}
	if err := s.DeleteBundleComment(ctx, firstComment.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("second DeleteBundleComment error = %v, want %v", err, ErrNotFound)
	}
}

func TestBundleCommentsKeepOptionCommentsSeparate(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()
	first := mustAdd(t, s, ItemInput{Name: "Cot", Category: "Nursery"})
	second := mustAdd(t, s, ItemInput{Name: "Pram", Category: "Travel"})
	bundle, err := s.AddBundle(ctx, BundleInput{
		Name: "Set", URL: "https://shop.example.com/set", Price: "10",
		Members: []BundleMemberInput{{ItemID: first.ID}, {ItemID: second.ID}},
	})
	if err != nil {
		t.Fatalf("AddBundle: %v", err)
	}
	if _, err := s.AddBundleComment(ctx, bundle.ID, "shared"); err != nil {
		t.Fatalf("AddBundleComment: %v", err)
	}
	if _, err := s.AddComment(ctx, bundle.Members[0].OptionID, "item-specific"); err != nil {
		t.Fatalf("AddComment: %v", err)
	}
	comments, err := s.ListBundleComments(ctx, bundle.ID)
	if err != nil || len(comments) != 1 || comments[0].Body != "shared" {
		t.Errorf("bundle comments = %+v, %v", comments, err)
	}
	optionComments, err := s.ListComments(ctx, bundle.Members[0].OptionID)
	if err != nil || len(optionComments) != 1 || optionComments[0].Body != "item-specific" {
		t.Errorf("option comments = %+v, %v", optionComments, err)
	}
}

func TestBundleCommentsMissingRecords(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()
	if _, err := s.AddBundleComment(ctx, 999, "nope"); !errors.Is(err, ErrNotFound) {
		t.Errorf("missing AddBundleComment error = %v, want %v", err, ErrNotFound)
	}
	if _, err := s.ListBundleComments(ctx, 999); !errors.Is(err, ErrNotFound) {
		t.Errorf("missing ListBundleComments error = %v, want %v", err, ErrNotFound)
	}
	if _, err := s.GetBundleComment(ctx, 999); !errors.Is(err, ErrNotFound) {
		t.Errorf("missing GetBundleComment error = %v, want %v", err, ErrNotFound)
	}
	if err := s.DeleteBundleComment(ctx, 999); !errors.Is(err, ErrNotFound) {
		t.Errorf("missing DeleteBundleComment error = %v, want %v", err, ErrNotFound)
	}
}
