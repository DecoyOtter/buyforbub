CREATE TABLE IF NOT EXISTS items (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    name       TEXT    NOT NULL,
    qty        INTEGER NOT NULL DEFAULT 1,
    category   TEXT    NOT NULL,
    status     TEXT    NOT NULL DEFAULT 'needed',
    notes      TEXT    NOT NULL DEFAULT '',
    budget_cents INTEGER,
    created_at TEXT    NOT NULL
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
