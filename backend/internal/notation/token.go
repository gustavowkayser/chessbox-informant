// Package notation finds chess moves written in book text.
package notation

import (
	"regexp"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"chessbox-informant/internal/chess"
)

type Side int

const (
	UnknownSide Side = iota
	White
	Black
)

// Token is a move candidate. It is not necessarily a move: a square named in
// prose ("a knight on d5") looks exactly like a pawn move, and only replay or
// the parse stage can tell them apart.
type Token struct {
	// ID is "m1", "m2", … in text order.
	ID string
	// Start and End are byte offsets into the tokenized text.
	Start, End int
	Text       string
	// Number is the move number, 0 when unknown. NumberPrinted is false when
	// the number was inferred from the white move just before it.
	Number        int
	NumberPrinted bool
	Side          Side
	// Piece is the letter or figurine as printed, in any convention; empty
	// for pawn moves and castling.
	Piece string
	// From is the disambiguation as printed: a file, a rank or a square.
	// For a pawn capture it is the pawn's file.
	From      string
	To        string
	Capture   bool
	Promotion string
	Castle    chess.Castle
	Check     bool
	Mate      bool
	// Glyph is the annotation glyph in ASCII form ("!", "?!", …).
	Glyph string
}

const (
	space        = `[ \t\x{00A0}]`
	pieces       = `KQRBNDTCAFLS♔♕♖♗♘♚♛♜♝♞`
	promotions   = `QRBNDTCAFLS♕♖♗♘♛♜♝♞`
	capture      = `[x×]`
	castleDash   = `[-–‑]`
	square       = `[a-h][1-8]`
	asciiGlyphs  = `!!|\?\?|!\?|\?!|!|\?`
	figureGlyphs = `‼|⁇|⁉|⁈`
)

var (
	numberPrefix = regexp.MustCompile(`^(?:(\d{1,3})(\.|…)` + space + `*)?(?:(\.{2,}|…)` + space + `*)?`)
	castleMove   = regexp.MustCompile(`^([O0])` + castleDash + `[O0](` + castleDash + `[O0])?`)
	pieceMove    = regexp.MustCompile(`^([` + pieces + `])([a-h]?[1-8]?)(` + capture + `)?(` + square + `)`)
	pawnMove     = regexp.MustCompile(`^(?:([a-h])(` + capture + `))?(` + square + `)(?:=?([` + promotions + `]))?`)
	suffix       = regexp.MustCompile(`^(\+\+|[+#])?(` + asciiGlyphs + `|` + figureGlyphs + `)?`)
)

var unicodeGlyphs = map[string]string{"‼": "!!", "⁇": "??", "⁉": "!?", "⁈": "?!"}

// Hyphens and en dashes are not boundaries, so ranges such as "a1-h8" are not
// taken for moves.
const (
	leftBoundaries  = `([{,;:"'“‘«—`
	rightBoundaries = `.,;:)]}"'”’»…—`
)

// Tokenize finds the move candidates in text, in any notation convention.
func Tokenize(text string) []Token {
	var tokens []Token
	for pos := 0; pos < len(text); {
		if tok, ok := scan(text, pos); ok {
			tok.ID = "m" + strconv.Itoa(len(tokens)+1)
			tokens = append(tokens, tok)
			pos = tok.End
			continue
		}
		_, size := utf8.DecodeRuneInString(text[pos:])
		pos += size
	}
	inferBlackMoves(text, tokens)
	return tokens
}

func scan(text string, start int) (Token, bool) {
	if !leftBoundary(text, start) {
		return Token{}, false
	}
	tok := Token{Start: start}
	pos := start

	m := numberPrefix.FindStringSubmatch(text[pos:])
	pos += len(m[0])
	black := m[2] == "…" || m[3] != ""
	if m[1] != "" {
		tok.Number, _ = strconv.Atoi(m[1])
		tok.NumberPrinted = true
		tok.Side = White
	}
	if black {
		tok.Side = Black
	}

	n, ok := scanMove(text[pos:], &tok)
	if !ok {
		return Token{}, false
	}
	pos += n

	m = suffix.FindStringSubmatch(text[pos:])
	pos += len(m[0])
	tok.Check = m[1] == "+"
	tok.Mate = m[1] == "#" || m[1] == "++"
	tok.Glyph = m[2]
	if ascii, ok := unicodeGlyphs[tok.Glyph]; ok {
		tok.Glyph = ascii
	}

	if !rightBoundary(text, pos) {
		return Token{}, false
	}
	tok.End = pos
	tok.Text = text[start:pos]
	return tok, true
}

// scanMove reads the move itself, after any move number, and returns its
// length in bytes.
func scanMove(s string, tok *Token) (int, bool) {
	if m := castleMove.FindStringSubmatch(s); m != nil {
		tok.Castle = chess.ShortCastle
		if m[2] != "" {
			tok.Castle = chess.LongCastle
		}
		return len(m[0]), true
	}
	if m := pieceMove.FindStringSubmatch(s); m != nil {
		tok.Piece, tok.From, tok.Capture, tok.To = m[1], m[2], m[3] != "", m[4]
		return len(m[0]), true
	}
	if m := pawnMove.FindStringSubmatch(s); m != nil {
		tok.From, tok.Capture, tok.To, tok.Promotion = m[1], m[2] != "", m[3], m[4]
		if tok.Promotion != "" && tok.To[1] != '1' && tok.To[1] != '8' {
			return 0, false
		}
		return len(m[0]), true
	}
	return 0, false
}

func leftBoundary(text string, pos int) bool {
	if pos == 0 {
		return true
	}
	r, _ := utf8.DecodeLastRuneInString(text[:pos])
	return unicode.IsSpace(r) || strings.ContainsRune(leftBoundaries, r)
}

func rightBoundary(text string, pos int) bool {
	if pos == len(text) {
		return true
	}
	r, _ := utf8.DecodeRuneInString(text[pos:])
	return unicode.IsSpace(r) || strings.ContainsRune(rightBoundaries, r)
}

// inferBlackMoves gives an unnumbered token the black side and the move number
// of the numbered white move it directly follows, as in "23.Nf5 b4".
func inferBlackMoves(text string, tokens []Token) {
	for i := 1; i < len(tokens); i++ {
		prev, tok := tokens[i-1], &tokens[i]
		if tok.Side != UnknownSide || prev.Side != White {
			continue
		}
		if strings.TrimSpace(text[prev.End:tok.Start]) == "" {
			tok.Side = Black
			tok.Number = prev.Number
		}
	}
}
