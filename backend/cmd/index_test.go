package cmd

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"chessbox-informant/internal/book"
	"chessbox-informant/internal/config"
	"chessbox-informant/internal/testdb"

	"github.com/jackc/pgx/v5"
)

const fixtureDir = "../tests/assets/classics"

func testConfig() config.Config {
	cfg := config.Default()
	cfg.PopplerDir = os.Getenv("CHESSINFO_POPPLER_DIR")
	return cfg
}

func TestIndexDryRunNeedsNoDatabase(t *testing.T) {
	cfg := testConfig()
	cfg.DatabaseURL = "postgres://nobody@127.0.0.1:1/unreachable"
	file := filepath.Join(fixtureDir, "book.epub")

	var out, errOut bytes.Buffer
	if err := indexBook(context.Background(), &out, &errOut, cfg, file, true); err != nil {
		t.Fatal(err)
	}
	doc, err := book.Extract(context.Background(), file, "")
	if err != nil {
		t.Fatal(err)
	}
	chunks := book.ChunkBlocks(doc.Blocks)
	sum, err := book.FileSHA256(file)
	if err != nil {
		t.Fatal(err)
	}

	for _, want := range []string{
		`(?m)^format\s+epub$`,
		`(?m)^sha256\s+` + sum + `$`,
		`(?m)^title\s+Classics Retold$`,
		`(?m)^chapters\s+5$`,
		fmt.Sprintf(`(?m)^chunks\s+%d$`, len(chunks)),
		`(?m)^1\s+-\s+Preface\s+This little book was written`,
		fmt.Sprintf(`(?m)^%d\s+-\s+Chapter 4: On Pawn Structure\s+`, len(chunks)),
	} {
		if !regexp.MustCompile(want).MatchString(out.String()) {
			t.Errorf("output does not match %s:\n%s", want, out.String())
		}
	}
	if errOut.Len() > 0 {
		t.Errorf("stderr = %q", errOut.String())
	}
}

func TestIndexTwiceDoesNothing(t *testing.T) {
	cfg := testConfig()
	cfg.DatabaseURL = testdb.CreateMigrated(t)
	formats := map[string]bool{"book.epub": true, "book.pdf": popplerAvailable(t, cfg)}

	ctx := context.Background()
	conn, err := pgx.Connect(ctx, cfg.DatabaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close(ctx)
	counts := func() (books, chunks int) {
		t.Helper()
		if err := conn.QueryRow(ctx, `select (select count(*) from books), (select count(*) from chunks)`).Scan(&books, &chunks); err != nil {
			t.Fatal(err)
		}
		return books, chunks
	}

	for name, available := range formats {
		t.Run(name, func(t *testing.T) {
			if !available {
				t.Skip("Poppler is not installed")
			}
			file := filepath.Join(fixtureDir, name)
			booksBefore, chunksBefore := counts()

			var out bytes.Buffer
			if err := indexBook(ctx, &out, &out, cfg, file, false); err != nil {
				t.Fatal(err)
			}
			if !strings.HasPrefix(out.String(), "indexed ") {
				t.Fatalf("first run: %q", out.String())
			}
			booksAfter, chunksAfter := counts()
			if booksAfter != booksBefore+1 || chunksAfter <= chunksBefore {
				t.Fatalf("first run stored %d books and %d chunks", booksAfter-booksBefore, chunksAfter-chunksBefore)
			}

			out.Reset()
			if err := indexBook(ctx, &out, &out, cfg, file, false); err != nil {
				t.Fatal(err)
			}
			if !strings.HasPrefix(out.String(), "already indexed: ") {
				t.Errorf("second run: %q", out.String())
			}
			if b, c := counts(); b != booksAfter || c != chunksAfter {
				t.Errorf("second run changed the counts to %d books and %d chunks", b, c)
			}
		})
	}
}

func popplerAvailable(t *testing.T, cfg config.Config) bool {
	err := book.PDFExtractor{PopplerDir: cfg.PopplerDir}.CheckPoppler(context.Background())
	if err != nil && os.Getenv("CHESSINFO_REQUIRE_POPPLER") != "" {
		t.Fatal(err)
	}
	return err == nil
}

func TestBookTitle(t *testing.T) {
	withTitle := book.Document{Metadata: book.Metadata{Title: "Classics Retold"}}
	if got := bookTitle(withTitle, "/books/x.pdf"); got != "Classics Retold" {
		t.Errorf("bookTitle = %q", got)
	}
	if got := bookTitle(book.Document{}, filepath.Join("books", "My System.v2.pdf")); got != "My System.v2" {
		t.Errorf("bookTitle = %q", got)
	}
}
