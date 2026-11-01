// Package notation finds chess moves written in book text.
package notation

import (
	"regexp"
	"slices"
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

type Options struct {
	// OCR also accepts what OCR leaves in move text: split move numbers,
	// letters read for digits, spaces inside a move and unreadable piece
	// glyphs. Such moves are kept only in move context: with a move number,
	// or next to another move.
	OCR bool
}

// Token is a move candidate. It is not necessarily a move: a square named in
// prose ("a knight on d5") looks exactly like a pawn move, and only replay or
// the parse stage can tell them apart.
type Token struct {
	// ID is "m1", "m2", … in text order.
	ID string
	// Start and End are byte offsets into the tokenized text.
	Start, End int
	Text       string
	// Move is the move as printed, without its number, check mark or glyph.
	Move string
	// Number is the move number, 0 when unknown. NumberPrinted is false when
	// the number was inferred from the white move just before it.
	Number        int
	NumberPrinted bool
	Side          Side
	// Depth is how many parentheses or brackets are open before the token in
	// its text: a move inside a variation has a depth of at least 1.
	Depth int
	// Piece through Castle are set only when Move is clean notation; an OCR
	// move is left to chess.Node.PlayPrinted.
	//
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

	// loose marks a move that only the OCR grammar accepts.
	loose bool
}

const (
	space        = `[ \t\x{00A0}]`
	ellipsis     = space + `*(?:\.(?:` + space + `?\.)+|…)` + space + `*`
	pieces       = `KQRBNDTCAFLS♔♕♖♗♘♚♛♜♝♞`
	promotions   = `QRBNDTCAFLS♕♖♗♘♛♜♝♞`
	capture      = `[x×]`
	castleDash   = `[-–‑]`
	square       = `[a-h][1-8]`
	asciiGlyphs  = `!!|\?\?|!\?|\?!|!|\?`
	figureGlyphs = `‼|⁇|⁉|⁈`

	// rankLike are the characters OCR leaves for a rank digit. Keep in sync
	// with chess.rankLike.
	rankLike  = `[1-8lIi|SB]`
	glyphChar = `[^\s\d()\[\]{},;]`
)

var (
	numberPrefix    = regexp.MustCompile(`^(?:(\d{1,3})(?:([.…])` + space + `*|` + space + `+))?(` + ellipsis + `)?`)
	ocrNumberPrefix = regexp.MustCompile(`^([\dlIO](?:` + space + `?[\dlIO]){0,2})` + space + `*([.…])` + space + `*(` + ellipsis + `)?`)
	castleMove      = regexp.MustCompile(`^([O0])` + castleDash + `[O0](` + castleDash + `[O0])?`)
	pieceMove       = regexp.MustCompile(`^([` + pieces + `])([a-h]?[1-8]?)(` + capture + `)?(` + square + `)`)
	pawnMove        = regexp.MustCompile(`^(?:([a-h])(` + capture + `))?(` + square + `)(?:=?([` + promotions + `]))?`)
	ocrMove         = regexp.MustCompile(`^(?:` + glyphChar + `{1,4} ?)??(?:[a-h]?` + rankLike + `?)?(?: ?[x×:] ?)?[a-h] ?` + rankLike + `(?:=` + glyphChar + `{1,3})?`)
	suffix          = regexp.MustCompile(`^(\+\+|[+#])?(` + asciiGlyphs + `|` + figureGlyphs + `)?`)
)

var unicodeGlyphs = map[string]string{"‼": "!!", "⁇": "??", "⁉": "!?", "⁈": "?!"}

// Hyphens and en dashes are not boundaries, so ranges such as "a1-h8" are not
// taken for moves. Evaluation signs often follow a move directly ("Rh4±").
const (
	leftBoundaries  = `([{,;:"'“‘«—`
	rightBoundaries = `.,;:)]}"'”’»…—±∓⩲⩱∞=`
	contextGap      = " \t\n ()[]±∓⩲⩱∞="
)

// stopwords are short words that the OCR grammar would otherwise read as a
// piece glyph, as in "on d5".
var stopwords = map[string]bool{
	"a": true, "an": true, "and": true, "as": true, "at": true, "by": true, "for": true,
	"from": true, "in": true, "is": true, "of": true, "on": true, "or": true, "the": true,
	"to": true, "via": true, "with": true,
}

// Tokenize finds the move candidates in text, in any notation convention.
func Tokenize(text string, opts Options) []Token {
	tokens := scanAll(text, opts.OCR)
	if opts.OCR {
		tokens = keepInContext(text, tokens)
		for _, tok := range scanAll(text, false) {
			if !slices.ContainsFunc(tokens, func(t Token) bool { return t.Start < tok.End && tok.Start < t.End }) {
				tokens = append(tokens, tok)
			}
		}
		slices.SortFunc(tokens, func(a, b Token) int { return a.Start - b.Start })
	}
	for i := range tokens {
		tokens[i].ID = "m" + strconv.Itoa(i+1)
	}
	setDepths(text, tokens)
	inferBlackMoves(text, tokens)
	return tokens
}

func setDepths(text string, tokens []Token) {
	depth, pos := 0, 0
	for i := range tokens {
		for _, r := range text[pos:tokens[i].Start] {
			switch r {
			case '(', '[':
				depth++
			case ')', ']':
				depth = max(0, depth-1)
			}
		}
		pos = tokens[i].Start
		tokens[i].Depth = depth
	}
}

func scanAll(text string, ocr bool) []Token {
	var tokens []Token
	for pos := 0; pos < len(text); {
		if tok, ok := scan(text, pos, ocr); ok {
			tokens = append(tokens, tok)
			pos = tok.End
			continue
		}
		_, size := utf8.DecodeRuneInString(text[pos:])
		pos += size
	}
	return tokens
}

func scan(text string, start int, ocr bool) (Token, bool) {
	if !leftBoundary(text, start) {
		return Token{}, false
	}
	tok := Token{Start: start}
	pos := start + scanNumber(text[start:], ocr, &tok)

	n, ok := scanMove(text[pos:], &tok)
	if !ok && ocr {
		n, ok = scanOCRMove(text[pos:])
		tok.loose = ok
	}
	if !ok {
		return Token{}, false
	}
	tok.Move = text[pos : pos+n]
	pos += n
	pos += scanSuffix(text[pos:], ocr, &tok)

	if !rightBoundary(text, pos) {
		return Token{}, false
	}
	tok.End = pos
	tok.Text = text[start:pos]
	return tok, true
}

// scanNumber reads the move number and ellipsis before a move, if any, and
// returns their length in bytes.
func scanNumber(s string, ocr bool, tok *Token) int {
	if ocr {
		if m := ocrNumberPrefix.FindStringSubmatch(s); m != nil && strings.ContainsAny(m[1], "0123456789") {
			setNumber(tok, ocrDigits.Replace(m[1]), m[2] == "…" || m[3] != "")
			return len(m[0])
		}
	}
	m := numberPrefix.FindStringSubmatch(s)
	black := m[2] == "…" || m[3] != ""
	if m[1] != "" {
		setNumber(tok, m[1], black)
	} else if black {
		tok.Side = Black
	}
	return len(m[0])
}

var ocrDigits = strings.NewReplacer(" ", "", "\t", "", " ", "", "l", "1", "I", "1", "O", "0")

func setNumber(tok *Token, digits string, black bool) {
	tok.Number, _ = strconv.Atoi(digits)
	tok.NumberPrinted = true
	tok.Side = White
	if black {
		tok.Side = Black
	}
}

// scanMove reads a move in clean notation, after any move number, and returns
// its length in bytes.
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

// scanOCRMove reads a move as OCR may leave it, such as "lll x e5" or
// ":i!ad l", and returns its length in bytes.
func scanOCRMove(s string) (int, bool) {
	move := ocrMove.FindString(s)
	if move == "" {
		return 0, false
	}
	if first, _, found := strings.Cut(move, " "); found && stopwords[strings.ToLower(first)] {
		return 0, false
	}
	// A lowercase word such as "feel" or "final" fits the grammar too.
	if len(move) >= 3 && strings.IndexFunc(move, func(r rune) bool { return r < 'a' || r > 'z' }) < 0 {
		return 0, false
	}
	return len(move), true
}

// scanSuffix reads a check mark and an annotation glyph, and returns their
// length in bytes. OCR may leave a space before them.
func scanSuffix(s string, ocr bool, tok *Token) int {
	m := suffix.FindStringSubmatch(s)
	offset := 0
	if ocr && m[0] == "" && strings.HasPrefix(s, " ") {
		if spaced := suffix.FindStringSubmatch(s[1:]); spaced[0] != "" {
			m, offset = spaced, 1
		}
	}
	tok.Check = m[1] == "+"
	tok.Mate = m[1] == "#" || m[1] == "++"
	tok.Glyph = m[2]
	if ascii, ok := unicodeGlyphs[tok.Glyph]; ok {
		tok.Glyph = ascii
	}
	return offset + len(m[0])
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

// keepInContext drops the moves that only the OCR grammar accepts unless they
// have a move number or sit next to another kept move.
func keepInContext(text string, tokens []Token) []Token {
	kept := make([]bool, len(tokens))
	for i, tok := range tokens {
		kept[i] = !tok.loose || tok.NumberPrinted
	}
	adjacent := func(a, b Token) bool {
		return strings.Trim(text[a.End:b.Start], contextGap) == ""
	}
	for changed := true; changed; {
		changed = false
		for i := range tokens {
			if kept[i] {
				continue
			}
			if (i > 0 && kept[i-1] && adjacent(tokens[i-1], tokens[i])) ||
				(i+1 < len(tokens) && kept[i+1] && adjacent(tokens[i], tokens[i+1])) {
				kept[i], changed = true, true
			}
		}
	}
	var out []Token
	for i, tok := range tokens {
		if kept[i] {
			out = append(out, tok)
		}
	}
	return out
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
