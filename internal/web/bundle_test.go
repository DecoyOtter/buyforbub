package web

import (
	"context"
	"net/http"
	"net/url"
	"testing"

	"github.com/DecoyOtter/buyforbub/internal/store"
)

func TestBundleCreateForm(t *testing.T) {
	s, st := newServer(t)
	cot := mustAdd(t, st, "Cot", "Nursery")
	pram := mustAdd(t, st, "Pram", "Travel")
	chosen := mustAddOption(t, st, pram.ID, store.OptionInput{URL: "https://shop.example/pram", Label: "City pram"})
	if _, err := st.ChooseOption(context.Background(), chosen.ID); err != nil {
		t.Fatalf("ChooseOption: %v", err)
	}

	rec := do(t, s, http.MethodGet, "/", nil)
	assertStatus(t, rec, http.StatusOK)
	body := rec.Body.String()
	assertContains(t, body, "Add Bundle")
	assertContains(t, body, `hx-post="/bundles"`)
	assertContains(t, body, `name="regular_price"`)
	assertContains(t, body, "The Bundle price is split equally per Item.")
	assertContains(t, body, `value="`+itoa(cot.ID)+`"`)
	assertContains(t, body, `name="component_label_`+itoa(cot.ID)+`"`)
	assertContains(t, body, "Nursery")
	assertContains(t, body, "Travel")
	assertContains(t, body, "bought · City pram")
}

func TestAddBundle(t *testing.T) {
	tests := []struct {
		name       string
		form       url.Values
		wantStatus int
		wantBody   string
	}{
		{
			name: "creates bundle and refreshes list",
			form: url.Values{
				"name": {"Sleep bundle"}, "url": {"https://shop.example/sleep"}, "price": {"99.99"},
				"item_id": {"1", "2"}, "component_label_1": {"Cot frame"},
			},
			wantStatus: http.StatusOK, wantBody: "Sleep bundle",
		},
		{
			name:       "requires two items",
			form:       url.Values{"name": {"Sleep bundle"}, "url": {"https://shop.example/sleep"}, "price": {"99.99"}, "item_id": {"1"}},
			wantStatus: http.StatusBadRequest, wantBody: "Choose at least two different items.",
		},
		{
			name:       "requires absolute url",
			form:       url.Values{"name": {"Sleep bundle"}, "url": {"shop.example/sleep"}, "price": {"99.99"}, "item_id": {"1", "2"}},
			wantStatus: http.StatusBadRequest, wantBody: "That does not look like a link.",
		},
		{
			name:       "requires valid price",
			form:       url.Values{"name": {"Sleep bundle"}, "url": {"https://shop.example/sleep"}, "price": {"99.999"}, "item_id": {"1", "2"}},
			wantStatus: http.StatusBadRequest, wantBody: "Bundle price must be greater than zero with at most two decimal places.",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s, st := newServer(t)
			first := mustAdd(t, st, "Cot", "Nursery")
			second := mustAdd(t, st, "Pram", "Travel")
			if values := tt.form["item_id"]; len(values) != 0 {
				tt.form["item_id"] = []string{itoa(first.ID), itoa(second.ID)}[:len(values)]
			}

			rec := do(t, s, http.MethodPost, "/bundles", tt.form)
			assertStatus(t, rec, tt.wantStatus)
			assertContains(t, rec.Body.String(), tt.wantBody)
			if tt.wantStatus == http.StatusOK {
				bundles, err := st.ListBundles(context.Background())
				if err != nil {
					t.Fatalf("ListBundles: %v", err)
				}
				if len(bundles) != 1 || len(bundles[0].Members) != 2 {
					t.Fatalf("bundles = %#v, want one with two members", bundles)
				}
			} else {
				bundles, err := st.ListBundles(context.Background())
				if err != nil {
					t.Fatalf("ListBundles: %v", err)
				}
				if len(bundles) != 0 {
					t.Fatalf("bundles = %#v, want none", bundles)
				}
			}
		})
	}
}

func TestBundleValuesAreEscaped(t *testing.T) {
	s, st := newServer(t)
	first := mustAdd(t, st, "Cot", "Nursery")
	second := mustAdd(t, st, "Pram", "Travel")
	rec := do(t, s, http.MethodPost, "/bundles", url.Values{
		"name": {`<script>alert(1)</script>`}, "url": {"https://shop.example/sleep"}, "price": {"100"},
		"item_id": {itoa(first.ID), itoa(second.ID)},
	})
	assertStatus(t, rec, http.StatusOK)
	assertNotContains(t, rec.Body.String(), "<script>alert(1)</script>")
}

func TestUpdateBundle(t *testing.T) {
	s, st := newServer(t)
	cot := mustAdd(t, st, "Cot", "Nursery")
	pram := mustAdd(t, st, "Pram", "Travel")
	seat := mustAdd(t, st, "Car seat", "Travel")
	bundle, err := st.AddBundle(context.Background(), store.BundleInput{
		Name: "Sleep bundle", URL: "https://shop.example/sleep", Price: "100", RegularPrice: "150",
		Members: []store.BundleMemberInput{{ItemID: cot.ID, ComponentLabel: "Frame"}, {ItemID: pram.ID}},
	})
	if err != nil {
		t.Fatalf("AddBundle: %v", err)
	}
	if _, err := st.ChooseBundle(context.Background(), bundle.ID); err != nil {
		t.Fatalf("ChooseBundle: %v", err)
	}

	rec := do(t, s, http.MethodGet, "/", nil)
	assertStatus(t, rec, http.StatusOK)
	body := rec.Body.String()
	assertContains(t, body, `hx-get="/bundles/`+itoa(bundle.ID)+`"`)

	rec = do(t, s, http.MethodGet, "/bundles/"+itoa(bundle.ID), nil)
	assertStatus(t, rec, http.StatusOK)
	body = rec.Body.String()
	assertContains(t, body, "Sleep bundle")
	assertContains(t, body, "Frame")
	assertContains(t, body, "$50")
	assertNotContains(t, body, `id="list"`)

	rec = do(t, s, http.MethodPost, "/bundles/"+itoa(bundle.ID), url.Values{
		"name": {"Travel bundle"}, "url": {"https://shop.example/travel"}, "price": {"120"},
		"item_id": {itoa(cot.ID), itoa(seat.ID)}, "component_label_" + itoa(seat.ID): {"Seat"},
	})
	assertStatus(t, rec, http.StatusOK)
	assertContains(t, rec.Body.String(), "Travel bundle")
	updated, err := st.GetBundle(context.Background(), bundle.ID)
	if err != nil {
		t.Fatalf("GetBundle: %v", err)
	}
	if len(updated.Members) != 2 || updated.Members[1].ItemID != seat.ID || updated.Members[1].ComponentLabel != "Seat" {
		t.Fatalf("members = %#v", updated.Members)
	}
	for _, id := range []int64{cot.ID, pram.ID} {
		item, err := st.Get(context.Background(), id)
		if err != nil || item.Bought() {
			t.Fatalf("item %d after update = %#v, %v", id, item, err)
		}
	}
}

func TestDeleteBundle(t *testing.T) {
	s, st := newServer(t)
	first := mustAdd(t, st, "Cot", "Nursery")
	second := mustAdd(t, st, "Pram", "Travel")
	bundle, err := st.AddBundle(context.Background(), store.BundleInput{
		Name: "Travel bundle", URL: "https://shop.example/travel", Price: "100",
		Members: []store.BundleMemberInput{{ItemID: first.ID}, {ItemID: second.ID}},
	})
	if err != nil {
		t.Fatalf("AddBundle: %v", err)
	}
	rec := do(t, s, http.MethodPost, "/bundles/"+itoa(bundle.ID)+"/delete", nil)
	assertStatus(t, rec, http.StatusOK)
	assertNotContains(t, rec.Body.String(), "Travel bundle")
	if _, err := st.GetBundle(context.Background(), bundle.ID); err != store.ErrNotFound {
		t.Fatalf("GetBundle error = %v, want ErrNotFound", err)
	}
}

func TestBundleMutationMissingOrMalformedID(t *testing.T) {
	s, _ := newServer(t)
	for _, request := range []struct {
		method string
		path   string
	}{
		{http.MethodGet, "/bundles/nope"}, {http.MethodPost, "/bundles/nope/delete"},
		{http.MethodGet, "/bundles/99"}, {http.MethodPost, "/bundles/99/delete"},
	} {
		rec := do(t, s, request.method, request.path, url.Values{"item_id": {"1", "2"}})
		assertStatus(t, rec, http.StatusNotFound)
	}
}

func TestBundleWorkspaceRendersWithoutChangingData(t *testing.T) {
	s, st := newServer(t)
	first := mustAdd(t, st, "Cot", "Nursery")
	second := mustAdd(t, st, "Pram", "Travel")
	bundle, err := st.AddBundle(context.Background(), store.BundleInput{
		Name: "Sleep bundle", URL: "https://shop.example/sleep", Price: "100", RegularPrice: "150",
		Members: []store.BundleMemberInput{{ItemID: first.ID, ComponentLabel: "Cot frame"}, {ItemID: second.ID}},
	})
	if err != nil {
		t.Fatalf("AddBundle: %v", err)
	}

	rec := do(t, s, http.MethodGet, "/bundles/"+itoa(bundle.ID), nil)
	assertStatus(t, rec, http.StatusOK)
	body := rec.Body.String()
	assertContains(t, body, `id="bundle-`+itoa(bundle.ID)+`"`)
	assertContains(t, body, "Sleep bundle")
	assertContains(t, body, "$100")
	assertContains(t, body, "Includes 2 Items")
	assertContains(t, body, "Cot frame")
	assertContains(t, body, "$50")
	assertContains(t, body, "Save $50 (33%)")
	assertContains(t, body, `href="https://shop.example/sleep"`)
	assertNotContains(t, body, `id="list"`)

	got, err := st.GetBundle(context.Background(), bundle.ID)
	if err != nil || got.Name != bundle.Name || len(got.Members) != 2 {
		t.Fatalf("bundle after workspace GET = %#v, %v", got, err)
	}
}
