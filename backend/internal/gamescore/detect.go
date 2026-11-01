package gamescore

import (
	"maps"
	"slices"

	"chessbox-informant/internal/chess"
	"chessbox-informant/internal/notation"
)

const (
	// A glyph is learned once it has been played at least minGlyphMoves
	// times and at least minGlyphAgreement of those moves were the same piece.
	// U+FFFD, which OCR leaves for several pieces, never agrees enough.
	minGlyphMoves     = 5
	minGlyphAgreement = 0.9
	maxLearningRounds = 5
)

// Detection is how a book names its pieces.
type Detection struct {
	Convention string
	// Names holds the convention's names and the learned glyphs.
	Names   chess.PieceNames
	Learned map[string]LearnedGlyph
	// Replayed counts the numbered moves replayed with Names.
	Replayed int
}

type LearnedGlyph struct {
	Piece chess.Piece
	// Moves counts the moves that taught it.
	Moves int
}

// Detect finds how a book names its pieces from its tokens, one slice per
// chunk. A move whose glyph is unknown can still replay when only one legal
// move fits, and those moves show what the glyph means. Every learned glyph
// settles moves that were ambiguous, which teaches more, so learning repeats
// until nothing new is learned.
//
// Learning starts from each convention, and from no letters at all for books
// whose OCR left too few real letters to vote. The candidate that replays the
// most numbered moves after learning wins: a convention's letters are never
// overridden, so a wrong one would also block learning what its letters
// really mean in this book.
func Detect(chunks [][]notation.Token) Detection {
	candidates := append(slices.Clone(notation.Conventions), notation.Convention{Name: Learned, Names: notation.Figurines()})
	var best Detection
	bestNamed := -1
	for i, c := range candidates {
		d, named := learn(chunks, c)
		if i == 0 || d.Replayed > best.Replayed || (d.Replayed == best.Replayed && named > bestNamed) {
			best, bestNamed = d, named
		}
	}
	return best
}

// Learned is the convention name of a book whose piece names were all
// learned.
const Learned = "learned"

func learn(chunks [][]notation.Token, c notation.Convention) (Detection, int) {
	d := Detection{Convention: c.Name, Names: maps.Clone(c.Names), Learned: make(map[string]LearnedGlyph)}
	var named int
	for round := 0; ; round++ {
		p := parseAll(chunks, d.Names)
		d.Replayed, named = score(p, d.Names)
		if round == maxLearningRounds {
			break
		}
		found := learnGlyphs(p.Outcomes(), d.Names)
		if len(found) == 0 {
			break
		}
		for glyph, l := range found {
			d.Names[glyph] = l.Piece
			d.Learned[glyph] = l
		}
	}
	return d, named
}

func parseAll(chunks [][]notation.Token, names chess.PieceNames) *Parser {
	p := New(names)
	for _, tokens := range chunks {
		p.Feed(tokens)
	}
	p.Finish()
	return p
}

// score counts the numbered moves replayed and, to break ties between
// candidates, the piece moves whose glyph is one of names.
func score(p *Parser, names chess.PieceNames) (replayed, named int) {
	for _, out := range p.Outcomes() {
		if out.Status != Played {
			continue
		}
		if out.Token.NumberPrinted {
			replayed++
		}
		if glyph, _, ok := chess.PrintedGlyph(out.Token.Move, out.Node.SAN); ok {
			if _, known := names[glyph]; known {
				named++
			}
		}
	}
	return replayed, named
}

func learnGlyphs(outcomes []Outcome, names chess.PieceNames) map[string]LearnedGlyph {
	counts := make(map[string]map[chess.Piece]int)
	for _, out := range outcomes {
		if out.Status != Played {
			continue
		}
		glyph, piece, ok := chess.PrintedGlyph(out.Token.Move, out.Node.SAN)
		if _, known := names[glyph]; !ok || known {
			continue
		}
		if counts[glyph] == nil {
			counts[glyph] = make(map[chess.Piece]int)
		}
		counts[glyph][piece]++
	}

	found := make(map[string]LearnedGlyph)
	for glyph, pieces := range counts {
		total, top := 0, LearnedGlyph{}
		for piece, n := range pieces {
			total += n
			if n > top.Moves {
				top = LearnedGlyph{Piece: piece, Moves: n}
			}
		}
		if total >= minGlyphMoves && float64(top.Moves) >= minGlyphAgreement*float64(total) {
			found[glyph] = LearnedGlyph{Piece: top.Piece, Moves: total}
		}
	}
	return found
}
