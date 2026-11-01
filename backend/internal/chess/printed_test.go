package chess_test

import (
	"errors"
	"strings"
	"testing"

	"chessbox-informant/internal/chess"
)

// The printed moves imitate what OCR leaves in move text; none is taken from a
// book.
func TestPlayPrinted(t *testing.T) {
	const (
		twoRooks  = "4k3/8/8/8/8/8/8/R4RK1 w - - 0 1"
		afterE4E5 = "rnbqkbnr/pppp1ppp/8/4p3/4P3/8/PPPP1PPP/RNBQKBNR w KQkq e6 0 2"
		promotion = "8/4P3/8/8/8/8/k7/4K3 w - - 0 1"
	)
	cases := []struct {
		name, fen, printed string
		names              chess.PieceNames
		want               string
	}{
		{"clean move", startFEN, "Nf3", nil, "Nf3"},
		{"check marks are ignored", "6k1/5ppp/8/8/8/8/8/R5K1 w - - 0 1", "Ra8+", chess.EnglishNames, "Ra8#"},
		{"disambiguation by file", twoRooks, "Rad1", nil, "Rad1"},
		{"glyph and disambiguation", twoRooks, ":i!ad l", nil, "Rad1"},
		{"glyph and the other rook", twoRooks, ":i!fd l", nil, "Rfd1"},
		{"glyph without disambiguation", twoRooks, ":i!d l", nil, "ambiguous: Rad1 Rfd1"},
		{"glyph with a space", startFEN, "lll f3", nil, "Nf3"},
		{"replacement character", startFEN, "�f3", nil, "Nf3"},
		{"glyph with several legal moves", afterE4E5, "�e2", nil, "ambiguous: Be2 Ke2 Ne2 Qe2"},
		{"learned glyph", afterE4E5, "lll e2", chess.PieceNames{"lll": chess.Knight}, "Ne2"},
		{"named piece that cannot move there", startFEN, "Bf3", chess.EnglishNames, "illegal"},
		{"glyph capture with spaces", "4k3/8/8/4p3/8/5N2/8/4K3 w - - 0 1", "lll x e5", nil, "Nxe5"},
		{"glyph capture without a capture mark", "4k3/8/8/4p3/8/5N2/8/4K3 w - - 0 1", "lll e5", nil, "illegal"},
		{"rank read as a letter", "4k3/8/8/8/3P4/8/8/4K3 w - - 0 1", "dS", nil, "d5"},
		{"pawn capture with spaces", "4k3/8/8/3p4/4P3/8/8/4K3 w - - 0 1", "e x dS", nil, "exd5"},
		{"castling with zeros", "r3k2r/8/8/8/8/8/8/R3K2R w KQkq - 0 1", "0-0", nil, "O-O"},
		{"long castling in lowercase", "r3k2r/8/8/8/8/8/8/R3K2R w KQkq - 0 1", "o-o-o", nil, "O-O-O"},
		{"promotion", promotion, "e8=Q", chess.EnglishNames, "e8=Q"},
		{"promotion without =", promotion, "e8Q", chess.EnglishNames, "e8=Q"},
		{"promotion to a glyph", promotion, "e8=W", chess.EnglishNames, "ambiguous: e8=B e8=N e8=Q e8=R"},
		{"not a move", startFEN, "lll", nil, "illegal"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			node, err := newTree(t, c.fen).PlayPrinted("m1", c.printed, c.names)
			if got := outcome(t, node, err); got != c.want {
				t.Errorf("PlayPrinted(%q) = %q, want %q", c.printed, got, c.want)
			}
		})
	}
}

func TestPlayPrintedError(t *testing.T) {
	_, err := newTree(t, startFEN).PlayPrinted("m4", "2 1 .:i!d l", nil)
	if want := "m4 `2 1 .:i!d l` is illegal in the starting position"; err == nil || err.Error() != want {
		t.Errorf("error = %v, want %q", err, want)
	}
}

func outcome(t *testing.T, node *chess.Node, err error) string {
	t.Helper()
	var moveErr *chess.MoveError
	switch {
	case err == nil:
		return node.SAN
	case errors.As(err, &moveErr) && moveErr.Ambiguous():
		return "ambiguous: " + strings.Join(moveErr.Candidates, " ")
	case errors.As(err, &moveErr):
		return "illegal"
	}
	t.Fatalf("unexpected error: %v", err)
	return ""
}
