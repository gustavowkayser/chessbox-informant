package store_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"chessbox-informant/internal/book"
	"chessbox-informant/internal/store"

	"github.com/jackc/pgx/v5"
)

func createBook(t *testing.T, s *store.Store, sha256, title string, chunks int) store.Book {
	t.Helper()
	cs := make([]book.Chunk, chunks)
	for i := range cs {
		cs[i] = book.Chunk{Ordinal: i + 1, Chapter: "Chapter 1", Page: i + 10, Text: "Some text."}
	}
	b, _, err := s.CreateBook(context.Background(), store.Book{SHA256: sha256, Title: title, Format: book.FormatEPUB}, cs)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func connect(t *testing.T, dbURL string) *pgx.Conn {
	t.Helper()
	conn, err := pgx.Connect(context.Background(), dbURL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close(context.Background()) })
	return conn
}

func exec(t *testing.T, conn *pgx.Conn, sql string, args ...any) {
	t.Helper()
	if _, err := conn.Exec(context.Background(), sql, args...); err != nil {
		t.Fatal(err)
	}
}

// addQuote stores a quote with one position link on the book's first chunk.
func addQuote(t *testing.T, conn *pgx.Conn, bookID string) {
	t.Helper()
	exec(t, conn, `
		with q as (
			insert into quotes (book_id, chunk_id, kind, content)
			select book_id, id, 'discussion', 'A quote.' from chunks where book_id = $1 and ordinal = 1
			returning id
		)
		insert into quote_positions (quote_id, position_key, match_type, ply)
		select id, 'key', 'exact', 0 from q`, bookID)
}

func TestListBooks(t *testing.T) {
	ctx := context.Background()
	s, dbURL := openStore(t)

	if books, err := s.ListBooks(ctx); err != nil || len(books) != 0 {
		t.Fatalf("ListBooks = %+v, %v; want none", books, err)
	}

	older := createBook(t, s, "zz-older", "Older", 3)
	newer := createBook(t, s, "zz-newer", "Newer", 2)
	conn := connect(t, dbURL)
	exec(t, conn, `update books set created_at = now() - interval '1 day' where id = $1`, older.ID)
	addQuote(t, conn, older.ID)

	books, err := s.ListBooks(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(books) != 2 {
		t.Fatalf("got %d books", len(books))
	}
	if books[0].ID != newer.ID || books[0].Chunks != 2 || books[0].Quotes != 0 {
		t.Errorf("books[0] = %+v", books[0])
	}
	if books[1].ID != older.ID || books[1].Title != "Older" || books[1].Chunks != 3 || books[1].Quotes != 1 {
		t.Errorf("books[1] = %+v", books[1])
	}
}

func TestResolveBook(t *testing.T) {
	ctx := context.Background()
	s, _ := openStore(t)
	one := createBook(t, s, "zz-one", "One", 1)
	two := createBook(t, s, "zz-two", "Two", 1)

	for _, tc := range []struct {
		ref  string
		want store.Book
	}{
		{one.ID, one},
		{strings.ToUpper(two.ID), two},
		{two.ID[:8], two},
		{"zz-one", one},
		{" zz-t ", two},
	} {
		got, err := s.ResolveBook(ctx, tc.ref)
		if err != nil || got != tc.want {
			t.Errorf("ResolveBook(%q) = %+v, %v; want %+v", tc.ref, got, err, tc.want)
		}
	}

	for _, tc := range []struct {
		ref  string
		want error
	}{
		{"zz-", store.ErrAmbiguousRef},
		{"zz-three", store.ErrBookNotFound},
		{"  ", store.ErrBookNotFound},
	} {
		if _, err := s.ResolveBook(ctx, tc.ref); !errors.Is(err, tc.want) {
			t.Errorf("ResolveBook(%q) error = %v; want %v", tc.ref, err, tc.want)
		}
	}
}

func TestBookStats(t *testing.T) {
	ctx := context.Background()
	s, dbURL := openStore(t)
	b := createBook(t, s, "zz-stats", "Stats", 4)
	createBook(t, s, "zz-other", "Other", 5)
	conn := connect(t, dbURL)
	exec(t, conn, `update chunks set stage = $2, error = 'm7 24.Qg3+ is illegal' where book_id = $1 and ordinal = 3`, b.ID, store.StageFailed)
	exec(t, conn, `update chunks set stage = $2, chapter = null, page = null where book_id = $1 and ordinal = 2`, b.ID, store.StageFailed)
	addQuote(t, conn, b.ID)

	stats, err := s.BookStats(ctx, b.ID)
	if err != nil {
		t.Fatal(err)
	}
	wantStages := []store.StageCount{{store.StageExtracted, 2}, {store.StageFailed, 2}}
	if len(stats.Stages) != 2 || stats.Stages[0] != wantStages[0] || stats.Stages[1] != wantStages[1] {
		t.Errorf("Stages = %+v; want %+v", stats.Stages, wantStages)
	}
	if stats.Chunks() != 4 || stats.Quotes != 1 {
		t.Errorf("Chunks() = %d, Quotes = %d; want 4 and 1", stats.Chunks(), stats.Quotes)
	}
	wantFailed := []store.FailedChunk{
		{Ordinal: 2},
		{Ordinal: 3, Chapter: "Chapter 1", Page: 12, Error: "m7 24.Qg3+ is illegal"},
	}
	if len(stats.FailedChunks) != 2 || stats.FailedChunks[0] != wantFailed[0] || stats.FailedChunks[1] != wantFailed[1] {
		t.Errorf("FailedChunks = %+v; want %+v", stats.FailedChunks, wantFailed)
	}
}

func TestRemoveBook(t *testing.T) {
	ctx := context.Background()
	s, dbURL := openStore(t)
	b := createBook(t, s, "zz-remove", "Remove", 3)
	kept := createBook(t, s, "zz-keep", "Keep", 2)
	conn := connect(t, dbURL)
	addQuote(t, conn, b.ID)
	addQuote(t, conn, kept.ID)

	removed, err := s.RemoveBook(ctx, b.ID)
	if err != nil || !removed {
		t.Fatalf("RemoveBook = %v, %v", removed, err)
	}
	var books, chunks, quotes, positions int
	if err := conn.QueryRow(ctx, `select
		(select count(*) from books), (select count(*) from chunks),
		(select count(*) from quotes), (select count(*) from quote_positions)`).Scan(&books, &chunks, &quotes, &positions); err != nil {
		t.Fatal(err)
	}
	if books != 1 || chunks != 2 || quotes != 1 || positions != 1 {
		t.Errorf("left %d books, %d chunks, %d quotes, %d positions; want only the kept book's", books, chunks, quotes, positions)
	}

	if removed, err := s.RemoveBook(ctx, b.ID); err != nil || removed {
		t.Errorf("second RemoveBook = %v, %v; want false", removed, err)
	}
}
