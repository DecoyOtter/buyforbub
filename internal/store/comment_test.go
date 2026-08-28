package store

import (
	"context"
	"errors"
	"testing"
	"time"
)

func mustAddComment(t *testing.T, s *Store, optionID int64, body string) Comment {
	t.Helper()
	c, err := s.AddComment(context.Background(), optionID, body)
	if err != nil {
		t.Fatalf("AddComment(%q): %v", body, err)
	}
	return c
}

func TestAddCommentValidation(t *testing.T) {
	s := newStore(t)
	it := mustAdd(t, s, ItemInput{Name: "Cot", Category: "Nursery"})
	opt := mustAddOption(t, s, it.ID, OptionInput{URL: "https://a.example.com"})

	c := mustAddComment(t, s, opt.ID, "  Good rounded edges for leaning  ")
	if c.Body != "Good rounded edges for leaning" {
		t.Errorf("body = %q, want it trimmed", c.Body)
	}
	if c.OptionID != opt.ID {
		t.Errorf("OptionID = %d, want %d", c.OptionID, opt.ID)
	}

	for _, body := range []string{"", "   ", "\n\t"} {
		if _, err := s.AddComment(context.Background(), opt.ID, body); !errors.Is(err, ErrInvalidComment) {
			t.Errorf("AddComment(%q) error = %v, want ErrInvalidComment", body, err)
		}
	}
}

func TestAddCommentToMissingOption(t *testing.T) {
	s := newStore(t)
	if _, err := s.AddComment(context.Background(), 999, "nope"); !errors.Is(err, ErrNotFound) {
		t.Errorf("error = %v, want ErrNotFound", err)
	}
}

func TestListCommentsIsInsertionOrdered(t *testing.T) {
	s := newStore(t)
	it := mustAdd(t, s, ItemInput{Name: "Cot", Category: "Nursery"})
	opt := mustAddOption(t, s, it.ID, OptionInput{URL: "https://a.example.com"})

	mustAddComment(t, s, opt.ID, "first")
	mustAddComment(t, s, opt.ID, "second")
	mustAddComment(t, s, opt.ID, "third")

	got, err := s.ListComments(context.Background(), opt.ID)
	if err != nil {
		t.Fatalf("ListComments: %v", err)
	}
	want := []string{"first", "second", "third"}
	if len(got) != len(want) {
		t.Fatalf("got %d comments, want %d", len(got), len(want))
	}
	for i, w := range want {
		if got[i].Body != w {
			t.Errorf("comment %d = %q, want %q", i, got[i].Body, w)
		}
	}
}

func TestCommentsByOption(t *testing.T) {
	s := newStore(t)
	it := mustAdd(t, s, ItemInput{Name: "Cot", Category: "Nursery"})
	other := mustAdd(t, s, ItemInput{Name: "Pram", Category: "Travel"})

	a := mustAddOption(t, s, it.ID, OptionInput{URL: "https://a.example.com"})
	b := mustAddOption(t, s, it.ID, OptionInput{URL: "https://b.example.com"})
	elsewhere := mustAddOption(t, s, other.ID, OptionInput{URL: "https://c.example.com"})

	mustAddComment(t, s, a.ID, "too expensive")
	mustAddComment(t, s, a.ID, "converts to a toddler bed")
	mustAddComment(t, s, elsewhere.ID, "belongs to the pram")

	byOption, err := s.CommentsByOption(context.Background(), it.ID)
	if err != nil {
		t.Fatalf("CommentsByOption: %v", err)
	}
	if len(byOption) != 1 {
		t.Fatalf("got comments for %d options, want 1", len(byOption))
	}
	if n := len(byOption[a.ID]); n != 2 {
		t.Errorf("option a has %d comments, want 2", n)
	}
	if _, ok := byOption[b.ID]; ok {
		t.Error("option b has no comments, so it should be absent from the map")
	}
	if _, ok := byOption[elsewhere.ID]; ok {
		t.Error("another item's comments leaked in")
	}
}

func TestDeleteComment(t *testing.T) {
	s := newStore(t)
	it := mustAdd(t, s, ItemInput{Name: "Cot", Category: "Nursery"})
	opt := mustAddOption(t, s, it.ID, OptionInput{URL: "https://a.example.com"})

	keep := mustAddComment(t, s, opt.ID, "keep me")
	drop := mustAddComment(t, s, opt.ID, "drop me")

	if err := s.DeleteComment(context.Background(), drop.ID); err != nil {
		t.Fatalf("DeleteComment: %v", err)
	}
	got, err := s.ListComments(context.Background(), opt.ID)
	if err != nil {
		t.Fatalf("ListComments: %v", err)
	}
	if len(got) != 1 || got[0].ID != keep.ID {
		t.Errorf("remaining = %+v, want only %q", got, keep.Body)
	}
	if err := s.DeleteComment(context.Background(), drop.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("second delete error = %v, want ErrNotFound", err)
	}
}

// Comments hang off an option, so removing the option — or the whole item —
// must take them with it.
func TestCommentsCascade(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()
	it := mustAdd(t, s, ItemInput{Name: "Cot", Category: "Nursery"})
	opt := mustAddOption(t, s, it.ID, OptionInput{URL: "https://a.example.com"})
	c := mustAddComment(t, s, opt.ID, "too expensive")

	if err := s.DeleteOption(ctx, opt.ID); err != nil {
		t.Fatalf("DeleteOption: %v", err)
	}
	if _, err := s.GetComment(ctx, c.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("after deleting the option, GetComment error = %v, want ErrNotFound", err)
	}

	opt2 := mustAddOption(t, s, it.ID, OptionInput{URL: "https://b.example.com"})
	c2 := mustAddComment(t, s, opt2.ID, "ships too late")
	if err := s.Delete(ctx, it.ID); err != nil {
		t.Fatalf("Delete item: %v", err)
	}
	if _, err := s.GetComment(ctx, c2.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("after deleting the item, GetComment error = %v, want ErrNotFound", err)
	}
}

// Choosing an option, or backing that out, says nothing about its comments.
func TestChoosingKeepsComments(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()
	it := mustAdd(t, s, ItemInput{Name: "Cot", Category: "Nursery"})
	opt := mustAddOption(t, s, it.ID, OptionInput{URL: "https://a.example.com"})
	mustAddComment(t, s, opt.ID, "too expensive")

	if _, err := s.ChooseOption(ctx, opt.ID); err != nil {
		t.Fatalf("ChooseOption: %v", err)
	}
	if _, err := s.Toggle(ctx, it.ID); err != nil {
		t.Fatalf("Toggle: %v", err)
	}
	got, err := s.ListComments(ctx, opt.ID)
	if err != nil {
		t.Fatalf("ListComments: %v", err)
	}
	if len(got) != 1 {
		t.Errorf("got %d comments, want 1", len(got))
	}
}

func TestCommentAge(t *testing.T) {
	now := time.Date(2026, 8, 29, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		ago  time.Duration
		want string
	}{
		{0, "now"},
		{30 * time.Second, "now"},
		{5 * time.Minute, "5m"},
		{90 * time.Minute, "1h"},
		{3 * time.Hour, "3h"},
		{50 * time.Hour, "2d"},
		{6 * 24 * time.Hour, "6d"},
		{9 * 24 * time.Hour, "1w"},
		{40 * 24 * time.Hour, "5w"},
	}
	for _, tt := range tests {
		if got := ageSince(now, now.Add(-tt.ago)); got != tt.want {
			t.Errorf("ageSince(%v ago) = %q, want %q", tt.ago, got, tt.want)
		}
	}
}
