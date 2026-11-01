package store_test

import (
	"context"
	"testing"

	"chessbox-informant/internal/book"
	"chessbox-informant/internal/store"
	"chessbox-informant/internal/testdb"

	"github.com/jackc/pgx/v5"
)

func openStore(t *testing.T) (*store.Store, string) {
	t.Helper()
	dbURL := testdb.CreateMigrated(t)
	s, err := store.Open(context.Background(), dbURL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	return s, dbURL
}

func TestCreateBook(t *testing.T) {
	ctx := context.Background()
	s, dbURL := openStore(t)

	newBook := store.Book{SHA256: "abc123", Title: "Classics Retold", Language: "en", Format: book.FormatEPUB}
	chunks := []book.Chunk{
		{Ordinal: 1, Chapter: "Preface", Text: "First chunk."},
		{Ordinal: 2, Chapter: "Chapter 1", Page: 12, Text: "Second chunk."},
	}
	created, ok, err := s.CreateBook(ctx, newBook, chunks)
	if err != nil {
		t.Fatal(err)
	}
	if !ok || created.ID == "" || created.Status != store.BookExtracted || created.CreatedAt.IsZero() {
		t.Fatalf("created = %+v, %v", created, ok)
	}
	if created.Title != "Classics Retold" || created.Author != "" || created.Language != "en" || created.Format != book.FormatEPUB {
		t.Errorf("created = %+v", created)
	}

	found, ok, err := s.FindBookBySHA256(ctx, "abc123")
	if err != nil || !ok || found != created {
		t.Errorf("FindBookBySHA256 = %+v, %v, %v; want %+v", found, ok, err, created)
	}

	conn, err := pgx.Connect(ctx, dbURL)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close(ctx)
	var author *string
	if err := conn.QueryRow(ctx, `select author from books where id = $1`, created.ID).Scan(&author); err != nil || author != nil {
		t.Errorf("author = %v, %v; want NULL", author, err)
	}
	rows, err := conn.Query(ctx, `select ordinal, chapter, page, text, stage from chunks where book_id = $1 order by ordinal`, created.ID)
	if err != nil {
		t.Fatal(err)
	}
	type chunkRow struct {
		Ordinal int
		Chapter *string
		Page    *int
		Text    string
		Stage   string
	}
	got, err := pgx.CollectRows(rows, pgx.RowToStructByPos[chunkRow])
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d chunks", len(got))
	}
	if got[0].Page != nil || *got[0].Chapter != "Preface" || got[0].Text != "First chunk." || got[0].Stage != store.StageExtracted {
		t.Errorf("chunk 1 = %+v", got[0])
	}
	if *got[1].Page != 12 || *got[1].Chapter != "Chapter 1" || got[1].Ordinal != 2 {
		t.Errorf("chunk 2 = %+v", got[1])
	}
}

func TestCreateBookTwiceKeepsTheFirst(t *testing.T) {
	ctx := context.Background()
	s, dbURL := openStore(t)

	first, _, err := s.CreateBook(ctx, store.Book{SHA256: "same", Title: "First", Format: book.FormatPDF},
		[]book.Chunk{{Ordinal: 1, Text: "Only chunk."}})
	if err != nil {
		t.Fatal(err)
	}
	second, created, err := s.CreateBook(ctx, store.Book{SHA256: "same", Title: "Second", Format: book.FormatPDF},
		[]book.Chunk{{Ordinal: 1, Text: "Other."}, {Ordinal: 2, Text: "More."}})
	if err != nil {
		t.Fatal(err)
	}
	if created || second != first {
		t.Errorf("second = %+v, created %v; want %+v", second, created, first)
	}

	conn, err := pgx.Connect(ctx, dbURL)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close(ctx)
	var books, chunks int
	if err := conn.QueryRow(ctx, `select (select count(*) from books), (select count(*) from chunks)`).Scan(&books, &chunks); err != nil {
		t.Fatal(err)
	}
	if books != 1 || chunks != 1 {
		t.Errorf("books = %d, chunks = %d; want 1 and 1", books, chunks)
	}
}

func TestFindBookBySHA256Missing(t *testing.T) {
	s, _ := openStore(t)
	if _, ok, err := s.FindBookBySHA256(context.Background(), "missing"); ok || err != nil {
		t.Errorf("ok = %v, err = %v", ok, err)
	}
}
