# Repository Guidelines

## Project Structure & Module Organization
- `main.go`: Go HTTP server (port 3001) exposing JSON APIs and an SSE stream; also serves static assets from `public/`. Supports both `/api/...` and `/itemchecklist/api/...` route prefixes for local dev vs. production compatibility.
- `translate.go`: Go utility (build tag `translate`) that parses a tab-separated material list file and regenerates `items.json`; replaces the old `raw.md` / `translate_raw.js` workflow. Run with `go run -tags translate .`.
- `items.json`: Current item checklist state (excluded from git). Timestamped backups are stored under `backups/` (also excluded from git) with an intelligent retention policy (see Backup System below).
- `items_example.json`: Example dataset (committed) useful for seeding a fresh environment.
- `config.json`: Local-only passwords array; never commit real credentials.
- `Caddyfile`: Production reverse-proxy config routing `/itemchecklist/*` to `localhost:3001`; excluded from git.
- `plan.md`: Scratch pad for in-progress feature ideas; not authoritative documentation.
- `public/`: Static frontend — `index.html`, `app.js`, `style.css`, `favicon.png`.
- `node-archive/`: Legacy assets; avoid changing unless intentionally restoring old behavior.

## API Routes

| Route | Method | Auth | Description |
|---|---|---|---|
| `/api/login` | POST | No | Accept `{"password":"…"}`, set `auth_token` cookie (30-day expiry) |
| `/api/check-auth` | GET | Yes | Verify current session validity |
| `/api/items` | GET | Yes | Return all items as JSON array |
| `/api/items/update` | POST | Yes | Set `gathered` for one item (clamped to `[0, target]`) |
| `/api/items/claim` | POST | Yes | Add or remove a named user's claim on a contiguous range of an item |
| `/api/config/passwords` | GET/POST | Yes | List passwords or add/remove one (last password cannot be deleted) |
| `/events` | GET (SSE) | Yes | Server-Sent Events stream; broadcasts updated items JSON to all connected clients on any change |

Authentication uses a cookie (`auth_token` = the password). `requireAuth()` validates it against the passwords array in `config.json`.

## Data Structures

```go
type item struct {
    Name     string
    Target   int      // goal quantity
    Gathered int      // current quantity
    Claims   []claim  // user claim ranges
}

type claim struct {
    Claimer    string  // display name of person claiming
    ClaimStart int     // inclusive range start
    ClaimEnd   int     // exclusive range end
}
```

## Backup System
Backups are written to `backups/` on server start and on every item update. A cleanup pass runs after each backup using a tiered retention policy:
- **Last 2 hours** — keep every backup
- **2–24 hours ago** — keep one per hour
- **1–7 days ago** — keep one per day
- **7–30 days ago** — keep one per week
- **30 days–1 year ago** — keep one per month
- **Beyond 1 year** — keep one per year
- The very first backup is always retained.

## Frontend Architecture (`public/app.js`)
- **Completion modes** (toggle via checkbox or completion bar click):
  - *Panel-based* (default): `sum(gathered) / sum(target)` — completion bar is **cyan**.
  - *Item-based*: average of `gathered/target` per item (zero-target items excluded) — completion bar is **yellow**.
- **Completion bar**: Fixed vertical bar on the right edge showing current percentage; click to toggle mode.
- **Fuzzy search**: fzf-style scoring with bonuses for consecutive matches, word-start matches, and shorter strings; matched characters are highlighted with a pulse animation.
- **Sorting**: Three sort keys (alphabetical, progress, target size) × three finished-item priorities (neutral, first, last); selection persisted to `localStorage`.
- **Claims visualization**: Colored bars below each slider represent claimed ranges per user.
- **Drag interaction**: Custom pointer-event slider with live count updates; a `pendingRender` queue prevents re-renders while dragging.
- **Real-time sync**: `EventSource` listener applies server broadcasts to all open tabs.

Key client state variables: `allItems`, `currentItems`, `claimMode`, `completionMode`, `dragActive`, `searchQuery`, `lastUpdate`.

## Build, Test, and Development Commands
- `go run .` (or `go run main.go`): Start the server at `http://localhost:3001`.
- `go run -tags translate .`: Regenerate `items.json` from the material-list text file via `translate.go`; verify a new file appears in `backups/`.
- `npm install` (root): Only if adding Node tooling for scripts; not required for normal Go work.

## Coding Style & Naming Conventions
- Go: Keep code `gofmt`-clean; idiomatic naming; prefer small helper functions for I/O, auth, and SSE handling.
- JavaScript: 4-space indentation with semicolons; API paths stay kebab-cased, client state camelCase.
- Config/paths: Keep existing API routes stable; avoid introducing non-ASCII unless already present.

## Testing Guidelines
- No automated suite wired. Manual checks: `go run .`, log in with a password from `config.json`, adjust sliders, refresh to confirm persistence.
- Open two tabs to confirm SSE sync (changes in one tab reflect in the other).
- Test claims: add a claim, verify the bar renders below the slider and the range is enforced server-side.
- Test search: type a partial/out-of-order name and confirm fuzzy scoring ranks expected results first.
- After `go run -tags translate .`, ensure `items.json` entries include `name`, `target`, `gathered`, `claims` and a fresh backup exists.
- Add focused tests under `tests/` if extracting new logic (e.g., backup rotation or auth helpers).

## Deployment
- Production: Caddy reverse-proxies `rules.tysmp.com/itemchecklist/*` → `localhost:3001`. The `/itemchecklist/api/...` route prefix mirrors `/api/...` so the same binary works in both environments without build flags.
- TLS 1.2+ enforced by Caddy; no TLS config needed in the Go binary.

## Commit & Pull Request Guidelines
- Commits: Short, present-tense subjects (e.g., `add backup cleanup`, `improve login error copy`); do not commit sensitive `config.json` contents, `items.json`, or large `backups/` artifacts.
- PRs: Include summary, how to run (`go run .`), manual test notes, and screenshots/GIFs for UI tweaks. Call out schema/config changes (`items.json` shape, backup retention, new API fields) so reviewers can validate deployments.
