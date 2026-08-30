package store

import (
	"errors"
	"slices"
	"strings"
	"time"
)

// Categories are fixed, and this is the order they are displayed in.
var Categories = []string{"Nursery", "Feeding", "Clothing", "Travel", "Health", "Other"}

const (
	StatusNeeded = "needed"
	StatusBought = "bought"
)

var (
	ErrNotFound        = errors.New("item not found")
	ErrInvalidName     = errors.New("name is required")
	ErrInvalidCategory = errors.New("unknown category")
	ErrInvalidStatus   = errors.New("unknown status")
	ErrInvalidBudget   = errors.New("budget is not a number")
)

// Item is one thing to buy.
type Item struct {
	ID          int64
	Name        string
	Qty         int
	Category    string
	Status      string
	Notes       string
	BudgetCents *int64
	CreatedAt   time.Time
	OptionCount int
}

func (i Item) Bought() bool { return i.Status == StatusBought }

func (i Item) BudgetText() string { return moneyText(i.BudgetCents) }

// ItemInput is the user-supplied half of an item; status is managed separately.
type ItemInput struct {
	Name     string `json:"name"`
	Qty      int    `json:"qty"`
	Category string `json:"category"`
	Notes    string `json:"notes,omitempty"`
	Budget   string `json:"budget,omitempty"`
}

// clean trims, applies defaults, and rejects anything unusable.
func (in ItemInput) clean() (ItemInput, error) {
	in.Name = strings.TrimSpace(in.Name)
	in.Notes = strings.TrimSpace(in.Notes)
	in.Category = strings.TrimSpace(in.Category)
	in.Budget = strings.TrimSpace(in.Budget)

	if in.Name == "" {
		return in, ErrInvalidName
	}
	if !ValidCategory(in.Category) {
		return in, ErrInvalidCategory
	}
	if in.Qty < 1 {
		in.Qty = 1
	}
	if _, err := parsePrice(in.Budget); err != nil {
		return in, ErrInvalidBudget
	}
	return in, nil
}

func ValidCategory(c string) bool {
	return slices.Contains(Categories, c)
}

func ValidStatus(s string) bool {
	return s == StatusNeeded || s == StatusBought
}

// CategoryGroup is a category's items, ready to render under one heading.
type CategoryGroup struct {
	Category string
	Items    []Item
}

// GroupByCategory buckets items into Categories order, sinking bought items to
// the bottom of each group. Relative order is otherwise preserved. Empty
// categories are omitted.
func GroupByCategory(items []Item) []CategoryGroup {
	needed := make(map[string][]Item, len(Categories))
	bought := make(map[string][]Item, len(Categories))
	for _, it := range items {
		if it.Bought() {
			bought[it.Category] = append(bought[it.Category], it)
		} else {
			needed[it.Category] = append(needed[it.Category], it)
		}
	}

	groups := make([]CategoryGroup, 0, len(Categories))
	for _, c := range Categories {
		if len(needed[c])+len(bought[c]) == 0 {
			continue
		}
		combined := make([]Item, 0, len(needed[c])+len(bought[c]))
		combined = append(combined, needed[c]...)
		combined = append(combined, bought[c]...)
		groups = append(groups, CategoryGroup{Category: c, Items: combined})
	}
	return groups
}

// Progress counts how many items have been bought.
func Progress(items []Item) (done, total int) {
	for _, it := range items {
		if it.Bought() {
			done++
		}
	}
	return done, len(items)
}

// BudgetSummary is the known spending position for one category or the list.
type BudgetSummary struct {
	BudgetCents   int64
	ActualCents   int64
	Unbudgeted    int
	UnknownActual int
}

func (s BudgetSummary) BudgetText() string { return moneyText(&s.BudgetCents) }
func (s BudgetSummary) ActualText() string { return moneyText(&s.ActualCents) }
func (s BudgetSummary) Over() bool         { return s.ActualCents > s.BudgetCents }
func (s BudgetSummary) DifferenceText() string {
	difference := s.BudgetCents - s.ActualCents
	if difference < 0 {
		difference = -difference
	}
	return moneyText(&difference)
}

// Summarize budgets items by category and for the complete list. A map entry
// records a chosen option; nil means that chosen option has no price.
func Summarize(items []Item, chosenPrices map[int64]*int64) (map[string]BudgetSummary, BudgetSummary) {
	byCategory := make(map[string]BudgetSummary, len(Categories))
	var overall BudgetSummary
	for _, item := range items {
		summary := byCategory[item.Category]
		addSummary(&summary, item, chosenPrices)
		byCategory[item.Category] = summary
		addSummary(&overall, item, chosenPrices)
	}
	return byCategory, overall
}

func addSummary(summary *BudgetSummary, item Item, chosenPrices map[int64]*int64) {
	if item.BudgetCents == nil {
		summary.Unbudgeted++
	} else {
		summary.BudgetCents += *item.BudgetCents
	}
	if price, chosen := chosenPrices[item.ID]; chosen && price != nil {
		summary.ActualCents += *price
	} else if item.Bought() {
		summary.UnknownActual++
	}
}
