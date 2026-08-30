// Package store persists the shopping list in SQLite.
package store

import (
	"context"
	"database/sql"
	_ "embed"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite"
)

//go:embed schema.sql
var schemaSQL string

// InMemory opens a throwaway database, for tests.
const InMemory = ":memory:"

type Store struct {
	db *sql.DB
}

// Open opens (creating if needed) the database at path and applies the schema.
func Open(path string) (*Store, error) {
	if path != InMemory {
		if dir := filepath.Dir(path); dir != "" {
			if err := os.MkdirAll(dir, 0o755); err != nil {
				return nil, fmt.Errorf("create db directory: %w", err)
			}
		}
	}

	dsn := fmt.Sprintf(
		"file:%s?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_pragma=synchronous(normal)&_pragma=foreign_keys(on)",
		path,
	)
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}

	// One writer, one reader is all this app ever needs, and serialising
	// removes any chance of SQLITE_BUSY.
	db.SetMaxOpenConns(1)

	if err := db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("connect to database: %w", err)
	}
	if _, err := db.Exec(schemaSQL); err != nil {
		db.Close()
		return nil, fmt.Errorf("apply schema: %w", err)
	}
	if err := migrateBudget(db); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrate schema: %w", err)
	}
	return &Store{db: db}, nil
}

func (s *Store) Close() error { return s.db.Close() }

const selectColumns = `
	SELECT i.id, i.name, i.qty, i.category, i.status, i.notes, i.budget_cents, i.created_at,
	       (SELECT COUNT(*) FROM options o WHERE o.item_id = i.id)
	FROM items i`

// List returns every item in insertion order.
func (s *Store) List(ctx context.Context) ([]Item, error) {
	rows, err := s.db.QueryContext(ctx, selectColumns+` ORDER BY i.id`)
	if err != nil {
		return nil, fmt.Errorf("list items: %w", err)
	}
	defer rows.Close()

	var items []Item
	for rows.Next() {
		it, err := scanItem(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, it)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list items: %w", err)
	}
	return items, nil
}

func (s *Store) Get(ctx context.Context, id int64) (Item, error) {
	row := s.db.QueryRowContext(ctx, selectColumns+` WHERE i.id = ?`, id)
	it, err := scanItem(row)
	if errors.Is(err, sql.ErrNoRows) {
		return Item{}, ErrNotFound
	}
	return it, err
}

func (s *Store) Add(ctx context.Context, in ItemInput) (Item, error) {
	in, err := in.clean()
	if err != nil {
		return Item{}, err
	}

	created := time.Now().UTC()
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO items (name, qty, category, status, notes, budget_cents, created_at) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		in.Name, in.Qty, in.Category, StatusNeeded, in.Notes, budgetCents(in), formatTime(created))
	if err != nil {
		return Item{}, fmt.Errorf("add item: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return Item{}, fmt.Errorf("add item: %w", err)
	}
	return Item{
		ID: id, Name: in.Name, Qty: in.Qty, Category: in.Category,
		Status: StatusNeeded, Notes: in.Notes, BudgetCents: budgetCents(in), CreatedAt: created,
	}, nil
}

// Update replaces the user-editable fields. Status is left alone.
func (s *Store) Update(ctx context.Context, id int64, in ItemInput) (Item, error) {
	in, err := in.clean()
	if err != nil {
		return Item{}, err
	}

	res, err := s.db.ExecContext(ctx,
		`UPDATE items SET name = ?, qty = ?, category = ?, notes = ?, budget_cents = ? WHERE id = ?`,
		in.Name, in.Qty, in.Category, in.Notes, budgetCents(in), id)
	if err != nil {
		return Item{}, fmt.Errorf("update item: %w", err)
	}
	if err := mustAffectOne(res, "update item"); err != nil {
		return Item{}, err
	}
	return s.Get(ctx, id)
}

func (s *Store) SetStatus(ctx context.Context, id int64, status string) (Item, error) {
	if !ValidStatus(status) {
		return Item{}, ErrInvalidStatus
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Item{}, fmt.Errorf("set status: %w", err)
	}
	defer tx.Rollback()

	res, err := tx.ExecContext(ctx, `UPDATE items SET status = ? WHERE id = ?`, status, id)
	if err != nil {
		return Item{}, fmt.Errorf("set status: %w", err)
	}
	if err := mustAffectOne(res, "set status"); err != nil {
		return Item{}, err
	}
	// Going back to needed means it was not bought after all, so no option can
	// still be the one that was chosen.
	if status == StatusNeeded {
		if _, err := tx.ExecContext(ctx, `UPDATE options SET chosen = 0 WHERE item_id = ?`, id); err != nil {
			return Item{}, fmt.Errorf("set status: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return Item{}, fmt.Errorf("set status: %w", err)
	}
	return s.Get(ctx, id)
}

// Toggle flips an item between needed and bought.
func (s *Store) Toggle(ctx context.Context, id int64) (Item, error) {
	it, err := s.Get(ctx, id)
	if err != nil {
		return Item{}, err
	}
	next := StatusBought
	if it.Bought() {
		next = StatusNeeded
	}
	return s.SetStatus(ctx, id, next)
}

func (s *Store) Delete(ctx context.Context, id int64) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM items WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("delete item: %w", err)
	}
	return mustAffectOne(res, "delete item")
}

// SeedIfEmpty inserts the default checklist, but only into an empty database.
// It reports how many items it added.
func (s *Store) SeedIfEmpty(ctx context.Context, inputs []ItemInput) (int, error) {
	cleaned := make([]ItemInput, 0, len(inputs))
	for i, in := range inputs {
		c, err := in.clean()
		if err != nil {
			return 0, fmt.Errorf("seed item %d (%q): %w", i, in.Name, err)
		}
		cleaned = append(cleaned, c)
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("seed: %w", err)
	}
	defer tx.Rollback()

	var count int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM items`).Scan(&count); err != nil {
		return 0, fmt.Errorf("seed: %w", err)
	}
	if count > 0 {
		return 0, nil
	}

	now := formatTime(time.Now().UTC())
	for _, in := range cleaned {
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO items (name, qty, category, status, notes, budget_cents, created_at) VALUES (?, ?, ?, ?, ?, ?, ?)`,
			in.Name, in.Qty, in.Category, StatusNeeded, in.Notes, budgetCents(in), now); err != nil {
			return 0, fmt.Errorf("seed: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("seed: %w", err)
	}
	return len(cleaned), nil
}

// scanner is satisfied by both *sql.Row and *sql.Rows.
type scanner interface {
	Scan(dest ...any) error
}

func scanItem(sc scanner) (Item, error) {
	var (
		it      Item
		budget  sql.NullInt64
		created string
	)
	if err := sc.Scan(&it.ID, &it.Name, &it.Qty, &it.Category, &it.Status, &it.Notes, &budget, &created, &it.OptionCount); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Item{}, err
		}
		return Item{}, fmt.Errorf("scan item: %w", err)
	}
	if budget.Valid {
		it.BudgetCents = &budget.Int64
	}
	t, err := time.Parse(time.RFC3339Nano, created)
	if err != nil {
		return Item{}, fmt.Errorf("scan item %d: bad created_at %q: %w", it.ID, created, err)
	}
	it.CreatedAt = t
	return it, nil
}

func budgetCents(in ItemInput) *int64 {
	budget, _ := parsePrice(in.Budget)
	return budget
}

func migrateBudget(db *sql.DB) error {
	rows, err := db.Query(`PRAGMA table_info(items)`)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var cid int
		var name, kind string
		var notNull, primaryKey int
		var defaultValue any
		if err := rows.Scan(&cid, &name, &kind, &notNull, &defaultValue, &primaryKey); err != nil {
			return err
		}
		if name == "budget_cents" {
			return nil
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if err := rows.Close(); err != nil {
		return err
	}
	_, err = db.Exec(`ALTER TABLE items ADD COLUMN budget_cents INTEGER`)
	return err
}

// ChosenOptionPrices returns every chosen option, retaining an unpriced choice
// as a nil value.
func (s *Store) ChosenOptionPrices(ctx context.Context) (map[int64]*int64, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT item_id, price_cents FROM options WHERE chosen = 1`)
	if err != nil {
		return nil, fmt.Errorf("list chosen prices: %w", err)
	}
	defer rows.Close()
	prices := make(map[int64]*int64)
	for rows.Next() {
		var itemID int64
		var price sql.NullInt64
		if err := rows.Scan(&itemID, &price); err != nil {
			return nil, fmt.Errorf("list chosen prices: %w", err)
		}
		if price.Valid {
			value := price.Int64
			prices[itemID] = &value
		} else {
			prices[itemID] = nil
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list chosen prices: %w", err)
	}
	return prices, nil
}

func formatTime(t time.Time) string { return t.UTC().Format(time.RFC3339Nano) }

func mustAffectOne(res sql.Result, op string) error {
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("%s: %w", op, err)
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}
