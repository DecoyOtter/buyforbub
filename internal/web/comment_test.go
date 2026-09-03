package web

import (
	"context"
	"net/http"
	"net/url"
	"testing"

	"github.com/DecoyOtter/buyforbub/internal/store"
)

func mustAddComment(t *testing.T, st *store.Store, optionID int64, body string) store.Comment {
	t.Helper()
	c, err := st.AddComment(context.Background(), optionID, body)
	if err != nil {
		t.Fatalf("AddComment: %v", err)
	}
	return c
}

func TestAddComment(t *testing.T) {
	tests := []struct {
		name       string
		form       url.Values
		wantStatus int
		wantBody   string
	}{
		{
			name:       "comment is shown under the option",
			form:       url.Values{"body": {"Good rounded edges for leaning"}},
			wantStatus: http.StatusOK,
			wantBody:   "Good rounded edges for leaning",
		},
		{
			name:       "blank comment rejected",
			form:       url.Values{"body": {"   "}},
			wantStatus: http.StatusBadRequest,
			wantBody:   "Write something first",
		},
		{
			name:       "missing comment rejected",
			form:       url.Values{},
			wantStatus: http.StatusBadRequest,
			wantBody:   "Write something first",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s, st := newServer(t)
			it := mustAdd(t, st, "Cot", "Nursery")
			opt := mustAddOption(t, st, it.ID, store.OptionInput{URL: "https://a.example.com"})

			rec := do(t, s, http.MethodPost, "/options/"+itoa(opt.ID)+"/comments", tt.form)
			assertStatus(t, rec, tt.wantStatus)
			assertContains(t, rec.Body.String(), tt.wantBody)
		})
	}
}

// Commenting returns the whole panel, still open, so more can be typed straight in.
func TestAddCommentReturnsOpenPanel(t *testing.T) {
	s, st := newServer(t)
	it := mustAdd(t, st, "Cot", "Nursery")
	opt := mustAddOption(t, st, it.ID, store.OptionInput{URL: "https://a.example.com"})

	rec := do(t, s, http.MethodPost, "/options/"+itoa(opt.ID)+"/comments",
		url.Values{"body": {"too expensive"}})
	assertStatus(t, rec, http.StatusOK)

	body := rec.Body.String()
	assertContains(t, body, `id="item-`+itoa(it.ID)+`"`)
	assertContains(t, body, "is-open")
	assertContains(t, body, "Add a comment")
	assertNotContains(t, body, "<!doctype html>")
}

func TestAddCommentInvalidHTMXRendersFieldError(t *testing.T) {
	s, st := newServer(t)
	it := mustAdd(t, st, "Cot", "Nursery")
	opt := mustAddOption(t, st, it.ID, store.OptionInput{URL: "https://a.example.com"})

	rec := doHTMX(t, s, http.MethodPost, "/options/"+itoa(opt.ID)+"/comments", url.Values{"body": {"   "}})
	assertStatus(t, rec, http.StatusOK)
	assertContains(t, rec.Header().Get("HX-Retarget"), "#workspace-content")
	assertContains(t, rec.Header().Get("HX-Trigger"), "comment-invalid")
	assertContains(t, rec.Body.String(), "Write something first.")

	comments, err := st.ListComments(context.Background(), opt.ID)
	if err != nil {
		t.Fatalf("ListComments: %v", err)
	}
	if len(comments) != 0 {
		t.Fatalf("invalid comment changed Store: %#v", comments)
	}
}

// Comments belong to one option, not to the item.
func TestCommentsStayOnTheirOption(t *testing.T) {
	s, st := newServer(t)
	it := mustAdd(t, st, "Cot", "Nursery")
	a := mustAddOption(t, st, it.ID, store.OptionInput{URL: "https://a.example.com", Label: "Boori"})
	b := mustAddOption(t, st, it.ID, store.OptionInput{URL: "https://b.example.com", Label: "Seena"})

	mustAddComment(t, st, a.ID, "too expensive")
	mustAddComment(t, st, b.ID, "ships too late")

	rec := do(t, s, http.MethodGet, "/items/"+itoa(it.ID), nil)
	assertStatus(t, rec, http.StatusOK)

	body := rec.Body.String()
	assertOrder(t, body, "Boori", "too expensive", "Seena", "ships too late")
}

func TestDeleteComment(t *testing.T) {
	s, st := newServer(t)
	it := mustAdd(t, st, "Cot", "Nursery")
	opt := mustAddOption(t, st, it.ID, store.OptionInput{URL: "https://a.example.com"})
	mustAddComment(t, st, opt.ID, "keep me")
	drop := mustAddComment(t, st, opt.ID, "drop me")

	rec := do(t, s, http.MethodPost, "/comments/"+itoa(drop.ID)+"/delete", nil)
	assertStatus(t, rec, http.StatusOK)

	body := rec.Body.String()
	assertContains(t, body, `id="item-`+itoa(it.ID)+`"`)
	assertContains(t, body, "keep me")
	assertNotContains(t, body, "drop me")
}

func TestMissingComments(t *testing.T) {
	s, _ := newServer(t)

	for _, path := range []string{
		"/options/999/comments",
		"/comments/999/delete",
		"/comments/nope/delete",
	} {
		rec := do(t, s, http.MethodPost, path, url.Values{"body": {"hi"}})
		assertStatus(t, rec, http.StatusNotFound)
	}
}

func TestCommentBodyIsEscaped(t *testing.T) {
	s, st := newServer(t)
	it := mustAdd(t, st, "Cot", "Nursery")
	opt := mustAddOption(t, st, it.ID, store.OptionInput{URL: "https://a.example.com"})
	mustAddComment(t, st, opt.ID, "<script>alert(1)</script>")

	rec := do(t, s, http.MethodGet, "/items/"+itoa(it.ID), nil)
	assertStatus(t, rec, http.StatusOK)
	assertNotContains(t, rec.Body.String(), "<script>alert(1)</script>")
}

func TestBundleCommentsRenderAndRefreshEveryCopy(t *testing.T) {
	s, st := newServer(t)
	cot := mustAdd(t, st, "Cot", "Nursery")
	pram := mustAdd(t, st, "Pram", "Travel")
	bundle, err := st.AddBundle(context.Background(), store.BundleInput{
		Name: "Sleep bundle", URL: "https://shop.example/sleep", Price: "100",
		Members: []store.BundleMemberInput{{ItemID: cot.ID}, {ItemID: pram.ID}},
	})
	if err != nil {
		t.Fatalf("AddBundle: %v", err)
	}

	rec := do(t, s, http.MethodPost, "/bundles/"+itoa(bundle.ID)+"/comments", url.Values{"body": {" includes adapter "}})
	assertStatus(t, rec, http.StatusOK)
	body := rec.Body.String()
	assertContains(t, body, `id="list"`)

	rec = do(t, s, http.MethodGet, "/bundles/"+itoa(bundle.ID), nil)
	body = rec.Body.String()
	assertContains(t, body, "Bundle Comments")
	assertContains(t, body, "includes adapter")
	assertContains(t, body, `hx-post="/bundle-comments/`)

	rec = do(t, s, http.MethodPost, "/options/"+itoa(bundle.Members[0].OptionID)+"/comments", url.Values{"body": {"item-specific"}})
	assertStatus(t, rec, http.StatusOK)
	body = rec.Body.String()
	assertContains(t, body, "Bundle Comments")
	assertContains(t, body, `aria-label="Comments"`)
	assertContains(t, body, "includes adapter")
	assertContains(t, body, "item-specific")
	assertContains(t, body, `hx-post="/bundles/`+itoa(bundle.ID)+`/comments" hx-target="#list"`)

	comment, err := st.ListBundleComments(context.Background(), bundle.ID)
	if err != nil || len(comment) != 1 {
		t.Fatalf("ListBundleComments = %#v, %v", comment, err)
	}
	rec = do(t, s, http.MethodPost, "/bundle-comments/"+itoa(comment[0].ID)+"/delete", nil)
	assertStatus(t, rec, http.StatusOK)
	assertContains(t, rec.Body.String(), `id="list"`)
	rec = do(t, s, http.MethodGet, "/bundles/"+itoa(bundle.ID), nil)
	assertNotContains(t, rec.Body.String(), "includes adapter")
}

func TestBundleCommentFailuresAndEscaping(t *testing.T) {
	s, st := newServer(t)
	first := mustAdd(t, st, "Cot", "Nursery")
	second := mustAdd(t, st, "Pram", "Travel")
	bundle, err := st.AddBundle(context.Background(), store.BundleInput{Name: "Sleep bundle", URL: "https://shop.example/sleep", Price: "100", Members: []store.BundleMemberInput{{ItemID: first.ID}, {ItemID: second.ID}}})
	if err != nil {
		t.Fatalf("AddBundle: %v", err)
	}
	for _, body := range []string{"", "   "} {
		rec := do(t, s, http.MethodPost, "/bundles/"+itoa(bundle.ID)+"/comments", url.Values{"body": {body}})
		assertStatus(t, rec, http.StatusBadRequest)
		assertContains(t, rec.Body.String(), "Write something first")
	}
	rec := do(t, s, http.MethodPost, "/bundles/"+itoa(bundle.ID)+"/comments", url.Values{"body": {"<script>alert(1)</script>"}})
	assertStatus(t, rec, http.StatusOK)
	assertNotContains(t, rec.Body.String(), "<script>alert(1)</script>")
	for _, path := range []string{"/bundles/nope/comments", "/bundles/999/comments", "/bundle-comments/nope/delete", "/bundle-comments/999/delete"} {
		rec := do(t, s, http.MethodPost, path, url.Values{"body": {"hi"}})
		assertStatus(t, rec, http.StatusNotFound)
	}
}
