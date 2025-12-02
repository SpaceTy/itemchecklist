# Repository Guidelines

## Project Structure & Module Organization
- `main.go`: Go HTTP server exposing JSON APIs (`/api/login`, `/api/check-auth`, `/api/items`, `/api/items/update`, `/api/config/passwords`) plus an SSE stream at `/events`; also serves static assets from `public/`.
- `items.json`: Current item checklist state; timestamped backups stored under `backups/` are created on start/update.
- `config.json`: Local-only passwords array; never commit real credentials.
- `raw.md` and `translate_raw.js`: Source lines (`"Name",5` with optional `✅`) and converter to rebuild `items.json`; run after bulk edits.
- `node-archive/`: Legacy assets; avoid changing unless intentionally restoring old behavior.

## Build, Test, and Development Commands
- `go run .` (or `go run main.go`): Start the server at `http://localhost:3001`, serving UI, APIs, and SSE updates.
- `node translate_raw.js`: Regenerate `items.json` from `raw.md`; verify a new file appears in `backups/`.
- `npm install` (root): Only if adding Node tooling for scripts; not required for normal Go work.

## Coding Style & Naming Conventions
- Go: Keep code `gofmt`-clean; idiomatic naming; prefer small helper functions for I/O, auth, and SSE handling.
- JavaScript: 4-space indentation with semicolons; API paths stay kebab-cased, client state camelCase.
- Config/paths: Keep existing API routes stable; avoid introducing non-ASCII unless already present.

## Testing Guidelines
- No automated suite wired. Manual checks: `go run .`, log in with a password from `config.json`, adjust sliders, refresh to confirm persistence.
- Open two tabs to confirm SSE sync (changes in one tab reflect in the other).
- After `node translate_raw.js`, ensure `items.json` entries include `name`, `target`, `gathered` and a fresh backup exists.
- Add focused tests under `tests/` if extracting new logic (e.g., backup rotation or auth helpers).

## Commit & Pull Request Guidelines
- Commits: Short, present-tense subjects (e.g., `add backup cleanup`, `improve login error copy`); do not commit sensitive `config.json` contents or large `backups/` artifacts.
- PRs: Include summary, how to run (`go run .`), manual test notes, and screenshots/GIFs for UI tweaks. Call out schema/config changes (`items.json` shape, backup retention) so reviewers can validate deployments.
