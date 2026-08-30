package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

const selectBundleColumns = `SELECT id, name, url, price_cents, regular_price_cents, created_at FROM bundles`
const selectBundleMemberColumns = `SELECT id, bundle_id, item_id, option_id, position, component_label FROM bundle_members`

// AddBundle creates one package and a generated, unchosen option per member.
func (s *Store) AddBundle(ctx context.Context, in BundleInput) (Bundle, error) {
	c, err := in.clean()
	if err != nil {
		return Bundle{}, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Bundle{}, fmt.Errorf("add bundle: %w", err)
	}
	defer tx.Rollback()

	for _, member := range c.members {
		var found int
		err := tx.QueryRowContext(ctx, `SELECT 1 FROM items WHERE id = ?`, member.ItemID).Scan(&found)
		if errors.Is(err, sql.ErrNoRows) {
			return Bundle{}, ErrNotFound
		}
		if err != nil {
			return Bundle{}, fmt.Errorf("add bundle: %w", err)
		}
	}

	created := time.Now().UTC()
	res, err := tx.ExecContext(ctx,
		`INSERT INTO bundles (name, url, price_cents, regular_price_cents, created_at) VALUES (?, ?, ?, ?, ?)`,
		c.name, c.url, c.priceCents, c.regularPriceCents, formatTime(created))
	if err != nil {
		return Bundle{}, fmt.Errorf("add bundle: %w", err)
	}
	bundleID, err := res.LastInsertId()
	if err != nil {
		return Bundle{}, fmt.Errorf("add bundle: %w", err)
	}

	share, remainder := c.priceCents/int64(len(c.members)), c.priceCents%int64(len(c.members))
	for position, member := range c.members {
		price := share
		if int64(position) < remainder {
			price++
		}
		optionRes, err := tx.ExecContext(ctx,
			`INSERT INTO options (item_id, url, label, price_cents, chosen, created_at) VALUES (?, ?, ?, ?, 0, ?)`,
			member.ItemID, c.url, member.ComponentLabel, price, formatTime(created))
		if err != nil {
			return Bundle{}, fmt.Errorf("add bundle: %w", err)
		}
		optionID, err := optionRes.LastInsertId()
		if err != nil {
			return Bundle{}, fmt.Errorf("add bundle: %w", err)
		}
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO bundle_members (bundle_id, item_id, option_id, position, component_label) VALUES (?, ?, ?, ?, ?)`,
			bundleID, member.ItemID, optionID, position, member.ComponentLabel); err != nil {
			return Bundle{}, fmt.Errorf("add bundle: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return Bundle{}, fmt.Errorf("add bundle: %w", err)
	}
	return s.GetBundle(ctx, bundleID)
}

// GetBundle returns a Bundle and its members in stable position order.
func (s *Store) GetBundle(ctx context.Context, id int64) (Bundle, error) {
	b, err := scanBundle(s.db.QueryRowContext(ctx, selectBundleColumns+` WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return Bundle{}, ErrNotFound
	}
	if err != nil {
		return Bundle{}, err
	}
	b.Members, err = s.ListBundleMembers(ctx, id)
	if err != nil {
		return Bundle{}, err
	}
	return b, nil
}

// ListBundles returns Bundles in creation order with their ordered members.
func (s *Store) ListBundles(ctx context.Context) ([]Bundle, error) {
	rows, err := s.db.QueryContext(ctx, selectBundleColumns+` ORDER BY id`)
	if err != nil {
		return nil, fmt.Errorf("list bundles: %w", err)
	}
	var bundles []Bundle
	for rows.Next() {
		b, err := scanBundle(rows)
		if err != nil {
			rows.Close()
			return nil, err
		}
		bundles = append(bundles, b)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, fmt.Errorf("list bundles: %w", err)
	}
	if err := rows.Close(); err != nil {
		return nil, fmt.Errorf("list bundles: %w", err)
	}
	for i := range bundles {
		bundles[i].Members, err = s.ListBundleMembers(ctx, bundles[i].ID)
		if err != nil {
			return nil, err
		}
	}
	return bundles, nil
}

// ListBundleMembers returns a Bundle's members in stable position order.
func (s *Store) ListBundleMembers(ctx context.Context, bundleID int64) ([]BundleMember, error) {
	rows, err := s.db.QueryContext(ctx, selectBundleMemberColumns+` WHERE bundle_id = ? ORDER BY position`, bundleID)
	if err != nil {
		return nil, fmt.Errorf("list bundle members: %w", err)
	}
	defer rows.Close()
	var members []BundleMember
	for rows.Next() {
		var member BundleMember
		if err := rows.Scan(&member.ID, &member.BundleID, &member.ItemID, &member.OptionID, &member.Position, &member.ComponentLabel); err != nil {
			return nil, fmt.Errorf("scan bundle member: %w", err)
		}
		members = append(members, member)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list bundle members: %w", err)
	}
	return members, nil
}

func scanBundle(sc scanner) (Bundle, error) {
	var (
		b       Bundle
		regular sql.NullInt64
		created string
	)
	if err := sc.Scan(&b.ID, &b.Name, &b.URL, &b.PriceCents, &regular, &created); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Bundle{}, err
		}
		return Bundle{}, fmt.Errorf("scan bundle: %w", err)
	}
	if regular.Valid {
		b.RegularPriceCents = &regular.Int64
	}
	parsed, err := time.Parse(time.RFC3339Nano, created)
	if err != nil {
		return Bundle{}, fmt.Errorf("scan bundle %d: bad created_at %q: %w", b.ID, created, err)
	}
	b.CreatedAt = parsed
	return b, nil
}
