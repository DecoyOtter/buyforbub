package store

import (
	"context"
	"testing"
)

func TestSummarizeBudgets(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()

	mustAdd(t, s, ItemInput{Name: "Cot", Qty: 4, Category: "Nursery", Budget: "$400"})
	mustAdd(t, s, ItemInput{Name: "Monitor", Category: "Nursery"})
	addBoughtWithOption(t, s, ItemInput{Name: "Mattress", Qty: 3, Category: "Nursery", Budget: "$100"}, "$80")
	addBoughtWithOption(t, s, ItemInput{Name: "Sheets", Category: "Nursery", Budget: "$100"}, "$100")
	addBoughtWithOption(t, s, ItemInput{Name: "Chair", Category: "Nursery", Budget: "$100"}, "$120")

	directBought := mustAdd(t, s, ItemInput{Name: "Bottles", Category: "Feeding", Budget: "$200"})
	if _, err := s.Toggle(ctx, directBought.ID); err != nil {
		t.Fatalf("Toggle direct-bought Item: %v", err)
	}
	addBoughtWithOption(t, s, ItemInput{Name: "Steriliser", Category: "Feeding", Budget: "$300"}, "")
	mustAdd(t, s, ItemInput{Name: "Formula", Category: "Feeding", Budget: "$50"})
	mustAdd(t, s, ItemInput{Name: "Pump", Category: "Feeding"})

	addBoughtWithOption(t, s, ItemInput{Name: "Pram", Category: "Travel"}, "$70")

	items, err := s.List(ctx)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	prices, err := s.ChosenOptionPrices(ctx)
	if err != nil {
		t.Fatalf("ChosenOptionPrices: %v", err)
	}
	summaries := SummarizeBudgets(items, prices)

	assertBudgetSummary(t, "Nursery", summaries.Categories["Nursery"], BudgetSummary{
		ActualCents: 30000, StillPlannedCents: 40000, ExpectedTotalCents: 70000, UnbudgetedNeeded: 1,
	})
	assertBudgetSummary(t, "Feeding", summaries.Categories["Feeding"], BudgetSummary{
		StillPlannedCents: 5000, ExpectedTotalCents: 5000, UnbudgetedNeeded: 1, UnknownActual: 2,
	})
	assertBudgetSummary(t, "Travel", summaries.Categories["Travel"], BudgetSummary{
		ActualCents: 7000, ExpectedTotalCents: 7000,
	})
	assertBudgetSummary(t, "overall", summaries.Overall, BudgetSummary{
		ActualCents: 37000, StillPlannedCents: 45000, ExpectedTotalCents: 82000,
		UnbudgetedNeeded: 2, UnknownActual: 2,
	})
}

func TestSummarizeBudgetsIgnoresPriorBudgetAfterPurchase(t *testing.T) {
	tests := []struct {
		name   string
		budget int64
		actual int64
	}{
		{name: "actual below prior budget", budget: 10000, actual: 8000},
		{name: "actual equals prior budget", budget: 10000, actual: 10000},
		{name: "actual above prior budget", budget: 10000, actual: 12000},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			item := Item{ID: 1, Qty: 5, Category: "Other", Status: StatusBought, BudgetCents: cents(tt.budget)}
			summary := SummarizeBudgets([]Item{item}, map[int64]*int64{item.ID: cents(tt.actual)}).Overall
			assertBudgetSummary(t, "overall", summary, BudgetSummary{
				ActualCents: tt.actual, ExpectedTotalCents: tt.actual,
			})
		})
	}
}

func TestChosenBundleAllocationsFeedBudgetSummaries(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()
	first := mustAdd(t, s, ItemInput{Name: "Cot", Category: "Nursery", Budget: "$40"})
	second := mustAdd(t, s, ItemInput{Name: "Pram", Category: "Travel", Budget: "$40"})
	third := mustAdd(t, s, ItemInput{Name: "Car seat", Category: "Travel", Budget: "$40"})
	bundle, err := s.AddBundle(ctx, BundleInput{
		Name: "Travel system", URL: "https://shop.example/system", Price: "100",
		Members: []BundleMemberInput{{ItemID: first.ID}, {ItemID: second.ID}, {ItemID: third.ID}},
	})
	if err != nil {
		t.Fatalf("AddBundle: %v", err)
	}
	if _, err := s.ChooseBundle(ctx, bundle.ID); err != nil {
		t.Fatalf("ChooseBundle: %v", err)
	}

	items, err := s.List(ctx)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	prices, err := s.ChosenOptionPrices(ctx)
	if err != nil {
		t.Fatalf("ChosenOptionPrices: %v", err)
	}
	summaries := SummarizeBudgets(items, prices)
	assertBudgetSummary(t, "Nursery", summaries.Categories["Nursery"], BudgetSummary{ActualCents: 3334, ExpectedTotalCents: 3334})
	assertBudgetSummary(t, "Travel", summaries.Categories["Travel"], BudgetSummary{ActualCents: 6666, ExpectedTotalCents: 6666})
	assertBudgetSummary(t, "overall", summaries.Overall, BudgetSummary{ActualCents: 10000, ExpectedTotalCents: 10000})

	if _, err := s.Toggle(ctx, first.ID); err != nil {
		t.Fatalf("Toggle: %v", err)
	}
	assertBundleBudgetSummary(t, s, BudgetSummary{StillPlannedCents: 12000, ExpectedTotalCents: 12000})
	if _, err := s.ChooseBundle(ctx, bundle.ID); err != nil {
		t.Fatalf("ChooseBundle again: %v", err)
	}
	if _, err := s.UpdateBundle(ctx, bundle.ID, BundleInput{
		Name: "Travel system", URL: "https://shop.example/system", Price: "100",
		Members: []BundleMemberInput{{ItemID: first.ID}, {ItemID: second.ID}, {ItemID: third.ID}},
	}); err != nil {
		t.Fatalf("UpdateBundle: %v", err)
	}
	assertBundleBudgetSummary(t, s, BudgetSummary{StillPlannedCents: 12000, ExpectedTotalCents: 12000})
	if _, err := s.ChooseBundle(ctx, bundle.ID); err != nil {
		t.Fatalf("ChooseBundle after update: %v", err)
	}
	normal := mustAddOption(t, s, second.ID, OptionInput{URL: "https://shop.example/pram", Price: "10"})
	if _, err := s.ChooseOption(ctx, normal.ID); err != nil {
		t.Fatalf("ChooseOption: %v", err)
	}
	assertBundleBudgetSummary(t, s, BudgetSummary{ActualCents: 1000, StillPlannedCents: 8000, ExpectedTotalCents: 9000})
	if _, err := s.ChooseBundle(ctx, bundle.ID); err != nil {
		t.Fatalf("ChooseBundle after displacement: %v", err)
	}
	if err := s.DeleteBundle(ctx, bundle.ID); err != nil {
		t.Fatalf("DeleteBundle: %v", err)
	}
	assertBundleBudgetSummary(t, s, BudgetSummary{StillPlannedCents: 12000, ExpectedTotalCents: 12000})
}

func assertBundleBudgetSummary(t *testing.T, s *Store, want BudgetSummary) {
	t.Helper()
	items, err := s.List(context.Background())
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	prices, err := s.ChosenOptionPrices(context.Background())
	if err != nil {
		t.Fatalf("ChosenOptionPrices: %v", err)
	}
	assertBudgetSummary(t, "bundle overall", SummarizeBudgets(items, prices).Overall, want)
}

func addBoughtWithOption(t *testing.T, s *Store, input ItemInput, price string) {
	t.Helper()
	item := mustAdd(t, s, input)
	option := mustAddOption(t, s, item.ID, OptionInput{URL: "https://example.com/" + input.Name, Price: price})
	if _, err := s.ChooseOption(context.Background(), option.ID); err != nil {
		t.Fatalf("ChooseOption(%q): %v", input.Name, err)
	}
}

func assertBudgetSummary(t *testing.T, name string, got, want BudgetSummary) {
	t.Helper()
	if got != want {
		t.Errorf("%s summary = %+v, want %+v", name, got, want)
	}
}
