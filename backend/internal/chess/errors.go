package chess

import (
	"fmt"
	"strings"
)

// MoveError reports a move that matches no legal move, or more than one. Its
// message is written to be sent back to the LLM in the repair loop.
type MoveError struct {
	Ref  string
	Text string
	// After is the line that leads to the position, empty at the root.
	After string
	// Candidates are the legal moves the spec matches, in SAN, when it is
	// ambiguous.
	Candidates []string
}

func (e *MoveError) Ambiguous() bool { return len(e.Candidates) > 1 }

func (e *MoveError) Error() string {
	where := "in the starting position"
	if e.After != "" {
		where = "after " + e.After
	}
	move := fmt.Sprintf("`%s`", e.Text)
	if e.Ref != "" {
		move = e.Ref + " " + move
	}
	if e.Ambiguous() {
		return fmt.Sprintf("%s is ambiguous %s: it could be %s", move, where, strings.Join(e.Candidates, " or "))
	}
	return fmt.Sprintf("%s is illegal %s", move, where)
}
