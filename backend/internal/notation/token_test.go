package notation

import (
	"fmt"
	"os"
	"slices"
	"strings"
	"testing"

	"chessbox-informant/internal/chess"
)

func TestTokenizeMove(t *testing.T) {
	cases := map[string]Token{
		"e4":           {To: "e4"},
		"Nf3":          {Piece: "N", To: "f3"},
		"1.e4":         {Number: 1, NumberPrinted: true, Side: White, To: "e4"},
		"23. Nf5":      {Number: 23, NumberPrinted: true, Side: White, Piece: "N", To: "f5"},
		"23...gxf5":    {Number: 23, NumberPrinted: true, Side: Black, From: "g", Capture: true, To: "f5"},
		"23. ... gxf5": {Number: 23, NumberPrinted: true, Side: Black, From: "g", Capture: true, To: "f5"},
		"23…gxf5":      {Number: 23, NumberPrinted: true, Side: Black, From: "g", Capture: true, To: "f5"},
		"...Qd7":       {Side: Black, Piece: "Q", To: "d7"},
		"Nbd7":         {Piece: "N", From: "b", To: "d7"},
		"R1e2":         {Piece: "R", From: "1", To: "e2"},
		"Qh4xe1":       {Piece: "Q", From: "h4", Capture: true, To: "e1"},
		"Bxf3":         {Piece: "B", Capture: true, To: "f3"},
		"L×e5":         {Piece: "L", Capture: true, To: "e5"},
		"♘f3":          {Piece: "♘", To: "f3"},
		"O-O":          {Castle: chess.ShortCastle},
		"0-0":          {Castle: chess.ShortCastle},
		"12.O-O-O":     {Number: 12, NumberPrinted: true, Side: White, Castle: chess.LongCastle},
		"0–0–0":        {Castle: chess.LongCastle},
		"exd8=Q+":      {From: "e", Capture: true, To: "d8", Promotion: "Q", Check: true},
		"exd8Q":        {From: "e", Capture: true, To: "d8", Promotion: "Q"},
		"a1=♕":         {To: "a1", Promotion: "♕"},
		"Rd8#":         {Piece: "R", To: "d8", Mate: true},
		"Qxf7++":       {Piece: "Q", Capture: true, To: "f7", Mate: true},
		"Nf5!":         {Piece: "N", To: "f5", Glyph: "!"},
		"b5?":          {To: "b5", Glyph: "?"},
		"Bg4?!":        {Piece: "B", To: "g4", Glyph: "?!"},
		"Rg1!?":        {Piece: "R", To: "g1", Glyph: "!?"},
		"Bd6!!":        {Piece: "B", To: "d6", Glyph: "!!"},
		"c8=Q??":       {To: "c8", Promotion: "Q", Glyph: "??"},
		"Qb8+‼":        {Piece: "Q", To: "b8", Check: true, Glyph: "!!"},
		"Ce3⁉":         {Piece: "C", To: "e3", Glyph: "!?"},
	}
	for text, want := range cases {
		t.Run(text, func(t *testing.T) {
			want.ID, want.Start, want.End, want.Text = "m1", 0, len(text), text
			got := Tokenize(text)
			if len(got) != 1 || got[0] != want {
				t.Errorf("Tokenize(%q) = %+v, want [%+v]", text, got, want)
			}
		})
	}
}

func TestTokenizeIgnoresNonMoves(t *testing.T) {
	for _, text := range []string{
		"the long a1-h8 diagonal",
		"the a1–h8 diagonal",
		"a rook on the c-file",
		"1-0",
		"0-1",
		"1/2-1/2",
		"Paris 1858.",
		"page 3. Be careful",
		"One4 Ab2c",
		"B12 and A1",
		"e4e5",
		"e5Q",
		"Nf3-g5",
	} {
		if got := Tokenize(text); len(got) != 0 {
			t.Errorf("Tokenize(%q) = %+v, want none", text, got)
		}
	}
}

func TestTokenizeBoundaries(t *testing.T) {
	text := "(4...dxe5 5.Qxd8+ Kxd8), after e4. Then Nf3—a fine move; \"O-O\""
	want := []string{"4...dxe5", "5.Qxd8+", "Kxd8", "e4", "Nf3", "O-O"}
	if got := texts(Tokenize(text)); !slices.Equal(got, want) {
		t.Errorf("tokens = %q, want %q", got, want)
	}
}

func TestTokenizeSides(t *testing.T) {
	type move struct {
		text    string
		side    Side
		number  int
		printed bool
	}
	cases := map[string][]move{
		"23.Nf5 b4": {
			{"23.Nf5", White, 23, true},
			{"b4", Black, 23, false},
		},
		"23.Nf5! b4 24.Qg3+ Kh8": {
			{"23.Nf5!", White, 23, true},
			{"b4", Black, 23, false},
			{"24.Qg3+", White, 24, true},
			{"Kh8", Black, 24, false},
		},
		"23...gxf5 Qd7": {
			{"23...gxf5", Black, 23, true},
			{"Qd7", UnknownSide, 0, false},
		},
		"23.Nf5 is strong, but gxf5 refutes it": {
			{"23.Nf5", White, 23, true},
			{"gxf5", UnknownSide, 0, false},
		},
		"Kh8 24.Qg3": {
			{"Kh8", UnknownSide, 0, false},
			{"24.Qg3", White, 24, true},
		},
	}
	for text, want := range cases {
		t.Run(text, func(t *testing.T) {
			var got []move
			for _, tok := range Tokenize(text) {
				got = append(got, move{tok.Text, tok.Side, tok.Number, tok.NumberPrinted})
			}
			if !slices.Equal(got, want) {
				t.Errorf("moves = %+v, want %+v", got, want)
			}
		})
	}
}

func TestTokenizeIDsAndOffsets(t *testing.T) {
	text := "23.Nf5! A move that changes everything. 23...gxf5 (23...Qd7 24.Nh6+ Kh8 25.Qg4 and Black is lost) 24.Qg3+ Kh8 25.Qh4 The point: h7 cannot be defended."
	want := []string{"23.Nf5!", "23...gxf5", "23...Qd7", "24.Nh6+", "Kh8", "25.Qg4", "24.Qg3+", "Kh8", "25.Qh4", "h7"}
	tokens := Tokenize(text)
	if got := texts(tokens); !slices.Equal(got, want) {
		t.Fatalf("tokens = %q, want %q", got, want)
	}
	for i, tok := range tokens {
		if id := fmt.Sprintf("m%d", i+1); tok.ID != id {
			t.Errorf("token %d ID = %q, want %q", i, tok.ID, id)
		}
		if text[tok.Start:tok.End] != tok.Text {
			t.Errorf("token %s spans %q, want %q", tok.ID, text[tok.Start:tok.End], tok.Text)
		}
	}
}

// Squares named in prose are tokens too: telling them apart from moves is left
// to replay and the parse stage.
func TestTokenizeFixtureStudy(t *testing.T) {
	data, err := os.ReadFile("../../tests/assets/classics/book.md")
	if err != nil {
		t.Fatal(err)
	}
	_, study, _ := strings.Cut(string(data), "# Chapter 3")
	study, _, _ = strings.Cut(study, "# Chapter 4")

	want := []string{
		"b6", "c6", "a1", "d5",
		"1.c7", "Rd6+", "2.Kb5", "Rd5+", "3.Kb4", "Rd4+", "4.Kb3", "Rd3+", "5.Kc2",
		"5...Rd4!", "6.c8=Q??", "Rc4+", "7.Qxc4",
		"6.c8=R!", "6...Ra4", "7.Kb3", "c1",
	}
	if got := texts(Tokenize(study)); !slices.Equal(got, want) {
		t.Errorf("tokens = %q, want %q", got, want)
	}
}

func texts(tokens []Token) []string {
	var out []string
	for _, tok := range tokens {
		out = append(out, tok.Text)
	}
	return out
}
