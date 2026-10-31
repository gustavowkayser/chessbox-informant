package cmd

import (
	"database/sql"
	"fmt"
	"os"
	"time"

	"chessbox-informant/database"
	"chessbox-informant/internal/config"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/spf13/cobra"
)

var migrateCmd = &cobra.Command{
	Use:   "migrate",
	Short: "Apply pending database migrations",
	Args:  cobra.NoArgs,
	RunE:  runMigrate,
}

func init() {
	rootCmd.AddCommand(migrateCmd)
}

func runMigrate(cmd *cobra.Command, args []string) error {
	cfg, err := config.Load(cmd.Flags(), os.Getenv)
	if err != nil {
		return err
	}
	db, err := sql.Open("pgx", cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer db.Close()

	ctx := cmd.Context()
	if err := db.PingContext(ctx); err != nil {
		return fmt.Errorf("connect to database: %w", err)
	}
	migrator, err := database.NewMigrator(db)
	if err != nil {
		return err
	}
	results, err := migrator.Up(ctx)
	if err != nil {
		return err
	}
	for _, r := range results {
		fmt.Fprintf(cmd.OutOrStdout(), "applied %s (%s)\n", r.Source.Path, r.Duration.Round(time.Millisecond))
	}
	version, err := migrator.GetDBVersion(ctx)
	if err != nil {
		return err
	}
	fmt.Fprintf(cmd.OutOrStdout(), "database at version %d\n", version)
	return nil
}
