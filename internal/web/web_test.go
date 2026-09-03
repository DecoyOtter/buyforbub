package web

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/DecoyOtter/buyforbub/internal/store"
)

func newServer(t *testing.T) (*Server, *store.Store) {
	t.Helper()

	st, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(func() { st.Close() })

	s, err := New(st, "Buy for Bub", slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatalf("web.New: %v", err)
	}
	return s, st
}

// do issues a request and returns the recorder.
func do(t *testing.T, s *Server, method, path string, form url.Values) *httptest.ResponseRecorder {
	t.Helper()

	var req *http.Request
	if form == nil {
		req = httptest.NewRequest(method, path, nil)
	} else {
		req = httptest.NewRequest(method, path, strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}

	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, req)
	return rec
}

func doHTMX(t *testing.T, s *Server, method, path string, form url.Values) *httptest.ResponseRecorder {
	t.Helper()

	var req *http.Request
	if form == nil {
		req = httptest.NewRequest(method, path, nil)
	} else {
		req = httptest.NewRequest(method, path, strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	req.Header.Set("HX-Request", "true")
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, req)
	return rec
}

func mustAdd(t *testing.T, st *store.Store, name, category string) store.Item {
	t.Helper()
	it, err := st.Add(context.Background(), store.ItemInput{Name: name, Category: category})
	if err != nil {
		t.Fatalf("Add(%q): %v", name, err)
	}
	return it
}

func assertStatus(t *testing.T, rec *httptest.ResponseRecorder, want int) {
	t.Helper()
	if rec.Code != want {
		t.Fatalf("status = %d, want %d (body: %s)", rec.Code, want, rec.Body.String())
	}
}

func assertContains(t *testing.T, body, want string) {
	t.Helper()
	if !strings.Contains(body, want) {
		t.Errorf("body does not contain %q", want)
	}
}

func assertNotContains(t *testing.T, body, want string) {
	t.Helper()
	if strings.Contains(body, want) {
		t.Errorf("body unexpectedly contains %q", want)
	}
}

func TestIndexRendersFullPage(t *testing.T) {
	s, st := newServer(t)
	mustAdd(t, st, "Cot", "Nursery")

	rec := do(t, s, http.MethodGet, "/", nil)
	assertStatus(t, rec, http.StatusOK)

	body := rec.Body.String()
	assertContains(t, body, "<!doctype html>")
	assertContains(t, body, "Buy for Bub")
	assertContains(t, body, "Cot")
	assertContains(t, body, `id="list"`)
	assertNotContains(t, body, `name="budget"`)
	// Every category must be offered in the add form.
	for _, c := range store.Categories {
		assertContains(t, body, `<option value="`+c+`">`)
	}
}

func TestIndexWhenEmpty(t *testing.T) {
	s, _ := newServer(t)

	rec := do(t, s, http.MethodGet, "/", nil)
	assertStatus(t, rec, http.StatusOK)
	assertContains(t, rec.Body.String(), "Nothing on the list yet")
}

func TestBundleRailRendersOrderedCards(t *testing.T) {
	s, st := newServer(t)
	cot := mustAdd(t, st, "Cot", "Nursery")
	pram := mustAdd(t, st, "Pram", "Travel")
	seat := mustAdd(t, st, "Car seat", "Travel")
	first, err := st.AddBundle(context.Background(), store.BundleInput{
		Name: "Sleep bundle", URL: "https://shop.example/sleep", Price: "100", RegularPrice: "150",
		Members: []store.BundleMemberInput{{ItemID: cot.ID, ComponentLabel: "Cot frame"}, {ItemID: pram.ID}},
	})
	if err != nil {
		t.Fatalf("AddBundle first: %v", err)
	}
	if _, err := st.ChooseBundle(context.Background(), first.ID); err != nil {
		t.Fatalf("ChooseBundle: %v", err)
	}
	if _, err := st.AddBundle(context.Background(), store.BundleInput{
		Name: "Travel bundle", URL: "https://shop.example/travel", Price: "99.99",
		Members: []store.BundleMemberInput{{ItemID: pram.ID}, {ItemID: seat.ID}},
	}); err != nil {
		t.Fatalf("AddBundle second: %v", err)
	}

	rec := do(t, s, http.MethodGet, "/", nil)
	assertStatus(t, rec, http.StatusOK)
	body := rec.Body.String()
	assertContains(t, body, `class="bundle-rail"`)
	assertContains(t, body, "Sleep bundle")
	assertContains(t, body, "https://shop.example/sleep")
	assertContains(t, body, "2 items · Chosen")
	assertContains(t, body, "Regular $150 · Save $50 (33%)")
	assertContains(t, body, "Cot frame")
	assertContains(t, body, "Pram")
	assertContains(t, body, "$50")
	assertOrder(t, body, "Sleep bundle", "Travel bundle")
	assertContains(t, body, `id="list"`)
	assertContains(t, body, `class="checklist"`)
}

func TestAdd(t *testing.T) {
	tests := []struct {
		name       string
		form       url.Values
		wantStatus int
		wantBody   string
	}{
		{
			name:       "valid item",
			form:       url.Values{"name": {"Pram"}, "category": {"Travel"}, "qty": {"1"}},
			wantStatus: http.StatusOK,
			wantBody:   "Pram",
		},
		{
			name:       "quantity above one is shown",
			form:       url.Values{"name": {"Bottles"}, "category": {"Feeding"}, "qty": {"6"}},
			wantStatus: http.StatusOK,
			wantBody:   "&times;6",
		},
		{
			name:       "blank name rejected",
			form:       url.Values{"name": {"  "}, "category": {"Travel"}},
			wantStatus: http.StatusBadRequest,
			wantBody:   "Give the item a name",
		},
		{
			name:       "unknown category rejected",
			form:       url.Values{"name": {"Pram"}, "category": {"Vehicles"}},
			wantStatus: http.StatusBadRequest,
			wantBody:   "Pick a category",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s, _ := newServer(t)
			rec := do(t, s, http.MethodPost, "/items", tt.form)
			assertStatus(t, rec, tt.wantStatus)
			assertContains(t, rec.Body.String(), tt.wantBody)
		})
	}
}

// The add response is the list fragment, not a whole page, so htmx can swap it.
func TestAddReturnsListFragment(t *testing.T) {
	s, _ := newServer(t)

	rec := do(t, s, http.MethodPost, "/items",
		url.Values{"name": {"Pram"}, "category": {"Travel"}})
	assertStatus(t, rec, http.StatusOK)

	body := rec.Body.String()
	assertContains(t, body, `id="list"`)
	assertNotContains(t, body, "<!doctype html>")
}

func TestAddItemSheet(t *testing.T) {
	s, _ := newServer(t)

	rec := do(t, s, http.MethodGet, "/", nil)
	assertStatus(t, rec, http.StatusOK)
	body := rec.Body.String()
	assertContains(t, body, `data-open-surface="#add-item-sheet"`)
	assertContains(t, body, `id="add-item-form"`)
	assertContains(t, body, `name="name"`)
	assertContains(t, body, `name="category"`)
	assertContains(t, body, `name="qty"`)
	start := strings.Index(body, `id="add-item-form"`)
	if start < 0 {
		t.Fatal("add item form missing")
	}
	end := strings.Index(body[start:], "</form>")
	if end < 0 {
		t.Fatal("add item form has no closing tag")
	}
	form := body[start : start+end]
	assertNotContains(t, form, `name="notes"`)
	assertNotContains(t, form, `name="budget"`)
	assertNotContains(t, form, `name="url"`)
}

func TestAddItemInvalidHTMXRendersFieldErrorWithoutSaving(t *testing.T) {
	tests := []struct {
		name      string
		form      url.Values
		field     string
		wantError string
	}{
		{name: "name", form: url.Values{"name": {"  "}, "category": {"Nursery"}, "qty": {"1"}}, field: "name", wantError: "Give the item a name."},
		{name: "category", form: url.Values{"name": {"Pram"}, "category": {"Vehicles"}, "qty": {"1"}}, field: "category", wantError: "Pick a category."},
		{name: "qty", form: url.Values{"name": {"Pram"}, "category": {"Travel"}, "qty": {"many"}}, field: "qty", wantError: "Qty must be a whole number greater than zero."},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s, st := newServer(t)
			rec := doHTMX(t, s, http.MethodPost, "/items", tt.form)
			assertStatus(t, rec, http.StatusOK)
			body := rec.Body.String()
			assertContains(t, body, `id="add-item-form"`)
			assertContains(t, body, `class="field-error" role="alert">`+tt.wantError+"</span>")
			assertContains(t, body, `class="surface-form__field has-error"`)
			items, err := st.List(context.Background())
			if err != nil {
				t.Fatalf("List: %v", err)
			}
			if len(items) != 0 {
				t.Fatalf("stored %d items after invalid add, want 0", len(items))
			}
		})
	}
}

func TestAddItemHTMXSuccessReturnsListForSheetClose(t *testing.T) {
	s, st := newServer(t)

	rec := doHTMX(t, s, http.MethodPost, "/items", url.Values{
		"name": {"Pram"}, "category": {"Travel"}, "qty": {"2"},
	})
	assertStatus(t, rec, http.StatusOK)
	assertContains(t, rec.Body.String(), `id="list"`)
	assertContains(t, rec.Body.String(), "Pram")
	if _, err := st.Get(context.Background(), 1); err != nil {
		t.Fatalf("Get added item: %v", err)
	}
}

func TestToggleSinksBoughtItemsWithinGroup(t *testing.T) {
	s, st := newServer(t)
	first := mustAdd(t, st, "Cot", "Nursery")
	mustAdd(t, st, "Sheets", "Nursery")
	mustAdd(t, st, "Monitor", "Nursery")

	rec := do(t, s, http.MethodPost, "/items/"+itoa(first.ID)+"/toggle", nil)
	assertStatus(t, rec, http.StatusOK)

	body := rec.Body.String()
	assertContains(t, body, "1 of 3 done")
	// Cot is bought, so it must now render after the two still needed.
	assertOrder(t, body, "Sheets", "Monitor", "Cot")

	// Toggling back restores it to the top of the group.
	rec = do(t, s, http.MethodPost, "/items/"+itoa(first.ID)+"/toggle", nil)
	assertStatus(t, rec, http.StatusOK)
	assertContains(t, rec.Body.String(), "0 of 3 done")
	assertOrder(t, rec.Body.String(), "Cot", "Sheets", "Monitor")
}

func TestDashboardShoppingTrail(t *testing.T) {
	s, st := newServer(t)
	bought := mustAddBudgetItem(t, st, store.ItemInput{Name: "Cot", Category: "Nursery", Budget: "$120"})
	needed := mustAddBudgetItem(t, st, store.ItemInput{Name: "Sheets", Category: "Nursery", Budget: "$25"})
	mustAdd(t, st, "Pram", "Travel")
	option, err := st.AddOption(context.Background(), bought.ID, store.OptionInput{URL: "https://shop.example/cot", Price: "$75"})
	if err != nil {
		t.Fatalf("AddOption: %v", err)
	}
	if _, err := st.ChooseOption(context.Background(), option.ID); err != nil {
		t.Fatalf("ChooseOption: %v", err)
	}

	rec := do(t, s, http.MethodGet, "/", nil)
	assertStatus(t, rec, http.StatusOK)
	body := rec.Body.String()
	assertContains(t, body, `class="dashboard"`)
	assertContains(t, body, "1 of 3")
	assertContains(t, body, `aria-label="1 Done of 3 Total"`)
	assertContains(t, body, `aria-label="Actual spend"`)
	assertContains(t, body, `<span>Actual</span><strong>$75</strong>`)
	assertContains(t, body, `<span>Still planned</span><strong>$25</strong>`)
	assertContains(t, body, `<span>Expected total</span><strong>$100</strong>`)
	assertContains(t, body, "1 item needs a budget")
	assertContains(t, body, `class="group trail-stop"`)
	assertContains(t, body, `class="item trail-item is-bought"`)
	assertOrder(t, body, "Sheets", "Cot")
	assertOrder(t, body, "Nursery", "Travel")

	rec = do(t, s, http.MethodPost, "/items/"+itoa(needed.ID)+"/toggle", nil)
	assertStatus(t, rec, http.StatusOK)
	assertContains(t, rec.Body.String(), `id="list"`)
	assertContains(t, rec.Body.String(), `aria-label="2 of 3 done"`)
}

func TestDetailPanel(t *testing.T) {
	s, st := newServer(t)
	it := mustAdd(t, st, "Cot", "Nursery")

	rec := do(t, s, http.MethodGet, "/items/"+itoa(it.ID), nil)
	assertStatus(t, rec, http.StatusOK)

	body := rec.Body.String()
	assertContains(t, body, "is-open")
	assertContains(t, body, "Cot")
	assertContains(t, body, "No options yet")
	assertContains(t, body, "Edit item")
	assertContains(t, body, "Delete item")
	// The edit form only appears once asked for.
	assertNotContains(t, body, `name="category"`)

	// Closing swaps the collapsed row back in.
	rec = do(t, s, http.MethodGet, "/items/"+itoa(it.ID)+"/row", nil)
	assertStatus(t, rec, http.StatusOK)
	body = rec.Body.String()
	assertContains(t, body, `id="item-`+itoa(it.ID)+`"`)
	assertNotContains(t, body, "is-open")
}

func TestItemWorkspaceAndOpenTarget(t *testing.T) {
	s, st := newServer(t)
	it, err := st.Add(context.Background(), store.ItemInput{
		Name: "Cot", Category: "Nursery", Qty: 2, Notes: "Keep it snug", Budget: "$500",
	})
	if err != nil {
		t.Fatalf("Add: %v", err)
	}

	rec := do(t, s, http.MethodGet, "/", nil)
	assertStatus(t, rec, http.StatusOK)
	assertContains(t, rec.Body.String(), `hx-target="#workspace-content"`)
	assertContains(t, rec.Body.String(), `data-open-surface="#workspace"`)

	rec = do(t, s, http.MethodGet, "/items/"+itoa(it.ID), nil)
	assertStatus(t, rec, http.StatusOK)
	body := rec.Body.String()
	assertContains(t, body, `id="item-`+itoa(it.ID)+`" class="item-workspace is-open"`)
	assertContains(t, body, "Cot")
	assertContains(t, body, "Nursery")
	assertContains(t, body, "Qty")
	assertContains(t, body, "2")
	assertContains(t, body, "Needed")
	assertContains(t, body, "Budget")
	assertContains(t, body, "$500")
	assertContains(t, body, "Edit item")
	assertContains(t, body, "Delete item")
	assertNotContains(t, body, "<!doctype html>")
}

func TestItemWorkspaceShowsActualSpend(t *testing.T) {
	s, st := newServer(t)
	it := mustAdd(t, st, "Cot", "Nursery")
	op := mustAddOption(t, st, it.ID, store.OptionInput{URL: "https://shop.example/cot", Price: "$425"})
	if _, err := st.ChooseOption(context.Background(), op.ID); err != nil {
		t.Fatalf("ChooseOption: %v", err)
	}

	rec := do(t, s, http.MethodGet, "/items/"+itoa(it.ID), nil)
	assertStatus(t, rec, http.StatusOK)
	body := rec.Body.String()
	assertContains(t, body, "Bought")
	assertContains(t, body, "Actual spend")
	assertContains(t, body, "$425")
}

func TestEditItemWorkspaceValidationAndSave(t *testing.T) {
	s, st := newServer(t)
	it := mustAdd(t, st, "Cot", "Nursery")

	rec := do(t, s, http.MethodGet, "/items/"+itoa(it.ID)+"/edit", nil)
	assertStatus(t, rec, http.StatusOK)
	body := rec.Body.String()
	assertContains(t, body, `id="edit-item-form"`)
	for _, field := range []string{`name="name"`, `name="category"`, `name="qty"`, `name="notes"`, `name="budget"`} {
		assertContains(t, body, field)
	}
	assertContains(t, body, `data-close-on-success="true"`)

	invalidEdits := []struct {
		form url.Values
		msg  string
	}{
		{url.Values{"name": {""}, "category": {"Nursery"}, "qty": {"1"}}, "Give the item a name."},
		{url.Values{"name": {"Cot"}, "category": {"Nursery"}, "qty": {"many"}}, "Qty must be a whole number greater than zero."},
		{url.Values{"name": {"Cot"}, "category": {"Nursery"}, "qty": {"1"}, "budget": {"bad"}}, "Budget should be a number."},
	}
	for _, invalid := range invalidEdits {
		rec = doHTMX(t, s, http.MethodPost, "/items/"+itoa(it.ID), invalid.form)
		assertStatus(t, rec, http.StatusOK)
		assertContains(t, rec.Body.String(), `id="edit-item-form"`)
		assertContains(t, rec.Body.String(), invalid.msg)
	}
	if got, err := st.Get(context.Background(), it.ID); err != nil || got.Name != "Cot" {
		t.Fatalf("item after invalid edit = %#v, %v", got, err)
	}

	rec = do(t, s, http.MethodPost, "/items/"+itoa(it.ID), url.Values{
		"name": {"Cot mattress"}, "category": {"Travel"}, "qty": {"3"}, "notes": {"firm"}, "budget": {"$1,200"},
	})
	assertStatus(t, rec, http.StatusOK)
	assertContains(t, rec.Body.String(), `id="list"`)
	assertContains(t, rec.Body.String(), "Cot mattress")
	assertNotContains(t, rec.Body.String(), `id="workspace"`)
	updated, err := st.Get(context.Background(), it.ID)
	if err != nil {
		t.Fatalf("Get updated item: %v", err)
	}
	if updated.Name != "Cot mattress" || updated.Category != "Travel" || updated.Qty != 3 || updated.Notes != "firm" || updated.BudgetText() != "$1,200" {
		t.Fatalf("updated item = %#v", updated)
	}
}

func TestDeleteItemWorkspaceRefreshesList(t *testing.T) {
	s, st := newServer(t)
	it := mustAdd(t, st, "Cot", "Nursery")

	rec := do(t, s, http.MethodGet, "/items/"+itoa(it.ID), nil)
	assertStatus(t, rec, http.StatusOK)
	assertContains(t, rec.Body.String(), `data-close-on-success="true"`)

	rec = do(t, s, http.MethodPost, "/items/"+itoa(it.ID)+"/delete", nil)
	assertStatus(t, rec, http.StatusOK)
	assertContains(t, rec.Body.String(), `id="list"`)
	assertNotContains(t, rec.Body.String(), "Cot")
}

func TestEditFormAndCancel(t *testing.T) {
	s, st := newServer(t)
	it := mustAdd(t, st, "Cot", "Nursery")

	rec := do(t, s, http.MethodGet, "/items/"+itoa(it.ID)+"/edit", nil)
	assertStatus(t, rec, http.StatusOK)

	body := rec.Body.String()
	assertContains(t, body, `value="Cot"`)
	assertContains(t, body, `<option value="Nursery" selected>`)
	assertContains(t, body, "Save")

	// Cancelling returns to the detail panel, not the bare row.
	rec = do(t, s, http.MethodGet, "/items/"+itoa(it.ID), nil)
	assertStatus(t, rec, http.StatusOK)
	body = rec.Body.String()
	assertContains(t, body, "is-open")
	assertNotContains(t, body, `name="category"`)
}

func TestEditBudget(t *testing.T) {
	s, st := newServer(t)
	it, err := st.Add(context.Background(), store.ItemInput{
		Name: "Cot", Category: "Nursery", Budget: "$1,200.50",
	})
	if err != nil {
		t.Fatalf("Add: %v", err)
	}

	rec := do(t, s, http.MethodGet, "/items/"+itoa(it.ID)+"/edit", nil)
	assertStatus(t, rec, http.StatusOK)
	assertContains(t, rec.Body.String(), `name="budget" value="$1,200.50"`)

	rec = do(t, s, http.MethodPost, "/items/"+itoa(it.ID), url.Values{
		"name": {"Cot"}, "category": {"Nursery"}, "budget": {"$1,500"},
	})
	assertStatus(t, rec, http.StatusOK)
	stored, err := st.Get(context.Background(), it.ID)
	if err != nil {
		t.Fatalf("Get set budget: %v", err)
	}
	if stored.BudgetText() != "$1,500" {
		t.Errorf("set budget = %q, want $1,500", stored.BudgetText())
	}

	rec = do(t, s, http.MethodPost, "/items/"+itoa(it.ID), url.Values{
		"name": {"Cot"}, "category": {"Nursery"}, "budget": {"lots"},
	})
	assertStatus(t, rec, http.StatusBadRequest)
	assertContains(t, rec.Body.String(), "Budget should be a number")
	stored, err = st.Get(context.Background(), it.ID)
	if err != nil {
		t.Fatalf("Get after invalid budget: %v", err)
	}
	if stored.BudgetText() != "$1,500" {
		t.Errorf("budget after invalid update = %q, want $1,500", stored.BudgetText())
	}

	rec = do(t, s, http.MethodPost, "/items/"+itoa(it.ID), url.Values{
		"name": {"Cot"}, "category": {"Nursery"}, "budget": {""},
	})
	assertStatus(t, rec, http.StatusOK)
	stored, err = st.Get(context.Background(), it.ID)
	if err != nil {
		t.Fatalf("Get cleared budget: %v", err)
	}
	if stored.BudgetCents != nil {
		t.Errorf("cleared budget = %v, want nil", stored.BudgetCents)
	}
}

func TestUpdate(t *testing.T) {
	s, st := newServer(t)
	it := mustAdd(t, st, "Cot", "Nursery")

	rec := do(t, s, http.MethodPost, "/items/"+itoa(it.ID), url.Values{
		"name": {"Cot mattress"}, "category": {"Feeding"}, "qty": {"2"}, "notes": {"firm"},
	})
	assertStatus(t, rec, http.StatusOK)

	body := rec.Body.String()
	assertContains(t, body, "Cot mattress")
	assertContains(t, body, "firm")
	assertContains(t, body, "&times;2")
	// It moved category, so it renders under the new heading.
	assertOrder(t, body, "Feeding", "Cot mattress")
	assertNotContains(t, body, "Nursery</h2>")
}

func TestDelete(t *testing.T) {
	s, st := newServer(t)
	it := mustAdd(t, st, "Cot", "Nursery")
	mustAdd(t, st, "Pram", "Travel")

	rec := do(t, s, http.MethodPost, "/items/"+itoa(it.ID)+"/delete", nil)
	assertStatus(t, rec, http.StatusOK)

	body := rec.Body.String()
	assertNotContains(t, body, "Cot")
	assertContains(t, body, "Pram")
	assertContains(t, body, "0 of 1 done")
}

func TestMissingItems(t *testing.T) {
	tests := []struct {
		method, path string
	}{
		{http.MethodGet, "/items/9999"},
		{http.MethodGet, "/items/9999/edit"},
		{http.MethodPost, "/items/9999/toggle"},
		{http.MethodPost, "/items/9999/delete"},
		{http.MethodGet, "/items/not-a-number"},
		{http.MethodPost, "/items/not-a-number/toggle"},
	}

	for _, tt := range tests {
		t.Run(tt.method+" "+tt.path, func(t *testing.T) {
			s, _ := newServer(t)
			rec := do(t, s, tt.method, tt.path, nil)
			assertStatus(t, rec, http.StatusNotFound)
		})
	}
}

func TestMissingOptions(t *testing.T) {
	tests := []struct{ method, path string }{
		{http.MethodPost, "/options/9999/choose"},
		{http.MethodPost, "/options/9999/delete"},
		{http.MethodPost, "/options/not-a-number/choose"},
		{http.MethodPost, "/options/not-a-number/delete"},
	}

	for _, tt := range tests {
		t.Run(tt.method+" "+tt.path, func(t *testing.T) {
			s, _ := newServer(t)
			rec := do(t, s, tt.method, tt.path, nil)
			assertStatus(t, rec, http.StatusNotFound)
		})
	}
}

func TestUpdateMissingItem(t *testing.T) {
	s, _ := newServer(t)
	rec := do(t, s, http.MethodPost, "/items/9999",
		url.Values{"name": {"Cot"}, "category": {"Nursery"}})
	assertStatus(t, rec, http.StatusNotFound)
}

func TestHealthz(t *testing.T) {
	s, _ := newServer(t)
	rec := do(t, s, http.MethodGet, "/healthz", nil)
	assertStatus(t, rec, http.StatusOK)
	if got := strings.TrimSpace(rec.Body.String()); got != "ok" {
		t.Errorf("body = %q, want %q", got, "ok")
	}
}

func TestStaticIsServed(t *testing.T) {
	s, _ := newServer(t)
	rec := do(t, s, http.MethodGet, "/static/app.css", nil)
	assertStatus(t, rec, http.StatusOK)
}

// Names are escaped, not injected raw into the page.
func TestItemNamesAreEscaped(t *testing.T) {
	s, st := newServer(t)
	mustAdd(t, st, `<script>alert(1)</script>`, "Other")

	rec := do(t, s, http.MethodGet, "/", nil)
	assertStatus(t, rec, http.StatusOK)
	assertNotContains(t, rec.Body.String(), "<script>alert(1)</script>")
}

// assertOrder checks the needles appear in the body in the given order.
func assertOrder(t *testing.T, body string, needles ...string) {
	t.Helper()

	at := 0
	for _, n := range needles {
		i := strings.Index(body[at:], n)
		if i < 0 {
			t.Fatalf("%q does not appear after the preceding items %v", n, needles)
		}
		at += i + len(n)
	}
}

func itoa(id int64) string {
	return strconv.FormatInt(id, 10)
}
