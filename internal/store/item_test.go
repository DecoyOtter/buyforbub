package store

import (
	"slices"
	"testing"
)

func TestGroupByCategory(t *testing.T) {
	// ids double as insertion order.
	items := []Item{
		{ID: 1, Name: "Cot", Category: "Nursery", Status: StatusNeeded},
		{ID: 2, Name: "Bottles", Category: "Feeding", Status: StatusBought},
		{ID: 3, Name: "Monitor", Category: "Nursery", Status: StatusBought},
		{ID: 4, Name: "Sheets", Category: "Nursery", Status: StatusNeeded},
		{ID: 5, Name: "Steriliser", Category: "Feeding", Status: StatusNeeded},
		{ID: 6, Name: "Pram", Category: "Travel", Status: StatusNeeded},
	}

	tests := []struct {
		category string
		want     []string
	}{
		// Needed first in insertion order, bought sunk to the bottom.
		{"Nursery", []string{"Cot", "Sheets", "Monitor"}},
		{"Feeding", []string{"Steriliser", "Bottles"}},
		{"Travel", []string{"Pram"}},
	}

	groups := GroupByCategory(items)
	if len(groups) != len(tests) {
		t.Fatalf("got %d groups, want %d (empty categories must be omitted)", len(groups), len(tests))
	}

	for i, tt := range tests {
		g := groups[i]
		if g.Category != tt.category {
			t.Errorf("group %d is %q, want %q (Categories order)", i, g.Category, tt.category)
			continue
		}
		got := make([]string, len(g.Items))
		for j, it := range g.Items {
			got[j] = it.Name
		}
		if !slices.Equal(got, tt.want) {
			t.Errorf("%s items = %v, want %v", tt.category, got, tt.want)
		}
	}
}

func TestGroupByCategoryEmpty(t *testing.T) {
	if got := GroupByCategory(nil); len(got) != 0 {
		t.Errorf("GroupByCategory(nil) = %v, want empty", got)
	}
}

func TestProgress(t *testing.T) {
	tests := []struct {
		name  string
		items []Item
		done  int
		total int
	}{
		{name: "empty"},
		{
			name:  "none bought",
			items: []Item{{Status: StatusNeeded}, {Status: StatusNeeded}},
			done:  0, total: 2,
		},
		{
			name:  "some bought",
			items: []Item{{Status: StatusBought}, {Status: StatusNeeded}, {Status: StatusBought}},
			done:  2, total: 3,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			done, total := Progress(tt.items)
			if done != tt.done || total != tt.total {
				t.Errorf("Progress = (%d, %d), want (%d, %d)", done, total, tt.done, tt.total)
			}
		})
	}
}

func TestSummarize(t *testing.T) {
	items := []Item{
		{ID: 1, Category: "Nursery", Qty: 2, BudgetCents: cents(10000), Status: StatusBought},
		{ID: 2, Category: "Nursery", BudgetCents: cents(5000), Status: StatusBought},
		{ID: 3, Category: "Feeding", Status: StatusBought},
		{ID: 4, Category: "Feeding", BudgetCents: cents(3000), Status: StatusBought},
	}
	byCategory, overall := Summarize(items, map[int64]*int64{
		1: cents(10000), // Qty does not multiply money.
		2: cents(6000),  // Over its category budget.
		4: nil,          // Chosen but unpriced.
	})

	nursery := byCategory["Nursery"]
	if nursery.BudgetCents != 15000 || nursery.ActualCents != 16000 || !nursery.Over() || nursery.DifferenceText() != "$10" {
		t.Errorf("Nursery summary = %+v", nursery)
	}
	feeding := byCategory["Feeding"]
	if feeding.BudgetCents != 3000 || feeding.ActualCents != 0 || feeding.Unbudgeted != 1 || feeding.UnknownActual != 2 || feeding.Over() {
		t.Errorf("Feeding summary = %+v", feeding)
	}
	if overall.BudgetCents != 18000 || overall.ActualCents != 16000 || overall.Unbudgeted != 1 || overall.UnknownActual != 2 || overall.Over() || overall.DifferenceText() != "$20" {
		t.Errorf("overall summary = %+v", overall)
	}
}

func TestValidCategoryAndStatus(t *testing.T) {
	for _, c := range Categories {
		if !ValidCategory(c) {
			t.Errorf("ValidCategory(%q) = false, want true", c)
		}
	}
	for _, c := range []string{"", "nursery", "Furniture"} {
		if ValidCategory(c) {
			t.Errorf("ValidCategory(%q) = true, want false", c)
		}
	}
	for _, s := range []string{StatusNeeded, StatusBought} {
		if !ValidStatus(s) {
			t.Errorf("ValidStatus(%q) = false, want true", s)
		}
	}
	for _, s := range []string{"", "Bought", "done"} {
		if ValidStatus(s) {
			t.Errorf("ValidStatus(%q) = true, want false", s)
		}
	}
}
