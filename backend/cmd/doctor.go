package cmd

import (
	"context"
	"fmt"
	"io"
	"os"

	"chessbox-informant/internal/config"
	"chessbox-informant/internal/doctor"

	"github.com/spf13/cobra"
)

var doctorCmd = &cobra.Command{
	Use:   "doctor",
	Short: "Check the database, Ollama and the models",
	Long: "Checks that PostgreSQL is migrated, that Ollama is " + doctor.MinOllamaVersion + " or newer,\n" +
		"that the LLM is pulled and that the System One model answers.\n" +
		"With --load, it also loads the LLM to see how much of it fits in VRAM.",
	Args: cobra.NoArgs,
	RunE: runDoctor,
}

func init() {
	rootCmd.AddCommand(doctorCmd)
	doctorCmd.Flags().Bool("load", false, "Load the LLM to measure how much of it fits in VRAM")
}

func runDoctor(cmd *cobra.Command, args []string) error {
	cfg, err := config.Load(cmd.Flags(), os.Getenv)
	if err != nil {
		return err
	}
	load, err := cmd.Flags().GetBool("load")
	if err != nil {
		return err
	}
	return runChecks(cmd.Context(), doctor.New(cfg, load), cmd.OutOrStdout())
}

func runChecks(ctx context.Context, d doctor.Doctor, out io.Writer) error {
	var checks, failed int
	d.Run(ctx, func(r doctor.Result) {
		checks++
		if r.Status == doctor.Fail {
			failed++
		}
		fmt.Fprintf(out, "%-4s  %-10s  %s\n", r.Status, r.Check, r.Detail)
	})
	if failed > 0 {
		return fmt.Errorf("%d of %d checks failed", failed, checks)
	}
	return nil
}
