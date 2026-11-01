package gamescore

import (
	"os"
	"slices"
	"strings"
	"testing"

	"chessbox-informant/internal/chess"
	"chessbox-informant/internal/notation"
)

func parse(names chess.PieceNames, chunks ...string) *Parser {
	p := New(names)
	for _, chunk := range chunks {
		p.Feed(notation.Tokenize(chunk, notation.Options{OCR: true}))
	}
	p.Finish()
	return p
}

func mainLine(root *chess.Node) []*chess.Node {
	var line []*chess.Node
	for n := root; len(n.Children) > 0; {
		n = n.Children[0]
		line = append(line, n)
	}
	return line
}

func mainLineSANs(root *chess.Node) string {
	var sans []string
	for _, n := range mainLine(root) {
		sans = append(sans, n.SAN)
	}
	return strings.Join(sans, " ")
}

func chapter(t *testing.T, title string) []string {
	t.Helper()
	data, err := os.ReadFile("../../tests/assets/classics/book.md")
	if err != nil {
		t.Fatal(err)
	}
	_, text, _ := strings.Cut(string(data), "# "+title)
	text, _, _ = strings.Cut(text, "\n# ")
	return strings.Split(strings.TrimSpace(text), "\n\n")
}

func TestOperaGame(t *testing.T) {
	p := parse(chess.EnglishNames, chapter(t, "Chapter 1")...)
	if len(p.Games()) != 1 {
		t.Fatalf("games = %d, want 1", len(p.Games()))
	}
	root := p.Games()[0]
	want := "e4 e5 Nf3 d6 d4 Bg4 dxe5 Bxf3 Qxf3 dxe5 Bc4 Nf6 Qb3 Qe7 Nc3 c6 Bg5 b5 Nxb5 cxb5 Bxb5+ Nbd7 O-O-O Rd8 Rxd7 Rxd7 Rd1 Qe6 Bxd7+ Nxd7 Qb8+ Nxb8 Rd8#"
	if got := mainLineSANs(root); got != want {
		t.Errorf("main line =\n%s\nwant\n%s", got, want)
	}
	line := mainLine(root)
	if got := line[len(line)-1].FEN(); got != "1n1Rkb1r/p4ppp/4q3/4p1B1/4P3/8/PPP2PPP/2K5 b k - 1 17" {
		t.Errorf("final FEN = %q", got)
	}

	afterDxe5 := line[6]
	if got := len(afterDxe5.Children); got != 2 {
		t.Fatalf("after 4.dxe5: %d children, want the main line and 4...dxe5", got)
	}
	variation := afterDxe5.Children[1]
	if got := variation.SAN; got != "dxe5" {
		t.Errorf("variation starts with %q, want dxe5", got)
	}
	afterBc4 := line[10]
	if got := afterBc4.Children[0].Line(); !strings.HasSuffix(got, "6.Bc4 Nf6") {
		t.Errorf("the game did not return to the main line after the variation: %q", got)
	}

	for _, out := range p.Outcomes() {
		if out.Token.NumberPrinted && out.Status != Played {
			t.Errorf("%s %q: status %d, want played", out.Token.ID, out.Token.Text, out.Status)
		}
	}
}

func TestImmortalGame(t *testing.T) {
	p := parse(chess.EnglishNames, chapter(t, "Chapter 2")...)
	line := mainLine(p.Games()[0])
	if got := line[len(line)-1].FEN(); got != "r1bk3r/p2pBpNp/n4n2/1p1NP2P/6P1/3P4/P1P1K3/q5b1 b - - 1 23" {
		t.Errorf("final FEN = %q", got)
	}
}

func TestGameContinuesAcrossChunks(t *testing.T) {
	p := parse(chess.EnglishNames, "1.e4 e5 2.Nf3 A classical start.", "2...Nc6 3.Bb5 a6")
	if got := mainLineSANs(p.Games()[0]); got != "e4 e5 Nf3 Nc6 Bb5 a6" {
		t.Errorf("main line = %q", got)
	}
	if out := p.Outcomes()[3]; out.Chunk != 1 || out.Status != Played {
		t.Errorf("2...Nc6 outcome = %+v, want played in chunk 1", out)
	}
}

// After 1.e4 e5, "2.�f3" may be Nf3 or Qf3. Only the knight can then reach
// g5.
func TestLookaheadSettlesAnAmbiguousMove(t *testing.T) {
	p := parse(chess.EnglishNames, "1.e4 e5 2.�f3 Nc6 3.Ng5")
	root := p.Games()[0]
	if got := mainLineSANs(root); got != "e4 e5 Nf3 Nc6 Ng5" {
		t.Errorf("main line = %q", got)
	}
	if got := len(mainLine(root)[1].Children); got != 1 {
		t.Errorf("after 1...e5: %d children, want the losing candidate removed", got)
	}
	for _, out := range p.Outcomes() {
		if out.Status != Played {
			t.Errorf("%q: status %d, want played", out.Token.Text, out.Status)
		}
	}
}

func TestLookaheadLeavesAnUnsettledMoveAmbiguous(t *testing.T) {
	p := parse(chess.EnglishNames, "1.e4 e5 2.�f3 Nc6 3.Bc4 Bc5 4.d3 d6 5.c3 a6 6.h3 h6 7.a3")
	root := p.Games()[0]
	if got := mainLineSANs(root); got != "e4 e5" {
		t.Errorf("main line = %q, want the game to stop before the ambiguous move", got)
	}
	out := p.Outcomes()[2]
	if out.Status != Ambiguous || !slices.Equal(out.Candidates, []string{"Nf3", "Qf3"}) {
		t.Errorf("2.�f3 outcome = %+v, want ambiguous between Nf3 and Qf3", out)
	}
	if last := p.Outcomes()[len(p.Outcomes())-1]; last.Status != NoGame {
		t.Errorf("7.a3 status = %d, want no game", last.Status)
	}
}

func TestMovesBeforeAGameAreNotPlaced(t *testing.T) {
	p := parse(chess.EnglishNames, "The knight on d5 is strong. 1.d4 d5")
	if got := p.Outcomes()[0].Status; got != NoGame {
		t.Errorf("d5 before the game: status %d, want no game", got)
	}
	if got := mainLineSANs(p.Games()[0]); got != "d4 d5" {
		t.Errorf("main line = %q", got)
	}
}
