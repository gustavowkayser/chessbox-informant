package book

import (
	"context"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestChunkBlocks(t *testing.T) {
	long := strings.Repeat("Long paragraph text. ", 12)
	cases := map[string]struct {
		blocks []Block
		want   []Chunk
	}{
		"short block merges with the next": {
			blocks: []Block{
				{Chapter: "A", Page: 3, Text: "28...Qd6?!"},
				{Chapter: "A", Page: 4, Text: long},
			},
			want: []Chunk{{Ordinal: 1, Chapter: "A", Page: 3, Text: "28...Qd6?!\n\n" + long}},
		},
		"chunks never cross chapters": {
			blocks: []Block{
				{Chapter: "A", Page: 1, Text: "Short end of chapter A."},
				{Chapter: "B", Page: 2, Text: long},
			},
			want: []Chunk{
				{Ordinal: 1, Chapter: "A", Page: 1, Text: "Short end of chapter A."},
				{Ordinal: 2, Chapter: "B", Page: 2, Text: long},
			},
		},
		"chapter heading dropped, other headings merged": {
			blocks: []Block{
				{Chapter: "Chapter 1", Page: 1, Text: "Chapter 1", Heading: true},
				{Chapter: "Chapter 1", Page: 1, Text: "Game 1: Morphy - Duke", Heading: true},
				{Chapter: "Chapter 1", Page: 1, Text: long},
			},
			want: []Chunk{{Ordinal: 1, Chapter: "Chapter 1", Page: 1, Text: "Game 1: Morphy - Duke\n\n" + long}},
		},
		"short blocks stop merging once they make a paragraph": {
			blocks: []Block{
				{Chapter: "A", Page: 1, Text: strings.Repeat("a", 150)},
				{Chapter: "A", Page: 1, Text: strings.Repeat("b", 100)},
				{Chapter: "A", Page: 2, Text: "13.Rxd7!"},
				{Chapter: "A", Page: 2, Text: long},
			},
			want: []Chunk{
				{Ordinal: 1, Chapter: "A", Page: 1, Text: strings.Repeat("a", 150) + "\n\n" + strings.Repeat("b", 100)},
				{Ordinal: 2, Chapter: "A", Page: 2, Text: "13.Rxd7!\n\n" + long},
			},
		},
		"a heading stays with what follows it": {
			blocks: []Block{
				{Chapter: "A", Page: 1, Text: strings.Repeat("a", 190)},
				{Chapter: "A", Page: 1, Text: "Game 2", Heading: true},
				{Chapter: "A", Page: 1, Text: long},
			},
			want: []Chunk{{Ordinal: 1, Chapter: "A", Page: 1, Text: strings.Repeat("a", 190) + "\n\nGame 2\n\n" + long}},
		},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			if got := ChunkBlocks(c.blocks); !slices.Equal(got, c.want) {
				t.Errorf("got  %+v\nwant %+v", got, c.want)
			}
		})
	}
}

func TestChunkBlocksSplitsLongParagraphs(t *testing.T) {
	sentence := "After 12. Nf3 the knight on f3 is strong, and Black must defend with care. "
	text := strings.TrimSpace(strings.Repeat(sentence, 200))
	chunks := ChunkBlocks([]Block{{Chapter: "A", Page: 7, Text: text}})

	if len(chunks) < 2 {
		t.Fatalf("got %d chunks, want the paragraph split", len(chunks))
	}
	var pieces []string
	for i, c := range chunks {
		if n := utf8.RuneCountInString(c.Text); n > maxChunkChars {
			t.Errorf("chunk %d has %d characters", i, n)
		}
		if !strings.HasSuffix(c.Text, "care.") {
			t.Errorf("chunk %d does not end at a sentence: %q", i, c.Text[len(c.Text)-20:])
		}
		if c.Ordinal != i+1 || c.Page != 7 || c.Chapter != "A" {
			t.Errorf("chunk %d = ordinal %d, page %d, chapter %q", i, c.Ordinal, c.Page, c.Chapter)
		}
		pieces = append(pieces, c.Text)
	}
	if got := strings.Join(pieces, " "); got != text {
		t.Error("the pieces do not add up to the paragraph")
	}
}

func TestSplitLongCutsOverlongSentences(t *testing.T) {
	text := strings.Repeat("word ", maxChunkChars/2)
	pieces := splitLong(text)
	if len(pieces) != 3 {
		t.Fatalf("got %d pieces, want 3", len(pieces))
	}
	for _, p := range pieces {
		if n := utf8.RuneCountInString(p); n > maxChunkChars {
			t.Errorf("piece has %d characters", n)
		}
		if strings.HasPrefix(p, "ord") || strings.HasSuffix(p, "wor") {
			t.Error("a word was cut")
		}
	}
}

func TestChunkBlocksFixtureEPUB(t *testing.T) {
	doc, err := EPUBExtractor{}.Extract(context.Background(), filepath.Join(fixtureDir, "book.epub"))
	if err != nil {
		t.Fatal(err)
	}
	chunks := ChunkBlocks(doc.Blocks)
	if len(chunks) < 15 {
		t.Fatalf("got %d chunks", len(chunks))
	}
	finalMoves := regexp.MustCompile(`(?s)^18\.\.\.Bxg1 .* 23\.Be7# 1-0\n\nAt the end White`)
	found := false
	for _, c := range chunks {
		if strings.HasPrefix(c.Text, "Chapter ") || c.Text == "Preface" {
			t.Errorf("chapter heading kept as text: %q", c.Text)
		}
		if finalMoves.MatchString(c.Text) {
			found = c.Chapter == "Chapter 2: The Immortal Game"
		}
	}
	if !found {
		t.Error("the short final moves were not merged with the comment after them")
	}
}

func TestChunkBlocksFixtureFormatsAgree(t *testing.T) {
	x := requirePoppler(t)
	pdf, err := x.Extract(context.Background(), filepath.Join(fixtureDir, "book.pdf"))
	if err != nil {
		t.Fatal(err)
	}
	epub, err := EPUBExtractor{}.Extract(context.Background(), filepath.Join(fixtureDir, "book.epub"))
	if err != nil {
		t.Fatal(err)
	}
	withoutPages := func(chunks []Chunk) []Chunk {
		for i := range chunks {
			chunks[i].Page = 0
		}
		return chunks
	}
	pdfChunks, epubChunks := withoutPages(ChunkBlocks(pdf.Blocks)), withoutPages(ChunkBlocks(epub.Blocks))
	if !slices.Equal(pdfChunks, epubChunks) {
		t.Errorf("chunks differ:\npdf  %+v\nepub %+v", pdfChunks, epubChunks)
	}
}
