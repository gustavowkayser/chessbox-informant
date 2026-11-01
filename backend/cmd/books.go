package cmd

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"text/tabwriter"

	"chessbox-informant/internal/config"
	"chessbox-informant/internal/store"

	"github.com/spf13/cobra"
)

const bookRefHelp = "<book> is a book ID or SHA-256, or a unique prefix of either."

var booksCmd = &cobra.Command{
	Use:   "books",
	Short: "Manage indexed books",
	Args:  cobra.NoArgs,
}

var booksListCmd = &cobra.Command{
	Use:   "list",
	Short: "List indexed books",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		return withStore(cmd, func(st *store.Store) error {
			return listBooks(cmd.Context(), cmd.OutOrStdout(), st)
		})
	},
}

var booksShowCmd = &cobra.Command{
	Use:   "show <book>",
	Short: "Show a book, its chunks by stage and its failed chunks",
	Long:  "Shows a book, its chunks by stage and its failed chunks.\n" + bookRefHelp,
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return withStore(cmd, func(st *store.Store) error {
			return showBook(cmd.Context(), cmd.OutOrStdout(), st, args[0])
		})
	},
}

var booksRemoveCmd = &cobra.Command{
	Use:   "remove <book>",
	Short: "Remove a book with its chunks and quotes",
	Long:  "Removes a book with its chunks and quotes.\n" + bookRefHelp,
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		yes, err := cmd.Flags().GetBool("yes")
		if err != nil {
			return err
		}
		return withStore(cmd, func(st *store.Store) error {
			return removeBook(cmd.Context(), cmd.InOrStdin(), cmd.OutOrStdout(), st, args[0], yes)
		})
	},
}

func init() {
	rootCmd.AddCommand(booksCmd)
	booksCmd.AddCommand(booksListCmd, booksShowCmd, booksRemoveCmd)
	booksRemoveCmd.Flags().BoolP("yes", "y", false, "Remove without asking for confirmation")
}

func withStore(cmd *cobra.Command, fn func(*store.Store) error) error {
	cfg, err := config.Load(cmd.Flags(), os.Getenv)
	if err != nil {
		return err
	}
	st, err := store.Open(cmd.Context(), cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer st.Close()
	return fn(st)
}

func listBooks(ctx context.Context, out io.Writer, st *store.Store) error {
	books, err := st.ListBooks(ctx)
	if err != nil {
		return err
	}
	if len(books) == 0 {
		fmt.Fprintln(out, "no books indexed")
		return nil
	}
	tw := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "id\ttitle\tauthor\tformat\tstatus\tchunks\tquotes\tindexed")
	for _, b := range books {
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%d\t%d\t%s\n",
			b.ID[:8], truncate(b.Title, 40), orDash(truncate(b.Author, 32)), b.Format, b.Status,
			b.Chunks, b.Quotes, b.CreatedAt.Local().Format("2006-01-02"))
	}
	return tw.Flush()
}

func showBook(ctx context.Context, out io.Writer, st *store.Store, ref string) error {
	b, err := st.ResolveBook(ctx, ref)
	if err != nil {
		return err
	}
	stats, err := st.BookStats(ctx, b.ID)
	if err != nil {
		return err
	}

	chunks := strconv.Itoa(stats.Chunks())
	if len(stats.Stages) > 0 {
		stages := make([]string, len(stats.Stages))
		for i, s := range stats.Stages {
			stages[i] = fmt.Sprintf("%s %d", s.Stage, s.Chunks)
		}
		chunks += " (" + strings.Join(stages, ", ") + ")"
	}
	tw := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	for _, field := range [][2]string{
		{"id", b.ID},
		{"sha256", b.SHA256},
		{"title", b.Title},
		{"author", b.Author},
		{"language", b.Language},
		{"format", string(b.Format)},
		{"status", b.Status},
		{"indexed", b.CreatedAt.Local().Format("2006-01-02 15:04")},
		{"chunks", chunks},
		{"quotes", strconv.Itoa(stats.Quotes)},
	} {
		fmt.Fprintf(tw, "%s\t%s\n", field[0], orDash(field[1]))
	}
	if err := tw.Flush(); err != nil {
		return err
	}
	if len(stats.FailedChunks) == 0 {
		return nil
	}

	fmt.Fprintln(out)
	fmt.Fprintln(out, "failed chunks")
	tw = tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "#\tpage\tchapter\terror")
	for _, c := range stats.FailedChunks {
		page := ""
		if c.Page > 0 {
			page = strconv.Itoa(c.Page)
		}
		fmt.Fprintf(tw, "%d\t%s\t%s\t%s\n", c.Ordinal, orDash(page), orDash(truncate(c.Chapter, 30)), orDash(strings.Join(strings.Fields(c.Error), " ")))
	}
	return tw.Flush()
}

func removeBook(ctx context.Context, in io.Reader, out io.Writer, st *store.Store, ref string, yes bool) error {
	b, err := st.ResolveBook(ctx, ref)
	if err != nil {
		return err
	}
	if !yes {
		stats, err := st.BookStats(ctx, b.ID)
		if err != nil {
			return err
		}
		fmt.Fprintf(out, "Remove %q with %d chunks and %d quotes? [y/N] ", b.Title, stats.Chunks(), stats.Quotes)
		answer, _ := bufio.NewReader(in).ReadString('\n')
		if a := strings.ToLower(strings.TrimSpace(answer)); a != "y" && a != "yes" {
			fmt.Fprintln(out, "not removed")
			return nil
		}
	}
	removed, err := st.RemoveBook(ctx, b.ID)
	if err != nil {
		return err
	}
	if !removed {
		return fmt.Errorf("%w %q", store.ErrBookNotFound, ref)
	}
	fmt.Fprintf(out, "removed %s %s\n", b.ID, b.Title)
	return nil
}
