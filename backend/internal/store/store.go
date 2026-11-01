// Package store holds the PostgreSQL repositories of chessinfo.
package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"chessbox-informant/internal/book"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	// BookExtracted is the status of a book whose chunks are stored and
	// waiting for the indexing pipeline.
	BookExtracted = "extracted"
	// StageExtracted is the stage of a chunk that has not been triaged yet.
	StageExtracted = "extracted"
)

type Store struct {
	pool *pgxpool.Pool
}

func Open(ctx context.Context, databaseURL string) (*Store, error) {
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		return nil, err
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("connect to database: %w", err)
	}
	return &Store{pool: pool}, nil
}

func (s *Store) Close() {
	s.pool.Close()
}

type Book struct {
	ID        string
	SHA256    string
	Title     string
	Author    string
	Language  string
	Format    book.Format
	Status    string
	CreatedAt time.Time
}

const bookColumns = `id::text, sha256, title, coalesce(author, ''), coalesce(language, ''), format, status, created_at`

func scanBook(row pgx.Row) (Book, error) {
	var b Book
	err := row.Scan(&b.ID, &b.SHA256, &b.Title, &b.Author, &b.Language, &b.Format, &b.Status, &b.CreatedAt)
	return b, err
}

func (s *Store) FindBookBySHA256(ctx context.Context, sha256 string) (Book, bool, error) {
	b, err := scanBook(s.pool.QueryRow(ctx, `select `+bookColumns+` from books where sha256 = $1`, sha256))
	if errors.Is(err, pgx.ErrNoRows) {
		return Book{}, false, nil
	}
	if err != nil {
		return Book{}, false, err
	}
	return b, true, nil
}

// CreateBook stores a book and its chunks in one transaction. When a book
// with the same SHA-256 already exists, nothing is written and the existing
// book is returned with created set to false.
func (s *Store) CreateBook(ctx context.Context, b Book, chunks []book.Chunk) (stored Book, created bool, err error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Book{}, false, err
	}
	defer tx.Rollback(ctx)

	stored, err = scanBook(tx.QueryRow(ctx, `
		insert into books (id, sha256, title, author, language, format, status)
		values (gen_random_uuid(), $1, $2, nullif($3, ''), nullif($4, ''), $5, $6)
		on conflict (sha256) do nothing
		returning `+bookColumns,
		b.SHA256, b.Title, b.Author, b.Language, b.Format, BookExtracted))
	if errors.Is(err, pgx.ErrNoRows) {
		existing, _, err := s.FindBookBySHA256(ctx, b.SHA256)
		return existing, false, err
	}
	if err != nil {
		return Book{}, false, err
	}

	rows := make([][]any, len(chunks))
	for i, c := range chunks {
		rows[i] = []any{stored.ID, c.Ordinal, nullIfZero(c.Chapter), nullIfZero(c.Page), c.Text, StageExtracted}
	}
	if _, err := tx.CopyFrom(ctx, pgx.Identifier{"chunks"},
		[]string{"book_id", "ordinal", "chapter", "page", "text", "stage"},
		pgx.CopyFromRows(rows)); err != nil {
		return Book{}, false, fmt.Errorf("store chunks: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return Book{}, false, err
	}
	return stored, true, nil
}

func nullIfZero[T comparable](v T) any {
	var zero T
	if v == zero {
		return nil
	}
	return v
}
