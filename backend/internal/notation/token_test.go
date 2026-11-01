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
		"e4":           {Move: "e4", To: "e4"},
		"Nf3":          {Move: "Nf3", Piece: "N", To: "f3"},
		"1.e4":         {Move: "e4", Number: 1, NumberPrinted: true, Side: White, To: "e4"},
		"23. Nf5":      {Move: "Nf5", Number: 23, NumberPrinted: true, Side: White, Piece: "N", To: "f5"},
		"23...gxf5":    {Move: "gxf5", Number: 23, NumberPrinted: true, Side: Black, From: "g", Capture: true, To: "f5"},
		"23. ... gxf5": {Move: "gxf5", Number: 23, NumberPrinted: true, Side: Black, From: "g", Capture: true, To: "f5"},
		"23…gxf5":      {Move: "gxf5", Number: 23, NumberPrinted: true, Side: Black, From: "g", Capture: true, To: "f5"},
		"...Qd7":       {Move: "Qd7", Side: Black, Piece: "Q", To: "d7"},
		"Nbd7":         {Move: "Nbd7", Piece: "N", From: "b", To: "d7"},
		"R1e2":         {Move: "R1e2", Piece: "R", From: "1", To: "e2"},
		"Qh4xe1":       {Move: "Qh4xe1", Piece: "Q", From: "h4", Capture: true, To: "e1"},
		"Bxf3":         {Move: "Bxf3", Piece: "B", Capture: true, To: "f3"},
		"L×e5":         {Move: "L×e5", Piece: "L", Capture: true, To: "e5"},
		"♘f3":          {Move: "♘f3", Piece: "♘", To: "f3"},
		"O-O":          {Move: "O-O", Castle: chess.ShortCastle},
		"0-0":          {Move: "0-0", Castle: chess.ShortCastle},
		"12.O-O-O":     {Move: "O-O-O", Number: 12, NumberPrinted: true, Side: White, Castle: chess.LongCastle},
		"0–0–0":        {Move: "0–0–0", Castle: chess.LongCastle},
		"exd8=Q+":      {Move: "exd8=Q", From: "e", Capture: true, To: "d8", Promotion: "Q", Check: true},
		"exd8Q":        {Move: "exd8Q", From: "e", Capture: true, To: "d8", Promotion: "Q"},
		"a1=♕":         {Move: "a1=♕", To: "a1", Promotion: "♕"},
		"Rd8#":         {Move: "Rd8", Piece: "R", To: "d8", Mate: true},
		"Qxf7++":       {Move: "Qxf7", Piece: "Q", Capture: true, To: "f7", Mate: true},
		"Nf5!":         {Move: "Nf5", Piece: "N", To: "f5", Glyph: "!"},
		"b5?":          {Move: "b5", To: "b5", Glyph: "?"},
		"Bg4?!":        {Move: "Bg4", Piece: "B", To: "g4", Glyph: "?!"},
		"Rg1!?":        {Move: "Rg1", Piece: "R", To: "g1", Glyph: "!?"},
		"Bd6!!":        {Move: "Bd6", Piece: "B", To: "d6", Glyph: "!!"},
		"c8=Q??":       {Move: "c8=Q", To: "c8", Promotion: "Q", Glyph: "??"},
		"Qb8+‼":        {Move: "Qb8", Piece: "Q", To: "b8", Check: true, Glyph: "!!"},
		"Ce3⁉":         {Move: "Ce3", Piece: "C", To: "e3", Glyph: "!?"},
	}
	for text, want := range cases {
		t.Run(text, func(t *testing.T) {
			want.ID, want.Start, want.End, want.Text = "m1", 0, len(text), text
			got := Tokenize(text, Options{})
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
		if got := Tokenize(text, Options{}); len(got) != 0 {
			t.Errorf("Tokenize(%q) = %+v, want none", text, got)
		}
	}
}

func TestTokenizeBoundaries(t *testing.T) {
	text := "(4...dxe5 5.Qxd8+ Kxd8), after e4. Then Nf3—a fine move; \"O-O\""
	want := []string{"4...dxe5", "5.Qxd8+", "Kxd8", "e4", "Nf3", "O-O"}
	if got := texts(Tokenize(text, Options{})); !slices.Equal(got, want) {
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
			for _, tok := range Tokenize(text, Options{}) {
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
	tokens := Tokenize(text, Options{})
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
	if got := texts(Tokenize(study, Options{})); !slices.Equal(got, want) {
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

type found struct {
	text   string
	move   string
	number int
	side   Side
}

func assertFound(t *testing.T, text string, opts Options, want []found) {
	t.Helper()
	var got []found
	for _, tok := range Tokenize(text, opts) {
		got = append(got, found{tok.Text, tok.Move, tok.Number, tok.Side})
	}
	if !slices.Equal(got, want) {
		t.Errorf("Tokenize(%q, %+v) =\n%+v\nwant\n%+v", text, opts, got, want)
	}
}

func TestTokenizeNumberStyles(t *testing.T) {
	cases := map[string][]found{
		"24 axb4 Qb6 25 Rc2": {
			{"24 axb4", "axb4", 24, White},
			{"Qb6", "Qb6", 24, Black},
			{"25 Rc2", "Rc2", 25, White},
		},
		"25 ... Qb6":    {{"25 ... Qb6", "Qb6", 25, Black}},
		"20 . . . Nxe5": {{"20 . . . Nxe5", "Nxe5", 20, Black}},
		"19.Rh4± with":  {{"19.Rh4", "Rh4", 19, White}},
		"in 24 hours":   nil,
	}
	for text, want := range cases {
		for _, opts := range []Options{{}, {OCR: true}} {
			assertFound(t, text, opts, want)
		}
	}
}

// The examples imitate what OCR leaves in move text; none is taken from a book.
func TestTokenizeOCR(t *testing.T) {
	cases := map[string][]found{
		"2 1 .Rad1":        {{"2 1 .Rad1", "Rad1", 21, White}},
		"l 8 . . . f6":     {{"l 8 . . . f6", "f6", 18, Black}},
		"1O.e4":            {{"1O.e4", "e4", 10, White}},
		"12.lll f3":        {{"12.lll f3", "lll f3", 12, White}},
		"12.Nf3 lll x e5":  {{"12.Nf3", "Nf3", 12, White}, {"lll x e5", "lll x e5", 12, Black}},
		"15.:i!ad l":       {{"15.:i!ad l", ":i!ad l", 15, White}},
		"16.dS":            {{"16.dS", "dS", 16, White}},
		"17.e 5 !":         {{"17.e 5 !", "e 5", 17, White}},
		"18.'Wb6 �e2":      {{"18.'Wb6", "'Wb6", 18, White}, {"�e2", "�e2", 18, Black}},
		"18...Rd8 19.iLb5": {{"18...Rd8", "Rd8", 18, Black}, {"19.iLb5", "iLb5", 19, White}},
	}
	for text, want := range cases {
		assertFound(t, text, Options{OCR: true}, want)
	}
}

func TestTokenizeOCRNeedsMoveContext(t *testing.T) {
	cases := map[string][]found{
		"a knight on d5 is strong":    {{"d5", "d5", 0, UnknownSide}},
		"the knight lll f3 is strong": {{"f3", "f3", 0, UnknownSide}},
		"which they feel 20.Nf3":      {{"20.Nf3", "Nf3", 20, White}},
		"the a1-h8 diagonal":          nil,
	}
	for text, want := range cases {
		assertFound(t, text, Options{OCR: true}, want)
	}
}

func TestTokenizeOCRKeepsCleanText(t *testing.T) {
	data, err := os.ReadFile("../../tests/assets/classics/book.md")
	if err != nil {
		t.Fatal(err)
	}
	strict := Tokenize(string(data), Options{})
	ocr := Tokenize(string(data), Options{OCR: true})
	if got, want := texts(ocr), texts(strict); !slices.Equal(got, want) {
		t.Errorf("OCR mode tokens differ on clean text:\n%q\nwant\n%q", got, want)
	}
}

func TestTokenizeOCRKeepsMovesApart(t *testing.T) {
	assertFound(t, "5.i.xc4 c5 6.Wfe2 b5", Options{OCR: true}, []found{
		{"5.i.xc4", "i.xc4", 5, White},
		{"c5", "c5", 5, Black},
		{"6.Wfe2", "Wfe2", 6, White},
		{"b5", "b5", 6, Black},
	})
}

func TestTokenizeSplitNumberIsNotMoveOne(t *testing.T) {
	assertFound(t, "2 1 .e4", Options{}, nil)
}
