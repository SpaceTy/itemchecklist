# Repository Guidelines

## Project Structure & Module Organization
- `src/main.go`: Go HTTP server (port 3001) entry point; opens SQLite, starts the change-tree pruner, and serves APIs plus static assets from `public/`. Supports both `/api/...` and `/itemchecklist/api/...` route prefixes for local dev vs. production compatibility.
- `src/db.go`: SQLite connection/schema setup and one-time import of legacy JSON files.
- `src/store.go`: SQLite-backed list read/write helpers and change-node recording.
- `src/changes.go`: change-tree history, rollback, pruning, and history API handlers.
- `src/items.go`: list/item/claim handlers plus normalization and contribution logic.
- `src/auth.go`, `src/users.go`: authentication plus admin user/settings handlers backed by SQLite.
- `src/translate.go`: Litematica material-list parser used by list import.
- `itemchecklist.db`: SQLite database (ignored by git) holding users, settings, lists, items, and the change tree. Older `items.json` / `lists.json` / `users.json` / `settings.json` files are imported once on first start and left untouched.
- `items_example.json`: Example dataset (committed) useful for seeding a fresh environment.
- `Caddyfile`: Production reverse-proxy config routing `/itemchecklist/*` to `localhost:3001`; excluded from git.
- `plan.md`: Scratch pad for in-progress feature ideas; not authoritative documentation.
- `public/`: Static frontend — `index.html`, `app.js`, `style.css`, `favicon.png`.
- `backups/`: Legacy backup files; no longer written by the server.
- `node-archive/`: Legacy assets; avoid changing unless intentionally restoring old behavior.

## API Routes

| Route | Method | Auth | Description |
|---|---|---|---|
| `/api/login` | POST | No | Accept `{"username":"…","password":"…"}`, set JWT cookie (30-day expiry) |
| `/api/register` | POST | No | Create an account (subject to registration lockdown) |
| `/api/logout` | POST | No | Clear the auth cookie |
| `/api/check-auth` | GET | Yes | Verify current session validity |
| `/api/lists` | GET | Yes | List accessible lists (`?scope=all` admin-only, includes soft-deleted lists) |
| `/api/lists/import` | POST | Yes | Import a Litematica material list |
| `/api/lists/join` | POST | Yes | Join a list with an invite code |
| `/api/lists/{id}` | GET/PATCH/DELETE | Yes | Read, rename/transfer, or soft-delete a list |
| `/api/lists/{id}/items` | GET/POST/DELETE | Yes | Read items, upsert an item, or delete an item |
| `/api/lists/{id}/items/update` | POST | Yes | Apply a gathered-amount delta |
| `/api/lists/{id}/items/claim` | POST | Yes | Add or remove a named claim |
| `/api/lists/{id}/invites` | GET/POST/DELETE | Yes | Manage invite codes |
| `/api/lists/{id}/members` | DELETE | Yes | Remove a collaborator |
| `/api/lists/{id}/history` | GET | Yes | List change-tree nodes (newest first) |
| `/api/lists/{id}/history/{changeID}` | GET/POST | Yes | Preview a node or roll the list back to it |
| `/api/admin/users` | GET/POST | Admin | List users or freeze/purge/delete/toggle-admin |
| `/api/admin/settings` | GET/POST | Admin | Read or update registration lockdown |
| `/events` | GET (SSE) | Yes | Server-Sent Events stream; broadcasts updated items to connected clients |

Authentication uses a signed JWT cookie (`auth_token`). State-changing requests are serialized in-process so read-modify-write handlers cannot overwrite each other.

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

## SQLite & Change Tree
All mutable data lives in `itemchecklist.db` (WAL mode). Tables materialize the current state (`users`, `settings`, `lists`, `items`, contributions, claims, collaborators, invites). The `change_nodes` table stores the history as a tree:

```go
type changeNode struct {
    ID        int64
    ListID    string
    ParentID  *int64 // previous head when recorded
    Actor     string
    Kind      string // gathered_updated, item_deleted, rollback, …
    Summary   string
    Snapshot  string // full itemList JSON at that node
    CreatedAt string
    Prunable  bool   // individual amount changes
}
```

- Every actual list mutation writes a node containing a full snapshot of the list and updates `lists.head_change_id`.
- Rollback restores a node's snapshot and writes a new `rollback` node whose parent is the restored node, so the original branch is preserved.
- Lists and users are soft-deleted (`deleted = 1`) rather than removed. Deleted lists can be inspected by admins via `?scope=all` and restored by rolling back their change tree.
- Individual amount changes (`gathered_updated`) are pruned after 7 days. Pruning reparents surviving children to the nearest retained ancestor and never removes a list's current head or any structural change.
- Legacy JSON files are imported once on first start and then left untouched.

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
- `go run ./src` (or `make run`): Start the server at `http://localhost:3001`; creates/imports `itemchecklist.db`.
- `go build -o itemchecklist ./src` (or `make build`): Build the production binary.
- `go test ./src`: Run focused tests for change-tree pruning, rollback, and storage logic.
- `gofmt -w src`: Format Go sources.
- `npm install` (root): Only if adding Node tooling for scripts; not required for normal Go work.

## Coding Style & Naming Conventions
- Go: Keep code `gofmt`-clean; idiomatic naming; prefer small helper functions for I/O, auth, and SSE handling.
- JavaScript: 4-space indentation with semicolons; API paths stay kebab-cased, client state camelCase.
- Config/paths: Keep existing API routes stable; avoid introducing non-ASCII unless already present.

## Testing Guidelines
- Run `go test ./src` for the change-tree pruning and rollback tests.
- Manual checks: `go run ./src`, register/login, import a list, adjust sliders, refresh to confirm persistence.
- Open two tabs to confirm SSE sync (changes in one tab reflect in the other).
- Test claims: add a claim, verify the bar renders below the slider and the range is enforced server-side.
- Test search: type a partial/out-of-order name and confirm fuzzy scoring ranks expected results first.
- Test history: open a list's Manage panel → History, preview a node, roll it back, and confirm a new `rollback` node appears while earlier branches remain.
- Test deletion: delete a list, verify it disappears for normal users, appears under Admin → All Lists as deleted, and can be restored by rolling back.
- Add focused tests under `src/` for new change-tree or storage logic.

## Deployment
- Production: Caddy reverse-proxies `rules.tysmp.com/itemchecklist/*` → `localhost:3001`. The `/itemchecklist/api/...` route prefix mirrors `/api/...` so the same binary works in both environments without build flags.
- TLS 1.2+ enforced by Caddy; no TLS config needed in the Go binary.

## Commit & Pull Request Guidelines
- Commits: Short, present-tense subjects (e.g., `add change-tree rollback`, `improve login error copy`); do not commit `itemchecklist.db*`, legacy JSON data files, `secret.key`, or legacy `backups/` artifacts.
- PRs: Include summary, how to run (`go run ./src`), manual test notes, and screenshots/GIFs for UI tweaks. Call out schema changes (new tables/columns, change-node kinds, pruning retention) so reviewers can validate deployments.
