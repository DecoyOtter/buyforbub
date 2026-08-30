CREATE TABLE IF NOT EXISTS items (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    name       TEXT    NOT NULL,
    qty        INTEGER NOT NULL DEFAULT 1,
    category   TEXT    NOT NULL,
    status     TEXT    NOT NULL DEFAULT 'needed',
    notes        TEXT    NOT NULL DEFAULT '',
    budget_cents INTEGER,
    created_at   TEXT    NOT NULL
);

-- Candidate products for an item: usually a link to something being considered.
-- At most one per item is chosen, which is the one that got bought.
CREATE TABLE IF NOT EXISTS options (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    item_id     INTEGER NOT NULL REFERENCES items(id) ON DELETE CASCADE,
    url         TEXT    NOT NULL,
    label       TEXT    NOT NULL DEFAULT '',
    price_cents INTEGER,
    chosen      INTEGER NOT NULL DEFAULT 0,
    created_at  TEXT    NOT NULL
);

CREATE INDEX IF NOT EXISTS options_by_item ON options(item_id);

-- Remarks recorded against an option while it is being weighed up.
CREATE TABLE IF NOT EXISTS comments (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    option_id  INTEGER NOT NULL REFERENCES options(id) ON DELETE CASCADE,
    body       TEXT    NOT NULL,
    created_at TEXT    NOT NULL
);

CREATE INDEX IF NOT EXISTS comments_by_option ON comments(option_id);

-- A store package considered as one purchase across multiple items.
CREATE TABLE IF NOT EXISTS bundles (
    id                  INTEGER PRIMARY KEY AUTOINCREMENT,
    name                TEXT    NOT NULL,
    url                 TEXT    NOT NULL,
    price_cents         INTEGER NOT NULL CHECK (price_cents > 0),
    regular_price_cents INTEGER CHECK (regular_price_cents IS NULL OR regular_price_cents >= price_cents),
    created_at          TEXT    NOT NULL
);

-- One generated option for each item included in a bundle.
CREATE TABLE IF NOT EXISTS bundle_members (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    bundle_id       INTEGER NOT NULL REFERENCES bundles(id) ON DELETE CASCADE,
    item_id         INTEGER NOT NULL REFERENCES items(id) ON DELETE CASCADE,
    option_id       INTEGER NOT NULL REFERENCES options(id) ON DELETE CASCADE,
    position        INTEGER NOT NULL,
    component_label TEXT    NOT NULL DEFAULT '',
    UNIQUE (bundle_id, item_id),
    UNIQUE (bundle_id, option_id),
    UNIQUE (option_id),
    UNIQUE (bundle_id, position)
);

CREATE INDEX IF NOT EXISTS bundle_members_by_bundle ON bundle_members(bundle_id, position);
CREATE INDEX IF NOT EXISTS bundle_members_by_item ON bundle_members(item_id);

-- Remarks shared by every option generated from a bundle.
CREATE TABLE IF NOT EXISTS bundle_comments (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    bundle_id  INTEGER NOT NULL REFERENCES bundles(id) ON DELETE CASCADE,
    body       TEXT    NOT NULL,
    created_at TEXT    NOT NULL
);

CREATE INDEX IF NOT EXISTS bundle_comments_by_bundle ON bundle_comments(bundle_id);
