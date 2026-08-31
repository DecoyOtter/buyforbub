package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

const selectOptionColumns = `SELECT id, item_id, url, label, price_cents, chosen, created_at FROM options`

// ListOptions returns an item's options in the order they were added.
func (s *Store) ListOptions(ctx context.Context, itemID int64) ([]Option, error) {
	rows, err := s.db.QueryContext(ctx, selectOptionColumns+` WHERE item_id = ? ORDER BY id`, itemID)
	if err != nil {
		return nil, fmt.Errorf("list options: %w", err)
	}
	defer rows.Close()

	var opts []Option
	for rows.Next() {
		o, err := scanOption(rows)
		if err != nil {
			return nil, err
		}
		opts = append(opts, o)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list options: %w", err)
	}
	return opts, nil
}

func (s *Store) GetOption(ctx context.Context, id int64) (Option, error) {
	row := s.db.QueryRowContext(ctx, selectOptionColumns+` WHERE id = ?`, id)
	o, err := scanOption(row)
	if errors.Is(err, sql.ErrNoRows) {
		return Option{}, ErrNotFound
	}
	return o, err
}

// ChosenOptionPrices returns each chosen Option price by Item.
func (s *Store) ChosenOptionPrices(ctx context.Context) (map[int64]*int64, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT item_id, price_cents FROM options WHERE chosen = 1`)
	if err != nil {
		return nil, fmt.Errorf("list chosen option prices: %w", err)
	}
	defer rows.Close()

	prices := make(map[int64]*int64)
	for rows.Next() {
		var itemID int64
		var price sql.NullInt64
		if err := rows.Scan(&itemID, &price); err != nil {
			return nil, fmt.Errorf("list chosen option prices: %w", err)
		}
		if price.Valid {
			value := price.Int64
			prices[itemID] = &value
		} else {
			prices[itemID] = nil
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list chosen option prices: %w", err)
	}
	return prices, nil
}

// AddOption attaches a candidate product to an item.
func (s *Store) AddOption(ctx context.Context, itemID int64, in OptionInput) (Option, error) {
	c, err := in.clean()
	if err != nil {
		return Option{}, err
	}
	// Checked up front so a missing item reads as ErrNotFound rather than a
	// foreign key violation.
	if _, err := s.Get(ctx, itemID); err != nil {
		return Option{}, err
	}

	created := time.Now().UTC()
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO options (item_id, url, label, price_cents, chosen, created_at) VALUES (?, ?, ?, ?, 0, ?)`,
		itemID, c.url, c.label, c.priceCents, formatTime(created))
	if err != nil {
		return Option{}, fmt.Errorf("add option: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return Option{}, fmt.Errorf("add option: %w", err)
	}
	return Option{
		ID: id, ItemID: itemID, URL: c.url, Label: c.label,
		PriceCents: c.priceCents, CreatedAt: created,
	}, nil
}

func (s *Store) DeleteOption(ctx context.Context, id int64) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM options WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("delete option: %w", err)
	}
	return mustAffectOne(res, "delete option")
}

// ChooseOption records that this is the option that got bought, and marks the
// item bought with it. Any previously chosen option for the item is cleared.
func (s *Store) ChooseOption(ctx context.Context, id int64) (Option, error) {
	o, err := s.GetOption(ctx, id)
	if err != nil {
		return Option{}, err
	}
	var bundleID int64
	err = s.db.QueryRowContext(ctx, `SELECT bundle_id FROM bundle_members WHERE option_id = ?`, id).Scan(&bundleID)
	if err == nil {
		if _, err := s.ChooseBundle(ctx, bundleID); err != nil {
			return Option{}, err
		}
		o.Chosen = true
		return o, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return Option{}, fmt.Errorf("choose option: %w", err)
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Option{}, fmt.Errorf("choose option: %w", err)
	}
	defer tx.Rollback()

	bundleIDs, err := chosenBundleIDsForItemTx(ctx, tx, o.ItemID)
	if err != nil {
		return Option{}, err
	}
	for _, bundleID := range bundleIDs {
		if err := unchooseBundleTx(ctx, tx, bundleID); err != nil {
			return Option{}, err
		}
	}
	if _, err := tx.ExecContext(ctx,
		`UPDATE options SET chosen = CASE WHEN id = ? THEN 1 ELSE 0 END WHERE item_id = ?`,
		id, o.ItemID); err != nil {
		return Option{}, fmt.Errorf("choose option: %w", err)
	}
	if _, err := tx.ExecContext(ctx,
		`UPDATE items SET status = ? WHERE id = ?`, StatusBought, o.ItemID); err != nil {
		return Option{}, fmt.Errorf("choose option: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return Option{}, fmt.Errorf("choose option: %w", err)
	}

	o.Chosen = true
	return o, nil
}

func scanOption(sc scanner) (Option, error) {
	var (
		o       Option
		price   sql.NullInt64
		chosen  int
		created string
	)
	if err := sc.Scan(&o.ID, &o.ItemID, &o.URL, &o.Label, &price, &chosen, &created); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Option{}, err
		}
		return Option{}, fmt.Errorf("scan option: %w", err)
	}
	if price.Valid {
		o.PriceCents = &price.Int64
	}
	o.Chosen = chosen != 0

	t, err := time.Parse(time.RFC3339Nano, created)
	if err != nil {
		return Option{}, fmt.Errorf("scan option %d: bad created_at %q: %w", o.ID, created, err)
	}
	o.CreatedAt = t
	return o, nil
}
