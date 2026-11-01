// Package books measures how the deterministic indexing code copes with real
// books. The books live in tests/assets/private, which is never committed, so
// the test skips when there are none and reports numbers only, never text.
package books

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"testing"

	"chessbox-informant/internal/book"
	"chessbox-informant/internal/chess"
	"chessbox-informant/internal/notation"
)

const (
	privateDir = "../assets/private"
	startFEN   = "rnbqkbnr/pppppppp/8/8/8/8/PPPPPPPP/RNBQKBNR w KQkq - 0 1"
)

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
	// GameMoves counts the numbered tokens that come after a game has
	// started; ReplayedMoves counts those that replayed.
	GameMoves     int
	ReplayedMoves int
	// Ambiguous counts the numbered moves that fit more than one legal move,
	// and Candidates the legal moves they fit, in total.
	Ambiguous  int
	Candidates int
	Games      int
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
	if r.Ambiguous > 0 {
		t.Logf("ambiguous numbered moves %d, %.1f legal candidates each on average",
			r.Ambiguous, float64(r.Candidates)/float64(r.Ambiguous))
	}
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
	var g *game
	for _, c := range chunks {
		r.PrintedNumbers += len(printedNumber.FindAllString(c.Text, -1))
		tokens := notation.Tokenize(c.Text, opts)
		r.Tokens += len(tokens)
		for _, tok := range tokens {
			if tok.NumberPrinted {
				r.NumberedTokens++
			}
			if tok.NumberPrinted && tok.Number == 1 && tok.Side == notation.White {
				if g != nil {
					r.Reached = append(r.Reached, g.reached())
				}
				g = newGame()
				r.Games++
			}
			if g == nil {
				continue
			}
			played, candidates := g.play(tok)
			if tok.NumberPrinted {
				r.GameMoves++
				if played {
					r.ReplayedMoves++
				} else if candidates > 1 {
					r.Ambiguous++
					r.Candidates += candidates
				}
			}
		}
	}
	if g != nil {
		r.Reached = append(r.Reached, g.reached())
	}
	return r
}

// game follows a game through the book text. It stands in for the bare game
// score parser, which is not built yet.
type game struct {
	root    *chess.Node
	current *chess.Node
}

func newGame() *game {
	root, err := chess.NewTree(startFEN)
	if err != nil {
		panic(err)
	}
	return &game{root: root, current: root}
}

// play places a move. A numbered move is played from the latest position on
// the current line where it is due, which handles variations, and otherwise
// from the main line, which handles the return from a variation. An
// unnumbered move continues the current line. When the move is not played,
// candidates is the number of legal moves it fits where it is ambiguous.
func (g *game) play(tok notation.Token) (played bool, candidates int) {
	nodes := []*chess.Node{g.current}
	if tok.Number > 0 {
		nodes = g.due(tok)
	}
	for _, node := range nodes {
		next, err := node.PlayPrinted(tok.ID, tok.Move, chess.EnglishNames)
		if err == nil {
			g.current = next
			return true, 0
		}
		var moveErr *chess.MoveError
		if errors.As(err, &moveErr) && moveErr.Ambiguous() && candidates == 0 {
			candidates = len(moveErr.Candidates)
		}
	}
	return false, candidates
}

func (g *game) due(tok notation.Token) []*chess.Node {
	side := chess.White
	if tok.Side == notation.Black {
		side = chess.Black
	}
	isDue := func(n *chess.Node) bool {
		number, s := n.NextMove()
		return number == tok.Number && s == side
	}

	var nodes []*chess.Node
	for n := g.current; n != nil; n = n.Parent {
		if isDue(n) {
			nodes = append(nodes, n)
		}
	}
	for n := g.root; ; n = n.Children[0] {
		if isDue(n) && !slices.Contains(nodes, n) {
			nodes = append(nodes, n)
		}
		if len(n.Children) == 0 {
			break
		}
	}
	return nodes
}

func (g *game) reached() int {
	n := g.root
	for len(n.Children) > 0 {
		n = n.Children[0]
	}
	return n.MoveNumber
}
