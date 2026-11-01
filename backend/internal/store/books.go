package store

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
)

// StageFailed is the stage of a chunk the pipeline gave up on; the reason is
// in chunks.error.
const StageFailed = "failed"

var (
	ErrBookNotFound = errors.New("no book matches")
	ErrAmbiguousRef = errors.New("more than one book matches")
)

type BookSummary struct {
	Book
	Chunks int
	Quotes int
}

func (s *Store) ListBooks(ctx context.Context) ([]BookSummary, error) {
	rows, err := s.pool.Query(ctx, `
		select `+bookColumns+`,
			(select count(*) from chunks where chunks.book_id = books.id),
			(select count(*) from quotes where quotes.book_id = books.id)
		from books
		order by created_at desc, title`)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (BookSummary, error) {
		var b BookSummary
		err := row.Scan(&b.ID, &b.SHA256, &b.Title, &b.Author, &b.Language, &b.Format, &b.Status, &b.CreatedAt, &b.Chunks, &b.Quotes)
		return b, err
	})
}

// ResolveBook finds the book whose ID or SHA-256 starts with ref.
func (s *Store) ResolveBook(ctx context.Context, ref string) (Book, error) {
	prefix := strings.ToLower(strings.TrimSpace(ref))
	if prefix == "" {
		return Book{}, fmt.Errorf("%w: empty book reference", ErrBookNotFound)
	}
	rows, err := s.pool.Query(ctx, `
		select `+bookColumns+` from books
		where starts_with(id::text, $1) or starts_with(sha256, $1)
		limit 2`, prefix)
	if err != nil {
		return Book{}, err
	}
	books, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (Book, error) { return scanBook(row) })
	if err != nil {
		return Book{}, err
	}
	switch len(books) {
	case 0:
		return Book{}, fmt.Errorf("%w %q", ErrBookNotFound, ref)
	case 1:
		return books[0], nil
	default:
		return Book{}, fmt.Errorf("%w %q; use a longer prefix", ErrAmbiguousRef, ref)
	}
}

type StageCount struct {
	Stage  string
	Chunks int
}

type FailedChunk struct {
	Ordinal int
	Chapter string
	Page    int
	Error   string
}

type BookStats struct {
	Stages       []StageCount
	Quotes       int
	FailedChunks []FailedChunk
}

func (s BookStats) Chunks() int {
	total := 0
	for _, st := range s.Stages {
		total += st.Chunks
	}
	return total
}

func (s *Store) BookStats(ctx context.Context, bookID string) (BookStats, error) {
	var stats BookStats
	rows, err := s.pool.Query(ctx, `
		select stage, count(*) from chunks
		where book_id = $1
		group by stage
		order by stage`, bookID)
	if err != nil {
		return BookStats{}, err
	}
	stats.Stages, err = pgx.CollectRows(rows, pgx.RowToStructByPos[StageCount])
	if err != nil {
		return BookStats{}, err
	}

	if err := s.pool.QueryRow(ctx, `select count(*) from quotes where book_id = $1`, bookID).Scan(&stats.Quotes); err != nil {
		return BookStats{}, err
	}

	rows, err = s.pool.Query(ctx, `
		select ordinal, coalesce(chapter, ''), coalesce(page, 0), coalesce(error, '')
		from chunks
		where book_id = $1 and stage = $2
		order by ordinal`, bookID, StageFailed)
	if err != nil {
		return BookStats{}, err
	}
	stats.FailedChunks, err = pgx.CollectRows(rows, pgx.RowToStructByPos[FailedChunk])
	if err != nil {
		return BookStats{}, err
	}
	return stats, nil
}

// RemoveBook deletes a book; the schema cascades the delete to its chunks,
// games, quotes and position links.
func (s *Store) RemoveBook(ctx context.Context, bookID string) (bool, error) {
	tag, err := s.pool.Exec(ctx, `delete from books where id = $1`, bookID)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() == 1, nil
}
