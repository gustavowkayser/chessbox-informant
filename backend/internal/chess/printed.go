package chess

import (
	"regexp"
	"strings"

	lib "github.com/corentings/chess/v2"
)

// PieceNames maps printed piece letters or glyphs to pieces.
type PieceNames map[string]Piece

var EnglishNames = PieceNames{"K": King, "Q": Queen, "R": Rook, "B": Bishop, "N": Knight}

// rankLike maps the characters OCR leaves for a rank digit to the rank. Keep
// in sync with notation.rankLike.
var rankLike = map[rune]byte{
	'1': '1', '2': '2', '3': '3', '4': '4', '5': '5', '6': '6', '7': '7', '8': '8',
	'l': '1', 'I': '1', 'i': '1', '|': '1', 'S': '5', 'B': '8',
}

var printedCastle = regexp.MustCompile(`^[O0o][-–‑][O0o]([-–‑][O0o])?$`)

// printedMove is a move as printed, possibly with OCR noise: a destination
// square, and before it a piece glyph and a disambiguation that are told apart
// only when matched against a legal move.
type printedMove struct {
	castle    Castle
	to        string
	capture   bool
	promotion string
	before    []rune
}

// PlayPrinted adds the legal move that printed could be a rendering of, as
// Play does. printed may carry OCR noise: spaces, look-alike rank characters
// and unreadable piece glyphs. A glyph listed in names must be that piece; any
// other glyph stands for any piece but a pawn.
func (n *Node) PlayPrinted(ref, printed string, names PieceNames) (*Node, error) {
	var matches []lib.Move
	if p, ok := parsePrinted(printed); ok {
		best := noMatch
		for _, m := range n.position.ValidMoves() {
			score := p.match(n.position, m, names)
			if score > best {
				best, matches = score, nil
			}
			if score == best && score != noMatch {
				matches = append(matches, m)
			}
		}
	}
	return n.play(ref, printed, matches)
}

func parsePrinted(printed string) (printedMove, bool) {
	s := strings.Join(strings.Fields(printed), "")
	var p printedMove
	if m := printedCastle.FindStringSubmatch(s); m != nil {
		p.castle = ShortCastle
		if m[1] != "" {
			p.castle = LongCastle
		}
		return p, true
	}

	if before, promotion, found := strings.Cut(s, "="); found {
		if promotion == "" {
			return p, false
		}
		s, p.promotion = before, promotion
	}
	runes := []rune(s)
	if n := len(runes); p.promotion == "" && n >= 3 && isFile(runes[n-3]) && rankOf(runes[n-2]) != 0 && rankOf(runes[n-1]) == 0 {
		p.promotion, runes = string(runes[n-1]), runes[:n-1]
	}

	n := len(runes)
	if n < 2 || !isFile(runes[n-2]) || rankOf(runes[n-1]) == 0 {
		return p, false
	}
	p.to = string([]byte{byte(runes[n-2]), rankOf(runes[n-1])})
	p.before = runes[:n-2]
	if k := len(p.before); k > 0 && strings.ContainsRune("x×:", p.before[k-1]) {
		p.capture, p.before = true, p.before[:k-1]
	}
	return p, true
}

// A move read through a known piece name beats one read through an unknown
// glyph: with "lll" known as a knight, "lll e2" is not also "ll" (any piece)
// from the first rank.
type matchScore int

const (
	noMatch matchScore = iota
	glyphMatch
	exactMatch
)

func (p printedMove) match(pos *lib.Position, m lib.Move, names PieceNames) matchScore {
	castle := m.HasTag(lib.KingSideCastle) || m.HasTag(lib.QueenSideCastle)
	switch p.castle {
	case ShortCastle:
		return exactIf(m.HasTag(lib.KingSideCastle))
	case LongCastle:
		return exactIf(m.HasTag(lib.QueenSideCastle))
	}
	capture := m.HasTag(lib.Capture) || m.HasTag(lib.EnPassant)
	if castle || m.S2().String() != p.to || capture != p.capture || !p.matchesPromotion(m, names) {
		return noMatch
	}

	from := m.S1().String()
	piece := libPieces[pos.Board().Piece(m.S1()).Type()]
	if piece == Pawn {
		if p.capture {
			return exactIf(len(p.before) == 1 && p.before[0] == rune(from[0]))
		}
		return exactIf(len(p.before) == 0)
	}

	// The text before the square is a piece glyph followed by an optional
	// disambiguation. A glyph never ends with a file letter, so "Rad1" is a
	// rook from the a-file and not a glyph "Ra".
	best := noMatch
	for k := 0; k <= 2 && k < len(p.before); k++ {
		glyph, disambiguation := p.before[:len(p.before)-k], p.before[len(p.before)-k:]
		if isFile(glyph[len(glyph)-1]) || !matchesFrom(disambiguation, from) {
			continue
		}
		named, ok := names[string(glyph)]
		switch {
		case ok && named == piece:
			return exactMatch
		case !ok:
			best = glyphMatch
		}
	}
	return best
}

func (p printedMove) matchesPromotion(m lib.Move, names PieceNames) bool {
	if m.Promo() == lib.NoPieceType {
		return p.promotion == ""
	}
	if p.promotion == "" {
		return false
	}
	named, ok := names[p.promotion]
	return !ok || named == libPieces[m.Promo()]
}

func matchesFrom(disambiguation []rune, from string) bool {
	switch len(disambiguation) {
	case 0:
		return true
	case 1:
		r := disambiguation[0]
		return r == rune(from[0]) || rankOf(r) == from[1]
	default:
		return disambiguation[0] == rune(from[0]) && rankOf(disambiguation[1]) == from[1]
	}
}

func isFile(r rune) bool { return r >= 'a' && r <= 'h' }

func rankOf(r rune) byte { return rankLike[r] }

func exactIf(ok bool) matchScore {
	if ok {
		return exactMatch
	}
	return noMatch
}
