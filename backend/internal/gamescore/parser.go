// Package gamescore follows games through the moves of a book without any
// model: it places each move in a variation tree and settles ambiguous moves
// by looking ahead.
package gamescore

import (
	"errors"
	"slices"

	"chessbox-informant/internal/chess"
	"chessbox-informant/internal/notation"
)

const startFEN = "rnbqkbnr/pppppppp/8/8/8/8/PPPPPPPP/RNBQKBNR w KQkq - 0 1"

const (
	// maxHypotheses caps the candidates of an ambiguous move that are
	// followed; a move with more is left ambiguous.
	maxHypotheses = 4
	// window is how many numbered moves an ambiguity stays open.
	window = 8
	// minLead is how many more numbered moves the best candidate must have
	// played than the next one to win when the window closes.
	minLead = 2
)

type Status int

const (
	// NoGame means no game was open: no "1." white move has been seen, or
	// the game stopped at a move that stayed ambiguous.
	NoGame Status = iota
	NotPlaced
	Played
	Ambiguous
)

type Outcome struct {
	// Chunk is the index of the Feed call that gave the token.
	Chunk  int
	Token  notation.Token
	Status Status
	// Node is the position after the move, when Played.
	Node *chess.Node
	// Candidates are the legal moves, in SAN, that an Ambiguous move fits.
	Candidates []string
}

// lines holds the line being read at each variation depth: lines[0] is the
// main line, lines[1] a variation inside it, and so on.
type lines []*chess.Node

func (l lines) at(depth int) *chess.Node { return l[min(depth, len(l)-1)] }

// leave ends the variations deeper than depth.
func (l lines) leave(depth int) lines { return l[:min(depth+1, len(l))] }

func (l lines) advance(depth int, next *chess.Node) lines {
	l = slices.Clone(l.leave(depth))
	for len(l) < depth {
		l = append(l, l[len(l)-1])
	}
	return append(l[:depth], next)
}

type Parser struct {
	names    chess.PieceNames
	games    []*chess.Node
	lines    lines
	open     *lookahead
	outcomes []Outcome
	chunk    int
	// depth is the variation depth of the move that created each node.
	depth map[*chess.Node]int
}

// lookahead follows every candidate of an ambiguous move until the moves after
// it tell them apart.
type lookahead struct {
	outcome    int
	depth      int
	candidates []string
	hypotheses []*hypothesis
	held       []Outcome
	numbered   int
}

type hypothesis struct {
	branch *chess.Node
	lines  lines
	played int
	// created holds the nodes this hypothesis added, to remove if it loses.
	created []*chess.Node
	// nodes holds, per held token, the node it was played to.
	nodes []*chess.Node
}

func New(names chess.PieceNames) *Parser {
	return &Parser{names: names, depth: make(map[*chess.Node]int)}
}

// Feed places the tokens of one chunk. A game continues from one chunk to the
// next.
func (p *Parser) Feed(tokens []notation.Token) {
	for _, tok := range tokens {
		p.feed(tok)
	}
	p.chunk++
}

// Finish settles an ambiguity still open at the end of the book.
func (p *Parser) Finish() {
	if p.open != nil {
		p.resolve()
	}
}

// Outcomes returns what happened to every token fed, in order. Call Finish
// first.
func (p *Parser) Outcomes() []Outcome { return p.outcomes }

// Games returns the root of each game's tree.
func (p *Parser) Games() []*chess.Node { return p.games }

func (p *Parser) feed(tok notation.Token) {
	if tok.NumberPrinted && tok.Number == 1 && tok.Side == notation.White {
		p.Finish()
		root, err := chess.NewTree(startFEN)
		if err != nil {
			panic(err)
		}
		p.games = append(p.games, root)
		p.lines = lines{root}
	}
	if p.open != nil {
		p.feedLookahead(tok)
		return
	}

	out := Outcome{Chunk: p.chunk, Token: tok}
	if p.lines == nil {
		p.outcomes = append(p.outcomes, out)
		return
	}
	pl := p.place(tok, p.lines.at(tok.Depth), nil)
	switch {
	case pl.next != nil:
		out.Status, out.Node = Played, pl.next
		p.lines = p.lines.advance(tok.Depth, pl.next)
	case len(pl.candidates) > 1 && tok.Number > 0 && len(pl.candidates) <= maxHypotheses:
		p.outcomes = append(p.outcomes, out)
		p.openLookahead(tok, pl.at, pl.candidates)
		return
	case len(pl.candidates) > 1:
		out.Status, out.Candidates = Ambiguous, pl.candidates
		p.lines = p.lines.leave(tok.Depth)
	default:
		out.Status = NotPlaced
		p.lines = p.lines.leave(tok.Depth)
	}
	p.outcomes = append(p.outcomes, out)
}

type placement struct {
	next, at *chess.Node
	// created is true when next is a new node rather than one already in
	// the tree.
	created    bool
	candidates []string
}

// place plays tok from the position where it belongs. A numbered move goes
// where its number is due on the current line, or else on the main line,
// which is how a book returns from a variation. An unnumbered move continues
// the current line. Within a lookahead, within limits placement to the
// hypothesis's own line.
func (p *Parser) place(tok notation.Token, current *chess.Node, within *hypothesis) placement {
	nodes := []*chess.Node{current}
	if tok.Number > 0 {
		nodes = due(tok, current, within)
	}
	var pl placement
	for _, node := range nodes {
		before := len(node.Children)
		next, err := node.PlayPrinted(tok.ID, tok.Move, p.names)
		if err == nil {
			created := len(node.Children) > before
			p.order(node, next, tok.Depth, created)
			return placement{next: next, at: node, created: created}
		}
		var moveErr *chess.MoveError
		if errors.As(err, &moveErr) && moveErr.Ambiguous() && pl.candidates == nil {
			pl.at, pl.candidates = node, moveErr.Candidates
		}
	}
	return pl
}

// order keeps the main line first. Books often give a variation before the
// main move it replaces, so the first move played from a position is not
// always the main one: a move outside parentheses outranks one inside.
func (p *Parser) order(at, next *chess.Node, depth int, created bool) {
	if d, ok := p.depth[next]; created || !ok || depth < d {
		p.depth[next] = depth
	}
	if main := at.Children[0]; main != next && p.depth[next] < p.depth[main] {
		next.Promote()
	}
}

func due(tok notation.Token, current *chess.Node, within *hypothesis) []*chess.Node {
	side := chess.White
	if tok.Side == notation.Black {
		side = chess.Black
	}
	isDue := func(n *chess.Node) bool {
		number, s := n.NextMove()
		return number == tok.Number && s == side
	}

	var nodes []*chess.Node
	for n := current; n != nil; n = n.Parent {
		if isDue(n) {
			nodes = append(nodes, n)
		}
		if within != nil && n == within.branch {
			return nodes
		}
	}
	root := current
	for root.Parent != nil {
		root = root.Parent
	}
	for n := root; ; n = n.Children[0] {
		if isDue(n) && !slices.Contains(nodes, n) {
			nodes = append(nodes, n)
		}
		if len(n.Children) == 0 {
			return nodes
		}
	}
}

func (p *Parser) openLookahead(tok notation.Token, at *chess.Node, candidates []string) {
	la := &lookahead{outcome: len(p.outcomes) - 1, depth: tok.Depth, candidates: candidates}
	for _, san := range candidates {
		before := len(at.Children)
		branch, err := at.PlayPrinted(tok.ID, san, chess.EnglishNames)
		if err != nil {
			panic("candidate " + san + " does not replay: " + err.Error())
		}
		created := len(at.Children) > before
		p.order(at, branch, tok.Depth, created)
		h := &hypothesis{branch: branch, lines: p.lines.advance(tok.Depth, branch)}
		if created {
			h.created = append(h.created, branch)
		}
		la.hypotheses = append(la.hypotheses, h)
	}
	p.open = la
}

func (p *Parser) feedLookahead(tok notation.Token) {
	la := p.open
	if tok.Depth < la.depth {
		// The variation that holds the ambiguous move has ended.
		p.resolve()
		p.feed(tok)
		return
	}
	placements := make([]placement, len(la.hypotheses))
	for i, h := range la.hypotheses {
		placements[i] = p.place(tok, h.lines.at(tok.Depth), h)
	}
	if tok.Number > 0 && !slices.ContainsFunc(placements, func(pl placement) bool { return pl.next != nil }) {
		// No candidate can place this move, so it says nothing about which one
		// is right: it is probably a return to an earlier line.
		p.resolve()
		p.feed(tok)
		return
	}

	la.held = append(la.held, Outcome{Chunk: p.chunk, Token: tok})
	if tok.Number > 0 {
		la.numbered++
	}
	var alive []*hypothesis
	for i, h := range la.hypotheses {
		pl := placements[i]
		h.nodes = append(h.nodes, pl.next)
		if pl.created {
			h.created = append(h.created, pl.next)
		}
		switch {
		case pl.next != nil:
			h.lines = h.lines.advance(tok.Depth, pl.next)
			if tok.Number > 0 {
				h.played++
			}
		case tok.Number > 0:
			h.remove()
			continue
		default:
			h.lines = h.lines.leave(tok.Depth)
		}
		alive = append(alive, h)
	}
	la.hypotheses = alive
	if len(alive) == 1 || la.numbered >= window {
		p.resolve()
	}
}

// resolve keeps the hypothesis that played clearly more moves than the
// others. With no clear winner the move stays ambiguous and the game stops,
// since a guess would put a move in the index that the text does not support.
func (p *Parser) resolve() {
	la := p.open
	p.open = nil
	slices.SortStableFunc(la.hypotheses, func(a, b *hypothesis) int { return b.played - a.played })

	var winner *hypothesis
	if len(la.hypotheses) == 1 || la.hypotheses[0].played-la.hypotheses[1].played >= minLead {
		winner = la.hypotheses[0]
	}
	for _, h := range la.hypotheses {
		if h != winner {
			h.remove()
		}
	}

	ambiguous := &p.outcomes[la.outcome]
	if winner == nil {
		ambiguous.Status, ambiguous.Candidates = Ambiguous, la.candidates
		p.outcomes = append(p.outcomes, la.held...)
		p.lines = nil
		return
	}
	ambiguous.Status, ambiguous.Node = Played, winner.branch
	for i, out := range la.held {
		out.Status = NotPlaced
		if node := winner.nodes[i]; node != nil {
			out.Status, out.Node = Played, node
		}
		p.outcomes = append(p.outcomes, out)
	}
	p.lines = winner.lines
}

func (h *hypothesis) remove() {
	for _, n := range slices.Backward(h.created) {
		n.Remove()
	}
}
