package web

import (
	"context"
	"net/http"
	"net/url"
	"testing"

	"github.com/DecoyOtter/buyforbub/internal/store"
)

func mustAddOption(t *testing.T, st *store.Store, itemID int64, in store.OptionInput) store.Option {
	t.Helper()
	o, err := st.AddOption(context.Background(), itemID, in)
	if err != nil {
		t.Fatalf("AddOption: %v", err)
	}
	return o
}

func TestAddOption(t *testing.T) {
	tests := []struct {
		name       string
		form       url.Values
		wantStatus int
		wantBody   string
	}{
		{
			name:       "link with label and price",
			form:       url.Values{"url": {"https://shop.example.com/cot"}, "label": {"Boori Daintree"}, "price": {"$1,199"}},
			wantStatus: http.StatusOK,
			wantBody:   "Boori Daintree",
		},
		{
			name:       "price is formatted",
			form:       url.Values{"url": {"https://shop.example.com/cot"}, "price": {"1199"}},
			wantStatus: http.StatusOK,
			wantBody:   "$1,199",
		},
		{
			name:       "bare host is accepted",
			form:       url.Values{"url": {"shop.example.com/cot"}},
			wantStatus: http.StatusOK,
			wantBody:   `href="https://shop.example.com/cot"`,
		},
		{
			name:       "no label falls back to the host",
			form:       url.Values{"url": {"https://www.ikea.com/au/sundvik"}},
			wantStatus: http.StatusOK,
			wantBody:   "ikea.com",
		},
		{
			name:       "blank link rejected",
			form:       url.Values{"url": {"  "}},
			wantStatus: http.StatusBadRequest,
			wantBody:   "does not look like a link",
		},
		{
			name:       "javascript url rejected",
			form:       url.Values{"url": {"javascript:alert(1)"}},
			wantStatus: http.StatusBadRequest,
			wantBody:   "does not look like a link",
		},
		{
			name:       "unparseable price rejected",
			form:       url.Values{"url": {"https://example.com"}, "price": {"heaps"}},
			wantStatus: http.StatusBadRequest,
			wantBody:   "Price should be a number",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s, st := newServer(t)
			it := mustAdd(t, st, "Cot", "Nursery")

			rec := do(t, s, http.MethodPost, "/items/"+itoa(it.ID)+"/options", tt.form)
			assertStatus(t, rec, tt.wantStatus)
			assertContains(t, rec.Body.String(), tt.wantBody)
		})
	}
}

// Adding an option leaves the panel open so more can be pasted in.
func TestAddOptionReturnsOpenPanel(t *testing.T) {
	s, st := newServer(t)
	it := mustAdd(t, st, "Cot", "Nursery")

	rec := do(t, s, http.MethodPost, "/items/"+itoa(it.ID)+"/options",
		url.Values{"url": {"https://a.example.com"}})
	assertStatus(t, rec, http.StatusOK)

	body := rec.Body.String()
	assertContains(t, body, "is-open")
	assertContains(t, body, "Choose")
	assertNotContains(t, body, "No options yet")
	assertNotContains(t, body, "<!doctype html>")
}

func TestOptionCountShownOnRow(t *testing.T) {
	s, st := newServer(t)
	it := mustAdd(t, st, "Cot", "Nursery")

	rec := do(t, s, http.MethodGet, "/", nil)
	assertNotContains(t, rec.Body.String(), "item__links")

	mustAddOption(t, st, it.ID, store.OptionInput{URL: "https://a.example.com"})
	rec = do(t, s, http.MethodGet, "/", nil)
	assertContains(t, rec.Body.String(), "1 option<")

	mustAddOption(t, st, it.ID, store.OptionInput{URL: "https://b.example.com"})
	rec = do(t, s, http.MethodGet, "/", nil)
	assertContains(t, rec.Body.String(), "2 options<")
}

func TestChooseOptionMarksItemBought(t *testing.T) {
	s, st := newServer(t)
	it := mustAdd(t, st, "Cot", "Nursery")
	mustAdd(t, st, "Sheets", "Nursery")
	opt := mustAddOption(t, st, it.ID, store.OptionInput{URL: "https://a.example.com", Label: "Boori"})

	rec := do(t, s, http.MethodPost, "/options/"+itoa(opt.ID)+"/choose", nil)
	assertStatus(t, rec, http.StatusOK)

	body := rec.Body.String()
	// The list comes back, with the item bought and sunk below the one still needed.
	assertContains(t, body, `id="list"`)
	assertContains(t, body, "1 of 2 done")
	assertOrder(t, body, "Sheets", "Cot")

	// Reopening shows which option was the one bought.
	rec = do(t, s, http.MethodGet, "/items/"+itoa(it.ID), nil)
	body = rec.Body.String()
	assertContains(t, body, "Bought this")
	assertNotContains(t, body, "Choose</button>")
}

// Marking the item needed again must clear the "we bought this one" badge.
func TestUntogglingClearsChosenBadge(t *testing.T) {
	s, st := newServer(t)
	it := mustAdd(t, st, "Cot", "Nursery")
	opt := mustAddOption(t, st, it.ID, store.OptionInput{URL: "https://a.example.com"})

	do(t, s, http.MethodPost, "/options/"+itoa(opt.ID)+"/choose", nil)
	do(t, s, http.MethodPost, "/items/"+itoa(it.ID)+"/toggle", nil)

	rec := do(t, s, http.MethodGet, "/items/"+itoa(it.ID), nil)
	assertStatus(t, rec, http.StatusOK)
	assertNotContains(t, rec.Body.String(), "Bought this")
}

func TestDeleteOption(t *testing.T) {
	s, st := newServer(t)
	it := mustAdd(t, st, "Cot", "Nursery")
	mustAddOption(t, st, it.ID, store.OptionInput{URL: "https://keep.example.com"})
	drop := mustAddOption(t, st, it.ID, store.OptionInput{URL: "https://drop.example.com"})

	rec := do(t, s, http.MethodPost, "/options/"+itoa(drop.ID)+"/delete", nil)
	assertStatus(t, rec, http.StatusOK)

	body := rec.Body.String()
	// The panel stays open, on the right item, minus the removed option.
	assertContains(t, body, "is-open")
	assertContains(t, body, `id="item-`+itoa(it.ID)+`"`)
	assertContains(t, body, "keep.example.com")
	assertNotContains(t, body, "drop.example.com")
}

func TestBundleOptionsRenderAndChooseAsBundle(t *testing.T) {
	s, st := newServer(t)
	cot := mustAdd(t, st, "Cot", "Nursery")
	pram := mustAdd(t, st, "Pram", "Travel")
	normal := mustAddOption(t, st, cot.ID, store.OptionInput{URL: "https://shop.example/normal", Label: "Normal cot"})
	if _, err := st.ChooseOption(context.Background(), normal.ID); err != nil {
		t.Fatalf("ChooseOption: %v", err)
	}
	bundle, err := st.AddBundle(context.Background(), store.BundleInput{
		Name: "Sleep bundle", URL: "https://shop.example/sleep", Price: "100",
		Members: []store.BundleMemberInput{{ItemID: cot.ID, ComponentLabel: "Cot frame"}, {ItemID: pram.ID}},
	})
	if err != nil {
		t.Fatalf("AddBundle: %v", err)
	}

	rec := do(t, s, http.MethodGet, "/items/"+itoa(cot.ID), nil)
	assertStatus(t, rec, http.StatusOK)
	body := rec.Body.String()
	assertContains(t, body, "Bundle")
	assertContains(t, body, "Cot frame")
	assertContains(t, body, `href="https://shop.example/sleep"`)
	assertContains(t, body, "Sleep bundle · $50 share of $100")
	assertContains(t, body, `hx-post="/bundles/`+itoa(bundle.ID)+`/choose"`)
	assertNotContains(t, body, `aria-label="Remove Cot frame"`)
	assertContains(t, body, "This replaces Normal cot.")

	rec = do(t, s, http.MethodPost, "/bundles/"+itoa(bundle.ID)+"/choose", nil)
	assertStatus(t, rec, http.StatusOK)
	assertContains(t, rec.Body.String(), "2 of 2 done")

	rec = do(t, s, http.MethodGet, "/items/"+itoa(pram.ID), nil)
	assertStatus(t, rec, http.StatusOK)
	assertContains(t, rec.Body.String(), "Bought this")
}

func TestNormalChoiceNamesBrokenBundle(t *testing.T) {
	s, st := newServer(t)
	cot := mustAdd(t, st, "Cot", "Nursery")
	pram := mustAdd(t, st, "Pram", "Travel")
	normal := mustAddOption(t, st, cot.ID, store.OptionInput{URL: "https://shop.example/normal", Label: "Normal cot"})
	bundle, err := st.AddBundle(context.Background(), store.BundleInput{
		Name: "Sleep bundle", URL: "https://shop.example/sleep", Price: "100",
		Members: []store.BundleMemberInput{{ItemID: cot.ID}, {ItemID: pram.ID}},
	})
	if err != nil {
		t.Fatalf("AddBundle: %v", err)
	}
	if _, err := st.ChooseBundle(context.Background(), bundle.ID); err != nil {
		t.Fatalf("ChooseBundle: %v", err)
	}

	rec := do(t, s, http.MethodGet, "/items/"+itoa(cot.ID), nil)
	assertStatus(t, rec, http.StatusOK)
	assertContains(t, rec.Body.String(), "Choosing this Option will unchoose Sleep bundle and mark Cot and Pram needed.")
	assertContains(t, rec.Body.String(), `hx-post="/options/`+itoa(normal.ID)+`/choose"`)
}

// html/template must neutralise a dangerous href even if one reaches the DB.
func TestOptionURLIsEscaped(t *testing.T) {
	s, st := newServer(t)
	it := mustAdd(t, st, "Cot", "Nursery")
	mustAddOption(t, st, it.ID, store.OptionInput{
		URL:   "https://example.com/\"><script>alert(1)</script>",
		Label: "<script>alert(2)</script>",
	})

	rec := do(t, s, http.MethodGet, "/items/"+itoa(it.ID), nil)
	assertStatus(t, rec, http.StatusOK)

	body := rec.Body.String()
	assertNotContains(t, body, "<script>alert(1)</script>")
	assertNotContains(t, body, "<script>alert(2)</script>")
}
