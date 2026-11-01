// Package books measures how the deterministic indexing code copes with real
// books. The books live in tests/assets/private, which is never committed, so
// the test skips when there are none and reports numbers only, never text.
package books

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"chessbox-informant/internal/book"
	"chessbox-informant/internal/chess"
	"chessbox-informant/internal/gamescore"
	"chessbox-informant/internal/notation"
)

const privateDir = "../assets/private"

type thresholds struct {
	NumberedTokens int `json:"numberedTokens"`
	// ReplayedMoves is a count rather than a share, because the share drops
	// whenever the tokenizer finds more moves.
	ReplayedMoves int `json:"replayedMoves"`
	Games         int `json:"games"`
	MedianReached int `json:"medianReached"`
}

// modes are the tokenizer modes measured, by their name in thresholds.json.
var modes = []struct {
	name string
	opts notation.Options
}{
	{"strict", notation.Options{}},
	{"ocr", notation.Options{OCR: true}},
}

type report struct {
	Chunks int
	Tokens int
	// PrintedNumbers estimates the dotted move numbers in the text with a
	// loose pattern, as a yardstick for the tokenizer.
	PrintedNumbers int
	NumberedTokens int
	// GameMoves counts the numbered tokens read while a game was open;
	// ReplayedMoves counts those that replayed.
	GameMoves     int
	ReplayedMoves int
	// Ambiguous counts the numbered moves left ambiguous after lookahead,
	// and Candidates the legal moves they fit, in total.
	Ambiguous  int
	Candidates int
	Games      int
	Detection  gamescore.Detection
	// Reached holds, per game, the move number at the end of its main line.
	Reached []int
}

func (r report) replayedShare() float64 {
	if r.GameMoves == 0 {
		return 0
	}
	return float64(r.ReplayedMoves) / float64(r.GameMoves)
}

func (r report) medianReached() int {
	if len(r.Reached) == 0 {
		return 0
	}
	sorted := slices.Sorted(slices.Values(r.Reached))
	return sorted[len(sorted)/2]
}

func TestPrivateBooks(t *testing.T) {
	var files []string
	for _, pattern := range []string{"*.pdf", "*.epub"} {
		matches, _ := filepath.Glob(filepath.Join(privateDir, pattern))
		files = append(files, matches...)
	}
	if len(files) == 0 {
		t.Skip("no books in " + privateDir)
	}

	data, err := os.ReadFile("thresholds.json")
	if err != nil {
		t.Fatal(err)
	}
	var minimums map[string]map[string]thresholds
	if err := json.Unmarshal(data, &minimums); err != nil {
		t.Fatal(err)
	}

	for _, file := range files {
		name := filepath.Base(file)
		t.Run(name, func(t *testing.T) {
			doc, err := book.Extract(context.Background(), file, os.Getenv("CHESSINFO_POPPLER_DIR"))
			if err != nil {
				t.Fatal(err)
			}
			chunks := book.ChunkBlocks(doc.Blocks)
			for _, mode := range modes {
				t.Run(mode.name, func(t *testing.T) {
					r := measure(chunks, mode.opts)
					r.log(t)
					want, ok := minimums[name][mode.name]
					if !ok {
						t.Logf("no %s thresholds for %s in thresholds.json; measured only", mode.name, name)
						return
					}
					r.check(t, want)
				})
			}
		})
	}
}

func (r report) log(t *testing.T) {
	t.Logf("chunks %d, tokens %d, numbered tokens %d (~%d dotted move numbers in the text)",
		r.Chunks, r.Tokens, r.NumberedTokens, r.PrintedNumbers)
	t.Logf("games %d, numbered moves replayed %d of %d (%.1f%%), main line reached: median move %d, max move %d",
		r.Games, r.ReplayedMoves, r.GameMoves, 100*r.replayedShare(), r.medianReached(), slices.Max(append(r.Reached, 0)))
	t.Logf("notation %s, %d glyphs learned%s", r.Detection.Convention, len(r.Detection.Learned), learnedSummary(r.Detection.Learned))
	if r.Ambiguous > 0 {
		t.Logf("ambiguous numbered moves %d, %.1f legal candidates each on average",
			r.Ambiguous, float64(r.Candidates)/float64(r.Ambiguous))
	}
}

var pieceNames = map[chess.Piece]string{
	chess.Knight: "N", chess.Bishop: "B", chess.Rook: "R", chess.Queen: "Q", chess.King: "K",
}

// learnedSummary lists the most used learned glyphs, such as "lll=N (120)".
func learnedSummary(learned map[string]gamescore.LearnedGlyph) string {
	glyphs := slices.SortedFunc(maps.Keys(learned), func(a, b string) int { return learned[b].Moves - learned[a].Moves })
	var parts []string
	for _, g := range glyphs[:min(len(glyphs), 12)] {
		parts = append(parts, fmt.Sprintf("%q=%s (%d)", g, pieceNames[learned[g].Piece], learned[g].Moves))
	}
	if len(parts) == 0 {
		return ""
	}
	return ": " + strings.Join(parts, ", ")
}

func (r report) check(t *testing.T, want thresholds) {
	if r.NumberedTokens < want.NumberedTokens {
		t.Errorf("numbered tokens = %d, want at least %d", r.NumberedTokens, want.NumberedTokens)
	}
	if r.ReplayedMoves < want.ReplayedMoves {
		t.Errorf("replayed moves = %d, want at least %d", r.ReplayedMoves, want.ReplayedMoves)
	}
	if r.Games < want.Games {
		t.Errorf("games = %d, want at least %d", r.Games, want.Games)
	}
	if r.medianReached() < want.MedianReached {
		t.Errorf("median move reached = %d, want at least %d", r.medianReached(), want.MedianReached)
	}
}

// printedNumber loosely matches a move number, including OCR forms such as
// "2 1 ." and "20 . . .". The trailing group keeps decimals such as "4.99" out.
var printedNumber = regexp.MustCompile(`(?:^|[^\w.])\d(?: ?\d){0,2} ?[.…](?:[^\d]|$)`)

func measure(chunks []book.Chunk, opts notation.Options) report {
	r := report{Chunks: len(chunks)}
	var tokens [][]notation.Token
	for _, c := range chunks {
		r.PrintedNumbers += len(printedNumber.FindAllString(c.Text, -1))
		tokens = append(tokens, notation.Tokenize(c.Text, opts))
		r.Tokens += len(tokens[len(tokens)-1])
	}
	r.Detection = gamescore.Detect(tokens)
	p := gamescore.New(r.Detection.Names)
	for _, chunk := range tokens {
		p.Feed(chunk)
	}
	p.Finish()

	for _, out := range p.Outcomes() {
		if !out.Token.NumberPrinted {
			continue
		}
		r.NumberedTokens++
		if out.Status == gamescore.NoGame {
			continue
		}
		r.GameMoves++
		switch out.Status {
		case gamescore.Played:
			r.ReplayedMoves++
		case gamescore.Ambiguous:
			r.Ambiguous++
			r.Candidates += len(out.Candidates)
		}
	}
	r.Games = len(p.Games())
	for _, root := range p.Games() {
		n := root
		for len(n.Children) > 0 {
			n = n.Children[0]
		}
		r.Reached = append(r.Reached, n.MoveNumber)
	}
	return r
}
