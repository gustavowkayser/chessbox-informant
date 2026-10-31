-- +goose Up
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

-- +goose Down
drop table quote_positions;
drop table quotes;
drop table games;
drop table chunks;
drop table books;
