package cmd

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"text/tabwriter"
	"unicode/utf8"

	"chessbox-informant/internal/book"
	"chessbox-informant/internal/config"
	"chessbox-informant/internal/store"

	"github.com/spf13/cobra"
)

var indexCmd = &cobra.Command{
	Use:   "index <file>",
	Short: "Index a book (PDF or ePUB)",
	Long: "Extracts the text of a book, splits it into chunks and stores them.\n" +
		"A book is identified by the SHA-256 of its file, so indexing it again does nothing.",
	Args: cobra.ExactArgs(1),
	RunE: runIndex,
}

func init() {
	rootCmd.AddCommand(indexCmd)
	indexCmd.Flags().Bool("dry-run", false, "Print the extracted chunks without using the database")
}

func runIndex(cmd *cobra.Command, args []string) error {
	cfg, err := config.Load(cmd.Flags(), os.Getenv)
	if err != nil {
		return err
	}
	dryRun, err := cmd.Flags().GetBool("dry-run")
	if err != nil {
		return err
	}
	return indexBook(cmd.Context(), cmd.OutOrStdout(), cmd.ErrOrStderr(), cfg, args[0], dryRun)
}

func indexBook(ctx context.Context, out, errOut io.Writer, cfg config.Config, file string, dryRun bool) error {
	sum, err := book.FileSHA256(file)
	if err != nil {
		return err
	}
	if dryRun {
		doc, chunks, err := extractBook(ctx, errOut, cfg, file)
		if err != nil {
			return err
		}
		return printDryRun(out, file, sum, doc, chunks)
	}

	st, err := store.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer st.Close()
	if existing, ok, err := st.FindBookBySHA256(ctx, sum); err != nil {
		return err
	} else if ok {
		fmt.Fprintf(out, "already indexed: %s %s\n", existing.ID, existing.Title)
		return nil
	}

	doc, chunks, err := extractBook(ctx, errOut, cfg, file)
	if err != nil {
		return err
	}
	stored, created, err := st.CreateBook(ctx, store.Book{
		SHA256:   sum,
		Title:    bookTitle(doc, file),
		Author:   doc.Metadata.Author,
		Language: doc.Metadata.Language,
		Format:   doc.Format,
	}, chunks)
	if err != nil {
		return err
	}
	if !created {
		fmt.Fprintf(out, "already indexed: %s %s\n", stored.ID, stored.Title)
		return nil
	}
	fmt.Fprintf(out, "indexed %s %s: %d chunks\n", stored.ID, stored.Title, len(chunks))
	return nil
}

func extractBook(ctx context.Context, errOut io.Writer, cfg config.Config, file string) (book.Document, []book.Chunk, error) {
	doc, err := book.Extract(ctx, file, cfg.PopplerDir)
	if err != nil {
		return book.Document{}, nil, fmt.Errorf("%s: %w", file, err)
	}
	for _, w := range doc.Warnings {
		fmt.Fprintf(errOut, "warning: %s\n", w)
	}
	return doc, book.ChunkBlocks(doc.Blocks), nil
}

// bookTitle falls back to the file name, since books.title is required and
// many PDFs have no title metadata.
func bookTitle(doc book.Document, file string) string {
	if doc.Metadata.Title != "" {
		return doc.Metadata.Title
	}
	base := filepath.Base(file)
	return strings.TrimSuffix(base, filepath.Ext(base))
}

func printDryRun(out io.Writer, file, sum string, doc book.Document, chunks []book.Chunk) error {
	chapters := map[string]bool{}
	for _, c := range chunks {
		if c.Chapter != "" {
			chapters[c.Chapter] = true
		}
	}
	tw := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	for _, field := range [][2]string{
		{"file", file},
		{"format", string(doc.Format)},
		{"sha256", sum},
		{"title", bookTitle(doc, file)},
		{"author", doc.Metadata.Author},
		{"language", doc.Metadata.Language},
		{"chapters", strconv.Itoa(len(chapters))},
		{"chunks", strconv.Itoa(len(chunks))},
	} {
		fmt.Fprintf(tw, "%s\t%s\n", field[0], orDash(field[1]))
	}
	if err := tw.Flush(); err != nil {
		return err
	}

	fmt.Fprintln(out)
	tw = tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "#\tpage\tchapter\ttext")
	for _, c := range chunks {
		page := ""
		if c.Page > 0 {
			page = strconv.Itoa(c.Page)
		}
		fmt.Fprintf(tw, "%d\t%s\t%s\t%s\n", c.Ordinal, orDash(page), orDash(truncate(c.Chapter, 30)), truncate(strings.Join(strings.Fields(c.Text), " "), 70))
	}
	return tw.Flush()
}

func orDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

func truncate(s string, limit int) string {
	if utf8.RuneCountInString(s) <= limit {
		return s
	}
	return string([]rune(s)[:limit-3]) + "..."
}
