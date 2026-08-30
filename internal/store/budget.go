package store

// BudgetSummary is the spending position for a Category or the whole list.
type BudgetSummary struct {
	ActualCents        int64
	StillPlannedCents  int64
	ExpectedTotalCents int64
	UnbudgetedNeeded   int
	UnknownActual      int
}

func (s BudgetSummary) ActualText() string       { return FormatMoney(s.ActualCents) }
func (s BudgetSummary) StillPlannedText() string { return FormatMoney(s.StillPlannedCents) }
func (s BudgetSummary) ExpectedTotalText() string {
	return FormatMoney(s.ExpectedTotalCents)
}

type BudgetSummaries struct {
	Categories map[string]BudgetSummary
	Overall    BudgetSummary
}

// SummarizeBudgets calculates Category and whole-list spending.
func SummarizeBudgets(items []Item, chosenPrices map[int64]*int64) BudgetSummaries {
	summaries := BudgetSummaries{Categories: make(map[string]BudgetSummary, len(Categories))}
	for _, item := range items {
		category := summaries.Categories[item.Category]
		addItemBudget(&category, item, chosenPrices)
		summaries.Categories[item.Category] = category
		addItemBudget(&summaries.Overall, item, chosenPrices)
	}
	return summaries
}

func addItemBudget(summary *BudgetSummary, item Item, chosenPrices map[int64]*int64) {
	if item.Bought() {
		price, chosen := chosenPrices[item.ID]
		if !chosen || price == nil {
			summary.UnknownActual++
		} else {
			summary.ActualCents += *price
		}
	} else if item.BudgetCents == nil {
		summary.UnbudgetedNeeded++
	} else {
		summary.StillPlannedCents += *item.BudgetCents
	}
	summary.ExpectedTotalCents = summary.ActualCents + summary.StillPlannedCents
}
