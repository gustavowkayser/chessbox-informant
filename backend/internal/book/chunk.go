package book

import (
	"regexp"
	"strings"
	"unicode/utf8"
)

const (
	// minChunkChars is the size below which a block is merged with the blocks
	// after it: a move set apart on its own line belongs with the commentary
	// that follows it.
	minChunkChars = 200
	// maxChunkChars keeps a chunk around 1,500 tokens, the budget of the
	// parse stage.
	maxChunkChars = 6000

	paragraphBreak = "\n\n"
)

type Chunk struct {
	// Ordinal is the chunk's position in the book, starting at 1.
	Ordinal int
	Chapter string
	Page    int
	Text    string
}

// sentenceEnd matches the end of a prose sentence. Requiring a lowercase
// letter before the punctuation keeps moves ("Nf3.", "O-O!", "exd8=Q?") and
// move numbers ("12. Nf3") from being taken as sentence ends.
var sentenceEnd = regexp.MustCompile(`[a-z][.!?]+["')\]]*\s+`)

// ChunkBlocks groups blocks into chunks, one paragraph each, that never cross
// a chapter boundary. Short blocks and headings are merged with the blocks
// that follow them, and paragraphs longer than maxChunkChars are split
// between sentences. A heading equal to its chapter title is dropped, since
// the chunk's chapter already holds it.
func ChunkBlocks(blocks []Block) []Chunk {
	var chunks []Chunk
	var group []Block
	groupChars := 0
	emit := func() {
		if len(group) == 0 {
			return
		}
		texts := make([]string, len(group))
		for i, b := range group {
			texts[i] = b.Text
		}
		for _, text := range splitLong(strings.Join(texts, paragraphBreak)) {
			chunks = append(chunks, Chunk{Ordinal: len(chunks) + 1, Chapter: group[0].Chapter, Page: group[0].Page, Text: text})
		}
		group, groupChars = nil, 0
	}

	for _, b := range blocks {
		if b.Heading && fold(b.Text) == fold(b.Chapter) {
			continue
		}
		if len(group) > 0 {
			last := group[len(group)-1]
			if last.Chapter != b.Chapter || (groupChars >= minChunkChars && !last.Heading) {
				emit()
			}
		}
		group = append(group, b)
		size := utf8.RuneCountInString(b.Text)
		groupChars += size
		if !b.Heading && size >= minChunkChars {
			emit()
		}
	}
	emit()
	return chunks
}

// splitLong splits text longer than maxChunkChars into pieces of whole
// sentences, keeping paragraph breaks. A single sentence longer than the limit
// is cut at the last space before it.
func splitLong(text string) []string {
	if utf8.RuneCountInString(text) <= maxChunkChars {
		return []string{text}
	}
	var pieces []string
	var current strings.Builder
	currentChars := 0
	flush := func() {
		if s := strings.TrimSpace(current.String()); s != "" {
			pieces = append(pieces, s)
		}
		current.Reset()
		currentChars = 0
	}
	add := func(s, sep string) {
		size := utf8.RuneCountInString(s)
		if currentChars > 0 && currentChars+len(sep)+size > maxChunkChars {
			flush()
		}
		if currentChars > 0 {
			current.WriteString(sep)
			currentChars += len(sep)
		}
		current.WriteString(s)
		currentChars += size
	}

	for paragraph := range strings.SplitSeq(text, paragraphBreak) {
		sep := paragraphBreak
		for _, s := range sentences(paragraph) {
			for utf8.RuneCountInString(s) > maxChunkChars {
				flush()
				cut := cutAt(s, maxChunkChars)
				pieces = append(pieces, strings.TrimSpace(s[:cut]))
				s = strings.TrimSpace(s[cut:])
			}
			add(s, sep)
			sep = " "
		}
	}
	flush()
	return pieces
}

func sentences(paragraph string) []string {
	var out []string
	start := 0
	for _, m := range sentenceEnd.FindAllStringIndex(paragraph, -1) {
		out = append(out, strings.TrimSpace(paragraph[start:m[1]]))
		start = m[1]
	}
	if rest := strings.TrimSpace(paragraph[start:]); rest != "" {
		out = append(out, rest)
	}
	return out
}

// cutAt returns the byte offset of the last space within the first limit
// characters of s, or of the limit itself when there is no space.
func cutAt(s string, limit int) int {
	offset, n := 0, 0
	lastSpace := -1
	for i, r := range s {
		if n == limit {
			offset = i
			break
		}
		if r == ' ' {
			lastSpace = i
		}
		n++
	}
	if lastSpace > 0 {
		return lastSpace
	}
	return offset
}
