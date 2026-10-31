# Architecture

## Overview

ChessBox Informant has two halves that share one PostgreSQL database:

- **`cbox`** (Go CLI, `backend/`) **writes**. It reads a PDF or ePUB, runs the [indexing pipeline](indexing.md) with local models, and stores quotes keyed by chess position.
- **The web app** (Next.js, `frontend/`) **reads**. It shows an interactive board, and after every position change it requests the quotes linked to that position from `/api/v1/quotes`.

```mermaid
flowchart LR
    book[PDF / ePUB] --> cli[cbox index]
    cli <--> laya[Laya<br/>System One]
    cli <--> ollama[Ollama<br/>generative LLM]
    cli --> db[(PostgreSQL)]
    db --> api[Next.js route<br/>/api/v1/quotes]
    api --> ui[Board + quotes panel]
```

All models run locally, and no book content is ever sent over the network.

## Components

### `cbox` CLI

| Command | Purpose |
|---|---|
| `cbox index <file>` | Index a book. Resumable; re-indexing an unchanged file does nothing. |
| `cbox books list` / `show <id>` / `remove <id>` | Manage indexed books and inspect failures. |
| `cbox migrate` | Apply database migrations. |
| `cbox doctor` | Check Postgres, Ollama, pulled models, the Laya endpoint and VRAM. |
| `cbox eval <fixture>` | Measure indexing accuracy against a fixture's ground truth. |

Planned package layout:

```
backend/
  cmd/                 cobra commands
  internal/
    config/            flags, env vars, config file
    book/              ePUB and PDF extractors, chunker
    notation/          move tokenizer, notation conventions (figurine, localized letters)
    chess/             replay, variation tree, position keys
    llm/               generative provider interface + Ollama implementation
    systemone/         System One client (Laya or Ollama)
    pipeline/          stages, per-chunk state, resume
    store/             PostgreSQL repositories
  database/migrations/
  tests/assets/
```

### Web app

The UI lives in `src/app`. The quotes API follows a hexagonal (ports and adapters) architecture in `src/modules/quotes`:

```
frontend/src/
  app/
    api/v1/quotes/route.ts             inbound adapter: HTTP request → use case → JSON
    components/                        board, notation, quotes panel
    contexts/chess.context.tsx         zustand chess store
  modules/quotes/
    domain/                            entities and value objects, no framework or I/O imports
      position-key.ts
      quote.ts
    application/                       use cases and the ports they depend on
      find-quotes-by-position.ts
      ports/quote-repository.ts
    infrastructure/                    adapters
      postgres/client.ts
      postgres/postgres-quote-repository.ts
      http/quotes-response.ts          domain → API contract mapping
    container.ts                       composition root: wires adapters into use cases
```

```mermaid
flowchart LR
    route[route.ts<br/>inbound adapter] --> usecase[FindQuotesByPosition<br/>application]
    usecase --> domain[Quote, PositionKey<br/>domain]
    usecase --> port[[QuoteRepository<br/>port]]
    pg[PostgresQuoteRepository<br/>outbound adapter] -. implements .-> port
    pg --> db[(PostgreSQL)]
```

- **Dependency rule:** `app` → `infrastructure` → `application` → `domain`. Inner layers never import outer ones.
- `route.ts` stays thin: it reads the query parameters, calls the use case through `container.ts`, and maps the result with `quotes-response.ts`.
- Infrastructure files import `server-only`, so database code can never end up in the client bundle.
- Use cases are tested with an in-memory `QuoteRepository` using `bun test`, without a database.
- The quotes panel calls `/api/v1/quotes` with `fetch` and imports only the response type from `infrastructure/http`.

## Position key

Quotes are looked up by a **position key**, never by raw FEN. Two FENs for the same position can differ in their move counters and en passant field, so the key keeps only the first three fields:

```
key = <placement> <side> <castling>
```

- The halfmove clock and fullmove number are dropped, so transpositions and positions reached at different move numbers still match.
- The en passant field is dropped too. FEN generators disagree on when to write it, and checking whether an en passant capture is legal would require move generation. To a reader, positions that differ only in en passant rights are the same position.

Example: `rnbqkbnr/pppp1ppp/8/4p3/4P3/8/PPPP1PPP/RNBQKBNR w KQkq e6 0 2` → `rnbqkbnr/pppp1ppp/8/4p3/4P3/8/PPPP1PPP/RNBQKBNR w KQkq`

Building the key is plain string handling, with no chess logic. It is implemented twice, in Go (`internal/chess`) and TypeScript (`frontend/src/modules/quotes/domain/position-key.ts`), and both are tested against the shared fixture `backend/tests/assets/position_keys.json`.

The web app does not validate the FEN. A malformed or unknown FEN simply matches no quotes.

## Data model

```sql
create table books (
    id          uuid primary key,
    sha256      text not null unique,
    title       text not null,
    author      text,
    language    text,
    format      text not null check (format in ('epub', 'pdf')),
    notation    text,
    status      text not null,
    models      jsonb not null default '{}',
    created_at  timestamptz not null default now()
);

create table chunks (
    id       bigserial primary key,
    book_id  uuid not null references books on delete cascade,
    ordinal  int not null,
    chapter  text,
    page     int,
    text     text not null,
    triage   jsonb,
    stage    text not null,
    error    text,
    unique (book_id, ordinal)
);

create table games (
    id         bigserial primary key,
    book_id    uuid not null references books on delete cascade,
    white      text,
    black      text,
    event      text,
    year       int,
    result     text,
    start_fen  text not null
);

create table quotes (
    id         bigserial primary key,
    book_id    uuid not null references books on delete cascade,
    chunk_id   bigint not null references chunks on delete cascade,
    game_id    bigint references games on delete set null,
    kind       text not null check (kind in ('annotation', 'discussion', 'diagram')),
    content         text not null,
    main_variation  jsonb,
    analysis        jsonb not null default '[]',
    relevancy       real not null default 0
);

create table quote_positions (
    quote_id      bigint not null references quotes on delete cascade,
    position_key  text not null,
    match_type    text not null check (match_type in ('exact', 'main', 'analysis')),
    line          int,
    ply           int not null,
    primary key (quote_id, position_key),
    check ((match_type = 'analysis') = (line is not null)),
    check ((match_type = 'exact') = (ply = 0))
);

create index quote_positions_position_key on quote_positions (position_key);
```

- `books.models` records which models and versions indexed the book, so results stay reproducible.
- `chunks.stage` drives resume. Its values are `extracted`, `triaged`, `skipped`, `parsed`, `linked` and `failed`.
- `quotes.main_variation` and `quotes.analysis` store the lines exactly as the API returns them in `mainVariation` and `moveAnalysis`.
- `quotes.relevancy` is Laya's `instructive` score divided by 3, giving 0, 0.33, 0.67 or 1. It is 0 when triage is off.
- `quote_positions` has these rows for each quote:
  - one `exact` row for the quoted position
  - one `main` row for each position reached on the main variation
  - one `analysis` row for each position reached inside an analysis line

  `line` is the index into `analysis` and is set only on `analysis` rows. `ply` is the number of half-moves into the main variation or the line. When a position occurs more than once in the same quote, only one row is kept: `exact` beats `main`, `main` beats `analysis`, and otherwise the shallowest wins.

## API

`GET /api/v1/quotes?fen=<FEN>&limit=20&cursor=<nextCursor>`

| Parameter | Default | Notes |
|---|---|---|
| `fen` | | Not validated. A missing, malformed or unknown FEN returns `200` with no results. |
| `limit` | `20` | At most `50`. |
| `cursor` | | The `nextCursor` from the previous page. |

```json
{
    "totalResults": 42,
    "nextCursor": "…",
    "results": [
        {
            "id": 812,
            "match": { "type": "exact", "ply": 0 },
            "quote": {
                "content": "The knight on d5 looks nice...",
                "page": 123,
                "chapter": "Chapter 4",
                "book": { "id": "6f1c…", "title": "…", "author": "…" }
            },
            "mainVariation": {
                "start": { "moveNumber": 18, "side": "w" },
                "moves": [
                    { "move": "Rad1", "annotation": "" },
                    { "move": "Qc7", "annotation": "?" }
                ]
            },
            "moveAnalysis": [
                {
                    "start": { "moveNumber": 18, "side": "w" },
                    "moves": [
                        { "move": "Nxd5", "annotation": "!" },
                        { "move": "Nxd5", "annotation": "" }
                    ],
                    "comment": "..."
                }
            ],
            "relevancy": 0.9
        },
        {
            "id": 853,
            "match": { "type": "main", "ply": 3 },
            …
        },
        {
            "id": 907,
            "match": { "type": "analysis", "line": 0, "ply": 2 },
            …
        }
    ]
}
```

- `match.type` says how the position relates to the quote:
  - `exact`: the book quotes this exact position.
  - `main`: the position is on the main variation, the book's main line after the quoted position.
  - `analysis`: the position appears inside one of the quote's analysis lines (sidelines).
- `match.line` appears only on `analysis` matches. It is the index in `moveAnalysis` of the line that contains the position.
- `match.ply` is how many half-moves deep into the main variation or the line the position is. It is always `0` for `exact` matches.
- `quote.content` is the book's prose about the quoted position, verbatim. It is empty when the book gives only moves.
- `mainVariation` holds the book's main-line moves from the quoted position up to the next quoted position, or to the end of the game. It has the same `start` and `moves` as an analysis line, but no `comment`, because the next comment belongs to the next quote. It is `null` when no main-line moves follow.
- `moveAnalysis` holds the sidelines the book gives from the quoted position:
  - `start` is the move number and side of the line's first move, so the UI can show `18.Nxd5! Nxd5` and play the line.
  - `annotation` is the move's glyph (`!`, `?`, `!?` …), or `""` if there is none.
  - `comment` is the book's comment at the end of the line, or `""` if there is none.
- `relevancy` is between 0 and 1 (see the data model).
- `totalResults` counts every match for the position. `nextCursor` is `null` on the last page.
- Results are ordered by match type (`exact`, then `main`, then `analysis`), then by `relevancy` (highest first), then by `ply` (shallowest first). The cursor encodes the sort key of the last result, so pages stay stable.

## Configuration

| Variable | Default | Used by |
|---|---|---|
| `CBOX_DATABASE_URL` | `postgres://postgres:postgres@localhost:5433/postgres` | cbox |
| `DATABASE_URL` | same as above | web app (`frontend/.env.local`) |
| `CBOX_OLLAMA_URL` | `http://localhost:11434` | cbox |
| `CBOX_LLM_MODEL` | `qwen3.5:9b` | cbox |
| `CBOX_LLM_NUM_CTX` | `8192` | cbox |
| `CBOX_SYSTEMONE_URL` | empty: cbox starts and stops Laya itself | cbox |
| `CBOX_SYSTEMONE_MODEL` | `laya` | cbox |

Each variable also has a matching CLI flag. Flags take precedence over environment variables.

## Technical decisions

All decisions below are **approved**. D5 is approved for now and will be revisited in M7.

| # | Topic | Decision | Reason / alternative |
|---|---|---|---|
| D1 | Serving quotes to the UI | The Next.js route handler reads PostgreSQL directly, using a read-only role. | The route stub already exists, and there is no second server to run. Alternative: a `cbox serve` HTTP API. |
| D2 | Generative provider | Ollama `/api/chat` with a JSON-schema `format`, behind an `llm.Provider` interface. | Easy local setup on every OS, with structured output. An OpenAI-compatible provider (LM Studio, llama.cpp) can be added later. |
| D3 | System One provider | Laya served by its Python package (`laya-serve`, `POST /v1/systemone`). The same client can target Ollama's `/v1/systemone`. | Laya is not in Ollama's library as of Oct 2026. Both servers speak the same wire format. |
| D4 | Default LLM | `qwen3.5:9b`, with `qwen3.5:4b` as the low-memory fallback. | See the [VRAM budget](indexing.md#models-and-the-8-gb-vram-budget). |
| D5 | PDF text | Poppler `pdftotext -layout`, behind a `book.Extractor` interface. | Robust, and needs no cgo on Windows. MuPDF (`go-fitz`) is the alternative if image diagrams (M7) need it. |
| D6 | ePUB text | Pure Go: `archive/zip` + `golang.org/x/net/html`. | No external dependency. |
| D7 | Chess rules | `github.com/corentings/chess/v2` | A maintained fork of `notnil/chess`. |
| D8 | Database access | `pgx/v5` in Go; `postgres` (postgres.js) in the web app. | Both are the standard choices. |
| D9 | Migrations | `goose` with SQL files in `backend/database/migrations`. | Plain SQL, and can be embedded in the binary. |
| D10 | Move extraction | A deterministic tokenizer finds move candidates. The LLM only references token IDs. | The LLM cannot hallucinate moves, and the replay validates everything. |
| D11 | License | MIT, in `LICENSE` at the repository root. | |
| D12 | Web app API structure | Hexagonal architecture: `domain`, `application` (use cases and ports), `infrastructure` (adapters). See [Web app](#web-app). | Keeps the query logic testable without Next.js or a database. |
| D13 | FEN input | Not validated. The key is built from the FEN as given. | A bad FEN matches no quotes, so validation adds nothing. |
| D14 | Position key | Placement, side to move and castling only. | Keeping en passant would require move generation in both languages. See [Position key](#position-key). |
| D15 | Quotes API contract | Cursor paging; `exact`, `main` and `analysis` matches with `ply` (`line` only on `analysis`); a separate `mainVariation` plus `moveAnalysis` sidelines, each with `start` and a per-move `annotation`. See [API](#api). | The main line is kept apart from sidelines. |
