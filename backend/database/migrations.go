package database

import (
	"database/sql"
	"embed"
	"io/fs"

	"github.com/pressly/goose/v3"
)

//go:embed migrations/*.sql
var embedded embed.FS

func NewMigrator(db *sql.DB) (*goose.Provider, error) {
	migrations, err := fs.Sub(embedded, "migrations")
	if err != nil {
		return nil, err
	}
	return goose.NewProvider(goose.DialectPostgres, db, migrations)
}
