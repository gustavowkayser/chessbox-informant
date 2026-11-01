package database_test

import (
	"context"
	"database/sql"
	"slices"
	"testing"

	"chessbox-informant/database"
	"chessbox-informant/internal/testdb"

	_ "github.com/jackc/pgx/v5/stdlib"
)

var schemaTables = []string{"books", "chunks", "games", "quote_positions", "quotes"}

func newTestDatabase(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("pgx", testdb.Create(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func tables(t *testing.T, db *sql.DB) []string {
	t.Helper()
	rows, err := db.Query("select table_name from information_schema.tables where table_schema = 'public' and table_name <> 'goose_db_version' order by table_name")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var names []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatal(err)
		}
		names = append(names, name)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return names
}

func TestMigrationsUpDownUp(t *testing.T) {
	ctx := context.Background()
	db := newTestDatabase(t)
	migrator, err := database.NewMigrator(db)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := migrator.Up(ctx); err != nil {
		t.Fatalf("up: %v", err)
	}
	if got := tables(t, db); !slices.Equal(got, schemaTables) {
		t.Fatalf("after up: tables = %v, want %v", got, schemaTables)
	}

	if _, err := migrator.DownTo(ctx, 0); err != nil {
		t.Fatalf("down: %v", err)
	}
	if got := tables(t, db); len(got) != 0 {
		t.Fatalf("after down: tables = %v, want none", got)
	}

	if _, err := migrator.Up(ctx); err != nil {
		t.Fatalf("up again: %v", err)
	}
	if got := tables(t, db); !slices.Equal(got, schemaTables) {
		t.Fatalf("after second up: tables = %v, want %v", got, schemaTables)
	}
}
