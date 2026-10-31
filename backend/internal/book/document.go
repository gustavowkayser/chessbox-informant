package book

import "context"

type Format string

const (
	FormatEPUB Format = "epub"
	FormatPDF  Format = "pdf"
)

type Metadata struct {
	Title    string
	Author   string
	Language string
}

// Block is one unit of text in reading order: a paragraph, a heading or a
// list item.
type Block struct {
	Chapter string
	// Page is the printed page number, or 0 when the source does not say.
	Page    int
	Text    string
	Heading bool
}

type Document struct {
	Format   Format
	Metadata Metadata
	Blocks   []Block
}

type Extractor interface {
	Extract(ctx context.Context, path string) (Document, error)
}
