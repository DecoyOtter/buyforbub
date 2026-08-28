package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

const selectCommentColumns = `SELECT id, option_id, body, created_at FROM comments`

// ListComments returns an option's comments oldest first.
func (s *Store) ListComments(ctx context.Context, optionID int64) ([]Comment, error) {
	rows, err := s.db.QueryContext(ctx, selectCommentColumns+` WHERE option_id = ? ORDER BY id`, optionID)
	if err != nil {
		return nil, fmt.Errorf("list comments: %w", err)
	}
	return scanComments(rows, "list comments")
}

// CommentsByOption returns every comment on an item's options, keyed by the
// option they belong to — one query for a whole detail panel.
func (s *Store) CommentsByOption(ctx context.Context, itemID int64) (map[int64][]Comment, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT c.id, c.option_id, c.body, c.created_at
		 FROM comments c JOIN options o ON o.id = c.option_id
		 WHERE o.item_id = ? ORDER BY c.id`, itemID)
	if err != nil {
		return nil, fmt.Errorf("comments by option: %w", err)
	}
	all, err := scanComments(rows, "comments by option")
	if err != nil {
		return nil, err
	}

	byOption := make(map[int64][]Comment)
	for _, c := range all {
		byOption[c.OptionID] = append(byOption[c.OptionID], c)
	}
	return byOption, nil
}

func (s *Store) GetComment(ctx context.Context, id int64) (Comment, error) {
	row := s.db.QueryRowContext(ctx, selectCommentColumns+` WHERE id = ?`, id)
	c, err := scanComment(row)
	if errors.Is(err, sql.ErrNoRows) {
		return Comment{}, ErrNotFound
	}
	return c, err
}

// AddComment records a remark against an option.
func (s *Store) AddComment(ctx context.Context, optionID int64, body string) (Comment, error) {
	body, err := cleanComment(body)
	if err != nil {
		return Comment{}, err
	}
	// Checked up front so a missing option reads as ErrNotFound rather than a
	// foreign key violation.
	if _, err := s.GetOption(ctx, optionID); err != nil {
		return Comment{}, err
	}

	created := time.Now().UTC()
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO comments (option_id, body, created_at) VALUES (?, ?, ?)`,
		optionID, body, formatTime(created))
	if err != nil {
		return Comment{}, fmt.Errorf("add comment: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return Comment{}, fmt.Errorf("add comment: %w", err)
	}
	return Comment{ID: id, OptionID: optionID, Body: body, CreatedAt: created}, nil
}

func (s *Store) DeleteComment(ctx context.Context, id int64) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM comments WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("delete comment: %w", err)
	}
	return mustAffectOne(res, "delete comment")
}

func scanComments(rows *sql.Rows, op string) ([]Comment, error) {
	defer rows.Close()

	var out []Comment
	for rows.Next() {
		c, err := scanComment(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("%s: %w", op, err)
	}
	return out, nil
}

func scanComment(sc scanner) (Comment, error) {
	var (
		c       Comment
		created string
	)
	if err := sc.Scan(&c.ID, &c.OptionID, &c.Body, &created); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Comment{}, err
		}
		return Comment{}, fmt.Errorf("scan comment: %w", err)
	}
	t, err := time.Parse(time.RFC3339Nano, created)
	if err != nil {
		return Comment{}, fmt.Errorf("scan comment %d: bad created_at %q: %w", c.ID, created, err)
	}
	c.CreatedAt = t
	return c, nil
}
