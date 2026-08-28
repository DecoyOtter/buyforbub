# Buy for Bub

[![CI](https://github.com/DecoyOtter/buyforbub/actions/workflows/ci.yml/badge.svg)](https://github.com/DecoyOtter/buyforbub/actions/workflows/ci.yml)

A dead-simple checklist for tracking what you need to buy before a baby arrives.
Server-rendered Go app, single SQLite file, one ~7 MB Docker image. Built to run
on Unraid.

## What it does

- Starts with a checklist of ~45 common pre-baby items, grouped by category.
- Tick things off as you buy them. Bought items grey out and sink to the bottom
  of their group, with a progress count at the top.
- Add, edit, and delete your own items.
- **Options per item** — paste links to the cots (or prams, or car seats) you are
  considering, with an optional label and price, and compare them in one place.
  Hitting **Choose** on one marks the item bought and records which one you went
  with.
- Works on a phone; add it to your home screen and it opens like an app.
- Light and dark, following your device.

There are no accounts and no login. It is meant for your own network.

## Running on Unraid

The image is public on GHCR, so no registry login is needed.

**1. Create the data directory and give it the right owner.** This step matters:
the container runs as `99:100` (`nobody:users`) and cannot create its database
in a directory it does not own.

```sh
mkdir -p /mnt/user/appdata/buyforbub
chown -R 99:100 /mnt/user/appdata/buyforbub
```

**2. Add a stack in Compose Manager** (Docker tab → Compose Manager → Add New
Stack), and paste in [`docker-compose.yml`](docker-compose.yml). Adjust before
starting:

- `TZ` — set your timezone.
- The host side of `8080:8080`, if something already uses port 8080.

**3. Start the stack**, then open `http://<your-unraid-ip>:8080`.

On first start it creates the database and seeds the default checklist. That
only happens once — restarts and updates keep your data.

### Updating

```sh
docker compose pull && docker compose up -d
```

Every push to `main` publishes a new `:latest`, plus a `:sha-<commit>` tag if you
would rather pin.

### Remote access

Keep it on the LAN. If you want it from outside, put it behind something that
handles authentication — Cloudflare Access, Tailscale, or an authenticating
reverse proxy. Do not port-forward it directly; anyone who reaches it can edit
the list.

## Configuration

| Variable    | Default              | Description                  |
|-------------|----------------------|------------------------------|
| `PORT`      | `8080`               | HTTP listen port             |
| `DB_PATH`   | `/data/buyforbub.db` | SQLite database file         |
| `TZ`        | system               | Timezone for log timestamps  |
| `APP_TITLE` | `Buy for Bub`        | Title shown in the UI        |

## Customising the starting checklist

The default items live in [`seed.json`](seed.json) — plain JSON, one object per
item:

```json
{ "name": "Cot mattress", "qty": 1, "category": "Nursery", "notes": "Must fit the cot snugly" }
```

`category` must be one of: `Nursery`, `Feeding`, `Clothing`, `Travel`, `Health`,
`Other`. `qty` and `notes` are optional.

Seeding only runs against an empty database, so editing this file changes what a
*fresh* install starts with. To re-seed an existing one, stop the container,
delete `buyforbub.db` from the appdata directory, and start it again — you will
lose anything you had added.

## Development

Needs Go 1.27+. Nothing else — no Node, no build step, no code generation. htmx
is vendored into `internal/web/static/`.

```sh
go run .                 # http://localhost:8080, database in ./data
go test ./...            # all tests
go test ./... -race      # what CI runs
```

Run configurations are checked in for both editors:

- **VS Code** — "Buy for Bub" (F5). Needs the `golang.go` extension, which is
  recommended automatically when you open the folder.
- **WebStorm** — "Buy for Bub", a shell-script configuration. WebStorm has no Go
  plugin (that is GoLand or IntelliJ IDEA Ultimate only), so it runs `go run .`
  without language support. VS Code is the better choice for the Go itself.

### Layout

```
main.go              config, server lifecycle, healthcheck flag
seed.json            default checklist (embedded)
internal/store/      SQLite: items, options, validation, grouping
internal/web/        handlers, templates, static assets
```

### How the UI updates

Every mutation returns the whole `#list` fragment rather than swapping
individual rows. Toggling re-sorts an item within its group and editing can move
it between categories, so a row-level swap would be wrong — and at this size the
re-render is free. Option changes return just the open item's panel, so it stays
open while you paste links in.

### Docker

```sh
docker build -t buyforbub .
docker run --rm -p 8080:8080 -v buyforbub-data:/data buyforbub
```

The image is `distroless/static:nonroot`, so it has no shell. Docker's
`HEALTHCHECK` runs `/buyforbub -healthcheck`, which the binary answers by
requesting its own `/healthz`.

## License

MIT — see [LICENSE](LICENSE).
