# CLAUDE.md

ChessBox Informant is an open-source tool that indexes chess positions to quotes from chess books the user owns (PDF/ePUB). The `chessinfo` CLI indexes a book with local models and stores quotes keyed by position in PostgreSQL; the web app shows the quotes for the position on the board.

Read before working: [README.md](README.md), [docs/architecture.md](docs/architecture.md), [docs/indexing.md](docs/indexing.md), [docs/milestones.md](docs/milestones.md).

## Working rules (mandatory)

1. **Plan first, then ask for approval.** Before writing or changing any code, present a plan: the goal, the files you will touch, the approach, and open questions. Wait for explicit approval before implementing. If the plan has to change materially during implementation, stop and confirm again. Work on one milestone item at a time.
2. **No excessive comments.** Code explains itself through names and structure. Comment only the non-obvious *why*: workarounds, invariants, external constraints. Do not write comments that restate the code, banner/section comments, commented-out code, or doc comments on trivial functions. In Go, add doc comments to exported identifiers only when the name is not self-explanatory.

## Repository map

```
backend/                 Go module `chessbox-informant`, binary `chessinfo` (cobra)
  cmd/                   CLI commands
  internal/              application packages (layout in docs/architecture.md)
  database/migrations/   SQL migrations
  tests/assets/          test fixtures (public-domain or project-authored only)
frontend/                Next.js 16 + React 19, Bun, Biome, Tailwind 4, zustand, chess.js, react-chessboard
  src/app/api/v1/quotes/ read-only quotes endpoint (inbound adapter)
  src/app/contexts/      zustand chess store
  src/app/components/    board, notation, quotes panel
  src/modules/quotes/    hexagonal API code: domain, application, infrastructure
docker-compose.yml       PostgreSQL on localhost:5433
docs/                    architecture, indexing pipeline, milestones
```

## Commands

```bash
docker compose up -d postgres            # database
cd backend && go run . --version         # run the CLI
cd backend && go test ./...              # backend tests
cd frontend && bun install && bun dev    # web app on :3000
cd frontend && bun run lint              # biome check
cd frontend && bun run format            # biome format --write
```

The Makefile wraps these: `make db-up migrate test`, `make build` (`backend/bin/chessinfo`), `make lint`, `make dev`, `make fixtures` (regenerates the fixture ePUB and PDF from `book.md`). `make test` runs the database tests against `CHESSINFO_TEST_DATABASE_URL` (default: the local compose database); each test creates and drops its own database.

## Conventions

- **Frontend:** this Next.js version has breaking changes compared to your training data. Read @frontend/AGENTS.md and the guides in `frontend/node_modules/next/dist/docs/` before writing frontend code. Use Bun, not npm. Biome formats with 4-space indentation.
- **Frontend API (hexagonal):** `domain` has no framework or I/O imports; `application` holds use cases and ports and depends only on `domain`; `infrastructure` implements the ports; route handlers in `src/app/api` are thin inbound adapters. Inner layers never import outer ones. FENs are not validated. Layout: docs/architecture.md#web-app.
- **Position keys:** never compare raw FENs. Always use the position-key normalization defined in docs/architecture.md. The Go and TypeScript implementations must pass the same shared fixtures.
- **Models:** local only, no cloud model APIs. Default models must fit an 8 GB VRAM GPU. The generative LLM must never invent chess moves: it references move tokens found by the tokenizer, and every move is checked by replaying it with a chess rules library.
- **Book content:** never commit copyrighted book text. Fixtures in `backend/tests/assets/` must be public-domain or written for this project.
- **Docs:** when a decision in docs/architecture.md changes, update it there. Tick milestone items in docs/milestones.md when they are done.
