// Package testdb creates scratch PostgreSQL databases for tests, on the server
// at CHESSINFO_TEST_DATABASE_URL. Each database is dropped when its test ends.
package testdb

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"os"
	"testing"
	"time"

	"chessbox-informant/database"

	_ "github.com/jackc/pgx/v5/stdlib"
)

// Create returns the URL of a new, empty database. It skips the test when
// CHESSINFO_TEST_DATABASE_URL is not set.
func Create(t testing.TB) string {
	t.Helper()
	adminURL := os.Getenv("CHESSINFO_TEST_DATABASE_URL")
	if adminURL == "" {
		t.Skip("CHESSINFO_TEST_DATABASE_URL is not set")
	}
	admin, err := sql.Open("pgx", adminURL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { admin.Close() })

	name := fmt.Sprintf("chessinfo_test_%d", time.Now().UnixNano())
	if _, err := admin.Exec("create database " + name); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if _, err := admin.Exec("drop database " + name + " with (force)"); err != nil {
			t.Error(err)
		}
	})

	u, err := url.Parse(adminURL)
	if err != nil {
		t.Fatal(err)
	}
	u.Path = "/" + name
	return u.String()
}

// CreateMigrated returns the URL of a new database with every migration
// applied.
func CreateMigrated(t testing.TB) string {
	t.Helper()
	dbURL := Create(t)
	db, err := sql.Open("pgx", dbURL)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	migrator, err := database.NewMigrator(db)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := migrator.Up(context.Background()); err != nil {
		t.Fatal(err)
	}
	return dbURL
}
