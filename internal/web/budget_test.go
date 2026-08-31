package web

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/DecoyOtter/buyforbub/internal/store"
)

func TestBudgetSummaryRendering(t *testing.T) {
	s, st := newServer(t)
	needed := mustAddBudgetItem(t, st, store.ItemInput{Name: "Cot", Category: "Nursery", Budget: "$400"})
	unbudgeted := mustAdd(t, st, "Monitor", "Nursery")
	bought := mustAddBudgetItem(t, st, store.ItemInput{Name: "Mattress", Category: "Nursery", Budget: "$500"})
	chosen := mustAddOption(t, st, bought.ID, store.OptionInput{URL: "https://example.com/mattress", Price: "$450"})
	if _, err := st.ChooseOption(context.Background(), chosen.ID); err != nil {
		t.Fatalf("ChooseOption: %v", err)
	}
	unknown := mustAddBudgetItem(t, st, store.ItemInput{Name: "Bassinet", Category: "Nursery", Budget: "$200"})
	if _, err := st.Toggle(context.Background(), unknown.ID); err != nil {
		t.Fatalf("Toggle: %v", err)
	}
	mustAddBudgetItem(t, st, store.ItemInput{Name: "Bottles", Category: "Feeding", Budget: "$50"})

	rec := do(t, s, http.MethodGet, "/", nil)
	assertStatus(t, rec, http.StatusOK)
	body := rec.Body.String()

	assertContains(t, body, `aria-label="Whole list budget"`)
	assertContains(t, body, `<span>Actual</span><strong>$450</strong>`)
	assertContains(t, body, `<span>Still planned</span><strong>$450</strong>`)
	assertContains(t, body, `<span>Expected total</span><strong>$900</strong>`)
	assertContains(t, body, "1 item needs a budget")
	assertContains(t, body, "1 bought item has an unknown actual")
	assertContains(t, body, `<strong>$450 actual</strong><span>$400 planned</span>`)
	assertContains(t, body, `<strong>$0 actual</strong><span>$50 planned</span>`)
	assertContains(t, body, `class="group__meter" value="45000" max="85000"`)

	neededRow := rowHTML(t, body, needed.ID)
	assertContains(t, neededRow, "<strong>$400</strong><span>Budget</span>")
	unbudgetedRow := rowHTML(t, body, unbudgeted.ID)
	assertContains(t, unbudgetedRow, "Set budget")
	assertContains(t, unbudgetedRow, `hx-get="/items/`+itoa(unbudgeted.ID)+`/edit"`)
	boughtRow := rowHTML(t, body, bought.ID)
	assertContains(t, boughtRow, "<strong>$450</strong><span>Actual</span>")
	assertNotContains(t, boughtRow, "$500")
	assertNotContains(t, boughtRow, "Budget")
	unknownRow := rowHTML(t, body, unknown.ID)
	assertNotContains(t, unknownRow, "item__money")
	assertNotContains(t, unknownRow, "Budget")
	assertNotContains(t, body, "leftover")
	assertNotContains(t, body, "overrun")
}

func TestBudgetCountsOmittedWhenZero(t *testing.T) {
	s, st := newServer(t)
	mustAddBudgetItem(t, st, store.ItemInput{Name: "Cot", Category: "Nursery", Budget: "$400"})
	bought := mustAddBudgetItem(t, st, store.ItemInput{Name: "Pram", Category: "Travel", Budget: "$500"})
	chosen := mustAddOption(t, st, bought.ID, store.OptionInput{URL: "https://example.com/pram", Price: "$450"})
	if _, err := st.ChooseOption(context.Background(), chosen.ID); err != nil {
		t.Fatalf("ChooseOption: %v", err)
	}

	rec := do(t, s, http.MethodGet, "/", nil)
	assertStatus(t, rec, http.StatusOK)
	assertNotContains(t, rec.Body.String(), "budget-total__counts")
}

func TestChosenBundleAllocationsRenderInCategoryAndOverallActuals(t *testing.T) {
	s, st := newServer(t)
	cot := mustAddBudgetItem(t, st, store.ItemInput{Name: "Cot", Category: "Nursery", Budget: "$40"})
	pram := mustAddBudgetItem(t, st, store.ItemInput{Name: "Pram", Category: "Travel", Budget: "$40"})
	seat := mustAddBudgetItem(t, st, store.ItemInput{Name: "Car seat", Category: "Travel", Budget: "$40"})
	bundle, err := st.AddBundle(context.Background(), store.BundleInput{
		Name: "Travel system", URL: "https://shop.example/system", Price: "100",
		Members: []store.BundleMemberInput{{ItemID: cot.ID}, {ItemID: pram.ID}, {ItemID: seat.ID}},
	})
	if err != nil {
		t.Fatalf("AddBundle: %v", err)
	}

	rec := do(t, s, http.MethodPost, "/bundles/"+itoa(bundle.ID)+"/choose", nil)
	assertStatus(t, rec, http.StatusOK)
	body := rec.Body.String()
	assertContains(t, body, `<span>Actual</span><strong>$100</strong>`)
	assertContains(t, body, `<strong>$33.34 actual</strong><span>$0 planned</span>`)
	assertContains(t, body, `<strong>$66.66 actual</strong><span>$0 planned</span>`)
}

func TestBudgetSummariesRefreshAfterMutations(t *testing.T) {
	s, st := newServer(t)
	item := mustAddBudgetItem(t, st, store.ItemInput{Name: "Cot", Category: "Nursery", Budget: "$100"})

	rec := do(t, s, http.MethodPost, "/items/"+itoa(item.ID), url.Values{
		"name": {"Cot"}, "category": {"Nursery"}, "budget": {"$200"},
	})
	assertStatus(t, rec, http.StatusOK)
	assertContains(t, rec.Body.String(), `<span>Still planned</span><strong>$200</strong>`)

	rec = do(t, s, http.MethodPost, "/items/"+itoa(item.ID)+"/options", url.Values{
		"url": {"https://example.com/cot"}, "price": {"$150"},
	})
	assertStatus(t, rec, http.StatusOK)
	assertContains(t, rec.Body.String(), "is-open")
	options, err := st.ListOptions(context.Background(), item.ID)
	if err != nil || len(options) != 1 {
		t.Fatalf("ListOptions = %+v, %v", options, err)
	}

	rec = do(t, s, http.MethodPost, "/options/"+itoa(options[0].ID)+"/choose", nil)
	assertStatus(t, rec, http.StatusOK)
	assertContains(t, rec.Body.String(), `<span>Actual</span><strong>$150</strong>`)
	assertContains(t, rec.Body.String(), `<span>Still planned</span><strong>$0</strong>`)

	rec = do(t, s, http.MethodGet, "/items/"+itoa(item.ID), nil)
	assertContains(t, rec.Body.String(), `hx-post="/options/`+itoa(options[0].ID)+`/delete" hx-target="#list"`)
	rec = do(t, s, http.MethodPost, "/options/"+itoa(options[0].ID)+"/delete", nil)
	assertStatus(t, rec, http.StatusOK)
	assertContains(t, rec.Body.String(), `id="list"`)
	assertContains(t, rec.Body.String(), "1 bought item has an unknown actual")

	rec = do(t, s, http.MethodPost, "/items/"+itoa(item.ID)+"/toggle", nil)
	assertStatus(t, rec, http.StatusOK)
	assertContains(t, rec.Body.String(), `<span>Still planned</span><strong>$200</strong>`)
	assertNotContains(t, rec.Body.String(), "unknown actual")

	rec = do(t, s, http.MethodPost, "/items/"+itoa(item.ID)+"/delete", nil)
	assertStatus(t, rec, http.StatusOK)
	assertNotContains(t, rec.Body.String(), "budget-total")
}

func mustAddBudgetItem(t *testing.T, st *store.Store, input store.ItemInput) store.Item {
	t.Helper()
	item, err := st.Add(context.Background(), input)
	if err != nil {
		t.Fatalf("Add(%+v): %v", input, err)
	}
	return item
}

func rowHTML(t *testing.T, body string, id int64) string {
	t.Helper()
	start := strings.Index(body, `id="item-`+itoa(id)+`"`)
	if start < 0 {
		t.Fatalf("row %d not found", id)
	}
	end := strings.Index(body[start:], "</li>")
	if end < 0 {
		t.Fatalf("row %d has no closing tag", id)
	}
	return body[start : start+end]
}
