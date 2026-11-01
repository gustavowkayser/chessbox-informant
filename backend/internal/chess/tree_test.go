package chess_test

import (
	"errors"
	"slices"
	"strings"
	"testing"

	"chessbox-informant/internal/chess"
	"chessbox-informant/internal/notation"
)

const startFEN = "rnbqkbnr/pppppppp/8/8/8/8/PPPPPPPP/RNBQKBNR w KQkq - 0 1"

// Final positions are taken from published sources, not from this code.
func TestReplayReferenceGames(t *testing.T) {
	cases := map[string]struct {
		start, moves, want string
	}{
		"Opera Game": {
			start: startFEN,
			moves: "1.e4 e5 2.Nf3 d6 3.d4 Bg4 4.dxe5 Bxf3 5.Qxf3 dxe5 6.Bc4 Nf6 7.Qb3 Qe7 8.Nc3 c6 9.Bg5 b5 10.Nxb5 cxb5 11.Bxb5+ Nbd7 12.O-O-O Rd8 13.Rxd7 Rxd7 14.Rd1 Qe6 15.Bxd7+ Nxd7 16.Qb8+ Nxb8 17.Rd8#",
			want:  "1n1Rkb1r/p4ppp/4q3/4p1B1/4P3/8/PPP2PPP/2K5 b k - 1 17",
		},
		"Immortal Game": {
			start: startFEN,
			moves: "1.e4 e5 2.f4 exf4 3.Bc4 Qh4+ 4.Kf1 b5 5.Bxb5 Nf6 6.Nf3 Qh6 7.d3 Nh5 8.Nh4 Qg5 9.Nf5 c6 10.g4 Nf6 11.Rg1 cxb5 12.h4 Qg6 13.h5 Qg5 14.Qf3 Ng8 15.Bxf4 Qf6 16.Nc3 Bc5 17.Nd5 Qxb2 18.Bd6 Bxg1 19.e5 Qxa1+ 20.Ke2 Na6 21.Nxg7+ Kd8 22.Qf6+ Nxf6 23.Be7#",
			want:  "r1bk3r/p2pBpNp/n4n2/1p1NP2P/6P1/3P4/P1P1K3/q5b1 b - - 1 23",
		},
		"Saavedra study": {
			start: "8/8/1KP5/3r4/8/8/8/k7 w - - 0 1",
			moves: "1.c7 Rd6+ 2.Kb5 Rd5+ 3.Kb4 Rd4+ 4.Kb3 Rd3+ 5.Kc2 Rd4 6.c8=R Ra4 7.Kb3",
			want:  "2R5/8/8/8/r7/1K6/8/k7 b - - 2 7",
		},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			root := newTree(t, c.start)
			if got := play(t, root, c.moves).FEN(); got != c.want {
				t.Errorf("final FEN = %q, want %q", got, c.want)
			}
		})
	}
}

func TestReplayVariation(t *testing.T) {
	root := newTree(t, startFEN)
	afterBxf3 := play(t, root, "1.e4 e5 2.Nf3 d6 3.d4 Bg4 4.dxe5 Bxf3")
	branch := afterBxf3.Parent

	end := play(t, branch, "4...dxe5 5.Qxd8+ Kxd8 6.Nxe5")
	if want := "rn1k1bnr/ppp2ppp/8/4N3/4P1b1/8/PPP2PPP/RNB1KB1R b KQ"; end.Key() != want {
		t.Errorf("variation key = %q, want %q", end.Key(), want)
	}
	if got := sans(branch.Children); !slices.Equal(got, []string{"Bxf3", "dxe5"}) {
		t.Errorf("children = %q, want the main line first", got)
	}
	if got := end.Line(); got != "1.e4 e5 2.Nf3 d6 3.d4 Bg4 4.dxe5 dxe5 5.Qxd8+ Kxd8 6.Nxe5" {
		t.Errorf("line = %q", got)
	}

	again := play(t, branch, "4...Bxf3")
	if again != afterBxf3 || len(branch.Children) != 2 {
		t.Errorf("replaying the main-line move added a node: %d children", len(branch.Children))
	}
}

func TestReplayMoveNumbers(t *testing.T) {
	root := newTree(t, "r1bqkb1r/pppp1ppp/2n2n2/4p3/2B1P3/5N2/PPPP1PPP/RNBQK2R b KQkq - 14 37")
	end := play(t, root, "37...Nxe4 38.O-O")
	if got := end.Line(); got != "37...Nxe4 38.O-O" {
		t.Errorf("line = %q", got)
	}
	black, white := end.Parent, end
	if black.MoveNumber != 37 || black.Side != chess.Black || white.MoveNumber != 38 || white.Side != chess.White {
		t.Errorf("moves = (%d, %v), (%d, %v), want (37, Black), (38, White)", black.MoveNumber, black.Side, white.MoveNumber, white.Side)
	}
	if n, side := root.NextMove(); n != 37 || side != chess.Black {
		t.Errorf("root next move = (%d, %v), want (37, Black)", n, side)
	}
	if n, side := end.NextMove(); n != 38 || side != chess.Black {
		t.Errorf("next move = (%d, %v), want (38, Black)", n, side)
	}
	if black.Ref != "m1" || white.Ref != "m2" {
		t.Errorf("refs = %q, %q, want m1, m2", black.Ref, white.Ref)
	}
}

func TestReplayResolve(t *testing.T) {
	cases := map[string]struct {
		fen  string
		spec chess.MoveSpec
		want string
	}{
		"castling written with zeros": {
			fen:  "r3k2r/8/8/8/8/8/8/R3K2R w KQkq - 0 1",
			spec: spec(t, "0-0"),
			want: "O-O",
		},
		"promotion without =": {
			fen:  "8/4P3/8/8/8/8/k7/4K3 w - - 0 1",
			spec: spec(t, "e8Q"),
			want: "e8=Q",
		},
		"disambiguation by rank": {
			fen:  "4k3/8/8/8/8/5N2/8/1N2K3 w - - 0 1",
			spec: spec(t, "N1d2"),
			want: "Nbd2",
		},
		"unneeded disambiguation": {
			fen:  startFEN,
			spec: spec(t, "Ngf3"),
			want: "Nf3",
		},
		"en passant": {
			fen:  "4k3/8/8/3pP3/8/8/8/4K3 w - d6 0 1",
			spec: spec(t, "exd6"),
			want: "exd6",
		},
		"check marks are ignored": {
			fen:  "6k1/5ppp/8/8/8/8/8/R5K1 w - - 0 1",
			spec: spec(t, "Ra8+"),
			want: "Ra8#",
		},
		"unknown piece with one legal move": {
			fen:  startFEN,
			spec: chess.MoveSpec{Text: "�f3", Piece: chess.UnknownPiece, To: "f3"},
			want: "Nf3",
		},
		"unknown promotion piece": {
			fen:  "8/4P3/8/8/8/8/k7/4K3 w - - 0 1",
			spec: chess.MoveSpec{Text: "e8=�", To: "e8", Promotion: chess.UnknownPiece},
			want: "ambiguous: e8=B e8=N e8=Q e8=R",
		},
		"unknown piece with several legal moves": {
			fen:  "rnbqkbnr/pppp1ppp/8/4p3/4P3/8/PPPP1PPP/RNBQKBNR w KQkq e6 0 2",
			spec: chess.MoveSpec{Text: "�e2", Piece: chess.UnknownPiece, To: "e2"},
			want: "ambiguous: Be2 Ke2 Ne2 Qe2",
		},
		"ambiguous move": {
			fen:  "4k3/8/8/8/8/5N2/8/1N2K3 w - - 0 1",
			spec: spec(t, "Nd2"),
			want: "ambiguous: Nbd2 Nfd2",
		},
		"capture mark without a capture": {
			fen:  startFEN,
			spec: spec(t, "Nxf3"),
			want: "illegal",
		},
		"capture without a capture mark": {
			fen:  "4k3/8/8/8/8/5p2/8/4K1N1 w - - 0 1",
			spec: spec(t, "Nf3"),
			want: "illegal",
		},
		"promotion without a piece": {
			fen:  "8/4P3/8/8/8/8/k7/4K3 w - - 0 1",
			spec: chess.MoveSpec{Text: "e8", To: "e8"},
			want: "illegal",
		},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			node, err := newTree(t, c.fen).Play("", c.spec)
			var got string
			var moveErr *chess.MoveError
			switch {
			case err == nil:
				got = node.SAN
			case errors.As(err, &moveErr) && moveErr.Ambiguous():
				got = "ambiguous: " + strings.Join(moveErr.Candidates, " ")
			case errors.As(err, &moveErr):
				got = "illegal"
			default:
				t.Fatalf("unexpected error: %v", err)
			}
			if got != c.want {
				t.Errorf("got %q, want %q", got, c.want)
			}
		})
	}
}

func TestMoveErrorMessages(t *testing.T) {
	root := newTree(t, startFEN)
	after := play(t, root, "1.e4 e5")

	_, err := after.Play("m3", spec(t, "2.Ke3"))
	if want := "m3 `2.Ke3` is illegal after 1.e4 e5"; err == nil || err.Error() != want {
		t.Errorf("error = %v, want %q", err, want)
	}

	_, err = newTree(t, "4k3/8/8/8/8/5N2/8/1N2K3 w - - 0 1").Play("m1", spec(t, "1.Nd2"))
	if want := "m1 `1.Nd2` is ambiguous in the starting position: it could be Nbd2 or Nfd2"; err == nil || err.Error() != want {
		t.Errorf("error = %v, want %q", err, want)
	}
}

func TestNewTreeRejectsInvalidFEN(t *testing.T) {
	if _, err := chess.NewTree("not a fen"); err == nil {
		t.Error("NewTree accepted an invalid FEN")
	}
}

func newTree(t *testing.T, fen string) *chess.Node {
	t.Helper()
	root, err := chess.NewTree(fen)
	if err != nil {
		t.Fatal(err)
	}
	return root
}

// play replays the moves in text one after another from node, and returns the
// last position.
func play(t *testing.T, node *chess.Node, text string) *chess.Node {
	t.Helper()
	for _, tok := range notation.Tokenize(text, notation.Options{}) {
		next, err := node.Play(tok.ID, englishSpec(tok))
		if err != nil {
			t.Fatal(err)
		}
		node = next
	}
	return node
}

func spec(t *testing.T, text string) chess.MoveSpec {
	t.Helper()
	tokens := notation.Tokenize(text, notation.Options{})
	if len(tokens) != 1 {
		t.Fatalf("%q is not a single move", text)
	}
	return englishSpec(tokens[0])
}

var englishPieces = map[string]chess.Piece{
	"":  chess.Pawn,
	"N": chess.Knight,
	"B": chess.Bishop,
	"R": chess.Rook,
	"Q": chess.Queen,
	"K": chess.King,
}

// englishSpec stands in for the notation conventions, which are not built yet.
func englishSpec(tok notation.Token) chess.MoveSpec {
	return chess.MoveSpec{
		Text:      tok.Text,
		Piece:     englishPieces[tok.Piece],
		From:      tok.From,
		To:        tok.To,
		Capture:   tok.Capture,
		Promotion: englishPieces[tok.Promotion],
		Castle:    tok.Castle,
	}
}

func sans(nodes []*chess.Node) []string {
	var out []string
	for _, n := range nodes {
		out = append(out, n.SAN)
	}
	return out
}
