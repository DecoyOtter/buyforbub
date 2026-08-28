package store

import (
	"context"
	"errors"
	"testing"
)

func mustAddOption(t *testing.T, s *Store, itemID int64, in OptionInput) Option {
	t.Helper()
	o, err := s.AddOption(context.Background(), itemID, in)
	if err != nil {
		t.Fatalf("AddOption(%+v): %v", in, err)
	}
	return o
}

func TestOptionInputCleaning(t *testing.T) {
	tests := []struct {
		name      string
		in        OptionInput
		wantErr   error
		wantURL   string
		wantPrice string // via PriceText
		wantTitle string
	}{
		{
			name:      "full option",
			in:        OptionInput{URL: "https://shop.example.com/cot", Label: " Boori Daintree ", Price: "$1,199.00"},
			wantURL:   "https://shop.example.com/cot",
			wantPrice: "$1,199",
			wantTitle: "Boori Daintree",
		},
		{
			name:      "bare host gets https",
			in:        OptionInput{URL: "shop.example.com/cot"},
			wantURL:   "https://shop.example.com/cot",
			wantTitle: "shop.example.com",
		},
		{
			name:      "no label falls back to host without www",
			in:        OptionInput{URL: "https://www.ikea.com/au/sundvik"},
			wantTitle: "ikea.com",
		},
		{
			name:      "price with cents",
			in:        OptionInput{URL: "https://example.com", Price: "199.95"},
			wantPrice: "$199.95",
		},
		{
			name:      "blank price is no price",
			in:        OptionInput{URL: "https://example.com", Price: "   "},
			wantPrice: "",
		},
		{name: "empty url rejected", in: OptionInput{URL: "  "}, wantErr: ErrInvalidURL},
		{name: "non-http scheme rejected", in: OptionInput{URL: "ftp://example.com"}, wantErr: ErrInvalidURL},
		{name: "javascript scheme rejected", in: OptionInput{URL: "javascript:alert(1)"}, wantErr: ErrInvalidURL},
		{
			name:    "unparseable price rejected",
			in:      OptionInput{URL: "https://example.com", Price: "about a grand"},
			wantErr: ErrInvalidPrice,
		},
		{
			name:    "negative price rejected",
			in:      OptionInput{URL: "https://example.com", Price: "-5"},
			wantErr: ErrInvalidPrice,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := newStore(t)
			item := mustAdd(t, s, ItemInput{Name: "Cot", Category: "Nursery"})

			got, err := s.AddOption(context.Background(), item.ID, tt.in)
			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("AddOption error = %v, want %v", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("AddOption: %v", err)
			}

			if tt.wantURL != "" && got.URL != tt.wantURL {
				t.Errorf("URL = %q, want %q", got.URL, tt.wantURL)
			}
			if got.PriceText() != tt.wantPrice {
				t.Errorf("PriceText = %q, want %q", got.PriceText(), tt.wantPrice)
			}
			if tt.wantTitle != "" && got.Title() != tt.wantTitle {
				t.Errorf("Title = %q, want %q", got.Title(), tt.wantTitle)
			}
			if got.Chosen {
				t.Error("a new option is already chosen")
			}
		})
	}
}

func TestAddOptionToMissingItem(t *testing.T) {
	s := newStore(t)
	_, err := s.AddOption(context.Background(), 9999, OptionInput{URL: "https://example.com"})
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("AddOption on missing item = %v, want %v", err, ErrNotFound)
	}
}

func TestListOptionsIsInsertionOrdered(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()
	item := mustAdd(t, s, ItemInput{Name: "Cot", Category: "Nursery"})

	for _, u := range []string{"https://a.example.com", "https://b.example.com", "https://c.example.com"} {
		mustAddOption(t, s, item.ID, OptionInput{URL: u})
	}

	opts, err := s.ListOptions(ctx, item.ID)
	if err != nil {
		t.Fatalf("ListOptions: %v", err)
	}
	if len(opts) != 3 {
		t.Fatalf("got %d options, want 3", len(opts))
	}
	for i, want := range []string{"a.example.com", "b.example.com", "c.example.com"} {
		if opts[i].Title() != want {
			t.Errorf("option %d = %q, want %q", i, opts[i].Title(), want)
		}
	}
}

func TestOptionCountOnItem(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()
	item := mustAdd(t, s, ItemInput{Name: "Cot", Category: "Nursery"})

	if got, _ := s.Get(ctx, item.ID); got.OptionCount != 0 {
		t.Errorf("OptionCount = %d on a new item, want 0", got.OptionCount)
	}

	o := mustAddOption(t, s, item.ID, OptionInput{URL: "https://a.example.com"})
	mustAddOption(t, s, item.ID, OptionInput{URL: "https://b.example.com"})

	if got, _ := s.Get(ctx, item.ID); got.OptionCount != 2 {
		t.Errorf("OptionCount = %d, want 2", got.OptionCount)
	}
	items, _ := s.List(ctx)
	if items[0].OptionCount != 2 {
		t.Errorf("List OptionCount = %d, want 2", items[0].OptionCount)
	}

	if err := s.DeleteOption(ctx, o.ID); err != nil {
		t.Fatalf("DeleteOption: %v", err)
	}
	if got, _ := s.Get(ctx, item.ID); got.OptionCount != 1 {
		t.Errorf("OptionCount after delete = %d, want 1", got.OptionCount)
	}
	if err := s.DeleteOption(ctx, o.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("second DeleteOption = %v, want %v", err, ErrNotFound)
	}
}

func TestChooseOption(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()
	item := mustAdd(t, s, ItemInput{Name: "Cot", Category: "Nursery"})
	first := mustAddOption(t, s, item.ID, OptionInput{URL: "https://a.example.com"})
	second := mustAddOption(t, s, item.ID, OptionInput{URL: "https://b.example.com"})

	if _, err := s.ChooseOption(ctx, first.ID); err != nil {
		t.Fatalf("ChooseOption: %v", err)
	}

	// Choosing marks the item bought.
	got, _ := s.Get(ctx, item.ID)
	if got.Status != StatusBought {
		t.Errorf("item status = %q after choosing, want %q", got.Status, StatusBought)
	}
	assertChosen(t, s, item.ID, first.ID)

	// Choosing another moves the flag rather than adding a second.
	if _, err := s.ChooseOption(ctx, second.ID); err != nil {
		t.Fatalf("ChooseOption: %v", err)
	}
	assertChosen(t, s, item.ID, second.ID)

	if _, err := s.ChooseOption(ctx, 9999); !errors.Is(err, ErrNotFound) {
		t.Errorf("ChooseOption on missing option = %v, want %v", err, ErrNotFound)
	}
}

// Marking an item needed again must not leave a stale "this is the one we
// bought" flag behind.
func TestUntogglingClearsChosenOption(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()
	item := mustAdd(t, s, ItemInput{Name: "Cot", Category: "Nursery"})
	opt := mustAddOption(t, s, item.ID, OptionInput{URL: "https://a.example.com"})

	if _, err := s.ChooseOption(ctx, opt.ID); err != nil {
		t.Fatalf("ChooseOption: %v", err)
	}
	assertChosen(t, s, item.ID, opt.ID)

	if _, err := s.Toggle(ctx, item.ID); err != nil {
		t.Fatalf("Toggle: %v", err)
	}
	assertChosen(t, s, item.ID, 0)
}

// Options are meaningless without their item, so the cascade must actually be
// switched on — SQLite ignores foreign keys unless the pragma is set.
func TestDeletingItemCascadesToOptions(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()
	item := mustAdd(t, s, ItemInput{Name: "Cot", Category: "Nursery"})
	keep := mustAdd(t, s, ItemInput{Name: "Pram", Category: "Travel"})

	opt := mustAddOption(t, s, item.ID, OptionInput{URL: "https://a.example.com"})
	kept := mustAddOption(t, s, keep.ID, OptionInput{URL: "https://b.example.com"})

	if err := s.Delete(ctx, item.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	if _, err := s.GetOption(ctx, opt.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("option survived its item's deletion: %v", err)
	}
	if _, err := s.GetOption(ctx, kept.ID); err != nil {
		t.Errorf("unrelated option was deleted: %v", err)
	}
}

// assertChosen checks exactly which of an item's options is flagged, where 0
// means none of them should be.
func assertChosen(t *testing.T, s *Store, itemID, wantID int64) {
	t.Helper()

	opts, err := s.ListOptions(context.Background(), itemID)
	if err != nil {
		t.Fatalf("ListOptions: %v", err)
	}
	var chosen []int64
	for _, o := range opts {
		if o.Chosen {
			chosen = append(chosen, o.ID)
		}
	}

	if wantID == 0 {
		if len(chosen) != 0 {
			t.Errorf("options %v are chosen, want none", chosen)
		}
		return
	}
	if len(chosen) != 1 || chosen[0] != wantID {
		t.Errorf("chosen options = %v, want exactly [%d]", chosen, wantID)
	}
}
