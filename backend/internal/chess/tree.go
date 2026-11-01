package chess

import (
	"fmt"
	"slices"
	"strings"

	lib "github.com/corentings/chess/v2"
)

type Color int

const (
	White Color = iota
	Black
)

// Node is a position in a variation tree. The root is the starting position;
// every other node is the position after its move.
type Node struct {
	// Ref identifies where the move came from, such as a token ID.
	Ref string
	SAN string
	// MoveNumber and Side are those of the move that reached this node.
	MoveNumber int
	Side       Color
	Parent     *Node
	// Children[0] continues the line; the others are variations.
	Children []*Node

	position *lib.Position
}

func NewTree(fen string) (*Node, error) {
	opt, err := lib.FEN(fen)
	if err != nil {
		return nil, fmt.Errorf("invalid FEN %q: %w", fen, err)
	}
	return &Node{position: lib.NewGame(opt).Position()}, nil
}

func (n *Node) FEN() string { return n.position.String() }

// NextMove returns the move number and side of the move due in this position.
func (n *Node) NextMove() (int, Color) {
	return (n.position.Ply() + 1) / 2, colorOf(n.position.Turn())
}

func (n *Node) Key() string { return PositionKey(n.FEN()) }

// Play adds the move that spec describes as a child of n. A move already
// played from n returns the existing child, so a variation that repeats the
// line it branches from does not duplicate it.
func (n *Node) Play(ref string, spec MoveSpec) (*Node, error) {
	matches := resolve(n.position, spec)
	if len(matches) != 1 {
		err := &MoveError{Ref: ref, Text: spec.Text, After: n.Line()}
		for _, m := range matches {
			err.Candidates = append(err.Candidates, lib.AlgebraicNotation{}.Encode(n.position, &m))
		}
		slices.Sort(err.Candidates)
		return nil, err
	}

	move := matches[0]
	san := lib.AlgebraicNotation{}.Encode(n.position, &move)
	if i := slices.IndexFunc(n.Children, func(c *Node) bool { return c.SAN == san }); i >= 0 {
		return n.Children[i], nil
	}
	number, side := n.NextMove()
	child := &Node{
		Ref:        ref,
		SAN:        san,
		MoveNumber: number,
		Side:       side,
		Parent:     n,
		position:   n.position.Update(&move),
	}
	n.Children = append(n.Children, child)
	return child, nil
}

// Path returns the nodes from the root's first move down to n.
func (n *Node) Path() []*Node {
	var path []*Node
	for node := n; node.Parent != nil; node = node.Parent {
		path = append(path, node)
	}
	slices.Reverse(path)
	return path
}

// Line formats the moves from the root to n, as in "23.Nf5 gxf5".
func (n *Node) Line() string {
	var parts []string
	for i, node := range n.Path() {
		switch {
		case node.Side == White:
			parts = append(parts, fmt.Sprintf("%d.%s", node.MoveNumber, node.SAN))
		case i == 0:
			parts = append(parts, fmt.Sprintf("%d...%s", node.MoveNumber, node.SAN))
		default:
			parts = append(parts, node.SAN)
		}
	}
	return strings.Join(parts, " ")
}

func colorOf(c lib.Color) Color {
	if c == lib.Black {
		return Black
	}
	return White
}
