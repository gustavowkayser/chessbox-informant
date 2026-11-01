package cmd

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"chessbox-informant/internal/book"
	"chessbox-informant/internal/store"
	"chessbox-informant/internal/testdb"

	"github.com/jackc/pgx/v5"
)

func indexFixture(t *testing.T) (*store.Store, *pgx.Conn, store.Book, int) {
	t.Helper()
	ctx := context.Background()
	cfg := testConfig()
	cfg.DatabaseURL = testdb.CreateMigrated(t)
	file := filepath.Join(fixtureDir, "book.epub")

	st, err := store.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(st.Close)
	conn, err := pgx.Connect(ctx, cfg.DatabaseURL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close(ctx) })

	var out bytes.Buffer
	if err := listBooks(ctx, &out, st); err != nil || out.String() != "no books indexed\n" {
		t.Fatalf("listBooks before indexing = %q, %v", out.String(), err)
	}
	if err := indexBook(ctx, &out, &out, cfg, file, false); err != nil {
		t.Fatal(err)
	}
	sum, err := book.FileSHA256(file)
	if err != nil {
		t.Fatal(err)
	}
	b, err := st.ResolveBook(ctx, sum)
	if err != nil {
		t.Fatal(err)
	}
	var chunks int
	if err := conn.QueryRow(ctx, `select count(*) from chunks where book_id = $1`, b.ID).Scan(&chunks); err != nil {
		t.Fatal(err)
	}
	return st, conn, b, chunks
}

func assertMatches(t *testing.T, out string, patterns ...string) {
	t.Helper()
	for _, p := range patterns {
		if !regexp.MustCompile(p).MatchString(out) {
			t.Errorf("output does not match %s:\n%s", p, out)
		}
	}
}

func TestBooksList(t *testing.T) {
	st, _, b, chunks := indexFixture(t)

	var out bytes.Buffer
	if err := listBooks(context.Background(), &out, st); err != nil {
		t.Fatal(err)
	}
	assertMatches(t, out.String(),
		`^id\s+title\s+author\s+format\s+status\s+chunks\s+quotes\s+indexed\n`,
		fmt.Sprintf(`(?m)^%s\s+Classics Retold\s+ChessBox Informant contributors\s+epub\s+extracted\s+%d\s+0\s+\d{4}-\d{2}-\d{2}$`, b.ID[:8], chunks),
	)
}

func TestBooksShow(t *testing.T) {
	ctx := context.Background()
	st, conn, b, chunks := indexFixture(t)

	var out bytes.Buffer
	if err := showBook(ctx, &out, st, b.SHA256[:10]); err != nil {
		t.Fatal(err)
	}
	assertMatches(t, out.String(),
		`(?m)^id\s+`+b.ID+`$`,
		`(?m)^sha256\s+`+b.SHA256+`$`,
		`(?m)^title\s+Classics Retold$`,
		`(?m)^author\s+ChessBox Informant contributors$`,
		`(?m)^format\s+epub$`,
		fmt.Sprintf(`(?m)^chunks\s+%d \(extracted %d\)$`, chunks, chunks),
		`(?m)^quotes\s+0$`,
	)
	if strings.Contains(out.String(), "failed chunks") {
		t.Errorf("output lists failed chunks:\n%s", out.String())
	}

	if _, err := conn.Exec(ctx, `update chunks set stage = $2, error = $3 where book_id = $1 and ordinal = 2`,
		b.ID, store.StageFailed, "m7 `24.Qg3+` is illegal after 23.Nf5 gxf5"); err != nil {
		t.Fatal(err)
	}
	out.Reset()
	if err := showBook(ctx, &out, st, b.ID[:8]); err != nil {
		t.Fatal(err)
	}
	assertMatches(t, out.String(),
		fmt.Sprintf(`(?m)^chunks\s+%d \(extracted %d, failed 1\)$`, chunks, chunks-1),
		`(?m)^failed chunks\n#\s+page\s+chapter\s+error\n2\s+-\s+\S.*\s+m7 `+"`24\\.Qg3\\+`"+` is illegal after 23\.Nf5 gxf5$`,
	)
}

func TestBooksRemove(t *testing.T) {
	ctx := context.Background()
	st, conn, b, chunks := indexFixture(t)
	countChunks := func() int {
		t.Helper()
		var n int
		if err := conn.QueryRow(ctx, `select count(*) from chunks`).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}

	for _, answer := range []string{"", "n\n", "no\n", "maybe\n"} {
		var out bytes.Buffer
		if err := removeBook(ctx, strings.NewReader(answer), &out, st, b.ID[:8], false); err != nil {
			t.Fatal(err)
		}
		want := fmt.Sprintf("Remove \"Classics Retold\" with %d chunks and 0 quotes? [y/N] not removed\n", chunks)
		if out.String() != want {
			t.Errorf("answer %q: output = %q; want %q", answer, out.String(), want)
		}
		if n := countChunks(); n != chunks {
			t.Fatalf("answer %q left %d chunks; want %d", answer, n, chunks)
		}
	}

	var out bytes.Buffer
	if err := removeBook(ctx, strings.NewReader("Y\n"), &out, st, b.SHA256, false); err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(out.String(), "[y/N] removed "+b.ID+" Classics Retold\n") {
		t.Errorf("output = %q", out.String())
	}
	if n := countChunks(); n != 0 {
		t.Errorf("%d chunks left after removal", n)
	}

	if err := removeBook(ctx, strings.NewReader("y\n"), &out, st, b.ID, false); !errors.Is(err, store.ErrBookNotFound) {
		t.Errorf("removing again: err = %v; want ErrBookNotFound", err)
	}
}

func TestBooksRemoveYesSkipsThePrompt(t *testing.T) {
	ctx := context.Background()
	st, _, b, _ := indexFixture(t)

	var out bytes.Buffer
	if err := removeBook(ctx, strings.NewReader(""), &out, st, b.ID, true); err != nil {
		t.Fatal(err)
	}
	if want := "removed " + b.ID + " Classics Retold\n"; out.String() != want {
		t.Errorf("output = %q; want %q", out.String(), want)
	}
	if _, err := st.ResolveBook(ctx, b.ID); !errors.Is(err, store.ErrBookNotFound) {
		t.Errorf("book still resolves: %v", err)
	}
}

func TestBooksUnknownRef(t *testing.T) {
	ctx := context.Background()
	st, _, _, _ := indexFixture(t)

	var out bytes.Buffer
	if err := showBook(ctx, &out, st, "not-a-book"); !errors.Is(err, store.ErrBookNotFound) {
		t.Errorf("showBook err = %v", err)
	}
	if err := removeBook(ctx, strings.NewReader("y\n"), &out, st, "not-a-book", false); !errors.Is(err, store.ErrBookNotFound) {
		t.Errorf("removeBook err = %v", err)
	}
	if out.Len() > 0 {
		t.Errorf("output = %q", out.String())
	}
}
