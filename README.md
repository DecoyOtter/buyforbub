# Buy for Bub

A dead-simple checklist for tracking what you need to buy before a baby arrives.
Server-rendered Go app, single SQLite file, one small Docker image. Built to run
on Unraid.

## Features

- Pre-seeded checklist of common pre-baby items — tick them off as you buy.
- Add, edit, and delete your own items.
- Grouped by category, with a progress count.
- Works on phone and desktop; add it to your home screen.
- No accounts, no login — intended for LAN use.

## Configuration

| Variable    | Default              | Description                    |
|-------------|----------------------|--------------------------------|
| `PORT`      | `8080`               | HTTP listen port               |
| `DB_PATH`   | `/data/buyforbub.db` | SQLite database file           |
| `TZ`        | system               | Timezone for displayed times   |
| `APP_TITLE` | `Buy for Bub`        | Title shown in the UI          |

## Running on Unraid

_TODO: Compose Manager steps._

## Development

_TODO._

## License

MIT — see [LICENSE](LICENSE).
