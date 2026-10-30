package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

var (
	version bool
)

var rootCmd = &cobra.Command{
	Use:     "cbox [command] [options]",
	Short:   "A chess book position indexer",
	Long:    "Tool for indexing chess positions by analysing chess books utilizing local LLMs models",
	Version: "1.0.0",
	Args:    cobra.NoArgs,
	Run:     run,
}

func Execute() {
	err := rootCmd.Execute()
	if err != nil {
		os.Exit(1)
	}
}

func init() {
	rootCmd.Flags().BoolP("help", "h", false, "Show help for the command")
	rootCmd.Flags().BoolVarP(&version, "version", "v", false, "Show version information")
}

func run(cmd *cobra.Command, args []string) {
	if version {
		fmt.Println("cbox version ", cmd.Version)
		return
	}
}
