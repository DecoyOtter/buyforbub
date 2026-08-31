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
const selectBundleCommentColumns = `SELECT id, bundle_id, body, created_at FROM bundle_comments`

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

// UpdateBundle replaces a bundle's details and member list.
func (s *Store) UpdateBundle(ctx context.Context, id int64, in BundleInput) (Bundle, error) {
	c, err := in.clean()
	if err != nil {
		return Bundle{}, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Bundle{}, fmt.Errorf("update bundle: %w", err)
	}
	defer tx.Rollback()

	var found int
	if err := tx.QueryRowContext(ctx, `SELECT 1 FROM bundles WHERE id = ?`, id).Scan(&found); errors.Is(err, sql.ErrNoRows) {
		return Bundle{}, ErrNotFound
	} else if err != nil {
		return Bundle{}, fmt.Errorf("update bundle: %w", err)
	}

	existing, err := listBundleMembersTx(ctx, tx, id)
	if err != nil {
		return Bundle{}, err
	}
	inputByItem := make(map[int64]BundleMemberInput, len(c.members))
	for _, member := range c.members {
		if err := tx.QueryRowContext(ctx, `SELECT 1 FROM items WHERE id = ?`, member.ItemID).Scan(&found); errors.Is(err, sql.ErrNoRows) {
			return Bundle{}, ErrNotFound
		} else if err != nil {
			return Bundle{}, fmt.Errorf("update bundle: %w", err)
		}
		inputByItem[member.ItemID] = member
	}

	chosen := false
	if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM bundle_members bm JOIN options o ON o.id = bm.option_id WHERE bm.bundle_id = ? AND o.chosen = 1)`, id).Scan(&chosen); err != nil {
		return Bundle{}, fmt.Errorf("update bundle: %w", err)
	}
	if chosen {
		if _, err := tx.ExecContext(ctx, `UPDATE options SET chosen = 0 WHERE id IN (SELECT option_id FROM bundle_members WHERE bundle_id = ?)`, id); err != nil {
			return Bundle{}, fmt.Errorf("update bundle: %w", err)
		}
		if _, err := tx.ExecContext(ctx, `UPDATE items SET status = ? WHERE id IN (SELECT item_id FROM bundle_members WHERE bundle_id = ?)`, StatusNeeded, id); err != nil {
			return Bundle{}, fmt.Errorf("update bundle: %w", err)
		}
	}

	retained := make([]BundleMember, 0, len(existing))
	for _, member := range existing {
		if _, ok := inputByItem[member.ItemID]; ok {
			retained = append(retained, member)
			delete(inputByItem, member.ItemID)
			continue
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM options WHERE id = ?`, member.OptionID); err != nil {
			return Bundle{}, fmt.Errorf("update bundle: %w", err)
		}
	}

	newItems, err := checklistOrderedItems(ctx, tx, inputByItem)
	if err != nil {
		return Bundle{}, err
	}
	members := make([]BundleMember, 0, len(c.members))
	members = append(members, retained...)
	nextPosition := 0
	for _, member := range retained {
		if member.Position >= nextPosition {
			nextPosition = member.Position + 1
		}
	}
	for _, itemID := range newItems {
		members = append(members, BundleMember{ItemID: itemID, Position: nextPosition})
		nextPosition++
	}

	if _, err := tx.ExecContext(ctx, `UPDATE bundles SET name = ?, url = ?, price_cents = ?, regular_price_cents = ? WHERE id = ?`, c.name, c.url, c.priceCents, c.regularPriceCents, id); err != nil {
		return Bundle{}, fmt.Errorf("update bundle: %w", err)
	}
	share, remainder := c.priceCents/int64(len(members)), c.priceCents%int64(len(members))
	for i, member := range members {
		input := findBundleMemberInput(c.members, member.ItemID)
		price := share
		if int64(i) < remainder {
			price++
		}
		if member.ID == 0 {
			option, err := tx.ExecContext(ctx, `INSERT INTO options (item_id, url, label, price_cents, chosen, created_at) VALUES (?, ?, ?, ?, 0, ?)`, member.ItemID, c.url, input.ComponentLabel, price, formatTime(time.Now().UTC()))
			if err != nil {
				return Bundle{}, fmt.Errorf("update bundle: %w", err)
			}
			optionID, err := option.LastInsertId()
			if err != nil {
				return Bundle{}, fmt.Errorf("update bundle: %w", err)
			}
			if _, err := tx.ExecContext(ctx, `INSERT INTO bundle_members (bundle_id, item_id, option_id, position, component_label) VALUES (?, ?, ?, ?, ?)`, id, member.ItemID, optionID, member.Position, input.ComponentLabel); err != nil {
				return Bundle{}, fmt.Errorf("update bundle: %w", err)
			}
			continue
		}
		if _, err := tx.ExecContext(ctx, `UPDATE bundle_members SET component_label = ? WHERE id = ?`, input.ComponentLabel, member.ID); err != nil {
			return Bundle{}, fmt.Errorf("update bundle: %w", err)
		}
		if _, err := tx.ExecContext(ctx, `UPDATE options SET url = ?, label = ?, price_cents = ? WHERE id = ?`, c.url, input.ComponentLabel, price, member.OptionID); err != nil {
			return Bundle{}, fmt.Errorf("update bundle: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return Bundle{}, fmt.Errorf("update bundle: %w", err)
	}
	return s.GetBundle(ctx, id)
}

// DeleteBundle removes a package and all of its generated options.
func (s *Store) DeleteBundle(ctx context.Context, id int64) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("delete bundle: %w", err)
	}
	defer tx.Rollback()

	if err := deleteBundleTx(ctx, tx, id); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("delete bundle: %w", err)
	}
	return nil
}

// ChooseBundle chooses every generated option and buys every member Item.
func (s *Store) ChooseBundle(ctx context.Context, id int64) (Bundle, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Bundle{}, fmt.Errorf("choose bundle: %w", err)
	}
	defer tx.Rollback()

	members, err := listBundleMembersTx(ctx, tx, id)
	if err != nil {
		return Bundle{}, err
	}
	if len(members) == 0 {
		var found int
		if err := tx.QueryRowContext(ctx, `SELECT 1 FROM bundles WHERE id = ?`, id).Scan(&found); errors.Is(err, sql.ErrNoRows) {
			return Bundle{}, ErrNotFound
		} else if err != nil {
			return Bundle{}, fmt.Errorf("choose bundle: %w", err)
		}
	}

	rows, err := tx.QueryContext(ctx, `
		SELECT DISTINCT conflicting.bundle_id
		FROM bundle_members target
		JOIN bundle_members conflicting ON conflicting.item_id = target.item_id
		JOIN options conflicting_option ON conflicting_option.id = conflicting.option_id
		WHERE target.bundle_id = ? AND conflicting.bundle_id != ? AND conflicting_option.chosen = 1
		ORDER BY conflicting.bundle_id`, id, id)
	if err != nil {
		return Bundle{}, fmt.Errorf("choose bundle: %w", err)
	}
	var conflictingIDs []int64
	for rows.Next() {
		var conflictingID int64
		if err := rows.Scan(&conflictingID); err != nil {
			rows.Close()
			return Bundle{}, fmt.Errorf("choose bundle: %w", err)
		}
		conflictingIDs = append(conflictingIDs, conflictingID)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return Bundle{}, fmt.Errorf("choose bundle: %w", err)
	}
	if err := rows.Close(); err != nil {
		return Bundle{}, fmt.Errorf("choose bundle: %w", err)
	}

	for _, conflictingID := range conflictingIDs {
		if err := unchooseBundleTx(ctx, tx, conflictingID); err != nil {
			return Bundle{}, err
		}
	}
	if _, err := tx.ExecContext(ctx, `UPDATE options SET chosen = 0 WHERE item_id IN (SELECT item_id FROM bundle_members WHERE bundle_id = ?)`, id); err != nil {
		return Bundle{}, fmt.Errorf("choose bundle: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `UPDATE options SET chosen = 1 WHERE id IN (SELECT option_id FROM bundle_members WHERE bundle_id = ?)`, id); err != nil {
		return Bundle{}, fmt.Errorf("choose bundle: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `UPDATE items SET status = ? WHERE id IN (SELECT item_id FROM bundle_members WHERE bundle_id = ?)`, StatusBought, id); err != nil {
		return Bundle{}, fmt.Errorf("choose bundle: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return Bundle{}, fmt.Errorf("choose bundle: %w", err)
	}
	return s.GetBundle(ctx, id)
}

func deleteBundleTx(ctx context.Context, tx *sql.Tx, id int64) error {
	members, err := listBundleMembersTx(ctx, tx, id)
	if err != nil {
		return err
	}
	if len(members) == 0 {
		var found int
		if err := tx.QueryRowContext(ctx, `SELECT 1 FROM bundles WHERE id = ?`, id).Scan(&found); errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		} else if err != nil {
			return fmt.Errorf("delete bundle: %w", err)
		}
	}
	if err := unchooseBundleTx(ctx, tx, id); err != nil {
		return err
	}
	for _, member := range members {
		if _, err := tx.ExecContext(ctx, `DELETE FROM options WHERE id = ?`, member.OptionID); err != nil {
			return fmt.Errorf("delete bundle: %w", err)
		}
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM bundles WHERE id = ?`, id); err != nil {
		return fmt.Errorf("delete bundle: %w", err)
	}
	return nil
}

func unchooseBundleTx(ctx context.Context, tx *sql.Tx, id int64) error {
	var chosen bool
	if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM bundle_members bm JOIN options o ON o.id = bm.option_id WHERE bm.bundle_id = ? AND o.chosen = 1)`, id).Scan(&chosen); err != nil {
		return fmt.Errorf("bundle lifecycle: %w", err)
	}
	if !chosen {
		return nil
	}
	if _, err := tx.ExecContext(ctx, `UPDATE options SET chosen = 0 WHERE id IN (SELECT option_id FROM bundle_members WHERE bundle_id = ?)`, id); err != nil {
		return fmt.Errorf("bundle lifecycle: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `UPDATE items SET status = ? WHERE id IN (SELECT item_id FROM bundle_members WHERE bundle_id = ?)`, StatusNeeded, id); err != nil {
		return fmt.Errorf("bundle lifecycle: %w", err)
	}
	return nil
}

func chosenBundleIDsForItemTx(ctx context.Context, tx *sql.Tx, itemID int64) ([]int64, error) {
	rows, err := tx.QueryContext(ctx, `
		SELECT DISTINCT bm.bundle_id
		FROM bundle_members bm
		JOIN options o ON o.id = bm.option_id
		WHERE bm.item_id = ? AND o.chosen = 1
		ORDER BY bm.bundle_id`, itemID)
	if err != nil {
		return nil, fmt.Errorf("bundle lifecycle: %w", err)
	}
	defer rows.Close()

	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("bundle lifecycle: %w", err)
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("bundle lifecycle: %w", err)
	}
	return ids, nil
}

func reallocateBundleTx(ctx context.Context, tx *sql.Tx, id int64) error {
	var price int64
	if err := tx.QueryRowContext(ctx, `SELECT price_cents FROM bundles WHERE id = ?`, id).Scan(&price); err != nil {
		return fmt.Errorf("delete item: %w", err)
	}
	members, err := listBundleMembersTx(ctx, tx, id)
	if err != nil {
		return err
	}
	share, remainder := price/int64(len(members)), price%int64(len(members))
	for i, member := range members {
		allocated := share
		if int64(i) < remainder {
			allocated++
		}
		if _, err := tx.ExecContext(ctx, `UPDATE options SET price_cents = ? WHERE id = ?`, allocated, member.OptionID); err != nil {
			return fmt.Errorf("delete item: %w", err)
		}
	}
	return nil
}

func listBundleMembersTx(ctx context.Context, tx *sql.Tx, bundleID int64) ([]BundleMember, error) {
	rows, err := tx.QueryContext(ctx, selectBundleMemberColumns+` WHERE bundle_id = ? ORDER BY position`, bundleID)
	if err != nil {
		return nil, fmt.Errorf("update bundle: %w", err)
	}
	defer rows.Close()
	var members []BundleMember
	for rows.Next() {
		var member BundleMember
		if err := rows.Scan(&member.ID, &member.BundleID, &member.ItemID, &member.OptionID, &member.Position, &member.ComponentLabel); err != nil {
			return nil, fmt.Errorf("update bundle: scan member: %w", err)
		}
		members = append(members, member)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("update bundle: %w", err)
	}
	return members, nil
}

func checklistOrderedItems(ctx context.Context, tx *sql.Tx, wanted map[int64]BundleMemberInput) ([]int64, error) {
	if len(wanted) == 0 {
		return nil, nil
	}
	rows, err := tx.QueryContext(ctx, `SELECT id FROM items ORDER BY id`)
	if err != nil {
		return nil, fmt.Errorf("update bundle: %w", err)
	}
	defer rows.Close()
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("update bundle: %w", err)
		}
		if _, ok := wanted[id]; ok {
			ids = append(ids, id)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("update bundle: %w", err)
	}
	return ids, nil
}

func findBundleMemberInput(members []BundleMemberInput, itemID int64) BundleMemberInput {
	for _, member := range members {
		if member.ItemID == itemID {
			return member
		}
	}
	return BundleMemberInput{}
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

// ListBundleComments returns a bundle's shared comments oldest first.
func (s *Store) ListBundleComments(ctx context.Context, bundleID int64) ([]BundleComment, error) {
	if _, err := s.GetBundle(ctx, bundleID); err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx, selectBundleCommentColumns+` WHERE bundle_id = ? ORDER BY id`, bundleID)
	if err != nil {
		return nil, fmt.Errorf("list bundle comments: %w", err)
	}
	defer rows.Close()

	var comments []BundleComment
	for rows.Next() {
		comment, err := scanBundleComment(rows)
		if err != nil {
			return nil, err
		}
		comments = append(comments, comment)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list bundle comments: %w", err)
	}
	return comments, nil
}

// GetBundleComment returns one shared bundle comment.
func (s *Store) GetBundleComment(ctx context.Context, id int64) (BundleComment, error) {
	comment, err := scanBundleComment(s.db.QueryRowContext(ctx, selectBundleCommentColumns+` WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return BundleComment{}, ErrNotFound
	}
	return comment, err
}

// AddBundleComment records a shared remark against a bundle.
func (s *Store) AddBundleComment(ctx context.Context, bundleID int64, body string) (BundleComment, error) {
	body, err := cleanComment(body)
	if err != nil {
		return BundleComment{}, err
	}
	if _, err := s.GetBundle(ctx, bundleID); err != nil {
		return BundleComment{}, err
	}

	created := time.Now().UTC()
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO bundle_comments (bundle_id, body, created_at) VALUES (?, ?, ?)`,
		bundleID, body, formatTime(created))
	if err != nil {
		return BundleComment{}, fmt.Errorf("add bundle comment: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return BundleComment{}, fmt.Errorf("add bundle comment: %w", err)
	}
	return BundleComment{ID: id, BundleID: bundleID, Body: body, CreatedAt: created}, nil
}

// DeleteBundleComment removes one shared bundle comment.
func (s *Store) DeleteBundleComment(ctx context.Context, id int64) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM bundle_comments WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("delete bundle comment: %w", err)
	}
	return mustAffectOne(res, "delete bundle comment")
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

func scanBundleComment(sc scanner) (BundleComment, error) {
	var (
		comment BundleComment
		created string
	)
	if err := sc.Scan(&comment.ID, &comment.BundleID, &comment.Body, &created); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return BundleComment{}, err
		}
		return BundleComment{}, fmt.Errorf("scan bundle comment: %w", err)
	}
	parsed, err := time.Parse(time.RFC3339Nano, created)
	if err != nil {
		return BundleComment{}, fmt.Errorf("scan bundle comment %d: bad created_at %q: %w", comment.ID, created, err)
	}
	comment.CreatedAt = parsed
	return comment, nil
}
