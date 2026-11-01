package gamescore

import (
	"strings"
	"testing"

	"chessbox-informant/internal/chess"
	"chessbox-informant/internal/notation"
)

func tokenize(chunks ...string) [][]notation.Token {
	var out [][]notation.Token
	for _, chunk := range chunks {
		out = append(out, notation.Tokenize(chunk, notation.Options{OCR: true}))
	}
	return out
}

func TestDetectConvention(t *testing.T) {
	cases := map[string]string{
		"portuguese": "1.e4 e5 2.Cf3 Cc6 3.Bc4 Bc5 4.c3 Cf6 5.d4 exd4 6.cxd4 Bb4+ 7.Cc3 Cxe4 8.O-O Cxc3 9.bxc3 Bxc3 10.Db3 d5",
		"german":     "1.e4 e5 2.Sf3 Sc6 3.Lb5 a6 4.La4 Sf6 5.O-O Le7 6.Te1 b5 7.Lb3 d6 8.c3 O-O 9.h3",
		"spanish":    "1.e4 e5 2.Cf3 Cc6 3.Ab5 a6 4.Aa4 Cf6 5.O-O Ae7 6.Te1 b5 7.Ab3 d6 8.c3 O-O 9.h3 Rh8",
		"french":     "1.e4 e5 2.Cf3 Cc6 3.Fb5 a6 4.Fa4 Cf6 5.O-O Fe7 6.Te1 b5 7.Fb3 d6 8.c3 O-O 9.h3 Rh8",
		"english":    "1.e4 e5 2.Nf3 Nc6 3.Bb5 a6 4.Ba4 Nf6 5.O-O Be7 6.Re1 b5 7.Bb3 d6 8.c3 O-O 9.h3",
	}
	for want, text := range cases {
		if got := Detect(tokenize(text)); got.Convention != want {
			t.Errorf("%s game detected as %s", want, got.Convention)
		}
	}
}

// Figurines mean the same in every convention, so a figurine book is left as
// English, which wins ties.
func TestDetectFigurines(t *testing.T) {
	text := "1.e4 e5 2.♘f3 ♘c6 3.♗b5 a6 4.♗a4 ♘f6 5.O-O ♗e7 6.♖e1 b5 7.♗b3 d6 8.c3 O-O 9.h3 ♔h8"
	got := Detect(tokenize(text))
	if got.Convention != "english" || got.Replayed != 9 {
		t.Errorf("detection = %s replaying %d moves, want english replaying all 9 numbered moves", got.Convention, got.Replayed)
	}
}

func TestDetectEnglishFixture(t *testing.T) {
	var text []string
	for _, title := range []string{"Chapter 1", "Chapter 2"} {
		text = append(text, chapter(t, title)...)
	}
	got := Detect(tokenize(text...))
	if got.Convention != "english" || len(got.Learned) > 0 {
		t.Errorf("detection = %s with learned glyphs %v, want english and none", got.Convention, got.Learned)
	}
}

// "_" stands for a knight five times where only a knight fits. Once learned,
// it settles "2._f3" in the second game, which lookahead alone cannot: Nf3 and
// Qf3 both stay legal.
func TestDetectLearnsGlyphs(t *testing.T) {
	chunks := tokenize(
		"1.d4 d5 2._c3 _f6 3._f3 e6 4.Bg5 _bd7 5.e3 c6 6._e5 Be7",
		"1.e4 e5 2._f3 Nc6 3.Bc4 Bc5 4.d3 d6 5.c3 a6 6.h3 h6 7.a3 b6 8.b3",
	)
	if p := parseAll(chunks, chess.EnglishNames); mainLineSANs(p.Games()[1]) != "e4 e5" {
		t.Fatalf("without learning, the second game should stop at the ambiguous move")
	}

	got := Detect(chunks)
	if l, ok := got.Learned["_"]; !ok || l.Piece != chess.Knight {
		t.Fatalf("learned = %v, want _ as a knight", got.Learned)
	}
	p := parseAll(chunks, got.Names)
	if line := mainLineSANs(p.Games()[1]); !strings.HasPrefix(line, "e4 e5 Nf3 Nc6") || !strings.HasSuffix(line, "b3") {
		t.Errorf("second game = %q, want it replayed to the end", line)
	}
}

func TestDetectDoesNotLearnAGlyphForSeveralPieces(t *testing.T) {
	chunks := tokenize("1.d4 d5 2.�c3 �f6 3.�f4 e6 4.e3 �b4 5.�b5+ c6 6.�ge2")
	played := 0
	for _, out := range parseAll(chunks, chess.EnglishNames).Outcomes() {
		if strings.HasPrefix(out.Token.Move, "�") && out.Status == Played {
			played++
		}
	}
	if played != 6 {
		t.Fatalf("%d moves with U+FFFD replayed, want all 6", played)
	}
	if l, ok := Detect(chunks).Learned["�"]; ok {
		t.Errorf("learned U+FFFD as %v", l)
	}
}
