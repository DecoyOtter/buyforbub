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
