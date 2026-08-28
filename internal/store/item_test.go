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
